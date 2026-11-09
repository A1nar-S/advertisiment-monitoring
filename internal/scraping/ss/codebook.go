package ss

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// The codebook is ss.com's own vocabulary for a category: the fields of its
// search form (https://www.ss.com/lv/transport/cars/search/), their option
// IDs and names, and the models of every make. Searches are written in these
// names, ad pages are read through these IDs, and parsed attrs use these
// names, so all three agree.
//
// It is a checked-in snapshot, regenerated with `go run ./cmd/admin
// ss-codebook`; a diff then shows exactly what ss.com changed.
//
//go:embed codebook/cars.json
var carsCodebookJSON []byte

// CarsCategory is the ss.com category the car codebook describes.
const CarsCategory = "transport/cars"

type Codebook struct {
	Category string   `json:"category"`
	Fetched  string   `json:"fetched"` // date of the snapshot
	Fields   []Field  `json:"fields"`
	Makes    []Make   `json:"makes,omitempty"`   // cars
	Regions  []Region `json:"regions,omitempty"` // flats
	byKey    map[string]*Field
	makeSlug map[string]*Make
	makeName map[string]*Make
}

// Field is one search-form field.
type Field struct {
	Key   string `json:"key"`   // our attr/filter key, e.g. "gearbox"
	Label string `json:"label"` // the form's Latvian label, e.g. "Ātrumkārba"
	// OptIDs are ss.com's field IDs (opt[35], topt[18]); the ad page shows the
	// value in td#tdo_<id>. Equipment merges several groups.
	OptIDs []string `json:"opt_ids"`
	Kind   string   `json:"kind"` // "multi" (any of), "all" (has all of), "range"
	// From says where the value is found: "list" (the feed table, free) or
	// "ad" (the ad page, read only for ads that pass the cheap filters).
	From    string   `json:"from"`
	Options []Option `json:"options,omitempty"`
}

type Option struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Make struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Slug   string   `json:"slug,omitempty"` // URL path part; empty if ss.com has no own page for it
	Models []Option `json:"models,omitempty"`
}

const (
	kindMulti = "multi"
	kindAll   = "all"
	kindRange = "range"
	fromList  = "list"
	fromAd    = "ad"
)

// carFields maps ss.com field IDs to our keys. The order is the order of the
// form. IDs not listed (month, gears, VIN) are not worth filtering on.
var carFields = []struct {
	key, label, kind, from string
	ids                    []string
}{
	{"make", "Marka", kindMulti, fromList, []string{"31"}},
	{"model", "Modelis", kindMulti, fromList, nil}, // per make, see Make.Models
	{"year", "Izlaiduma gads", kindRange, fromList, []string{"18"}},
	{"engine_l", "Motora tilpums, l", kindRange, fromList, []string{"15"}},
	{"fuel", "Dzinēja tips", kindMulti, fromList, []string{"34"}},
	// The car feeds have no per-city pages, so the city is read from the ad
	// page's "Vieta" line. Options are the site's region list (see carRegions).
	{"region", "Pilsēta, rajons", kindMulti, fromAd, nil},
	{"gearbox", "Ātrumkārba", kindMulti, fromAd, []string{"35"}},
	{"mileage_km", "Nobraukums, km", kindRange, fromList, []string{"16"}},
	{"color", "Krāsa", kindMulti, fromAd, []string{"17"}},
	{"body", "Virsbūves tips", kindMulti, fromAd, []string{"32"}},
	{"seats", "Vietu skaits", kindRange, fromAd, []string{"1716"}},
	{"inspection", "Tehniskā apskate", kindMulti, fromAd, []string{"223"}},
	{"price", "Cena", kindRange, fromList, []string{"8"}},
	{"equipment", "Aprīkojums", kindAll, fromAd, []string{"945", "961", "949", "951", "947", "953", "955", "959", "957"}},
}

var (
	carsOnce sync.Once
	carsCB   *Codebook
	carsErr  error
)

// CarsCodebook returns the embedded car codebook.
func CarsCodebook() (*Codebook, error) {
	carsOnce.Do(func() { carsCB, carsErr = LoadCodebook(carsCodebookJSON) })
	return carsCB, carsErr
}

func LoadCodebook(raw []byte) (*Codebook, error) {
	var cb Codebook
	if err := json.Unmarshal(raw, &cb); err != nil {
		return nil, fmt.Errorf("codebook: %w", err)
	}
	cb.index()
	return &cb, nil
}

func (cb *Codebook) index() {
	cb.byKey = map[string]*Field{}
	for i := range cb.Fields {
		cb.byKey[cb.Fields[i].Key] = &cb.Fields[i]
	}
	cb.makeSlug, cb.makeName = map[string]*Make{}, map[string]*Make{}
	for i := range cb.Makes {
		m := &cb.Makes[i]
		cb.makeName[fold(m.Name)] = m
		if m.Slug != "" {
			cb.makeSlug[m.Slug] = m
		}
	}
}

