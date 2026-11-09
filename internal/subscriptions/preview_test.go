package subscriptions

import (
	"slices"
	"testing"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/scraping/ss"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
)

func TestPreview(t *testing.T) {
	svc := NewService(nil, sources.NewRegistry(ss.NewClient(0)), 3)
	car := func(location string, attrs map[string]any) events.ListingDiscovered {
		return events.ListingDiscovered{Source: "ss.com", ExternalID: location, Location: location, Attrs: attrs}
	}
	candidates := []events.ListingDiscovered{
		0: car("transport/cars/audi/a4", map[string]any{"make": "Audi", "year": 2010.0}),
		1: car("transport/cars/audi/a6", map[string]any{"make": "Audi", "year": 2001.0}),                       // too old
		2: car("transport/cars/bmw/x3", map[string]any{"make": "BMW", "year": 2015.0}),                         // other make
		3: car("transport/cars/audi/a4", map[string]any{"make": "Audi", "year": 2012.0, "gearbox": "Manuāla"}), // page read: manual
		4: car("transport/cars/audi/a4", map[string]any{"make": "Audi", "year": 2012.0, "gearbox": "Automāts"}),
	}

	p, err := svc.Preview(SearchInput{Category: "cars", Filters: map[string]any{
		"make": []any{"Audi"}, "year": map[string]any{"min": 2005.0}, "gearbox": []any{"Automāts"},
	}}, candidates)
	if err != nil {
		t.Fatal(err)
	}
	// Car searches share the one transport/cars feed, so every car is in
	// scope and the make is a filter.
	if p.InScope != 5 {
		t.Errorf("in scope = %d, want all 5 cars", p.InScope)
	}
	if !slices.Equal(p.Matched, []int{4}) {
		t.Errorf("matched = %v, want only the automatic whose page was read", p.Matched)
	}
	if !slices.Equal(p.Unsure, []int{0}) || !slices.Equal(p.UnsureKeys, []string{"gearbox"}) {
		t.Errorf("unsure = %v %v, want the 2010 A4 pending its gearbox", p.Unsure, p.UnsureKeys)
	}

	if _, err := svc.Preview(SearchInput{Category: "boats"}, candidates); err == nil {
		t.Error("unknown category: want an error")
	}
}
