package listings

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
)

// Needs a real Postgres: TEST_DATABASE_URL=postgres://... go test ./internal/listings
func TestIngest(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS listings, listings_migrations`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(pool)
	for i := 0; i < 2; i++ { // second run proves migrations are idempotent
		if err := svc.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}

	rooms, price := 2, 300.0
	l := events.ListingDiscovered{
		Source: "ss.com", ExternalID: "1", Title: "Flat", URL: "https://x/1",
		Rooms: &rooms, PriceValue: &price, Price: "300 €/mēn.", PostedAt: time.Now(),
	}

	fresh, err := svc.Ingest(ctx, []events.ListingDiscovered{l})
	if err != nil || len(fresh) != 1 {
		t.Fatalf("first ingest: fresh=%d err=%v", len(fresh), err)
	}

	l.Title = "Flat (renamed)"
	fresh, err = svc.Ingest(ctx, []events.ListingDiscovered{l})
	if err != nil || len(fresh) != 0 {
		t.Fatalf("second ingest: fresh=%d err=%v", len(fresh), err)
	}

	var title string
	var seenLater bool
	if err := pool.QueryRow(ctx,
		`SELECT title, last_seen_at > first_seen_at FROM listings WHERE external_id = '1'`).Scan(&title, &seenLater); err != nil {
		t.Fatal(err)
	}
	if title != "Flat (renamed)" || !seenLater {
		t.Errorf("title=%q seenLater=%v", title, seenLater)
	}
}
