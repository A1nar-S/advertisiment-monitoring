package subscriptions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

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

// Needs a real Postgres: TEST_DATABASE_URL=postgres://... go test ./internal/subscriptions
func TestSeedAndNotifications(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS matches, searches, channels, users, subscriptions_migrations`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(pool, sources.NewRegistry(fakeSource{}), 3)
	if err := svc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	seed := filepath.Join(t.TempDir(), "users.yaml")
	if err := os.WriteFile(seed, []byte(`
users:
  - name: alice
    channels: [{kind: discord, target: "111"}]
    searches:
      - name: cheap centre
        feeds: [flats/centre]
        filters:
          price: {max: 450}
  - name: bob
    channels: [{kind: discord, target: "222"}]
    searches:
      - name: teika
        feeds: [flats/teika]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // reseeding must not duplicate
		if err := svc.SeedFromFile(ctx, seed); err != nil {
			t.Fatal(err)
		}
	}

	feeds, err := svc.ActiveFeeds(ctx, "ss.com")
	if err != nil || !reflect.DeepEqual(feeds, []string{"flats/centre", "flats/teika"}) {
		t.Fatalf("feeds = %v, err = %v", feeds, err)
	}

	l := events.ListingDiscovered{Source: "ss.com", ExternalID: "1", Location: "flats/centre", PriceValue: ptr(400.0)}
	got, err := svc.Notifications(ctx, l)
	if err != nil || len(got) != 1 {
		t.Fatalf("notifications = %v, err = %v", got, err)
	}
	if got[0].Channel.Target != "111" || got[0].Key == "" || got[0].Listing == nil {
		t.Errorf("unexpected notification %+v", got[0])
	}
	if err := svc.Record(ctx, got); err != nil {
		t.Fatal(err)
	}
	if again, _ := svc.Notifications(ctx, l); len(again) != 0 {
		t.Errorf("after Record, redelivery produced %d notifications, want 0", len(again))
	}

	// A seed file naming a source nobody registered is rejected, not stored.
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("users:\n  - name: c\n    channels: [{kind: discord, target: \"3\"}]\n    searches:\n      - {name: x, source: nope, feeds: [a]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.SeedFromFile(ctx, bad); err == nil {
		t.Error("expected an error for an unknown source")
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

func TestParseFiltersAll(t *testing.T) {
	f, err := parseFilters(map[string]any{"equipment": map[string]any{"all": []any{"ABS", "ESP"}}})
	if err != nil || !reflect.DeepEqual(f.AllOf, map[string][]string{"equipment": {"ABS", "ESP"}}) {
		t.Errorf("got %+v, %v", f, err)
	}
}

// enrichingSource reads "ad pages" by counting calls.
type enrichingSource struct {
	fakeSource
	calls int
	err   error
}

func (*enrichingSource) EnrichableKeys() []string { return []string{"gearbox"} }
func (e *enrichingSource) Enrich(_ context.Context, l events.ListingDiscovered) (events.ListingDiscovered, error) {
	e.calls++
	if e.err != nil {
		return l, e.err
	}
	out := l
	out.Attrs = maps(l.Attrs)
	out.Attrs["gearbox"] = "Automāts"
	return out, nil
}

func TestNotificationsEnrichOnce(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS matches, searches, channels, users, subscriptions_migrations`); err != nil {
		t.Fatal(err)
	}
	src := &enrichingSource{}
	svc := NewService(pool, sources.NewRegistry(src), 3)
	if err := svc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(t.TempDir(), "users.yaml")
	// Two users, three searches; two need the gearbox, one also wants manual.
	if err := os.WriteFile(seed, []byte(`
users:
  - name: alice
    channels: [{kind: discord, target: "111"}]
    searches:
      - {name: auto, feeds: [cars], filters: {gearbox: [Automāts]}}
      - {name: cheap, feeds: [cars], filters: {price: {max: 6000}}}
  - name: bob
    channels: [{kind: discord, target: "222"}]
    searches:
      - {name: auto-or-manual, feeds: [cars], filters: {gearbox: [Automāts, Manuāla], year: {min: 2010}}}
      - {name: manual, feeds: [cars], filters: {gearbox: [Manuāla]}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.SeedFromFile(ctx, seed); err != nil {
		t.Fatal(err)
	}

	l := events.ListingDiscovered{Source: "ss.com", ExternalID: "7", Location: "cars", PriceValue: ptr(5000.0), Attrs: map[string]any{"year": 2015.0}}
	got, err := svc.Notifications(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, n := range got {
		names = append(names, n.SearchName)
		if v, _ := n.Listing.Str("gearbox"); v != "Automāts" {
			t.Errorf("%s: notification lacks the enriched gearbox", n.SearchName)
		}
	}
	slices.Sort(names)
	if !reflect.DeepEqual(names, []string{"auto", "auto-or-manual", "cheap"}) || src.calls != 1 {
		t.Errorf("matched %v with %d ad-page reads, want 3 searches and 1 read", names, src.calls)
	}

	// The ad page failing: candidates still hear about it, flagged.
	src.err = errors.New("403")
	l.ExternalID = "8"
	got, err = svc.Notifications(ctx, l)
	if err != nil || len(got) != 4 {
		t.Fatalf("unconfirmed: %d notifications, err %v; want all 4", len(got), err)
	}
	if f := got[len(got)-1].Listing.Fields; len(f) == 0 || !strings.Contains(f[len(f)-1].Value, "gearbox") {
		t.Errorf("unconfirmed listing not flagged: %+v", f)
	}

	// A removed ad matches nothing that needed its page.
	src.err = sources.ErrGone
	l.ExternalID = "9"
	got, _ = svc.Notifications(ctx, l)
	if len(got) != 1 || got[0].SearchName != "cheap" {
		t.Errorf("gone: %+v, want only the cheap search", got)
	}
}