// Field returns the field for key.
func (cb *Codebook) Field(key string) (*Field, bool) {
	f, ok := cb.byKey[key]
	return f, ok
}

// Keys lists the filter keys, in form order.
func (cb *Codebook) Keys() []string {
	keys := make([]string, len(cb.Fields))
	for i, f := range cb.Fields {
		keys[i] = f.Key
	}
	return keys
}

// AdKeys are the keys only an ad page provides.
func (cb *Codebook) AdKeys() []string {
	var keys []string
	for _, f := range cb.Fields {
		if f.From == fromAd {
			keys = append(keys, f.Key)
		}
	}
	return keys
}

// MakeBySlug finds a make by its URL slug ("alfa-romeo").
func (cb *Codebook) MakeBySlug(slug string) (*Make, bool) {
	m, ok := cb.makeSlug[slug]
	return m, ok
}

// MakeByName finds a make by name, ignoring case and diacritics.
func (cb *Codebook) MakeByName(name string) (*Make, bool) {
	m, ok := cb.makeName[fold(name)]
	return m, ok
}

// Model finds a model of m by name or URL slug ("a4", "rav-4").
func (m *Make) Model(nameOrSlug string) (Option, bool) {
	want := fold(nameOrSlug)
	slug := slugish(nameOrSlug)
	for _, o := range m.Models {
		if fold(o.Name) == want || slugish(o.Name) == slug {
			return o, true
		}
	}
	return Option{}, false
}

// Option finds an option by name, ignoring case, diacritics and a trailing
// dot: ss.com abbreviates ("Jaun.", "119."), and YAML reads 119. as a number.
func (f *Field) Option(name string) (Option, bool) {
	want := strings.TrimSuffix(fold(name), ".")
	for _, o := range f.Options {
		if strings.TrimSuffix(fold(o.Name), ".") == want {
			return o, true
		}
	}
	return Option{}, false
}

// OptionPrefix finds the option that text starts with, longest first, so
// "Melna metālika" is "Melna" and "Benzīns/gāze" is not "Benzīns". Ad pages
// write values with extras ("Automāts 6 ātrumi").
func (f *Field) OptionPrefix(text string) (Option, bool) {
	t := fold(text)
	var best Option
	for _, o := range f.Options {
		n := fold(o.Name)
		if n != "" && strings.HasPrefix(t, n) && len(n) > len(fold(best.Name)) {
			best = o
		}
	}
	return best, best.Name != ""
}

func (f *Field) optionNames() []string {
	names := make([]string, len(f.Options))
	for i, o := range f.Options {
		names[i] = o.Name
	}
	return names
}

// fold makes names comparable: lower case, no Latvian (or other common)
// diacritics, single spaces. "Dīzelis" and "dizelis" fold alike.
func fold(s string) string {
	return strings.Join(strings.Fields(diacritics.Replace(strings.ToLower(s))), " ")
}

