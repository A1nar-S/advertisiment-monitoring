// Package listings is the slice that owns stored advertisements: persistence,
// new-listing detection and lifecycle. Other slices reach it only through
// Service and the events package.
//
// Schema lives in migrations/ (goose), queries in queries/ and the typed code
// in listingsdb/ is generated from both by sqlc: run `go generate ./...`.
package listings

//go:generate go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/listings/listingsdb"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/db"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Service ingests discovered listings.
type Service struct {
	pool       *pgxpool.Pool
	q          *listingsdb.Queries
	seedWindow time.Duration
}

func NewService(pool *pgxpool.Pool, seedWindow time.Duration) *Service {
	return &Service{pool: pool, q: listingsdb.New(pool), seedWindow: seedWindow}
}

// Migrate brings this slice's schema up to date.
func (s *Service) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	return db.Migrate(ctx, s.pool, "listings", sub)
}

// DefaultSeedWindow is how long after a feed is first seen its listings are
// stored without being announced. It must outlast the scraper's first fetch of
// that feed (up to a few pages plus Kafka lag). The cost of too long a window
// is that a genuinely new ad inside it goes unannounced.
const DefaultSeedWindow = 2 * time.Minute

// Ingest stores one discovered listing. If it was never seen before and its
// feed is past the seed window, it also queues the listing in the outbox, in
// the same transaction, for the relay to publish. It reports whether the
// listing was new.
func (s *Service) Ingest(ctx context.Context, l events.ListingDiscovered) (bool, error) {
	var isNew bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		seeding := false
		if l.Feed != "" {
			age, err := q.TouchFeed(ctx, listingsdb.TouchFeedParams{Source: l.Source, Feed: l.Feed})
			if err != nil {
				return fmt.Errorf("touch feed: %w", err)
			}
			seeding = time.Duration(age*float64(time.Second)) < s.seedWindow
		}

		params, err := upsertParams(l)
		if err != nil {
			return err
		}
		if isNew, err = q.UpsertListing(ctx, params); err != nil {
			return fmt.Errorf("upsert %s/%s: %w", l.Source, l.ExternalID, err)
		}
		if !isNew || seeding {
			return nil
		}
		payload, err := json.Marshal(l)
		if err != nil {
			return err
		}
		slog.Debug("new listing", "source", l.Source, "id", l.ExternalID, "title", l.Title)
		return q.InsertOutbox(ctx, payload)
	})
	return isNew, err
}

// RelayOutbox publishes up to batch unpublished outbox entries through
// publish and marks them done, all in one transaction: if publish fails for
// an entry, the earlier ones in the batch are still marked and the rest are
// retried on the next call. Returns how many were published.
func (s *Service) RelayOutbox(ctx context.Context, batch int32, publish func(context.Context, events.ListingDiscovered) error) (int, error) {
	var published int
	var pubErr error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		rows, err := q.ClaimOutbox(ctx, batch)
		if err != nil {
			return err
		}
		var ids []int64
		for _, r := range rows {
			var l events.ListingDiscovered
			if err := json.Unmarshal(r.Payload, &l); err != nil {
				slog.Error("bad outbox payload, dropping", "id", r.ID, "err", err)
			} else if pubErr = publish(ctx, l); pubErr != nil {
				break
			}
			ids = append(ids, r.ID)
		}
		published = len(ids)
		if len(ids) > 0 {
			if err := q.MarkOutboxPublished(ctx, ids); err != nil {
				published = 0
				return err
			}
		}
		return nil // commit what was published; pubErr is reported below
	})
	if err == nil {
		err = pubErr
	}
	return published, err
}

// KnownIDs reports which of ids are already stored for source.
func (s *Service) KnownIDs(ctx context.Context, source string, ids []string) (map[string]bool, error) {
	rows, err := s.q.KnownIDs(ctx, listingsdb.KnownIDsParams{Source: source, Ids: ids})
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(rows))
	for _, id := range rows {
		known[id] = true
	}
	return known, nil
}

// FeedSeen reports whether feed was ever ingested.
func (s *Service) FeedSeen(ctx context.Context, source, feed string) (bool, error) {
	return s.q.FeedSeen(ctx, listingsdb.FeedSeenParams{Source: source, Feed: feed})
}

// Summary is a stored listing as shown in the dashboard.
type Summary struct {
	ExternalID string         `json:"externalId"`
	Title      string         `json:"title"`
	URL        string         `json:"url"`
	Location   string         `json:"location"`
	Price      string         `json:"price"`
	ImageURL   string         `json:"imageUrl"`
	Attrs      map[string]any `json:"attrs"`
	FirstSeen  time.Time      `json:"firstSeen"`
	Active     bool           `json:"active"`
}

// Summaries returns the stored listings among ids, keyed by external ID.
// Purged ones are simply missing.
func (s *Service) Summaries(ctx context.Context, source string, ids []string) (map[string]Summary, error) {
	rows, err := s.q.GetListings(ctx, listingsdb.GetListingsParams{Source: source, Ids: ids})
	if err != nil {
		return nil, err
	}
	out := make(map[string]Summary, len(rows))
	for _, r := range rows {
		var attrs map[string]any
		_ = json.Unmarshal(r.Attrs, &attrs)
		out[r.ExternalID] = Summary{
			ExternalID: r.ExternalID, Title: r.Title, URL: r.Url, Location: r.Location, Price: r.Price,
			ImageURL: r.ImageUrl, Attrs: attrs, FirstSeen: r.FirstSeenAt.Time, Active: r.IsActive,
		}
	}
	return out, nil
}

