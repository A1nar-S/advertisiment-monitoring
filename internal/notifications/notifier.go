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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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

// ErrUndeliverable is returned (wrapped) by a Notifier when retrying cannot
// help: the user closed their DMs, the channel is gone. Such a message is
// dropped, not retried.
var ErrUndeliverable = errors.New("undeliverable")

// Service routes notification requests to the right Notifier exactly once per key.
type Service struct {
	pool      *pgxpool.Pool
	q         *notificationsdb.Queries
	notifiers map[string]Notifier
	delay     time.Duration

	// Flood guard (see WithBurst); burst 0 means unlimited.
	burst       int
	burstWindow time.Duration
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

// WithBurst limits each (user, search) to burst ad messages per window. The
// ads beyond that wait in the digest queue and go out as one digest per
// search once the window has passed (see RunDigests). A burst of matches, a
// catch-up after downtime or an overly broad search never floods a channel.
func (s *Service) WithBurst(burst int, window time.Duration) *Service {
	s.burst, s.burstWindow = burst, window
	return s
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
		claimed, err := s.q.ClaimDelivery(ctx, notificationsdb.ClaimDeliveryParams{
			Key: req.Key, UserID: nonZero(req.UserID), SearchName: nonEmpty(req.SearchName),
		})
		if err != nil {
			return fmt.Errorf("claim %s: %w", req.Key, err)
		}
		if claimed == 0 {
			slog.Debug("already delivered", "key", req.Key)
			return nil
		}
	}

	if s.limited(req) {
		queued, err := s.queueIfOverBurst(ctx, req)
		if err != nil {
			s.release(ctx, req.Key)
			return err
		}
		if queued {
			slog.Info("burst limit reached, queued for digest", "search", req.SearchName, "key", req.Key)
			return nil
		}
	}

	var err error
	if req.Listing != nil {
		err = n.NotifyListing(ctx, req.Channel.Target, *req.Listing)
	} else {
		err = n.NotifyText(ctx, req.Channel.Target, req.Text)
	}
	if errors.Is(err, ErrUndeliverable) {
		slog.Warn("undeliverable, dropped", "target", req.Channel.Target, "search", req.SearchName, "err", err)
		return nil // keep the claim: a retry would fail the same way
	}
	if err != nil {
		s.release(ctx, req.Key)
		return fmt.Errorf("%s: %w", n.Kind(), err)
	}
	s.pause(ctx)
	return nil
}

// limited: only a search's ad messages count against the burst; operational
// texts always go through.
func (s *Service) limited(req events.NotificationRequested) bool {
	return s.burst > 0 && req.Key != "" && req.Listing != nil && req.SearchName != ""
}

// queueIfOverBurst moves the request to the digest queue when its search
// already sent burst messages within the window.
func (s *Service) queueIfOverBurst(ctx context.Context, req events.NotificationRequested) (bool, error) {
	sent, err := s.q.CountRecentSent(ctx, notificationsdb.CountRecentSentParams{
		UserID: nonZero(req.UserID), SearchName: nonEmpty(req.SearchName), Key: req.Key,
		WindowSeconds: s.burstWindow.Seconds(),
	})
	if err != nil {
		return false, fmt.Errorf("count recent: %w", err)
	}
	if sent < int64(s.burst) {
		return false, nil
	}
	listing, err := json.Marshal(req.Listing)
	if err != nil {
		return false, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.MarkDigested(ctx, req.Key); err != nil {
			return err
		}
		return q.QueueDigest(ctx, notificationsdb.QueueDigestParams{
			Key: req.Key, UserID: req.UserID, SearchName: req.SearchName,
			ChannelKind: req.Channel.Kind, ChannelTarget: req.Channel.Target, Listing: listing,
		})
	})
	if err != nil {
		return false, fmt.Errorf("queue digest: %w", err)
	}
	return true, nil
}

// RunDigests sends due digests every interval until ctx is done.
func (s *Service) RunDigests(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.FlushDigests(ctx); err != nil && ctx.Err() == nil {
				slog.Error("flush digests", "err", err)
			}
		}
	}
}