var diacritics = strings.NewReplacer(
	"ā", "a", "č", "c", "ē", "e", "ģ", "g", "ī", "i", "ķ", "k", "ļ", "l", "ņ", "n",
	"š", "s", "ū", "u", "ž", "z", "ō", "o", "ŗ", "r", "ä", "a", "ö", "o", "ü", "u",
	"é", "e", "è", "e", "ë", "e", "á", "a", "ó", "o", "í", "i", "ú", "u",
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// slugish reduces a name to letters and digits, to compare a model name
// with its URL slug ("RAV 4" ~ "rav4" ~ "rav-4").
func slugish(s string) string { return nonAlnum.ReplaceAllString(fold(s), "") }

// closest returns up to three names nearest to s, for "did you mean".
func closest(s string, names []string) []string {
	type cand struct {
		name string
		d    int
	}
	var cs []cand
	for _, n := range names {
		cs = append(cs, cand{n, levenshtein(fold(s), fold(n))})
	}
	slices.SortStableFunc(cs, func(a, b cand) int { return a.d - b.d })
	var out []string
	for _, c := range cs {
		if len(out) == 3 || c.d > max(2, len(s)/2) {
			break
		}
		out = append(out, c.name)
	}
	return out
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// --- Building the snapshot from the live site -------------------------------

var optIDRe = regexp.MustCompile(`^t?opt\[(\d+)\]`)

// BuildCarsCodebook reads the car search form and every make's own search
// form (for its models) from ss.com. That is one request per make, gap apart:
// run it rarely (`go run ./cmd/admin ss-codebook`).
func (c *Client) BuildCarsCodebook(ctx context.Context, gap time.Duration) (*Codebook, error) {
	c = c.withGap(gap)
	page, err := c.get(ctx, c.base+"/lv/"+CarsCategory+"/search/")
	if err != nil {
		return nil, err
	}
	options, err := parseForm(page)
	if err != nil {
		return nil, err
	}
	if len(options["31"].options) == 0 {
		return nil, fmt.Errorf("search form has no make options: layout changed?")
	}

	cb := &Codebook{Category: CarsCategory, Fetched: time.Now().UTC().Format("2006-01-02")}
	for _, spec := range carFields {
		f := Field{Key: spec.key, Label: spec.label, OptIDs: spec.ids, Kind: spec.kind, From: spec.from}
		switch {
		case spec.key == "region":
			if f.Options, err = carRegions(); err != nil {
				return nil, err
			}
		case spec.kind != kindRange:
			f.Options = groupedOptions(spec.ids, options)
		}
		cb.Fields = append(cb.Fields, f)
	}
	for _, o := range options["31"].options {
		cb.Makes = append(cb.Makes, Make{ID: o.ID, Name: o.Name})
	}
	cb.index()

	// Makes with their own category page have a slug and a model list.
	index, err := c.get(ctx, c.base+"/lv/"+CarsCategory+"/")
	if err != nil {
		return nil, err
	}
	for slug, name := range parseMakeLinks(index) {
		m, ok := cb.MakeByName(name)
		if !ok {
			slog.Debug("category link that is not a make", "slug", slug, "name", name)
			continue
		}
		m.Slug = slug
		m.Name = name // the index spells names properly ("BMW", the form "Bmw")
		form, err := c.get(ctx, c.base+"/lv/"+CarsCategory+"/"+slug+"/search/")
		if err != nil {
			return nil, fmt.Errorf("models of %s: %w", name, err)
		}
		if m.Models, err = parseModels(form); err != nil {
			return nil, fmt.Errorf("models of %s: %w", name, err)
		}
		slog.Info("codebook make", "make", m.Name, "models", len(m.Models))
	}
	cb.index()
	return cb, nil
}

// carRegions is ss.com's region list, which the car search form only loads
// by script. Real estate has the same list as category pages, so it is taken
// from the flats codebook, minus its "flats abroad" entry.
func carRegions() ([]Option, error) {
	flats, err := FlatsCodebook()
	if err != nil {
		return nil, err
	}
	var out []Option
	for _, r := range flats.Regions {
		if r.Slug != "flats-abroad-latvia" {
			out = append(out, Option{ID: r.Slug, Name: r.Name})
		}
	}
	return out, nil
}

// groupedOptions merges the options of several form fields. A name that
// occurs in more than one (equipment: "Apsildāmi" seats and mirrors) is
// qualified with its group's label, "Sēdekļi: Apsildāmi", the way the ad
// page groups it too.
func groupedOptions(ids []string, form map[string]formField) []Option {
	count := map[string]int{}
	for _, id := range ids {
		for _, o := range form[id].options {
			count[fold(o.Name)]++
		}
	}
	var out []Option
	for _, id := range ids {
		for _, o := range form[id].options {
			if count[fold(o.Name)] > 1 {
				o.Name = qualify(form[id].label, o.Name)
			}
			out = append(out, o)
		}
	}
	return out
}

func qualify(group, name string) string { return group + ": " + name }

// MarshalIndent renders the snapshot as reviewable JSON.
func (cb *Codebook) MarshalIndent() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cb); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type formField struct {
	label   string
	options []Option
}

// parseForm reads every opt[ID]/topt[ID] field of a search form with its
// label and options.
func parseForm(page []byte) (map[string]formField, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(page))
	if err != nil {
		return nil, err
	}
	out := map[string]formField{}
	doc.Find("select[name], input[name]").Each(func(_ int, s *goquery.Selection) {
		m := optIDRe.FindStringSubmatch(attr(s, "name"))
		if m == nil {
			return
		}
		f := out[m[1]]
		if f.label == "" {
			f.label = strings.TrimSuffix(text(s.Closest("tr").Find("td").First()), ":")
		}
		if goquery.NodeName(s) == "select" && strings.HasPrefix(attr(s, "name"), "opt[") {
			s.Find("option").Each(func(_ int, o *goquery.Selection) {
				v, name := attr(o, "value"), text(o)
				if v != "" && v != "0" && name != "" && name != "-" {
					f.options = append(f.options, Option{ID: v, Name: name})
				}
			})
		}
		out[m[1]] = f
	})
	return out, nil
}

var makeLinkRe = regexp.MustCompile(`^/lv/transport/cars/([a-z0-9-]+)/$`)

// parseMakeLinks reads the make links of the cars index page.
func parseMakeLinks(page []byte) map[string]string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(page))
	if err != nil {
		return nil
	}
	out := map[string]string{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		m := makeLinkRe.FindStringSubmatch(attr(a, "href"))
		if m == nil || m[1] == "new" || m[1] == "search" || m[1] == "today" || m[1] == "sell" || m[1] == "buy" {
			return
		}
		if name := text(a); name != "" {
			out[m[1]] = name
		}
	})
	return out
}

func parseModels(page []byte) ([]Option, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(page))
	if err != nil {
		return nil, err
	}
	var out []Option
	doc.Find(`select[name="cid[]"] option`).Each(func(_ int, o *goquery.Selection) {
		if v, name := attr(o, "value"), text(o); v != "" && name != "" {
			out = append(out, Option{ID: v, Name: name})
		}
	})
	return out, nil
}
