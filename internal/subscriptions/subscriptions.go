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
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
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

	var out []Notification
	for _, r := range rows {
		search, err := searchFromRow(r)
		if err != nil {
			slog.Error("skipping search with bad filters", "search", r.ID, "err", err)
			continue
		}
		if slices.Contains(done, search.ID) || !search.Match(src, l) {
			continue
		}
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