// FlushDigests sends one digest for every search whose oldest queued ad has
// waited a full burst window. A digest that fails to send stays queued.
func (s *Service) FlushDigests(ctx context.Context) (int, error) {
	if s.burst == 0 {
		return 0, nil
	}
	due, err := s.q.DueDigests(ctx, s.burstWindow.Seconds())
	if err != nil {
		return 0, err
	}
	var sent int
	var errs []error
	for _, d := range due {
		n, ok := s.notifiers[d.ChannelKind]
		if !ok {
			continue
		}
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			rows, err := s.q.WithTx(tx).TakeDigest(ctx, notificationsdb.TakeDigestParams(d))
			if err != nil {
				return err
			}
			var ls []events.ListingDiscovered
			for _, r := range rows {
				var l events.ListingDiscovered
				if err := json.Unmarshal(r.Listing, &l); err != nil {
					slog.Error("bad digest payload, dropping", "err", err)
					continue
				}
				ls = append(ls, l)
			}
			for _, msg := range digestMessages(d.SearchName, ls, s.burst, s.burstWindow) {
				if err := n.NotifyText(ctx, d.ChannelTarget, msg); errors.Is(err, ErrUndeliverable) {
					slog.Warn("digest undeliverable, dropped", "target", d.ChannelTarget, "err", err)
					return nil // commits the delete
				} else if err != nil {
					return fmt.Errorf("%s: %w", n.Kind(), err) // rolls back: stays queued
				}
				s.pause(ctx)
			}
			return nil
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("digest %q: %w", d.SearchName, err))
			continue
		}
		sent++
	}
	return sent, errors.Join(errs...)
}

const (
	maxMessageLen = 2000 // Discord rejects longer message content
	maxDigestAds  = 30
	maxTitleRunes = 60
)

// digestMessages renders queued ads as a few compact text messages: one line
// per ad, at most maxDigestAds lines, each message under maxMessageLen.
func digestMessages(search string, ls []events.ListingDiscovered, burst int, window time.Duration) []string {
	if len(ls) == 0 {
		return nil
	}
	head := fmt.Sprintf("📦 **%d more %s for “%s”** (over %d in %s, so bundled)",
		len(ls), plural(len(ls), "match", "matches"), search, burst, window)
	var lines []string
	for i, l := range ls {
		if i == maxDigestAds {
			lines = append(lines, fmt.Sprintf("…and %d more", len(ls)-maxDigestAds))
			break
		}
		lines = append(lines, digestLine(l))
	}

	var msgs []string
	cur := head
	for _, line := range lines {
		if len(cur)+1+len(line) > maxMessageLen {
			msgs = append(msgs, cur)
			cur = line
			continue
		}
		cur += "\n" + line
	}
	return append(msgs, cur)
}

// digestLine is one ad in a digest. The source's first display field (a
// car's make and model, a flat's address) says more in a line than the ad's
// free-text title.
func digestLine(l events.ListingDiscovered) string {
	summary := l.Title
	if len(l.Fields) > 0 && l.Fields[0].Value != "" {
		summary = l.Fields[0].Value
	}
	title := []rune(strings.Join(strings.Fields(summary), " "))
	if len(title) > maxTitleRunes {
		title = append(title[:maxTitleRunes-1], '…')
	}
	var parts []string
	if l.Price != "" {
		parts = append(parts, "**"+strings.Join(strings.Fields(l.Price), " ")+"**")
	}
	if len(title) > 0 {
		parts = append(parts, string(title))
	}
	if l.URL != "" {
		parts = append(parts, "<"+l.URL+">") // <> stops Discord expanding a preview per line
	}
	return "• " + strings.Join(parts, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// release forgets a claimed key so a retry can deliver it.
func (s *Service) release(ctx context.Context, key string) {
	if key == "" {
		return
	}
	// Fresh context: ctx may be the reason the send failed.
	rel, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.q.ReleaseDelivery(rel, key); err != nil {
		slog.Error("release delivery", "key", key, "err", err)
	}
}

func (s *Service) pause(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(s.delay):
	}
}

// PurgeDeliveries forgets deliveries older than olderThan.
func (s *Service) PurgeDeliveries(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.q.PurgeDeliveries(ctx, olderThan.Seconds())
}

func nonZero(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func nonEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
