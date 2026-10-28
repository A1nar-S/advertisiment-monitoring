package subscriptions

import (
	"context"
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
