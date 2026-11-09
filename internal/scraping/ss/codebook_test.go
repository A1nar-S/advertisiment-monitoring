package ss

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
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

func TestCompileCarSearch(t *testing.T) {
	c := NewClient(time.Second)
	got, err := c.CompileSearch(CategoryCars, map[string]any{
		"make":      []any{"skoda", "TOYOTA"},
		"model":     []any{"octavia", "Avensis"},
		"fuel":      []any{"D", "dizelis"},
		"gearbox":   "automats",
		"body":      []any{"Universāls"},
		"year":      map[string]any{"min": 2012.0},
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
		"year":      map[string]any{"min": 2012.0},
		"equipment": map[string]any{"all": []any{"Kondicionieris", "Sēdekļi: Apsildāmi"}},
	}
	if !reflect.DeepEqual(got.Filters, want) || !reflect.DeepEqual(got.Feeds, []string{"transport/cars"}) {
		t.Errorf("got  %v %v\nwant %v", got.Feeds, got.Filters, want)
	}
	if !reflect.DeepEqual(got.Spec["equipment"], []any{"Kondicionieris", "Sēdekļi: Apsildāmi"}) || !reflect.DeepEqual(got.Spec["fuel"], []any{"Dīzelis"}) {
		t.Errorf("spec = %v", got.Spec)
	}

	bad := []struct {
		filters map[string]any
		wantErr string
	}{
		{map[string]any{"make": []any{"Skodaa"}}, `did you mean "Skoda"`},
		{map[string]any{"gearbx": []any{"Automāts"}}, `did you mean "gearbox"`},
		{map[string]any{"make": []any{"Skoda"}, "model": []any{"Corolla"}}, `unknown model "Corolla"`},
		{map[string]any{"equipment": []any{"Apsildāmi"}}, `several groups`},
		{map[string]any{"year": 2012}, `give a range`},
		{map[string]any{"year": map[string]any{"min": "new"}}, `must be a number`},
		{map[string]any{"price": map[string]any{"min": 9000.0, "max": 100.0}}, `above max`},
		{map[string]any{"body": []any{"Sedan"}}, `unknown value "Sedan"`},
	}
	for _, b := range bad {
		_, err := c.CompileSearch(CategoryCars, b.filters)
		var inv *sources.InvalidSearch
		if !errors.As(err, &inv) || !strings.Contains(err.Error(), b.wantErr) {
			t.Errorf("%v: err = %v, want an InvalidSearch mentioning %s", b.filters, err, b.wantErr)
		}
	}
	if _, err := c.CompileSearch("boats", nil); err == nil {
		t.Error("unknown category should fail")
	}

	// A trailing * takes every generation; the spec keeps the wildcard.
	got, err = c.CompileSearch(CategoryCars, map[string]any{"make": []any{"Volkswagen"}, "model": []any{"passat*"}})
	if err != nil {
		t.Fatal(err)
	}
	if ms, _ := got.Filters["model"].([]any); len(ms) < 6 || !containsAny(ms, "Passat (B5)") {
		t.Errorf("passat* = %v", got.Filters["model"])
	}
	if !reflect.DeepEqual(got.Spec["model"], []any{"passat*"}) {
		t.Errorf("spec model = %v, want the wildcard kept", got.Spec["model"])
	}

	// Petrol/gas is indistinguishable from petrol on the list page.
	got, _ = c.CompileSearch(CategoryCars, map[string]any{"fuel": []any{"Benzīns/gāze"}})
	if !reflect.DeepEqual(got.Filters["fuel"], []any{"Benzīns", "Benzīns/gāze"}) || !reflect.DeepEqual(got.Spec["fuel"], []any{"Benzīns/gāze"}) {
		t.Errorf("petrol/gas = %v / spec %v", got.Filters["fuel"], got.Spec["fuel"])
	}
}

func TestCategories(t *testing.T) {
	cats := NewClient(time.Second).Categories()
	if len(cats) != 2 || cats[0].ID != CategoryCars || cats[1].ID != CategoryFlats {
		t.Fatalf("categories = %+v", cats)
	}
	byKey := map[string]sources.FieldSpec{}
	for _, f := range cats[0].Fields {
		byKey[f.Key] = f
	}
	if m := byKey["model"]; m.DependsOn != "make" || len(m.OptionsBy["Skoda"]) == 0 {
		t.Errorf("model field = %+v", m)
	}
	if g := byKey["gearbox"]; !g.Detail || len(g.Options) != 2 {
		t.Errorf("gearbox field = %+v", g)
	}
	if r := byKey["region"]; !r.Detail || r.Required || len(r.Options) < 25 {
		t.Errorf("car region field = %+v (optional, from the ad page, every region)", r)
	}
	if y := byKey["year"]; y.Kind != "range" || len(y.Options) != 0 {
		t.Errorf("year field = %+v", y)
	}
	for _, f := range cats[1].Fields {
		if f.Key == "district" && len(f.OptionsBy["Rīga"]) < 50 {
			t.Errorf("districts of Rīga = %d", len(f.OptionsBy["Rīga"]))
		}
		if f.Key == "region" && !f.Required {
			t.Error("region should be required")
		}
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
	for k, v := range map[string]any{"gearbox": "Automāts", "color": "Melna", "body": "Universāls", "fuel": "Dīzelis", "inspection": "Jā", "region": "Rīga"} {
		if attrs[k] != v {
			t.Errorf("%s = %v, want %v", k, attrs[k], v)
		}
	}
	eq, _ := attrs["equipment"].([]any)
	if !containsAny(eq, "Kondicionieris") || containsAny(eq, "Atpakaļskata kamera") {
		t.Errorf("equipment = %v (bold items only)", eq)
	}
	text, _ := attrs[TextAttr].(string)
	if !strings.HasPrefix(text, "Kopts 508, 1, 6D automāts Ekonomisks, ērts") || !strings.HasSuffix(text, "kopta ārpuse") {
		t.Errorf("text = %q: want the description only, without the options table", text)
	}
}

func TestKeywords(t *testing.T) {
	c := NewClient(time.Second)
	got, err := c.CompileSearch(CategoryCars, map[string]any{"make": "Audi", "keywords": []any{" quattro ", "4x4", "Quattro", ""}})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got.Filters[TextAttr]) != "map[contains:[quattro 4x4]]" || fmt.Sprint(got.Spec[KeywordsKey]) != "[quattro 4x4]" {
		t.Errorf("compiled = %+v", got)
	}
	if got, _ := c.CompileSearch(CategoryCars, map[string]any{"keywords": []any{}}); len(got.Filters) != 0 {
		t.Errorf("empty keywords = %+v", got.Filters)
	}
	for _, bad := range []any{[]any{"x"}, []any{strings.Repeat("a", 51)}, []any{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "b1", "b2"}} {
		if _, err := c.CompileSearch(CategoryCars, map[string]any{"keywords": bad}); err == nil {
			t.Errorf("keywords %v accepted", bad)
		}
	}
	if _, err := c.CompileSearch(CategoryFlats, map[string]any{"region": "Rīga", "deal": "Izīrē", "keywords": []any{"balkons"}}); err == nil {
		t.Error("flats have no keywords filter")
	}

	var keys []string
	for _, cat := range c.Categories() {
		if cat.ID == CategoryCars {
			for _, f := range cat.Fields {
				keys = append(keys, f.Key)
			}
		}
	}
	if i := slices.Index(keys, KeywordsKey); i < 1 || keys[i-1] != "model" {
		t.Errorf("car fields = %v: want keywords after model", keys)
	}
	if !slices.Contains(c.EnrichableKeys(), TextAttr) {
		t.Error("the ad text should be enrichable")
	}
}

func TestRegionOption(t *testing.T) {
	cb, err := CarsCodebook()
	if err != nil {
		t.Fatal(err)
	}
	f, _ := cb.Field("region")
	for in, want := range map[string]string{
		"Rīga": "Rīga", "Jelgava un raj.": "Jelgava un rajons", "Rīgas raj.": "Rīgas rajons",
		"Daugavpils un rajons": "Daugavpils un rajons", "Rīga, Teika": "Rīga", "": "", "Marsa": "",
	} {
		o, _ := regionOption(f, in)
		if o.Name != want {
			t.Errorf("regionOption(%q) = %q, want %q", in, o.Name, want)
		}
	}
}

func TestCarRegionSearch(t *testing.T) {
	got, err := NewClient(time.Second).CompileSearch(CategoryCars, map[string]any{"region": []any{"riga", "Jelgava un rajons"}})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got.Filters["region"]) != "[Rīga Jelgava un rajons]" || len(got.Feeds) != 1 {
		t.Errorf("compiled = %+v", got)
	}
	if _, err := NewClient(time.Second).CompileSearch(CategoryCars, map[string]any{"region": "Rigaa"}); err == nil {
		t.Error("unknown region accepted")
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
