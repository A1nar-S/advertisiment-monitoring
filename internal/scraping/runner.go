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

// History is what was stored on earlier runs, so a restart knows where it
// left off instead of starting from nothing.
type History interface {
	// KnownIDs reports which of ids are already stored for source.
	KnownIDs(ctx context.Context, source string, ids []string) (map[string]bool, error)
	// FeedSeen reports whether feed was ever scraped before.
	FeedSeen(ctx context.Context, source, feed string) (bool, error)
}

// statusError is implemented by fetch errors that carry an HTTP status.
type statusError interface{ HTTPStatus() int }

// boundaryRows is how many rows at the end of a page must already be known to
// stop paging. Feeds are ordered by last bump, so a known row near the top
// says nothing (it was bumped); known rows at the bottom mean everything
// further down is older still. Two rows, because a single bumped ad can land
// exactly on a page boundary during a burst.
const boundaryRows = 2

// minWait is the shortest pause between cycles while catching up a burst.
const minWait = time.Minute

// Runner scrapes the active feeds every Interval and publishes what it finds.
// It keeps the number of requests to the site as low as it can: one page per
// feed, and the next page only while the bottom of the current one is still
// unseen, up to MaxPages.
type Runner struct {
	Source   sources.Source
	Feeds    FeedSource
	History  History // optional; without it only the previous cycle is remembered
	Publish  func(context.Context, events.ListingDiscovered) error
	Alert    func(ctx context.Context, text string) // optional operational messages
	Interval time.Duration
	Gap      time.Duration // minimum time between two requests
	MaxPages int

	lastReq time.Time
	// seen holds the IDs from the previous cycle per feed, covering the time
	// before ingest has stored them.
	seen     map[string]map[string]bool
	failures int
	// gaps are the feeds whose last scan hit MaxPages before reaching known
	// ads: some may be missed, so the next cycle comes sooner.
	gaps map[string]bool
	// idle: no feeds yet (searches not seeded, or all disabled); look again
	// soon rather than a full interval later.
	idle bool
}

// Run scrapes until ctx is done.
func (r *Runner) Run(ctx context.Context) {
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
// block is not hammered, and halved while a burst is being caught up.
func (r *Runner) nextWait() time.Duration {
	d := r.Interval
	for i := 1; i < r.failures && d < time.Hour; i++ {
		d *= 2
	}
	d = min(d, max(time.Hour, r.Interval))
	if r.failures == 0 && len(r.gaps) > 0 {
		d = max(d/2, min(minWait, r.Interval))
	}
	if r.failures == 0 && r.idle {
		d = min(minWait, r.Interval)
	}
	return d + time.Duration((rand.Float64()*0.2-0.1)*float64(d))
}

func (r *Runner) cycle(ctx context.Context) error {
	if r.seen == nil {
		r.seen = map[string]map[string]bool{}
	}
	if r.gaps == nil {
		r.gaps = map[string]bool{}
	}
	feeds, err := r.Feeds.ActiveFeeds(ctx, r.Source.Name())
	if err != nil {
		return fmt.Errorf("active feeds: %w", err)
	}
	r.idle = len(feeds) == 0
	if r.idle {
		slog.Info("no active feeds yet")
		return nil
	}

	var errs []error
	for _, feed := range feeds {
		gap, err := r.scrapeFeed(ctx, feed)
		if err != nil {
			errs = append(errs, fmt.Errorf("feed %s: %w", feed, err))
			var se statusError
			if errors.As(err, &se) && blocked(se.HTTPStatus()) {
				break // the site is pushing back; don't try the other feeds
			}
		} else {
			r.noteGap(ctx, feed, gap)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return errors.Join(errs...)
}

// noteGap alerts once when a feed starts outrunning MaxPages and once when it
// has caught up again.
func (r *Runner) noteGap(ctx context.Context, feed string, gap bool) {
	switch {
	case gap && !r.gaps[feed]:
		r.gaps[feed] = true
		slog.Warn("feed outran max pages; some ads may be missed", "feed", feed, "max_pages", r.MaxPages)
		r.alert(ctx, fmt.Sprintf("⚠️ **Feed busier than %d pages**\n`%s`: some new ads may be missed. Scanning more often until it catches up.", r.MaxPages, feed))
	case !gap && r.gaps[feed]:
		delete(r.gaps, feed)
		slog.Info("feed caught up", "feed", feed)
		r.alert(ctx, fmt.Sprintf("✅ **Feed caught up**\n`%s`", feed))
	}
}

// scrapeFeed fetches pages of feed until it reaches ads it already knows. It
// reports a gap when MaxPages ran out first.
func (r *Runner) scrapeFeed(ctx context.Context, feed string) (gap bool, err error) {
	prev := r.seen[feed]
	cur := map[string]bool{}

	firstEver := false
	if len(prev) == 0 {
		firstEver = !r.feedSeen(ctx, feed)
	}

	for page := 1; page <= r.MaxPages; page++ {
		if !r.throttle(ctx) {
			return false, ctx.Err()
		}
		ads, err := r.Source.FetchFeed(ctx, feed, page)
		if err != nil {
			return false, err
		}
		if len(ads) == 0 {
			// A 200 with no rows is how a layout change would look.
			slog.Warn("feed page parsed to zero listings", "feed", feed, "page", page)
			break
		}

		ids := make([]string, len(ads))
		for i, l := range ads {
			ids[i] = l.ExternalID
		}
		known := r.known(ctx, prev, ids)

		for _, l := range ads {
			l.Feed = feed
			cur[l.ExternalID] = true
			if err := r.Publish(ctx, l); err != nil {
				return false, fmt.Errorf("publish: %w", err)
			}
		}
		slog.Info("scraped", "feed", feed, "page", page, "ads", len(ads), "known", len(known))

		// A feed never scraped before is stored silently by ingest anyway;
		// paging deeper would only cost requests.
		if firstEver || reachedKnown(ids, known) {
			break
		}
		if page == r.MaxPages {
			gap = true
		}
	}
	r.seen[feed] = cur
	return gap, nil
}

// reachedKnown: the last boundaryRows rows of the page are known, so the
// rest of the feed is older than what was seen before.
func reachedKnown(ids []string, known map[string]bool) bool {
	n := min(boundaryRows, len(ids))
	for _, id := range ids[len(ids)-n:] {
		if !known[id] {
			return false
		}
	}
	return true
}

// known merges what the previous cycle saw with what is stored. A failing
// lookup only makes the scan rely on memory; it does not fail the cycle.
func (r *Runner) known(ctx context.Context, prev map[string]bool, ids []string) map[string]bool {
	out := map[string]bool{}
	for _, id := range ids {
		if prev[id] {
			out[id] = true
		}
	}
	if r.History == nil {
		return out
	}
	stored, err := r.History.KnownIDs(ctx, r.Source.Name(), ids)
	if err != nil {
		slog.Warn("known ids lookup failed; using memory only", "err", err)
		return out
	}
	for id := range stored {
		out[id] = true
	}
	return out
}

func (r *Runner) feedSeen(ctx context.Context, feed string) bool {
	if r.History == nil {
		return false
	}
	seen, err := r.History.FeedSeen(ctx, r.Source.Name(), feed)
	if err != nil {
		slog.Warn("feed history lookup failed", "feed", feed, "err", err)
		return false
	}
	return seen
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
