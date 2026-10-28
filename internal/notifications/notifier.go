// Package notifications is the slice that delivers messages to users. Each
// channel (Discord, Telegram, ...) is a subpackage implementing Notifier.
//
// Schema lives in migrations/ (goose), queries in queries/ and the typed code
// in notificationsdb/ is generated from both by sqlc: run `go generate ./...`.
package notifications

//go:generate go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/notifications/notificationsdb"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/db"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Notifier delivers messages to one platform. The target (channel or chat ID)
// comes with each call because one bot serves every user's channel.
type Notifier interface {
	// Kind matches events.Channel.Kind, e.g. "discord".
	Kind() string
	NotifyListing(ctx context.Context, target string, l events.ListingDiscovered) error
	NotifyText(ctx context.Context, target, msg string) error
}

// Service routes notification requests to the right Notifier exactly once per key.
type Service struct {
	pool      *pgxpool.Pool
	q         *notificationsdb.Queries
	notifiers map[string]Notifier
	delay     time.Duration
}

// NewService. delay is the pause after each delivery, to stay under the
// platforms' rate limits; the consumer handles one request at a time.
func NewService(pool *pgxpool.Pool, delay time.Duration, notifiers ...Notifier) *Service {
	m := make(map[string]Notifier, len(notifiers))
	for _, n := range notifiers {
		m[n.Kind()] = n
	}
	return &Service{pool: pool, q: notificationsdb.New(pool), notifiers: m, delay: delay}
}

func (s *Service) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	return db.Migrate(ctx, s.pool, "notifications", sub)
}

// Handle delivers one request. A request with a key is claimed in the database
// before sending: a redelivered Kafka record then posts nothing instead of a
// duplicate. The price is that a crash between claim and send drops the
// message, which beats spamming a user. A failed send releases the claim so
// the retry can go through.
func (s *Service) Handle(ctx context.Context, req events.NotificationRequested) error {
	n, ok := s.notifiers[req.Channel.Kind]
	if !ok {
		// Retrying cannot fix a missing notifier; drop it loudly.
		slog.Error("no notifier for channel kind", "kind", req.Channel.Kind, "key", req.Key)
		return nil
	}

	if req.Key != "" {
		claimed, err := s.q.ClaimDelivery(ctx, req.Key)
		if err != nil {
			return fmt.Errorf("claim %s: %w", req.Key, err)
		}
		if claimed == 0 {
			slog.Debug("already delivered", "key", req.Key)
			return nil
		}
	}

	var err error
	if req.Listing != nil {
		err = n.NotifyListing(ctx, req.Channel.Target, *req.Listing)
	} else {
		err = n.NotifyText(ctx, req.Channel.Target, req.Text)
	}
	if err != nil {
		if req.Key != "" {
			// Fresh context: ctx may be the reason the send failed.
			rel, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if rerr := s.q.ReleaseDelivery(rel, req.Key); rerr != nil {
				slog.Error("release delivery", "key", req.Key, "err", rerr)
			}
		}
		return fmt.Errorf("%s: %w", n.Kind(), err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(s.delay):
	}
	return nil
}

// PurgeDeliveries forgets deliveries older than olderThan.
func (s *Service) PurgeDeliveries(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.q.PurgeDeliveries(ctx, olderThan.Seconds())
}
