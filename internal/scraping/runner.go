// Package scraping runs a site scraper on a schedule. Each site is a
// subpackage (ss, later city24, ...) implementing sources.Source.
package scraping

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
)

// FeedSource says which feeds currently have someone watching them.
type FeedSource interface {
	ActiveFeeds(ctx context.Context, source string) ([]string, error)
}

// statusError is implemented by fetch errors that carry an HTTP status.
type statusError interface{ HTTPStatus() int }

// Runner scrapes the active feeds every Interval and publishes what it finds.
// It keeps the number of requests to the site as low as it can: one page per
// feed unless that page was entirely new since the last cycle.
type Runner struct {
	Source   sources.Source
	Feeds    FeedSource
	Publish  func(context.Context, events.ListingDiscovered) error
	Alert    func(ctx context.Context, text string) // optional operational messages
	Interval time.Duration
	Gap      time.Duration // minimum time between two requests
	MaxPages int

	lastReq time.Time
	// seen holds the IDs from the previous cycle per feed. Empty on a cold
	// start, which keeps the first cycle to one page per feed.
	seen     map[string]map[string]bool
	failures int
}

// Run scrapes until ctx is done.
func (r *Runner) Run(ctx context.Context) {
	r.seen = map[string]map[string]bool{}
	for {
		err := r.cycle(ctx)
		if ctx.Err() != nil {
			return
		}
		r.noteResult(ctx, err)
		if !sleep(ctx, r.nextWait()) {
			return
		}
	}
}

func (r *Runner) noteResult(ctx context.Context, err error) {
	if err == nil {
		if r.failures > 0 {
			r.alert(ctx, "✅ **Crawl recovered**")
		}
		r.failures = 0
		return
	}
	slog.Error("crawl failed", "err", err)
	r.failures++
	if r.failures == 1 { // one alert per streak, not one per tick
		r.alert(ctx, "❌ **Crawl Error**\n"+err.Error())
	}
}

// nextWait is the interval with +-10% jitter so we don't hit the site on a
// fixed beat, doubled for every consecutive failed cycle (capped at 1h) so a
// block is not hammered.
func (r *Runner) nextWait() time.Duration {
	d := r.Interval
	for i := 1; i < r.failures && d < time.Hour; i++ {
		d *= 2
	}
	d = min(d, max(time.Hour, r.Interval))
	return d + time.Duration((rand.Float64()*0.2-0.1)*float64(d))
}

func (r *Runner) cycle(ctx context.Context) error {
	feeds, err := r.Feeds.ActiveFeeds(ctx, r.Source.Name())
	if err != nil {
		return fmt.Errorf("active feeds: %w", err)
	}
	if len(feeds) == 0 {
		slog.Info("no active feeds")
		return nil
	}

	var errs []error
	for _, feed := range feeds {
		if err := r.scrapeFeed(ctx, feed); err != nil {
			errs = append(errs, fmt.Errorf("feed %s: %w", feed, err))
			var se statusError
			if errors.As(err, &se) && blocked(se.HTTPStatus()) {
				break // the site is pushing back; don't try the other feeds
			}
		}
		if ctx.Err() != nil {
			break
		}
	}
	return errors.Join(errs...)
}

func (r *Runner) scrapeFeed(ctx context.Context, feed string) error {
	prev := r.seen[feed]
	cur := map[string]bool{}

	for page := 1; page <= r.MaxPages; page++ {
		if !r.throttle(ctx) {
			return ctx.Err()
		}
		ads, err := r.Source.FetchFeed(ctx, feed, page)
		if err != nil {
			return err
		}
		if len(ads) == 0 {
			// A 200 with no rows is how a layout change would look.
			slog.Warn("feed page parsed to zero listings", "feed", feed, "page", page)
		}

		known := 0
		for _, l := range ads {
			l.Feed = feed
			if prev[l.ExternalID] {
				known++
			}
			cur[l.ExternalID] = true
			if err := r.Publish(ctx, l); err != nil {
				return fmt.Errorf("publish: %w", err)
			}
		}
		slog.Info("scraped", "feed", feed, "page", page, "ads", len(ads), "known", known)

		// Go deeper only when this whole page is new to us: then older new ads
		// may be sitting on the next page. Not on a cold start (nothing to compare).
		if len(prev) == 0 || len(ads) == 0 || known > 0 {
			break
		}
	}
	r.seen[feed] = cur
	return nil
}

// throttle waits until Gap has passed since the previous request.
func (r *Runner) throttle(ctx context.Context) bool {
	if wait := r.Gap - time.Since(r.lastReq); wait > 0 && !sleep(ctx, wait) {
		return false
	}
	r.lastReq = time.Now()
	return true
}

func (r *Runner) alert(ctx context.Context, text string) {
	if r.Alert != nil {
		r.Alert(ctx, text)
	}
}

func blocked(code int) bool { return code == 429 || code == 403 || code >= 500 }

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
