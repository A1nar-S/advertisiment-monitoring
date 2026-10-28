package ss

import (
	"context"
	"io"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// fixtureFeeds maps each saved page to the feed it was fetched from.
var fixtureFeeds = map[string]string{
	"testdata/feed_district.html":   "real-estate/flats/riga/centre/hand_over",
	"testdata/feed_city.html":       "real-estate/flats/riga/all/hand_over",
	"testdata/feed_cars_model.html": "transport/cars/audi/a4",
	"testdata/feed_cars_make.html":  "transport/cars/audi",
}

func headerNames(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	doc.Find("tr#head_line a.a18").Each(func(_ int, s *goquery.Selection) { names = append(names, text(s)) })
	return names
}

// The scraper reads only page 1 unless that whole page is new, so a brand-new
// ad must land at the top. Feeds are ordered by last bump, not by ID (an old
// ad can sit near the top after being refreshed), so the check is that the
// highest ID on the page, the newest ad, is among the first few rows.
func TestFixturesPutNewestAdOnTop(t *testing.T) {
	for file := range fixtureFeeds {
		t.Run(file, func(t *testing.T) {
			ads := parseFile(t, file)
			newest, at := 0, 0
			for i, a := range ads {
				if n, _ := strconv.Atoi(a.ExternalID); n > newest {
					newest, at = n, i
				}
			}
			if at > 4 {
				t.Errorf("newest ad %d is row %d; page 1 may not be ordered newest-first", newest, at)
			}
		})
	}
}

// TestLiveStructure checks the saved pages still match what ss.com serves.
// It makes a handful of real requests, so it only runs on request:
//
//	SS_LIVE=1 go test ./internal/scraping/ss -run TestLiveStructure -v
//
// A failure means the site changed (or a fixture is stale): re-save the page
// and adjust the parser.
func TestLiveStructure(t *testing.T) {
	if os.Getenv("SS_LIVE") == "" {
		t.Skip("SS_LIVE not set")
	}
	c := NewClient(30 * time.Second)
	ctx := context.Background()

	for file, feed := range fixtureFeeds {
		t.Run(feed, func(t *testing.T) {
			time.Sleep(2 * time.Second) // be polite

			ads, err := c.FetchFeed(ctx, feed, 1)
			if err != nil {
				t.Fatalf("live fetch+parse failed: %v", err)
			}
			if len(ads) != 30 {
				t.Errorf("live page has %d ads, fixture has 30", len(ads))
			}

			// Same columns as the fixture, in the same order.
			resp, err := c.http.Get(FeedURL(feed, 1))
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			tmp := t.TempDir() + "/live.html"
			if err := os.WriteFile(tmp, body, 0o600); err != nil {
				t.Fatal(err)
			}
			if live, saved := headerNames(t, tmp), headerNames(t, file); !reflect.DeepEqual(live, saved) {
				t.Errorf("column headers changed: live %v, fixture %v", live, saved)
			}

			// Deeper pages follow the documented URL scheme.
			if feed == "real-estate/flats/riga/centre/hand_over" {
				time.Sleep(2 * time.Second)
				if p2, err := c.FetchFeed(ctx, feed, 2); err != nil || len(p2) == 0 {
					t.Errorf("page 2: %d ads, err %v", len(p2), err)
				}
			}
		})
	}
}
