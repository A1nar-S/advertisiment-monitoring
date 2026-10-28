// Package events holds the contracts that cross slice boundaries. It is the
// only package every slice may import. Keep it free of logic: these structs
// are the Kafka message payloads (JSON for now).
package events

import "time"

// Kafka topics connecting the services.
const (
	TopicListingsDiscovered     = "listings.discovered"     // scraper -> ingest
	TopicListingsNew            = "listings.new"            // ingest -> matcher
	TopicNotificationsRequested = "notifications.requested" // matcher/scraper -> notifier
)

// ListingDiscovered is emitted by a source for every ad found on a feed page,
// whether or not it has been seen before.
//
// It is deliberately source-neutral so a new site (city24, vinted, ...) needs
// no change here: what is specific to a site goes in Attrs (for filtering)
// and Fields (for display).
type ListingDiscovered struct {
	Source     string `json:"source"`      // e.g. "ss.com"
	ExternalID string `json:"external_id"` // ID on the source site, unique per source
	// Feed is the feed this ad was scraped from. Ingest uses it to store a
	// brand-new feed's existing ads without notifying anyone.
	Feed string `json:"feed"`
	// Location is a source-defined slug (e.g. "riga/centre"). Searches never
	// interpret it; the source's InScope does.
	Location string `json:"location,omitempty"`

	Title      string   `json:"title"`
	URL        string   `json:"url"`
	ImageURL   string   `json:"image_url,omitempty"`
	Price      string   `json:"price,omitempty"` // as displayed, e.g. "290 €/mēn."
	PriceValue *float64 `json:"price_value,omitempty"`

	// Attrs are the filterable facts: numbers (float64) or strings, e.g.
	// {"rooms": 2, "series": "Jaun."}. A search filters on them by key.
	Attrs map[string]any `json:"attrs,omitempty"`
	// Fields are the facts to show in a notification, in order. Only the
	// source knows which ones matter, so notifiers render them blindly.
	Fields []Field `json:"fields,omitempty"`

	PostedAt time.Time `json:"posted_at"`
}

// Field is one labelled fact in a notification.
type Field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// Num returns a numeric attribute, as sources put numbers in Attrs and JSON
// round-trips them as float64.
func (l ListingDiscovered) Num(key string) (float64, bool) {
	v, ok := l.Attrs[key].(float64)
	return v, ok
}

// Str returns a string attribute.
func (l ListingDiscovered) Str(key string) (string, bool) {
	v, ok := l.Attrs[key].(string)
	return v, ok
}

// Channel is where a user wants to be notified.
type Channel struct {
	Kind   string `json:"kind"`   // "discord", later "telegram"
	Target string `json:"target"` // channel/chat ID on that platform
}

// NotificationRequested asks the notifier to deliver one message. Key is the
// idempotency key: delivering the same key twice must post once.
type NotificationRequested struct {
	Key        string             `json:"key"`
	UserID     int64              `json:"user_id"`
	SearchName string             `json:"search_name,omitempty"`
	Channel    Channel            `json:"channel"`
	Listing    *ListingDiscovered `json:"listing,omitempty"`
	Text       string             `json:"text,omitempty"` // used when Listing is nil
}
