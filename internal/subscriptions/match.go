package subscriptions

import (
	"strings"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
)

// Range is an inclusive numeric bound; nil means unbounded on that side.
type Range struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// priceKey is the one Ranges key that reads the listing's price instead of an
// attribute, because every source has a price.
const priceKey = "price"

// Filters narrow what a search's feeds return. They are keyed by attribute
// name (see events.ListingDiscovered.Attrs), so they work for any source: a
// flat search filters "rooms", a car search filters "year" or "mileage_km".
type Filters struct {
	// Ranges bound numeric attributes, plus "price".
	Ranges map[string]Range `json:"ranges,omitempty"`
	// OneOf restricts string attributes to a set, ignoring case.
	OneOf map[string][]string `json:"one_of,omitempty"`
}

// Search is one user's saved search.
type Search struct {
	ID      int64
	UserID  int64
	Name    string
	Source  string
	Feeds   []string // what to watch, in the source's own terms
	Filters Filters
	Channel events.Channel
}

// Match reports whether a listing satisfies the search. An attribute the
// listing lacks fails only the filters that need it, so a car ad with no
// mileage still matches a search that does not ask about mileage.
func (s Search) Match(src sources.Source, l events.ListingDiscovered) bool {
	// Scope first: a planned feed can be broader than any one search, so only
	// the source can say whether this ad is what the search asked for.
	if !src.InScope(s.Feeds, l) {
		return false
	}
	for key, r := range s.Filters.Ranges {
		var v float64
		var ok bool
		if key == priceKey {
			if l.PriceValue != nil {
				v, ok = *l.PriceValue, true
			}
		} else {
			v, ok = l.Num(key)
		}
		if !ok || (r.Min != nil && v < *r.Min) || (r.Max != nil && v > *r.Max) {
			return false
		}
	}
	for key, allowed := range s.Filters.OneOf {
		v, ok := l.Str(key)
		if !ok || !containsFold(allowed, v) {
			return false
		}
	}
	return true
}

func containsFold(list []string, v string) bool {
	v = strings.TrimSpace(v)
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), v) {
			return true
		}
	}
	return false
}
