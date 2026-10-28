package scraping

import (
	"context"
	"fmt"
	"testing"

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

func TestScrapeFeedDepth(t *testing.T) {
	f := &fakeFetcher{pages: map[string][][]string{"a": {{"1", "2"}, {"3", "4"}}}}
	var published []events.ListingDiscovered
	r := &Runner{
		Source: f, Feeds: fakeFeeds{"a"}, MaxPages: 3, seen: map[string]map[string]bool{},
		Publish: func(_ context.Context, l events.ListingDiscovered) error {
			published = append(published, l)
			return nil
		},
	}
	ctx := context.Background()

	// Cold start: one page only.
	if err := r.cycle(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || len(published) != 2 || published[0].Feed != "a" {
		t.Fatalf("cold start: calls=%v published=%d", f.calls, len(published))
	}

	// Page 1 shares an ad with the last cycle: no need to look deeper.
	f.calls = nil
	f.pages["a"] = [][]string{{"9", "1"}, {"2", "3"}}
	if err := r.cycle(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Errorf("partially known page: calls=%v, want 1", f.calls)
	}

	// Page 1 entirely new: older new ads may be on page 2.
	f.calls = nil
	f.pages["a"] = [][]string{{"20", "21"}, {"10", "9"}, {"7", "8"}, {"5", "6"}}
	if err := r.cycle(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 {
		t.Errorf("all-new page 1: calls=%v, want 2 (page 2 has known ad 9)", f.calls)
	}

	// Never beyond MaxPages.
	f.calls = nil
	f.pages["a"] = [][]string{{"30"}, {"31"}, {"32"}, {"33"}}
	if err := r.cycle(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 3 {
		t.Errorf("max pages: calls=%v, want 3", f.calls)
	}
}
