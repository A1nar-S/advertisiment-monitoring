package notifications

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
)

type fake struct {
	sent     []string
	fail     bool
	failText bool
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
	if f.failText {
		return errors.New("boom")
	}
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
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS digest_queue, deliveries, notifications_migrations`); err != nil {
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

func newTestService(t *testing.T, f *fake) (*Service, *pgxpool.Pool) {
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
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS digest_queue, deliveries, notifications_migrations`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(pool, 0, f).WithBurst(5, time.Hour)
	if err := svc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return svc, pool
}

func adRequest(search, id string) events.NotificationRequested {
	return events.NotificationRequested{
		Key: "1:" + search + ":" + id, UserID: 1, SearchName: search,
		Channel: events.Channel{Kind: "fake", Target: "chan"},
		Listing: &events.ListingDiscovered{ExternalID: id, Title: "Car " + id, Price: "1,000  €", URL: "https://x/" + id},
	}
}

func TestBurstGoesToDigest(t *testing.T) {
	f := &fake{}
	svc, pool := newTestService(t, f)
	ctx := context.Background()

	// 12 matches for one search: 5 posted, 7 queued. Redelivering one of the
	// queued keys must neither post nor queue it twice.
	for i := range 12 {
		if err := svc.Handle(ctx, adRequest("audi", fmt.Sprint(i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Handle(ctx, adRequest("audi", "11")); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 5 {
		t.Fatalf("sent %d messages, want 5: %v", len(f.sent), f.sent)
	}

	// Another search has its own budget.
	if err := svc.Handle(ctx, adRequest("bmw", "b1")); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 6 {
		t.Fatalf("other search: sent %d, want 6", len(f.sent))
	}

	// Not due yet: the window has not passed.
	if n, err := svc.FlushDigests(ctx); err != nil || n != 0 {
		t.Fatalf("early flush: n=%d err=%v", n, err)
	}

	// A failed digest stays queued.
	if _, err := pool.Exec(ctx, `UPDATE digest_queue SET queued_at = now() - interval '2 hours'`); err != nil {
		t.Fatal(err)
	}
	f.failText = true
	if _, err := svc.FlushDigests(ctx); err == nil {
		t.Fatal("expected digest send error")
	}
	f.failText = false

	n, err := svc.FlushDigests(ctx)
	if err != nil || n != 1 {
		t.Fatalf("flush: n=%d err=%v", n, err)
	}
	if len(f.sent) != 7 {
		t.Fatalf("after flush sent %d, want 7 (one digest)", len(f.sent))
	}
	digest := f.sent[6]
	if !strings.Contains(digest, "7 more matches") || !strings.Contains(digest, "<https://x/11>") {
		t.Errorf("digest = %q", digest)
	}
	if n, _ := svc.FlushDigests(ctx); n != 0 {
		t.Errorf("second flush sent %d digests, want 0", n)
	}
}

func TestDigestMessagesSplit(t *testing.T) {
	var ls []events.ListingDiscovered
	for i := range 45 {
		ls = append(ls, events.ListingDiscovered{
			Title: strings.Repeat("long title ", 20), Price: "12,345  €",
			URL: fmt.Sprintf("https://www.ss.com/msg/lv/transport/cars/audi/a4/ad%03d.html", i),
		})
	}
	msgs := digestMessages("audi", ls, 5, 10*time.Minute)
	if len(msgs) < 2 {
		t.Fatalf("want several messages, got %d", len(msgs))
	}
	lines := 0
	for _, m := range msgs {
		if len(m) > maxMessageLen {
			t.Errorf("message of %d bytes exceeds the limit", len(m))
		}
		lines += strings.Count(m, "• ")
	}
	if lines != maxDigestAds || !strings.Contains(msgs[len(msgs)-1], "…and 15 more") {
		t.Errorf("lines=%d, last=%q", lines, msgs[len(msgs)-1])
	}
}