// ErrNotFound: no stored listing with that ID (or it was purged).
var ErrNotFound = errors.New("listing not found")

// Stored is a stored listing as the matcher sees one (ad-page details merged
// into Attrs), plus what only the store knows.
type Stored struct {
	ID         int64
	Listing    events.ListingDiscovered
	FirstSeen  time.Time
	Active     bool
	DetailedAt *time.Time // when the ad page was last read; nil = never
}

// Recent returns up to limit of a source's newest stored listings.
func (s *Service) Recent(ctx context.Context, source string, limit int32) ([]Stored, error) {
	rows, err := s.q.RecentListings(ctx, listingsdb.RecentListingsParams{Source: source, MaxRows: limit})
	if err != nil {
		return nil, err
	}
	out := make([]Stored, 0, len(rows))
	for _, r := range rows {
		out = append(out, Stored{
			ID:        r.ID,
			Listing:   fromRow(source, r.ExternalID, r.Feed, r.Title, r.Url, r.Location, r.Price, r.PriceValue, r.ImageUrl, r.Attrs, r.PostedAt),
			FirstSeen: r.FirstSeenAt.Time,
			Active:    r.IsActive,
		})
	}
	return out, nil
}

// Get returns one stored listing by its row ID.
func (s *Service) Get(ctx context.Context, id int64) (Stored, error) {
	r, err := s.q.GetListingByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stored{}, ErrNotFound
	}
	if err != nil {
		return Stored{}, err
	}
	st := Stored{
		ID:        r.ID,
		Listing:   fromRow(r.Source, r.ExternalID, r.Feed, r.Title, r.Url, r.Location, r.Price, r.PriceValue, r.ImageUrl, r.Attrs, r.PostedAt),
		FirstSeen: r.FirstSeenAt.Time,
		Active:    r.IsActive,
	}
	if r.DetailedAt.Valid {
		st.DetailedAt = &r.DetailedAt.Time
	}
	return st, nil
}

// SaveDetails keeps what an ad page added to a listing (enriched, compared
// with the feed's version before), so the dashboard can show it and
// previews can filter on it. The feed's attrs are left alone: every scrape
// rewrites them.
func (s *Service) SaveDetails(ctx context.Context, before, enriched events.ListingDiscovered) error {
	details := map[string]any{}
	for k, v := range enriched.Attrs {
		if old, ok := before.Attrs[k]; !ok || !reflect.DeepEqual(old, v) {
			details[k] = v
		}
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return s.q.SaveListingDetails(ctx, listingsdb.SaveListingDetailsParams{
		Details: raw, Source: enriched.Source, ExternalID: enriched.ExternalID,
	})
}

// MarkGone records that a listing's ad page is gone.
func (s *Service) MarkGone(ctx context.Context, source, externalID string) error {
	return s.q.MarkListingGone(ctx, listingsdb.MarkListingGoneParams{Source: source, ExternalID: externalID})
}

func fromRow(source, externalID, feed, title, url, location, price string, priceValue *float64,
	imageURL string, attrs []byte, postedAt pgtype.Timestamptz) events.ListingDiscovered {
	l := events.ListingDiscovered{
		Source: source, ExternalID: externalID, Feed: feed, Location: location,
		Title: title, URL: url, ImageURL: imageURL, Price: price, PriceValue: priceValue,
		PostedAt: postedAt.Time,
	}
	// Numbers come back as float64, as after the Kafka round trip the
	// matcher normally sees.
	_ = json.Unmarshal(attrs, &l.Attrs)
	return l
}

// Purge deletes listings not seen on any feed for olderThan, and published
// outbox entries older than a day.
func (s *Service) Purge(ctx context.Context, olderThan time.Duration) (int64, error) {
	n, err := s.q.PurgeStaleListings(ctx, olderThan.Seconds())
	if err != nil {
		return n, err
	}
	if _, err := s.q.PurgePublishedOutbox(ctx, (24 * time.Hour).Seconds()); err != nil {
		return n, err
	}
	return n, nil
}

func upsertParams(l events.ListingDiscovered) (listingsdb.UpsertListingParams, error) {
	attrs, err := json.Marshal(l.Attrs)
	if err != nil {
		return listingsdb.UpsertListingParams{}, err
	}
	if l.Attrs == nil {
		attrs = []byte("{}") // the column is NOT NULL; "null" would be valid JSON but not an object
	}
	return listingsdb.UpsertListingParams{
		Source:     l.Source,
		ExternalID: l.ExternalID,
		Feed:       l.Feed,
		Title:      l.Title,
		Url:        l.URL,
		Location:   l.Location,
		Attrs:      attrs,
		Price:      l.Price,
		PriceValue: l.PriceValue,
		ImageUrl:   l.ImageURL,
		PostedAt:   pgtype.Timestamptz{Time: l.PostedAt, Valid: !l.PostedAt.IsZero()},
	}, nil
}
