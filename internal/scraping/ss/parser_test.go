package ss

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
)

func parseFile(t *testing.T, path string) []events.ListingDiscovered {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	ads, err := Parse(f, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ads
}

// The feed_* fixtures are real pages saved from ss.com. TestLiveStructure
// re-checks their structure against the live site.
func TestParseFixtures(t *testing.T) {
	tests := []struct {
		file, locationPrefix string
		required             []string // attrs every ad must have
	}{
		{"testdata/feed_district.html", "real-estate/flats/riga/", []string{"address", "rooms", "area_m2", "series", "floor"}},
		{"testdata/feed_city.html", "real-estate/flats/riga/", []string{"address", "rooms", "area_m2", "series", "floor"}},
		{"testdata/feed_cars_model.html", "transport/cars/audi/a4", []string{"year", "engine_l"}},
		{"testdata/feed_cars_make.html", "transport/cars/audi/", []string{"model", "year", "engine_l"}},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			ads := parseFile(t, tt.file)
			if len(ads) != 30 {
				t.Fatalf("got %d ads, want 30 (banner row must be skipped)", len(ads))
			}
			for _, a := range ads {
				if !strings.HasPrefix(a.Location, tt.locationPrefix) || a.PriceValue == nil || a.URL == "" || a.Title == "" {
					t.Errorf("%s: incomplete parse: %+v", a.ExternalID, a)
				}
				for _, k := range tt.required {
					if _, ok := a.Attrs[k]; !ok {
						t.Errorf("%s: missing attr %q: %v", a.ExternalID, k, a.Attrs)
					}
				}
			}
		})
	}
}

// Mileage is legitimately blank on some car ads, so it is not a required attr
// above; this checks the column is not silently dropped.
func TestParseCarsMostlyHaveMileage(t *testing.T) {
	with := 0
	for _, a := range parseFile(t, "testdata/feed_cars_model.html") {
		if _, ok := a.Num("mileage_km"); ok {
			with++
		}
	}
	if with < 20 {
		t.Errorf("only %d of 30 ads have mileage; column mapping broken?", with)
	}
}

func TestParseDistrictFeedColumns(t *testing.T) {
	a := parseFile(t, "testdata/feed_district.html")[0]
	if a.ExternalID != "58230432" || a.Location != "real-estate/flats/riga/centre" {
		t.Errorf("id/location = %q / %q", a.ExternalID, a.Location)
	}
	want := map[string]any{"address": "Sadovņikova 22", "rooms": 2.0, "area_m2": 35.0, "floor": "2/4", "series": "Jaun."}
	if !reflect.DeepEqual(a.Attrs, want) {
		t.Errorf("attrs = %v, want %v", a.Attrs, want)
	}
	if a.PriceValue == nil || *a.PriceValue != 590 {
		t.Errorf("price = %v", a.PriceValue)
	}
}

func TestParseCityFeedColumns(t *testing.T) {
	a := parseFile(t, "testdata/feed_city.html")[0]
	want := map[string]any{
		"district": "Pļavnieki", "address": "J. Vācieša 2G", "rooms": 3.0, "area_m2": 70.0, "floor": "8/16", "series": "Specpr.",
	}
	if a.Location != "real-estate/flats/riga/plyavnieki" || !reflect.DeepEqual(a.Attrs, want) {
		t.Errorf("location = %q, attrs = %v, want %v", a.Location, a.Attrs, want)
	}
	if len(a.Fields) == 0 || a.Fields[0].Value != "Pļavnieki, J. Vācieša 2G" {
		t.Errorf("fields = %+v", a.Fields)
	}
}

func TestParseCarColumns(t *testing.T) {
	a := parseFile(t, "testdata/feed_cars_model.html")[0]
	if a.ExternalID != "" && a.Location != "transport/cars/audi/a4" {
		t.Errorf("location = %q", a.Location)
	}
	// First row of the fixture: 2012, 3.0D, "250 tūkst.", "6,900 €".
	want := map[string]any{"year": 2012.0, "engine_l": 3.0, "fuel": "D", "mileage_km": 250000.0}
	if !reflect.DeepEqual(a.Attrs, want) {
		t.Errorf("attrs = %v, want %v", a.Attrs, want)
	}
	if a.PriceValue == nil || *a.PriceValue != 6900 {
		t.Errorf("price = %v (%q), want 6900", a.PriceValue, a.Price)
	}

	if m, _ := parseFile(t, "testdata/feed_cars_make.html")[0].Str("model"); m != "Q5" {
		t.Errorf("model = %q, want Q5", m)
	}
}

