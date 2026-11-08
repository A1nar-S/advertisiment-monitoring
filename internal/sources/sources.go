// Package sources defines what a listing site must provide to plug into the
// platform. The services only know this interface: adding city24, vinted, ...
// means writing one implementation under internal/scraping/<site> and
// registering it in the scraper and matcher commands.
package sources

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
)

// Source is one listing site.
type Source interface {
	// Name is the value of events.ListingDiscovered.Source.
	Name() string

	// FetchFeed reads one page of a feed, newest first. A feed is an opaque,
	// source-defined string (an SS.com category path, a vinted search, ...).
	FetchFeed(ctx context.Context, feed string, page int) ([]events.ListingDiscovered, error)

	// PlanFeeds turns the feeds searches asked for into the feeds to fetch,
	// merging where one broader request is cheaper than several narrow ones.
	// Only the site knows what is cheaper, hence it lives here.
	PlanFeeds(wanted []string, collapseAt int) []string

	// InScope reports whether a listing belongs to what a search asked for.
	// It is needed because a planned feed can be broader than any one search.
	InScope(wanted []string, l events.ListingDiscovered) bool
}

// SearchNormalizer is optionally implemented by a source whose searches use
// its own vocabulary (ss.com: the search form's field and option names). It
// validates a search as written in the users file and returns its feeds and
// filters in canonical form; some filters (a flat's region) may become feeds.
// Unknown keys or values are errors, with suggestions, so a typo fails at
// startup instead of silently matching nothing.
type SearchNormalizer interface {
	NormalizeSearch(feeds []string, filters map[string]any) ([]string, map[string]any, error)
}

// Enricher is optionally implemented by a source whose feed pages lack some
// facts that its ad pages have (ss.com cars: gearbox, body, color, ...).
// The matcher calls Enrich only for a listing that passes every filter it can
// check without them, and at most once per listing, whoever is watching.
type Enricher interface {
	// EnrichableKeys are the attrs Enrich can add.
	EnrichableKeys() []string
	// Enrich reads the ad page and returns the listing with those attrs
	// added. ErrGone means the ad no longer exists.
	Enrich(ctx context.Context, l events.ListingDiscovered) (events.ListingDiscovered, error)
}

// ErrGone is returned by Enrich for an ad that was removed meanwhile.
var ErrGone = errors.New("listing no longer exists")

// Registry holds the sources a process knows about.
type Registry map[string]Source

func NewRegistry(srcs ...Source) Registry {
	r := make(Registry, len(srcs))
	for _, s := range srcs {
		r[s.Name()] = s
	}
	return r
}

func (r Registry) Get(name string) (Source, error) {
	if s, ok := r[name]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("unknown source %q", name)
}

// Names lists the registered sources, sorted.
func (r Registry) Names() []string {
	names := make([]string, 0, len(r))
	for n := range r {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
