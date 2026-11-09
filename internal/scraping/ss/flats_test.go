package ss

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCompileFlatSearch(t *testing.T) {
	c := NewClient(time.Second)
	got, err := c.CompileSearch(CategoryFlats, map[string]any{
		"region":   "riga",
		"district": []any{"centrs", "Teika"},
		"deal":     "izire",
		"rooms":    map[string]any{"min": 2.0},
		"series":   []any{"jaun"},
	})
	if err != nil {
		t.Fatal(err)
	}
	feeds := slices.Sorted(slices.Values(got.Feeds))
	want := []string{"real-estate/flats/riga/centre/hand_over", "real-estate/flats/riga/teika/hand_over"}
	if !reflect.DeepEqual(feeds, want) {
		t.Errorf("feeds = %v, want %v", feeds, want)
	}
	wantFilters := map[string]any{"rooms": map[string]any{"min": 2.0}, "series": []any{"Jaun."}}
	if !reflect.DeepEqual(got.Filters, wantFilters) {
		t.Errorf("filters = %v, want %v", got.Filters, wantFilters)
	}
	// The spec keeps the area, spelled as on ss.com, for editing.
	if !reflect.DeepEqual(got.Spec["region"], []any{"Rīga"}) || !reflect.DeepEqual(got.Spec["district"], []any{"Centrs", "Teika"}) ||
		!reflect.DeepEqual(got.Spec["deal"], []any{"Izīrē"}) {
		t.Errorf("spec = %v", got.Spec)
	}

	// No district: the whole region. Two deals: two feeds.
	got, err = c.CompileSearch(CategoryFlats, map[string]any{"region": "Jūrmala", "deal": []any{"Pārdod", "Izīrē"}})
	if err != nil {
		t.Fatal(err)
	}
	feeds = slices.Sorted(slices.Values(got.Feeds))
	if !reflect.DeepEqual(feeds, []string{"real-estate/flats/jurmala/all/hand_over", "real-estate/flats/jurmala/all/sell"}) {
		t.Errorf("whole region feeds = %v", feeds)
	}

	for _, bad := range []struct {
		filters map[string]any
		wantErr string
	}{
		{map[string]any{"deal": "Izīrē"}, "choose a region"},
		{map[string]any{"region": "Rīga"}, "choose a deal"},
		{map[string]any{"region": "Riga", "deal": "Izīrē", "district": "Tieka"}, `did you mean "Teika"`},
		{map[string]any{"region": "Riga", "deal": "Izīrē", "series": "Jaunais"}, `unknown value "Jaunais"`},
		{map[string]any{"region": "Riga", "deal": "Izīrē", "rooms": 2}, "give a range"},
		{map[string]any{"region": "Riga", "deal": "Izīrē", "gearbox": "Automāts"}, `unknown filter "gearbox"`},
	} {
		_, err := c.CompileSearch(CategoryFlats, bad.filters)
		if err == nil || !strings.Contains(err.Error(), bad.wantErr) {
			t.Errorf("%v: err = %v, want %q", bad.filters, err, bad.wantErr)
		}
	}
}

func TestParseFlatAdPage(t *testing.T) {
	f, err := os.Open("testdata/ad_flat.html")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	cb, err := FlatsCodebook()
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := parseAdPage(cb, f)
	if err != nil {
		t.Fatal(err)
	}
	if attrs["house_type"] != "Ķieģeļu" {
		t.Errorf("attrs = %v, want house_type Ķieģeļu", attrs)
	}
}
