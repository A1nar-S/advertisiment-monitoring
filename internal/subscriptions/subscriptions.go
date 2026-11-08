// Package subscriptions is the slice that owns who wants what: users, their
// delivery channels and saved searches. It decides which scrape feeds are
// worth fetching and which new listings concern which user.
//
// Schema lives in migrations/ (goose), queries in queries/ and the typed code
// in subscriptionsdb/ is generated from both by sqlc: run `go generate ./...`.
package subscriptions

//go:generate go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/db"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions/subscriptionsdb"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Service struct {
	pool       *pgxpool.Pool
	q          *subscriptionsdb.Queries
	sources    sources.Registry
	collapseAt int
}

// NewService. collapseAt is how many sibling feeds a source may merge into
// one broader request (see sources.Source.PlanFeeds).
func NewService(pool *pgxpool.Pool, reg sources.Registry, collapseAt int) *Service {
	return &Service{pool: pool, q: subscriptionsdb.New(pool), sources: reg, collapseAt: collapseAt}
}

func (s *Service) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	return db.Migrate(ctx, s.pool, "subscriptions", sub)
}

// ActiveFeeds returns the feeds of source worth scraping right now. The
// scraper calls it every cycle, so edited searches apply without a restart.
func (s *Service) ActiveFeeds(ctx context.Context, source string) ([]string, error) {
	src, err := s.sources.Get(source)
	if err != nil {
		return nil, err
	}
	wanted, err := s.q.ListEnabledFeeds(ctx, source)
	if err != nil {
		return nil, err
	}
	return src.PlanFeeds(wanted, s.collapseAt), nil
}

// Notification is a request to tell one search's user about a listing.
type Notification struct {
	SearchID int64
	events.NotificationRequested
}

// Notifications returns a request for every search that matches l and was not
// told about it yet. It records nothing: the caller publishes first and then
// calls Record, so a crash in between re-publishes (the notifier dedups by
// Key) instead of silently losing the match.
func (s *Service) Notifications(ctx context.Context, l events.ListingDiscovered) ([]Notification, error) {
	src, err := s.sources.Get(l.Source)
	if err != nil {
		// Retrying cannot fix an unregistered source; drop it loudly.
		slog.Error("listing from unknown source", "source", l.Source, "id", l.ExternalID)
		return nil, nil
	}
	rows, err := s.q.ListEnabledSearches(ctx, l.Source)
	if err != nil {
		return nil, err
	}
	done, err := s.q.ListMatchedSearchIDs(ctx, subscriptionsdb.ListMatchedSearchIDsParams{Source: l.Source, ExternalID: l.ExternalID})
	if err != nil {
		return nil, err
	}

	var enricher sources.Enricher
	enrichable := map[string]bool{}
	if e, ok := src.(sources.Enricher); ok {
		enricher = e
		for _, k := range e.EnrichableKeys() {
			enrichable[k] = true
		}
	}

	// Pass 1 on what the feed page showed. Searches that pass every filter
	// they can check, but filter on something only the ad page has, wait for
	// pass 2.
	var matched, pending []Search
	for _, r := range rows {
		search, err := searchFromRow(r)
		if err != nil {
			slog.Error("skipping search with bad filters", "search", r.ID, "err", err)
			continue
		}
		if slices.Contains(done, search.ID) {
			continue
		}
		switch search.Evaluate(src, l, enrichable) {
		case Matched:
			matched = append(matched, search)
		case NeedsDetail:
			pending = append(pending, search)
		}
	}

	// Pass 2: one ad-page read for this listing, shared by every waiting
	// search of every user.
	if len(pending) > 0 {
		enriched, err := enricher.Enrich(ctx, l)
		switch {
		case errors.Is(err, sources.ErrGone):
			slog.Info("ad removed before its page was read", "id", l.ExternalID)
		case err != nil:
			// Better an unconfirmed alert than a missed deal: tell the
			// candidates, flagged, rather than failing (and so dropping) the
			// listing for everyone.
			slog.Warn("ad page unavailable; notifying unconfirmed", "id", l.ExternalID, "err", err)
			l.Fields = append(l.Fields, events.Field{Name: "⚠️ Unconfirmed", Value: "Could not read the ad page to check: " + strings.Join(pendingKeys(pending, l, enrichable), ", ")})
			matched = append(matched, pending...)
		default:
			l = enriched
			for _, search := range pending {
				if search.Evaluate(src, l, nil) == Matched {
					matched = append(matched, search)
				}
			}
		}
	}

	out := make([]Notification, 0, len(matched))
	for _, search := range matched {
		listing := l
		out = append(out, Notification{SearchID: search.ID, NotificationRequested: events.NotificationRequested{
			Key:        fmt.Sprintf("%d:%s:%s", search.ID, l.Source, l.ExternalID),
			UserID:     search.UserID,
			SearchName: search.Name,
			Channel:    search.Channel,
			Listing:    &listing,
		}})
	}
	return out, nil
}

// pendingKeys lists the filters that could not be checked, for the warning.
func pendingKeys(searches []Search, l events.ListingDiscovered, enrichable map[string]bool) []string {
	var keys []string
	for _, s := range searches {
		for _, m := range []map[string]bool{keysOf(s.Filters.Ranges), keysOf(s.Filters.OneOf), keysOf(s.Filters.AllOf)} {
			for k := range m {
				if _, has := l.Attrs[k]; enrichable[k] && !has {
					keys = append(keys, k)
				}
			}
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

func keysOf[V any](m map[string]V) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// Record marks the notifications as handed off.
func (s *Service) Record(ctx context.Context, ns []Notification) error {
	for _, n := range ns {
		err := s.q.RecordMatch(ctx, subscriptionsdb.RecordMatchParams{
			SearchID: n.SearchID, Source: n.Listing.Source, ExternalID: n.Listing.ExternalID,
		})
		if err != nil {
			return fmt.Errorf("record match: %w", err)
		}
	}
	return nil
}

// PurgeMatches forgets matches older than olderThan. Keep this longer than
// the listings retention, or a re-scraped old ad could be announced twice.
func (s *Service) PurgeMatches(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.q.PurgeMatches(ctx, olderThan.Seconds())
}

func searchFromRow(r subscriptionsdb.ListEnabledSearchesRow) (Search, error) {
	s := Search{
		ID: r.ID, UserID: r.UserID, Name: r.Name, Source: r.Source, Feeds: r.Feeds,
		Channel: events.Channel{Kind: r.ChannelKind, Target: r.ChannelTarget},
	}
	if err := json.Unmarshal(r.Filters, &s.Filters); err != nil {
		return s, err
	}
	return s, nil
}