func TestParseFailsOnUnknownLayout(t *testing.T) {
	if _, err := Parse(strings.NewReader("<html><body><table><tr id='tr_1'></tr></table></body></html>"), time.Now()); err == nil {
		t.Error("expected an error for a page with no header row")
	}
	// A row whose cell count disagrees with the headers means the layout moved.
	page := `<table><tr id="head_line"><td><a class="a19">datums</a></td><td><a class="a18">Cena</a></td></tr>
		<tr id="tr_1"><td class="msg2"><a class="am" href="/msg/lv/x/y/z.html">t</a></td></tr></table>`
	if _, err := Parse(strings.NewReader(page), time.Now()); err == nil {
		t.Error("expected an error for a row that does not match the headers")
	}
}

func TestPriceValue(t *testing.T) {
	tests := map[string]float64{
		"290 €/mēn.":        290,
		"1 200  €/mēn.":     1200,
		"1\u00a0200 €/mēn.": 1200, // nbsp
		"590  €/mēn.":       590,
		"16.86 €":           16.86,
		"6,900 €":           6900,
		"12,500 €":          12500,
		"99,5 €":            99.5,
	}
	for in, want := range tests {
		if got := priceValue(in); got == nil || *got != want {
			t.Errorf("priceValue(%q) = %v, want %v", in, got, want)
		}
	}
	if priceValue("pērku") != nil {
		t.Error("expected nil for a price with no digits")
	}
}

func TestFeedURL(t *testing.T) {
	if got := FeedURL("/real-estate/flats/riga/centre/hand_over/", 1); got != "https://www.ss.com/lv/real-estate/flats/riga/centre/hand_over/" {
		t.Errorf("page 1: %s", got)
	}
	if got := FeedURL("transport/cars/audi/a4", 3); got != "https://www.ss.com/lv/transport/cars/audi/a4/page3.html" {
		t.Errorf("page 3: %s", got)
	}
}

func TestPlanFeeds(t *testing.T) {
	const flats = "real-estate/flats/riga/"
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"dedupes shared feeds", []string{flats + "centre/hand_over", flats + "centre/hand_over"}, []string{flats + "centre/hand_over"}},
		{"keeps few districts", []string{flats + "centre/hand_over", flats + "teika/hand_over"}, []string{flats + "centre/hand_over", flats + "teika/hand_over"}},
		{"collapses many districts", []string{flats + "a/hand_over", flats + "b/hand_over", flats + "c/hand_over", flats + "d/hand_over"}, []string{flats + "all/hand_over"}},
		{"city feed swallows districts", []string{flats + "all/hand_over", flats + "centre/hand_over"}, []string{flats + "all/hand_over"}},
		{"deals stay separate", []string{flats + "all/hand_over", flats + "centre/sell"}, []string{flats + "all/hand_over", flats + "centre/sell"}},
		{"collapses many car models into the make", []string{"transport/cars/audi/a1", "transport/cars/audi/a3", "transport/cars/audi/a4", "transport/cars/audi/a6"}, []string{"transport/cars/audi"}},
		{"make swallows its models", []string{"transport/cars/audi", "transport/cars/audi/a4"}, []string{"transport/cars/audi"}},
		{"never collapses past the make", []string{"transport/cars/audi", "transport/cars/bmw", "transport/cars/ford", "transport/cars/kia"}, []string{"transport/cars/audi", "transport/cars/bmw", "transport/cars/ford", "transport/cars/kia"}},
	}
	c := NewClient(time.Second)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.PlanFeeds(tt.in, 3); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInScope(t *testing.T) {
	const flats = "real-estate/flats/riga/"
	rent := events.ListingDiscovered{Location: flats + "centre", Feed: flats + "all/hand_over"}
	car := events.ListingDiscovered{Location: "transport/cars/audi/a4", Feed: "transport/cars/audi"}
	tests := []struct {
		name   string
		wanted []string
		l      events.ListingDiscovered
		want   bool
	}{
		{"district via city-wide scrape", []string{flats + "centre/hand_over"}, rent, true},
		{"other district", []string{flats + "teika/hand_over"}, rent, false},
		{"city-wide search", []string{flats + "all/hand_over"}, rent, true},
		{"other deal", []string{flats + "centre/sell"}, rent, false},
		{"one of several", []string{flats + "teika/hand_over", flats + "centre/hand_over"}, rent, true},
		{"model via make scrape", []string{"transport/cars/audi/a4"}, car, true},
		{"other model", []string{"transport/cars/audi/a6"}, car, false},
		{"other make with shared prefix", []string{"transport/cars/aud"}, car, false},
	}
	c := NewClient(time.Second)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.InScope(tt.wanted, tt.l); got != tt.want {
				t.Errorf("InScope = %v, want %v", got, tt.want)
			}
		})
	}
}
