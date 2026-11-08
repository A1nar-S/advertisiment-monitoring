package scraping

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
)

type fakeFetcher struct {
	pages map[string][][]string // feed -> pages -> ad IDs
	calls []string
}

func (f *fakeFetcher) Name() string                                    { return "ss.com" }
func (f *fakeFetcher) PlanFeeds(w []string, _ int) []string            { return w }
func (f *fakeFetcher) InScope([]string, events.ListingDiscovered) bool { return true }
func (f *fakeFetcher) FetchFeed(_ context.Context, feed string, page int) ([]events.ListingDiscovered, error) {
	f.calls = append(f.calls, fmt.Sprintf("%s#%d", feed, page))
	var ads []events.ListingDiscovered
	if pages := f.pages[feed]; page <= len(pages) {
		for _, id := range pages[page-1] {
			ads = append(ads, events.ListingDiscovered{ExternalID: id})
		}
	}
	return ads, nil
}

type fakeFeeds []string

func (f fakeFeeds) ActiveFeeds(context.Context, string) ([]string, error) { return f, nil }

// fakeHistory stands in for the listings table.
type fakeHistory struct {
	stored map[string]bool
	feeds  map[string]bool
}

func (h *fakeHistory) KnownIDs(_ context.Context, _ string, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		if h.stored[id] {
			out[id] = true
		}
	}
	return out, nil
}

func (h *fakeHistory) FeedSeen(_ context.Context, _, feed string) (bool, error) {
	return h.feeds[feed], nil
}

// ids renders "p1:a,b|p2:c" style page layouts compactly: pages("1 2", "3 4").
func pages(ps ...string) [][]string {
	out := make([][]string, len(ps))
	for i, p := range ps {
		out[i] = strings.Fields(p)
	}
	return out
}

func newRunner(f *fakeFetcher, h History, published *[]events.ListingDiscovered) *Runner {
	return &Runner{
		Source: f, Feeds: fakeFeeds{"a"}, History: h, MaxPages: 3, Interval: 10 * time.Minute,
		Publish: func(_ context.Context, l events.ListingDiscovered) error {
			*published = append(*published, l)
			return nil
		},
	}
}

func TestScrapeFeedDepth(t *testing.T) {
	f := &fakeFetcher{pages: map[string][][]string{"a": pages("1 2", "3 4")}}
	var published []events.ListingDiscovered
	r := newRunner(f, nil, &published)
	ctx := context.Background()

	// First ever scrape: one page only (ingest stores it silently anyway).
	if err := r.cycle(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || len(published) != 2 || published[0].Feed != "a" {
		t.Fatalf("first scrape: calls=%v published=%d", f.calls, len(published))
	}

	// The bottom of page 1 is known: everything below is older.
	f.calls = nil
	f.pages["a"] = pages("9 1 2", "3 4")
	if err := r.cycle(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Errorf("bottom known: calls=%v, want 1", f.calls)
	}

	// Bumped known ads at the top must not stop the scan: rows below them
	// and on page 2 are new.
	f.calls = nil
	f.pages["a"] = pages("1 2 20 21", "22 9 2", "5 6")
	if err := r.cycle(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 {
		t.Errorf("bumped on top: calls=%v, want 2 (page 2 ends in known 9 2)", f.calls)
	}

	// Never beyond MaxPages, and running out is reported as a gap.
	f.calls = nil
	f.pages["a"] = pages("30", "31", "32", "33")
	gap, err := r.scrapeFeed(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 3 || !gap {
		t.Errorf("max pages: calls=%v gap=%v, want 3 calls and a gap", f.calls, gap)
	}
}

func TestRestartUsesHistory(t *testing.T) {
	// Ads 1-4 were stored before the restart; 40 new ones arrived meanwhile,
	// with two old ads bumped to the top.
	h := &fakeHistory{stored: map[string]bool{"1": true, "2": true, "3": true, "4": true}, feeds: map[string]bool{"a": true}}
	f := &fakeFetcher{pages: map[string][][]string{"a": pages("3 4 n1 n2", "n3 n4", "n5 1 2", "x y")}}
	var published []events.ListingDiscovered
	r := newRunner(f, h, &published)

	if err := r.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 3 {
		t.Errorf("restart: calls=%v, want 3 (page 3 ends in stored 1 2)", f.calls)
	}
	if len(r.gaps) != 0 {
		t.Errorf("restart caught up but gap recorded: %v", r.gaps)
	}
}

func TestFirstEverWithHistory(t *testing.T) {
	h := &fakeHistory{stored: map[string]bool{}, feeds: map[string]bool{}}
	f := &fakeFetcher{pages: map[string][][]string{"a": pages("1 2", "3 4", "5 6")}}
	var published []events.ListingDiscovered
	r := newRunner(f, h, &published)
	if err := r.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Errorf("first ever: calls=%v, want 1", f.calls)
	}
}

func TestGapAlertsAndSpeedsUp(t *testing.T) {
	h := &fakeHistory{stored: map[string]bool{}, feeds: map[string]bool{"a": true}}
	f := &fakeFetcher{pages: map[string][][]string{"a": pages("1 2", "3 4", "5 6", "7 8")}}
	var published []events.ListingDiscovered
	var alerts []string
	r := newRunner(f, h, &published)
	r.Alert = func(_ context.Context, text string) { alerts = append(alerts, text) }
	ctx := context.Background()

	// Two busy cycles: one alert, not two.
	for range 2 {
		if err := r.cycle(ctx); err != nil {
			t.Fatal(err)
		}
		// Each cycle is all-new again.
		for _, l := range published {
			delete(r.seen["a"], l.ExternalID)
		}
	}
	if len(alerts) != 1 || !strings.Contains(alerts[0], "busier") {
		t.Fatalf("alerts=%q, want one gap alert", alerts)
	}
	if w := r.nextWait(); w > 6*time.Minute {
		t.Errorf("wait during catch-up = %s, want about half of 10m", w)
	}

	// Caught up: one recovery alert and the normal pace again.
	h.stored = map[string]bool{"1": true, "2": true}
	if err := r.cycle(ctx); err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 2 || !strings.Contains(alerts[1], "caught up") {
		t.Fatalf("alerts=%q, want a caught-up alert", alerts)
	}
	if w := r.nextWait(); w < 9*time.Minute {
		t.Errorf("wait after catch-up = %s, want about 10m", w)
	}
}
