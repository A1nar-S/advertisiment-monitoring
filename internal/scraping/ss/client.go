package ss

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// Client fetches and parses ss.com feed pages.
type Client struct {
	http *http.Client
	base string
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
	url := feedURL(c.base, feed, page)
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
	return Parse(resp.Body, time.Now())
}

// Name implements sources.Source.
func (c *Client) Name() string { return Source }

// HTTPStatus lets the scraper tell "the site is pushing back" from other errors.
func (e *StatusError) HTTPStatus() int { return e.Code }
