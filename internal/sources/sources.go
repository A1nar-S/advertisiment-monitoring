// Package sources defines what a listing site must provide to plug into the
// platform. The services only know this interface: adding city24, vinted, ...
// means writing one implementation under internal/scraping/<site> and
// registering it in the scraper and matcher commands.
package sources

import (
	"context"
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
