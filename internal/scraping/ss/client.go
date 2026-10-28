package ss

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
)

// Client fetches and parses ss.com search results.
type Client struct {
	http *http.Client
}

func NewClient(timeout time.Duration) *Client {
	// net/http follows the search POST's 302 redirect (as a GET).
	return &Client{http: &http.Client{Timeout: timeout}}
}

// ParseFormData turns "k=v&k2=v2" (values unencoded, as copied from the
// browser) into url.Values.
func ParseFormData(raw string) url.Values {
	form := url.Values{}
	for _, pair := range strings.Split(raw, "&") {
		if k, v, ok := strings.Cut(pair, "="); ok {
			form.Add(k, v)
		}
	}
	return form
}

// Fetch POSTs the search form (or GETs when form is empty) and parses the result.
func (c *Client) Fetch(ctx context.Context, searchURL string, form url.Values) ([]events.ListingDiscovered, error) {
	var req *http.Request
	var err error
	if len(form) > 0 {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, searchURL, strings.NewReader(form.Encode()))
		if req != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	}
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", searchURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("fetch %s: status %d", searchURL, resp.StatusCode)
	}
	return Parse(resp.Body, time.Now())
}
