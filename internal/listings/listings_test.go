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
func newTestService(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS listings, listings_outbox, feed_state, listings_migrations`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(pool, DefaultSeedWindow)
	for i := 0; i < 2; i++ { // second run proves migrations are idempotent
		if err := svc.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return svc, pool
}

func listing(id, feed string) events.ListingDiscovered {
	price := 300.0
	return events.ListingDiscovered{
		Source: "ss.com", ExternalID: id, Title: "Flat", URL: "https://x/" + id, Feed: feed,
		Attrs: map[string]any{"rooms": 2.0}, PriceValue: &price, Price: "300 €/mēn.", PostedAt: time.Now(),
	}
}

func TestIngestUpsert(t *testing.T) {
	svc, pool := newTestService(t)
	ctx := context.Background()

	l := listing("1", "")
	if isNew, err := svc.Ingest(ctx, l); err != nil || !isNew {
		t.Fatalf("first ingest: new=%v err=%v", isNew, err)
	}
	l.Title = "Flat (renamed)"
	if isNew, err := svc.Ingest(ctx, l); err != nil || isNew {
		t.Fatalf("second ingest: new=%v err=%v", isNew, err)
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

func TestSeedingAndOutbox(t *testing.T) {
	svc, pool := newTestService(t)
	ctx := context.Background()

	// A feed's first listings are stored but not announced.
	if _, err := svc.Ingest(ctx, listing("1", "riga/centre/hand_over")); err != nil {
		t.Fatal(err)
	}
	var queued int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM listings_outbox`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Fatalf("seed window: outbox has %d rows, want 0", queued)
	}

	// Once the feed is old enough, new listings are queued exactly once.
	if _, err := pool.Exec(ctx, `UPDATE feed_state SET first_seen_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.Ingest(ctx, listing("2", "riga/centre/hand_over")); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM listings_outbox`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("outbox has %d rows, want 1", queued)
	}

	var got []string
	n, err := svc.RelayOutbox(ctx, 10, func(_ context.Context, l events.ListingDiscovered) error {
		got = append(got, l.ExternalID)
		return nil
	})
	if err != nil || n != 1 || len(got) != 1 || got[0] != "2" {
		t.Fatalf("relay: n=%d got=%v err=%v", n, got, err)
	}
	if n, _ := svc.RelayOutbox(ctx, 10, func(context.Context, events.ListingDiscovered) error { return nil }); n != 0 {
		t.Errorf("second relay published %d, want 0", n)
	}
}

func TestPurgeKeepsRecentlySeen(t *testing.T) {
	svc, pool := newTestService(t)
	ctx := context.Background()

	for _, id := range []string{"old", "kept"} {
		if _, err := svc.Ingest(ctx, listing(id, "")); err != nil {
			t.Fatal(err)
		}
	}
	// "old" was first seen long ago and not since; "kept" is old but still on a feed.
	if _, err := pool.Exec(ctx, `UPDATE listings SET first_seen_at = now() - interval '30 days',
		last_seen_at = now() - interval '10 days' WHERE external_id = 'old'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE listings SET first_seen_at = now() - interval '30 days' WHERE external_id = 'kept'`); err != nil {
		t.Fatal(err)
	}

	n, err := svc.Purge(ctx, 7*24*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("purge: n=%d err=%v", n, err)
	}
	var left string
	if err := pool.QueryRow(ctx, `SELECT external_id FROM listings`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != "kept" {
		t.Errorf("left = %q, want kept", left)
	}
}
