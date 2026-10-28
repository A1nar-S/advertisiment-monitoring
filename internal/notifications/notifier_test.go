package notifications

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
)

type fake struct {
	sent []string
	fail bool
}

func (f *fake) Kind() string { return "fake" }
func (f *fake) NotifyListing(_ context.Context, target string, l events.ListingDiscovered) error {
	if f.fail {
		return errors.New("boom")
	}
	f.sent = append(f.sent, target+":"+l.ExternalID)
	return nil
}
func (f *fake) NotifyText(_ context.Context, target, msg string) error {
	f.sent = append(f.sent, target+":"+msg)
	return nil
}

// Needs a real Postgres: TEST_DATABASE_URL=postgres://... go test ./internal/notifications
func TestHandleIsIdempotent(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS deliveries, notifications_migrations`); err != nil {
		t.Fatal(err)
	}

	f := &fake{}
	svc := NewService(pool, 0, f)
	if err := svc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	req := events.NotificationRequested{
		Key: "1:ss.com:42", Channel: events.Channel{Kind: "fake", Target: "chan"},
		Listing: &events.ListingDiscovered{ExternalID: "42"},
	}

	// A failed send must not burn the key, so the retry delivers.
	f.fail = true
	if err := svc.Handle(ctx, req); err == nil {
		t.Fatal("expected send error")
	}
	f.fail = false
	for i := 0; i < 2; i++ {
		if err := svc.Handle(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.sent) != 1 || f.sent[0] != "chan:42" {
		t.Errorf("sent = %v, want exactly one delivery", f.sent)
	}

	// Keyless text (operational alerts) is never deduplicated.
	text := events.NotificationRequested{Channel: req.Channel, Text: "hi"}
	for i := 0; i < 2; i++ {
		if err := svc.Handle(ctx, text); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.sent) != 3 {
		t.Errorf("sent = %v, want 3 after two keyless texts", f.sent)
	}
}
