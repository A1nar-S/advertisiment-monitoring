package ss

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
)

// EnrichableKeys implements sources.Enricher: the facts only the ad page has
// (cars: gearbox, body, ..., the full description; flats: house type), plus
// a car's fuel, which the list page sometimes omits ("-" engine).
func (*Client) EnrichableKeys() []string {
	keys := []string{"fuel", TextAttr}
	for _, load := range []func() (*Codebook, error){CarsCodebook, FlatsCodebook} {
		if cb, err := load(); err == nil {
			keys = append(keys, cb.AdKeys()...)
		}
	}
	return keys
}

// Enrich implements sources.Enricher: it reads the ad's page and adds what
// the feed table lacks. Throttled by the client's gap (WithGap).
func (c *Client) Enrich(ctx context.Context, l events.ListingDiscovered) (events.ListingDiscovered, error) {
	cb := codebookFor(l.Location)
	if cb == nil || l.URL == "" {
		return l, nil
	}
	body, err := c.get(ctx, c.adURL(l.URL))
	var se *StatusError
	if errors.As(err, &se) && (se.Code == http.StatusNotFound || se.Code == http.StatusGone) {
		return l, sources.ErrGone
	}
	if err != nil {
		return l, err
	}
	if !bytes.Contains(body, []byte("tdo_")) {
		return l, sources.ErrGone // ss.com shows a "removed" page with 200
	}
	attrs, err := parseAdPage(cb, bytes.NewReader(body))
	if err != nil {
		return l, err
	}

	out := l
	out.Attrs = make(map[string]any, len(l.Attrs)+len(attrs))
	for k, v := range l.Attrs {
		out.Attrs[k] = v
	}
	for k, v := range attrs {
		out.Attrs[k] = v // the ad page is the more exact one (petrol vs petrol/gas)
	}
	out.Fields = append(append([]events.Field(nil), l.Fields...), adFields(out)...)
	return out, nil
}

// adURL keeps tests' fake hosts working: an ss.com URL is re-rooted on the
// client's base.
func (c *Client) adURL(u string) string {
	if rest, ok := strings.CutPrefix(u, baseURL); ok {
		return c.base + rest
	}
	return u
}

var (
	inspectionRe = regexp.MustCompile(`\d{2}\.\d{4}`)
	leadingNum   = regexp.MustCompile(`^[\d.,\s]+`)
)

// ParseAdPage reads a car ad page into codebook-named attrs.
func ParseAdPage(r io.Reader) (map[string]any, error) {
	cb, err := CarsCodebook()
	if err != nil {
		return nil, err
	}
	return parseAdPage(cb, r)
}

// parseAdPage reads an ad page through cb. Cells are td#tdo_<field ID>, the
// same IDs as the search form; a car's equipment is bold (b.auto_c) under
// its group heading.
func parseAdPage(cb *Codebook, r io.Reader) (map[string]any, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("parse ad page: %w", err)
	}
	cell := func(id string) string {
		return strings.Join(strings.Fields(doc.Find("td#tdo_"+id).First().Text()), " ")
	}
	attrs := map[string]any{}
	set := func(key string, o Option, ok bool) {
		if ok {
			attrs[key] = o.Name
		}
	}

	// Plain one-option cells: "Automāts 6 ātrumi" is Automāts, "Melna
	// metālika" is Melna, "Ķieģeļu" is Ķieģeļu.
	for _, f := range cb.Fields {
		if f.From != fromAd || f.Kind != kindMulti || len(f.OptIDs) != 1 || f.Key == "inspection" {
			continue
		}
		if v := cell(f.OptIDs[0]); v != "" {
			o, ok := f.OptionPrefix(v)
			set(f.Key, o, ok)
		}
	}
	if cb.Category != CarsCategory {
		return attrs, nil
	}

	if f, ok := cb.Field("fuel"); ok {
		// "1.6 dīzelis": the fuel follows the engine size.
		if v := leadingNum.ReplaceAllString(cell("15"), ""); v != "" {
			o, ok := f.OptionPrefix(v)
			set("fuel", o, ok)
		}
	}
	if f, ok := cb.Field("inspection"); ok {
		// A date ("02.2027") means it has one; otherwise the site says "Nav".
		switch v := cell("223"); {
		case inspectionRe.MatchString(v):
			o, ok := f.Option("Jā")
			set("inspection", o, ok)
		case v != "":
			o, ok := f.OptionPrefix(v)
			set("inspection", o, ok)
		}
	}
	if f, ok := cb.Field("region"); ok {
		o, ok := regionOption(f, contact(doc, "Vieta:"))
		set("region", o, ok)
	}
	if n, err := strconv.Atoi(strings.Fields(cell("1716") + " x")[0]); err == nil {
		attrs["seats"] = float64(n)
	}
	if d := description(doc); d != "" {
		attrs[TextAttr] = d
	}

	if f, ok := cb.Field("equipment"); ok {
		var have []any
		group := ""
		doc.Find("div.auto_c_head, b.auto_c").Each(func(_ int, s *goquery.Selection) {
			if s.HasClass("auto_c_head") {
				group = text(s)
				return
			}
			name := text(s)
			if o, ok := f.Option(qualify(group, name)); ok {
				have = append(have, o.Name)
			} else if o, ok := f.Option(name); ok {
				have = append(have, o.Name)
			}
		})
		if len(have) > 0 {
			attrs["equipment"] = have
		}
	}
	return attrs, nil
}

// maxText caps the description kept for keyword search.
const maxText = 4000

// description is the ad's own text: what #msg_div_msg holds before the
// options table.
func description(doc *goquery.Document) string {
	msg := doc.Find("div#msg_div_msg").First().Clone()
	msg.Find("table, div, script").Remove()
	d := strings.Join(strings.Fields(msg.Text()), " ")
	if r := []rune(d); len(r) > maxText {
		d = string(r[:maxText])
	}
	return d
}

// contact reads a line of the ad's contact block, e.g. "Vieta:" → "Rīga".
func contact(doc *goquery.Document, label string) string {
	var v string
	doc.Find("td.ads_contacts_name").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if text(s) != label {
			return true
		}
		v = text(s.Next())
		return false
	})
	return v
}

var rajAbbrev = regexp.MustCompile(`(?i)\braj\.?$`)

// regionOption maps an ad's "Vieta" to a region: car ads abbreviate the
// list's "Jelgava un rajons" to "Jelgava un raj.".
func regionOption(f *Field, v string) (Option, bool) {
	v, _, _ = strings.Cut(v, ",")
	v = rajAbbrev.ReplaceAllString(strings.TrimSpace(v), "rajons")
	if v == "" {
		return Option{}, false
	}
	return f.Option(v)
}

// adFields are the ad-page facts worth showing in a notification.
func adFields(l events.ListingDiscovered) []events.Field {
	var fs []events.Field
	for _, f := range []struct{ key, name string }{
		{"region", "📍 Location"}, {"gearbox", "🕹️ Gearbox"}, {"body", "🚙 Body"}, {"color", "🎨 Color"}, {"inspection", "🔧 Inspection"},
		{"house_type", "🏠 House"},
	} {
		if v, ok := l.Str(f.key); ok {
			fs = append(fs, events.Field{Name: f.name, Value: v, Inline: true})
		}
	}
	return fs
}
