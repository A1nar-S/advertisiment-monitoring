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
	// AllOf requires a list attribute (car equipment) to contain every value.
	AllOf map[string][]string `json:"all_of,omitempty"`
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

// Verdict is the outcome of checking a listing against a search.
type Verdict int

const (
	NoMatch Verdict = iota
	Matched
	// NeedsDetail: every filter that could be checked passed, but some need
	// attrs only the ad page has (see sources.Enricher).
	NeedsDetail
)

// Match reports whether a listing satisfies the search. An attribute the
// listing lacks fails only the filters that need it, so a car ad with no
// mileage still matches a search that does not ask about mileage.
func (s Search) Match(src sources.Source, l events.ListingDiscovered) bool {
	return s.Evaluate(src, l, nil) == Matched
}

// Evaluate is Match with a third outcome: a filter on a missing attr in
// enrichable does not fail the listing yet but asks for its ad page.
func (s Search) Evaluate(src sources.Source, l events.ListingDiscovered, enrichable map[string]bool) Verdict {
	// Scope first: a planned feed can be broader than any one search, so only
	// the source can say whether this ad is what the search asked for.
	if !src.InScope(s.Feeds, l) {
		return NoMatch
	}
	need := false
	// undecided: a missing attr the ad page may still provide.
	undecided := func(key string) bool {
		need = need || enrichable[key]
		return enrichable[key]
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
		if !ok {
			if !undecided(key) {
				return NoMatch
			}
			continue
		}
		if (r.Min != nil && v < *r.Min) || (r.Max != nil && v > *r.Max) {
			return NoMatch
		}
	}
	for key, allowed := range s.Filters.OneOf {
		v, ok := l.Str(key)
		if !ok {
			if !undecided(key) {
				return NoMatch
			}
			continue
		}
		if !containsFold(allowed, v) {
			return NoMatch
		}
	}
	for key, required := range s.Filters.AllOf {
		have, ok := strList(l.Attrs[key])
		if !ok {
			if !undecided(key) {
				return NoMatch
			}
			continue
		}
		for _, r := range required {
			if !containsFold(have, r) {
				return NoMatch
			}
		}
	}
	if need {
		return NeedsDetail
	}
	return Matched
}

// strList reads a list attr; it is []string in-process and []any after a
// JSON round trip through Kafka.
func strList(v any) ([]string, bool) {
	switch t := v.(type) {
	case []string:
		return t, true
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out, true
	}
	return nil, false
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
