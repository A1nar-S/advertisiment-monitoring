package ss

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
)

const (
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)

// StatusError is returned for HTTP responses >= 400. The scraper uses the
// code to decide whether to back off (429, 403, 5xx).
type StatusError struct {
	URL  string
	Code int
}

func (e *StatusError) Error() string { return fmt.Sprintf("fetch %s: status %d", e.URL, e.Code) }

// Client fetches and parses ss.com pages.
type Client struct {
	http *http.Client
	base string

	// gap spaces out requests the client throttles itself: ad pages and
	// codebook builds. Feed pages are paced by the scraper's runner.
	gap     time.Duration
	mu      sync.Mutex
	lastReq time.Time
}

func NewClient(timeout time.Duration) *Client {
	return &Client{http: &http.Client{Timeout: timeout}, base: baseURL}
}

// WithBaseURL points the client at another host (a local fake in tests).
func (c *Client) WithBaseURL(u string) *Client {
	c.base = strings.TrimRight(u, "/")
	return c
}

// FeedURL builds the URL of one page of a feed. A feed is SS.com's own
// category path below /lv/, e.g. "real-estate/flats/riga/centre/hand_over" or
// "transport/cars/audi/a4".
func FeedURL(feed string, page int) string { return feedURL(baseURL, feed, page) }

func feedURL(base, feed string, page int) string {
	u := base + "/lv/" + strings.Trim(feed, "/") + "/"
	if page > 1 {
		u += fmt.Sprintf("page%d.html", page)
	}
	return u
}

// FetchFeed GETs one page of a feed and parses it. Feed pages list the newest
// (or most recently refreshed) ads first, 30 per page.
func (c *Client) FetchFeed(ctx context.Context, feed string, page int) ([]events.ListingDiscovered, error) {
	body, err := c.get(ctx, feedURL(c.base, feed, page))
	if err != nil {
		return nil, err
	}
	return Parse(bytes.NewReader(body), time.Now())
}

// WithGap makes the client wait at least gap between the requests it
// throttles itself (ad pages, codebook builds).
func (c *Client) WithGap(gap time.Duration) *Client {
	c.gap = gap
	return c
}

func (c *Client) withGap(gap time.Duration) *Client {
	if gap > c.gap {
		c.gap = gap
	}
	return c
}

// throttle waits until gap has passed since the previous throttled request.
func (c *Client) throttle(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if wait := c.gap - time.Since(c.lastReq); wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	c.lastReq = time.Now()
	return nil
}

// maxPage caps a page read; ss.com pages are well under 1 MB.
const maxPage = 4 << 20

// get GETs url, throttled by gap when one is set.
func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	if c.gap > 0 {
		if err := c.throttle(ctx); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "lv,en;q=0.8")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, &StatusError{URL: url, Code: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPage))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return body, nil
}

// Name implements sources.Source.
func (c *Client) Name() string { return Source }

// HTTPStatus lets the scraper tell "the site is pushing back" from other errors.
func (e *StatusError) HTTPStatus() int { return e.Code }
