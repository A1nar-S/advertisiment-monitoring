package app

import (
	"time"

	"github.com/A1nar-S/advertisiment-monitoring/internal/scraping/ss"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
)

// KnownSources is the one place a new site is registered. A source with no
// searches watching it costs nothing: the scraper finds no feeds and idles.
func KnownSources(requestTimeout time.Duration) sources.Registry {
	return sources.NewRegistry(
		ss.NewClient(requestTimeout),
		// city24.NewClient(...), vinted.NewClient(...)
	)
}
