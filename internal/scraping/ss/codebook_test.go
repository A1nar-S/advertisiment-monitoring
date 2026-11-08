package ss

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
)

func TestCodebookLoads(t *testing.T) {
	cb, err := CarsCodebook()
	if err != nil {
		t.Fatal(err)
	}
	if len(cb.Makes) < 50 {
		t.Fatalf("only %d makes", len(cb.Makes))
	}
	skoda, ok := cb.MakeByName("ŠKODA") // case and diacritics do not matter
	if !ok || skoda.Slug != "skoda" {
		t.Fatalf("skoda = %+v", skoda)
	}
	if o, ok := skoda.Model("octavia"); !ok || o.Name != "Octavia" {
		t.Errorf("octavia = %+v", o)
	}
	for _, key := range []string{"make", "model", "year", "fuel", "gearbox", "body", "color", "equipment", "price"} {
		if _, ok := cb.Field(key); !ok {
			t.Errorf("missing field %q", key)
		}
	}
}

func TestNormalizeCarSearch(t *testing.T) {
	c := NewClient(time.Second)
	feeds := []string{"transport/cars"}
	_, got, err := c.NormalizeSearch(feeds, map[string]any{
		"make":      []any{"skoda", "TOYOTA"},
		"model":     []any{"octavia", "Avensis"},
		"fuel":      []any{"D", "dizelis"},
		"gearbox":   "automats",
		"body":      []any{"Universāls"},
		"year":      map[string]any{"min": uint64(2012)},
		"equipment": []any{"kondicionieris", "Sēdekļi: Apsildāmi"},
		"color":     []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"make":      []any{"Skoda", "Toyota"},
		"model":     []any{"Octavia", "Avensis"},
		"fuel":      []any{"Dīzelis"},
		"gearbox":   []any{"Automāts"},
		"body":      []any{"Universāls"},
		"year":      map[string]any{"min": uint64(2012)},
		"equipment": map[string]any{"all": []any{"Kondicionieris", "Sēdekļi: Apsildāmi"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}

	bad := []struct {
		filters map[string]any
		wantErr string
	}{
		{map[string]any{"make": []any{"Skodaa"}}, `did you mean "Skoda"`},
		{map[string]any{"gearbx": []any{"Automāts"}}, `did you mean "gearbox"`},
		{map[string]any{"make": []any{"Skoda"}, "model": []any{"Corolla"}}, `unknown model "Corolla"`},
		{map[string]any{"equipment": []any{"Apsildāmi"}}, `several groups`},
		{map[string]any{"year": 2012}, `{min: ..., max: ...}`},
		{map[string]any{"body": []any{"Sedan"}}, `unknown value "Sedan"`},
	}
	for _, b := range bad {
		_, _, err := c.NormalizeSearch(feeds, b.filters)
		if err == nil || !strings.Contains(err.Error(), b.wantErr) {
			t.Errorf("%v: err = %v, want it to mention %s", b.filters, err, b.wantErr)
		}
	}

	// A trailing * takes every generation of a model.
	_, got, err = c.NormalizeSearch(feeds, map[string]any{"make": []any{"Volkswagen"}, "model": []any{"passat*"}})
	if err != nil {
		t.Fatal(err)
	}
	if ms, _ := got["model"].([]any); len(ms) < 6 || !containsAny(ms, "Passat (B5)") {
		t.Errorf("passat* = %v", got["model"])
	}

	// Petrol/gas is indistinguishable from petrol on the list page.
	_, got, _ = c.NormalizeSearch(feeds, map[string]any{"fuel": []any{"Benzīns/gāze"}})
	if !reflect.DeepEqual(got["fuel"], []any{"Benzīns", "Benzīns/gāze"}) {
		t.Errorf("petrol/gas = %v", got["fuel"])
	}

	// Flats are not car searches: left alone.
	flats := map[string]any{"rooms": map[string]any{"min": 2}}
	if _, got, err := c.NormalizeSearch([]string{"real-estate/flats/riga/centre/hand_over"}, flats); err != nil || !reflect.DeepEqual(got, flats) {
		t.Errorf("flats: %v %v", got, err)
	}
	if _, _, err := c.NormalizeSearch([]string{"transport/cars", "real-estate/flats/riga/all/sell"}, flats); err == nil {
		t.Error("mixing categories should fail")
	}
}

func TestParseAdPage(t *testing.T) {
	f, err := os.Open("testdata/ad_car.html")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	attrs, err := ParseAdPage(f)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]any{"gearbox": "Automāts", "color": "Melna", "body": "Universāls", "fuel": "Dīzelis", "inspection": "Jā"} {
		if attrs[k] != v {
			t.Errorf("%s = %v, want %v", k, attrs[k], v)
		}
	}
	eq, _ := attrs["equipment"].([]any)
	if !containsAny(eq, "Kondicionieris") || containsAny(eq, "Atpakaļskata kamera") {
		t.Errorf("equipment = %v (bold items only)", eq)
	}
}

func TestEnrich(t *testing.T) {
	page, err := os.ReadFile("testdata/ad_car.html")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/gone.html") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(page)
	}))
	defer srv.Close()
	c := NewClient(time.Second).WithBaseURL(srv.URL)

	l := events.ListingDiscovered{
		Location: "transport/cars/peugeot/508", URL: baseURL + "/msg/lv/transport/cars/peugeot/508/x.html",
		Attrs: map[string]any{"year": 2017.0, "fuel": "Dīzelis"},
	}
	got, err := c.Enrich(context.Background(), l)
	if err != nil {
		t.Fatal(err)
	}
	if got.Attrs["gearbox"] != "Automāts" || got.Attrs["year"] != 2017.0 || len(l.Attrs) != 2 {
		t.Errorf("enriched attrs = %v (original must stay untouched: %v)", got.Attrs, l.Attrs)
	}
	l.URL = baseURL + "/msg/lv/transport/cars/peugeot/508/gone.html"
	if _, err := c.Enrich(context.Background(), l); !errors.Is(err, sources.ErrGone) {
		t.Errorf("removed ad: err = %v, want ErrGone", err)
	}
}

func containsAny(list []any, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
