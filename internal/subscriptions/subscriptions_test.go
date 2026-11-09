package subscriptions

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
)

func ptr[T any](v T) *T { return &v }

// fakeSource scopes by plain prefix and plans feeds as a sorted, de-duplicated
// list, which is enough to test the slice without any real site.
type fakeSource struct{}

func (fakeSource) Name() string { return "ss.com" }
func (fakeSource) FetchFeed(context.Context, string, int) ([]events.ListingDiscovered, error) {
	return nil, nil
}
func (fakeSource) PlanFeeds(wanted []string, _ int) []string {
	out := slices.Clone(wanted)
	slices.Sort(out)
	return slices.Compact(out)
}
func (fakeSource) InScope(wanted []string, l events.ListingDiscovered) bool {
	return slices.ContainsFunc(wanted, func(w string) bool { return strings.HasPrefix(l.Location, w) })
}

// Its catalog takes the category as the feed and any filters except "nope".
func (fakeSource) Categories() []sources.Category { return []sources.Category{{ID: "cars"}} }
func (fakeSource) CompileSearch(category string, filters map[string]any) (sources.Compiled, error) {
	if _, bad := filters["nope"]; bad {
		return sources.Compiled{}, &sources.InvalidSearch{Msg: `unknown filter "nope"`}
	}
	return sources.Compiled{Spec: filters, Feeds: []string{category}, Filters: filters}, nil
}

func TestMatch(t *testing.T) {
	base := events.ListingDiscovered{
		Location:   "flats/centre",
		PriceValue: ptr(400.0),
		Attrs:      map[string]any{"rooms": 2.0, "area_m2": 40.0, "series": "Jaun.", "year": 2012.0},
	}
	search := func(f Filters) Search { return Search{Feeds: []string{"flats/centre"}, Filters: f} }
	rng := func(key string, min, max *float64) Filters {
		return Filters{Ranges: map[string]Range{key: {Min: min, Max: max}}}
	}
	tests := []struct {
		name   string
		search Search
		mod    func(*events.ListingDiscovered)
		want   bool
	}{
		{"no filters", search(Filters{}), nil, true},
		{"out of scope", Search{Feeds: []string{"flats/teika"}}, nil, false},
		{"rooms below min", search(rng("rooms", ptr(3.0), nil)), nil, false},
		{"rooms above max", search(rng("rooms", nil, ptr(1.0))), nil, false},
		{"rooms in range", search(rng("rooms", ptr(1.0), ptr(2.0))), nil, true},
		{"bounds are inclusive", search(rng("rooms", ptr(2.0), ptr(2.0))), nil, true},
		{"missing attr fails its filter", search(rng("rooms", ptr(1.0), nil)), func(l *events.ListingDiscovered) { delete(l.Attrs, "rooms") }, false},
		{"missing attr is fine without a filter", search(Filters{}), func(l *events.ListingDiscovered) { l.Attrs = nil }, true},
		{"filter on an attr other sources use", search(rng("year", ptr(2010.0), nil)), nil, true},
		{"price too high", search(rng("price", nil, ptr(350.0))), nil, false},
		{"price too low", search(rng("price", ptr(450.0), nil)), nil, false},
		{"unknown price fails a price filter", search(rng("price", nil, ptr(500.0))), func(l *events.ListingDiscovered) { l.PriceValue = nil }, false},
		{"area too small", search(rng("area_m2", ptr(50.0), nil)), nil, false},
		{"series ignores case", search(Filters{OneOf: map[string][]string{"series": {"jaun."}}}), nil, true},
		{"series mismatch", search(Filters{OneOf: map[string][]string{"series": {"P. kara"}}}), nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := base
			l.Attrs = maps(base.Attrs)
			if tt.mod != nil {
				tt.mod(&l)
			}
			if got := tt.search.Match(fakeSource{}, l); got != tt.want {
				t.Errorf("Match = %v, want %v", got, tt.want)
			}
		})
	}
}

