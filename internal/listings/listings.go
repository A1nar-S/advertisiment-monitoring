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
	"fmt"
	"io/fs"
	"log/slog"

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
	pool *pgxpool.Pool
	q    *listingsdb.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: listingsdb.New(pool)}
}

// Migrate brings this slice's schema up to date.
func (s *Service) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	return db.Migrate(ctx, s.pool, "listings", sub)
}

// Ingest stores every discovered listing and returns those never seen before.
// Already known listings get their fields and last_seen_at refreshed.
func (s *Service) Ingest(ctx context.Context, found []events.ListingDiscovered) ([]events.ListingDiscovered, error) {
	var fresh []events.ListingDiscovered
	for _, l := range found {
		isNew, err := s.q.UpsertListing(ctx, upsertParams(l))
		if err != nil {
			return fresh, fmt.Errorf("upsert %s/%s: %w", l.Source, l.ExternalID, err)
		}
		if isNew {
			slog.Debug("new listing", "source", l.Source, "id", l.ExternalID, "title", l.Title)
			fresh = append(fresh, l)
		}
	}
	slog.Info("ingested listings", "found", len(found), "new", len(fresh))
	return fresh, nil
}

func upsertParams(l events.ListingDiscovered) listingsdb.UpsertListingParams {
	return listingsdb.UpsertListingParams{
		Source:     l.Source,
		ExternalID: l.ExternalID,
		Title:      l.Title,
		Url:        l.URL,
		Category:   l.Category,
		District:   l.District,
		Address:    l.Address,
		Rooms:      int32Ptr(l.Rooms),
		AreaM2:     int32Ptr(l.AreaM2),
		Series:     l.Series,
		Price:      l.Price,
		PriceValue: l.PriceValue,
		HasPhoto:   l.HasPhoto,
		ImageUrl:   l.ImageURL,
		PostedAt:   pgtype.Timestamptz{Time: l.PostedAt, Valid: !l.PostedAt.IsZero()},
	}
}

func int32Ptr(n *int) *int32 {
	if n == nil {
		return nil
	}
	v := int32(*n)
	return &v
}
