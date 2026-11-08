package ss

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// The flats codebook is the vocabulary of
// https://www.ss.com/lv/real-estate/flats/search/: regions with their
// districts, deals, and the filterable fields. Region, district and deal are
// not filters but choose the feed (the URL path), so the request is shared by
// everyone watching that area.
//
//go:embed codebook/flats.json
var flatsCodebookJSON []byte

// FlatsCategory is the ss.com category the flats codebook describes.
const FlatsCategory = "real-estate/flats"

// Region is a city or district area with its own feed.
type Region struct {
	Name      string   `json:"name"`
	Slug      string   `json:"slug"`
	Districts []Option `json:"districts,omitempty"` // ID = URL slug
}

const fromFeed = "feed"

// deals maps the form's deal names to the URL's last path part.
var flatDeals = []Option{
	{ID: "hand_over", Name: "Izīrē"}, // rent out (the usual "for rent")
	{ID: "sell", Name: "Pārdod"},
	{ID: "rent", Name: "Īrē"}, // wants to rent
	{ID: "buy", Name: "Pērk"},
	{ID: "exchange", Name: "Maina"},
}

var flatFields = []struct {
	key, label, kind, from string
	ids                    []string
}{
	{"region", "Pilsēta, rajons", kindMulti, fromFeed, []string{"20"}},
	{"district", "Pagasts / rajons", kindMulti, fromFeed, []string{"856"}},
	{"deal", "Darījuma veids", kindMulti, fromFeed, nil},
	{"rooms", "Istabu skaits", kindRange, fromList, []string{"1"}},
	{"area_m2", "Platība, m²", kindRange, fromList, []string{"3"}},
	{"floor", "Stāvs", kindRange, fromList, []string{"4"}},
	{"floors", "Stāvu skaits mājā", kindRange, fromList, nil},
	{"series", "Sērija", kindMulti, fromList, []string{"6"}},
	{"house_type", "Mājas tips", kindMulti, fromAd, []string{"2"}},
	{"price", "Cena", kindRange, fromList, []string{"8"}},
}

var (
	flatsOnce sync.Once
	flatsCB   *Codebook
	flatsErr  error
)

// FlatsCodebook returns the embedded flats codebook.
func FlatsCodebook() (*Codebook, error) {
	flatsOnce.Do(func() { flatsCB, flatsErr = LoadCodebook(flatsCodebookJSON) })
	return flatsCB, flatsErr
}

// codebookFor picks the codebook of a category path (a feed or an ad's
// location); nil for categories without one.
func codebookFor(path string) *Codebook {
	scope := parseFeed(path).scope
	under := func(cat string) bool { return scope == cat || strings.HasPrefix(scope, cat+"/") }
	var cb *Codebook
	var err error
	switch {
	case under(CarsCategory):
		cb, err = CarsCodebook()
	case under(FlatsCategory):
		cb, err = FlatsCodebook()
	default:
		return nil
	}
	if err != nil {
		slog.Error("codebook", "err", err)
		return nil
	}
	return cb
}

// Region finds a region by name or slug, ignoring case and diacritics.
func (cb *Codebook) Region(nameOrSlug string) (*Region, bool) {
	for i := range cb.Regions {
		r := &cb.Regions[i]
		if fold(r.Name) == fold(nameOrSlug) || r.Slug == nameOrSlug {
			return r, true
		}
	}
	return nil, false
}

// District finds a district of r by name or slug.
func (r *Region) District(nameOrSlug string) (Option, bool) {
	for _, d := range r.Districts {
		if fold(d.Name) == fold(nameOrSlug) || d.ID == nameOrSlug {
			return d, true
		}
	}
	return Option{}, false
}

func (cb *Codebook) regionNames() []string {
	out := make([]string, len(cb.Regions))
	for i, r := range cb.Regions {
		out[i] = r.Name
	}
	return out
}

var regionLinkRe = regexp.MustCompile(`^/lv/real-estate/flats/([a-z0-9-]+)/$`)

// BuildFlatsCodebook reads the flats search form, the region list and each
// region's district list: one request per region, gap apart.
func (c *Client) BuildFlatsCodebook(ctx context.Context, gap time.Duration) (*Codebook, error) {
	c = c.withGap(gap)
	page, err := c.get(ctx, c.base+"/lv/"+FlatsCategory+"/search/")
	if err != nil {
		return nil, err
	}
	form, err := parseForm(page)
	if err != nil {
		return nil, err
	}
	if len(form["6"].options) == 0 {
		return nil, fmt.Errorf("flats search form has no series options: layout changed?")
	}

	cb := &Codebook{Category: FlatsCategory, Fetched: time.Now().UTC().Format("2006-01-02")}
	for _, spec := range flatFields {
		f := Field{Key: spec.key, Label: spec.label, OptIDs: spec.ids, Kind: spec.kind, From: spec.from}
		if spec.kind != kindRange && spec.from != fromFeed {
			f.Options = groupedOptions(spec.ids, form)
		}
		if spec.key == "deal" {
			f.Options = flatDeals
		}
		cb.Fields = append(cb.Fields, f)
	}

	index, err := c.get(ctx, c.base+"/lv/"+FlatsCategory+"/")
	if err != nil {
		return nil, err
	}
	regions, err := parseLinks(index, regionLinkRe)
	if err != nil {
		return nil, err
	}
	for _, r := range regions {
		page, err := c.get(ctx, c.base+"/lv/"+FlatsCategory+"/"+r.ID+"/")
		if err != nil {
			return nil, fmt.Errorf("districts of %s: %w", r.Name, err)
		}
		districtRe := regexp.MustCompile(`^/lv/real-estate/flats/` + regexp.QuoteMeta(r.ID) + `/([a-z0-9-]+)/$`)
		districts, err := parseLinks(page, districtRe)
		if err != nil {
			return nil, err
		}
		cb.Regions = append(cb.Regions, Region{Name: r.Name, Slug: r.ID, Districts: districts})
		slog.Info("codebook region", "region", r.Name, "districts", len(districts))
	}
	cb.index()
	return cb, nil
}

// parseLinks collects the category links matching re (slug in group 1),
// skipping the "new ad" and "search" links, in page order.
func parseLinks(page []byte, re *regexp.Regexp) ([]Option, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(page))
	if err != nil {
		return nil, err
	}
	var out []Option
	seen := map[string]bool{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		m := re.FindStringSubmatch(attr(a, "href"))
		if m == nil || seen[m[1]] || m[1] == "new" || m[1] == "search" || m[1] == "all" || m[1] == "rss" {
			return
		}
		if name := text(a); name != "" {
			seen[m[1]] = true
			out = append(out, Option{ID: m[1], Name: name})
		}
	})
	return out, nil
}
