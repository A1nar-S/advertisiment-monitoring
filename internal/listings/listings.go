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
	"fmt"
	"io/fs"
	"log/slog"
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