func maps(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestParseFilters(t *testing.T) {
	f, err := parseFilters(map[string]any{
		"price":  map[string]any{"min": uint64(250), "max": 430.5},
		"series": []any{"Jaun.", "P. kara"},
		"fuel":   []any{}, // empty list means any
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Filters{
		Ranges: map[string]Range{"price": {Min: ptr(250.0), Max: ptr(430.5)}},
		OneOf:  map[string][]string{"series": {"Jaun.", "P. kara"}},
	}
	if !reflect.DeepEqual(f, want) {
		t.Errorf("got %+v, want %+v", f, want)
	}
	for _, bad := range []map[string]any{
		{"rooms": 3},
		{"rooms": map[string]any{"minimum": 1}},
		{"rooms": map[string]any{"min": "one"}},
	} {
		if _, err := parseFilters(bad); err == nil {
			t.Errorf("parseFilters(%v) succeeded, want error", bad)
		}
	}
}

func TestEvaluateWithDetail(t *testing.T) {
	l := events.ListingDiscovered{Location: "cars", PriceValue: ptr(5000.0), Attrs: map[string]any{"year": 2015.0}}
	enrichable := map[string]bool{"gearbox": true, "equipment": true}
	s := Search{Feeds: []string{"cars"}, Filters: Filters{
		Ranges: map[string]Range{"year": {Min: ptr(2012.0)}},
		OneOf:  map[string][]string{"gearbox": {"Automāts"}},
	}}
	if v := s.Evaluate(fakeSource{}, l, enrichable); v != NeedsDetail {
		t.Errorf("cheap filters pass, gearbox unknown: %v, want NeedsDetail", v)
	}
	// A cheap filter that already fails never asks for the ad page.
	l.Attrs["year"] = 2005.0
	if v := s.Evaluate(fakeSource{}, l, enrichable); v != NoMatch {
		t.Errorf("year fails: %v, want NoMatch", v)
	}
	// Without enrichment a missing attr simply fails.
	l.Attrs["year"] = 2015.0
	if s.Match(fakeSource{}, l) {
		t.Error("Match without the ad page should fail on unknown gearbox")
	}
	l.Attrs["gearbox"] = "automāts"
	if v := s.Evaluate(fakeSource{}, l, enrichable); v != Matched {
		t.Errorf("gearbox known: %v, want Matched", v)
	}

	// Keywords: the title (the text's first line) is enough when it has one;
	// otherwise the full text from the ad page decides.
	s.Filters = Filters{Contains: map[string][]string{"text": {"quattro", "4x4"}}}
	l.Title = "Audi A6 Allroad QUATTRO 3.0 TDI"
	if v := s.Evaluate(fakeSource{}, l, map[string]bool{"text": true}); v != Matched {
		t.Errorf("keyword in title: %v, want Matched", v)
	}
	l.Title = "Audi A6, pilna komplektācija"
	if v := s.Evaluate(fakeSource{}, l, map[string]bool{"text": true}); v != NeedsDetail {
		t.Errorf("keyword not in title: %v, want NeedsDetail", v)
	}
	if s.Match(fakeSource{}, l) {
		t.Error("without the ad text a keyword must fail")
	}
	l.Attrs["text"] = "Audi A6, pilna komplektācija. Pilnpiedziņa (Quattro), ādas salons."
	if !s.Match(fakeSource{}, l) {
		t.Error("keyword in the ad text should match")
	}
	l.Attrs["text"] = "Audi A6, priekšpiedziņa."
	if v := s.Evaluate(fakeSource{}, l, map[string]bool{"text": true}); v != NoMatch {
		t.Errorf("ad text without keywords: %v, want NoMatch", v)
	}
	s.Filters.Contains["text"] = []string{"priekspiedzina"}
	if !s.Match(fakeSource{}, l) {
		t.Error("keywords should ignore diacritics")
	}

	// Equipment must contain every required item; JSON gives []any.
	s.Filters.AllOf = map[string][]string{"equipment": {"ABS", "Kondicionieris"}}
	l.Attrs["equipment"] = []any{"ABS", "Kondicionieris", "ESP"}
	if !s.Match(fakeSource{}, l) {
		t.Error("equipment superset should match")
	}
	l.Attrs["equipment"] = []any{"ABS"}
	if s.Match(fakeSource{}, l) {
		t.Error("missing equipment item should fail")
	}
}

func TestParseFiltersContains(t *testing.T) {
	f, err := parseFilters(map[string]any{"text": map[string]any{"contains": []any{"quattro"}}})
	if err != nil || !reflect.DeepEqual(f.Contains, map[string][]string{"text": {"quattro"}}) {
		t.Errorf("got %+v, %v", f, err)
	}
}

func TestParseFiltersAll(t *testing.T) {
	f, err := parseFilters(map[string]any{"equipment": map[string]any{"all": []any{"ABS", "ESP"}}})
	if err != nil || !reflect.DeepEqual(f.AllOf, map[string][]string{"equipment": {"ABS", "ESP"}}) {
		t.Errorf("got %+v, %v", f, err)
	}
}
