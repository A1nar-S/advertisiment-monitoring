// Package events holds the contracts that cross slice boundaries. It is the
// only package every slice may import. Keep it free of logic: these structs
// become the Kafka message payloads when the slices are split into services.
package events

import "time"

// ListingDiscovered is emitted by a scraping source for every advertisement
// found on a search page, whether or not it has been seen before.
type ListingDiscovered struct {
	Source     string // e.g. "ss.com"
	ExternalID string // ID on the source site, e.g. "57139540"
	Title      string
	URL        string
	Category   string // e.g. "Rīga : Iļģuciems"
	District   string // e.g. "Rīga"
	Address    string // e.g. "Reņģes 7"
	Rooms      *int
	AreaM2     *int
	Series     string // e.g. "Hrušč.", "LT proj."
	Price      string // raw, e.g. "290 €/mēn."
	PriceValue *float64
	HasPhoto   bool
	ImageURL   string
	PostedAt   time.Time
}
