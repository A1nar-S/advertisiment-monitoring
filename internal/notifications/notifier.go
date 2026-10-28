// Package notifications is the slice that delivers messages to users. Each
// channel (Discord, Telegram, ...) is a subpackage implementing Notifier.
package notifications

import (
	"context"
	"log/slog"
	"time"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
)

// Notifier delivers messages to one channel.
type Notifier interface {
	Name() string
	NotifyListing(ctx context.Context, l events.ListingDiscovered) error
	NotifyText(ctx context.Context, msg string) error
}

// Dispatcher fans listings out to notifiers, pausing between messages to
// stay under channel rate limits.
type Dispatcher struct {
	Notifiers []Notifier
	Delay     time.Duration
}

// NotifyListings sends each listing to every notifier. A failure on one
// message is logged and does not stop the rest.
func (d *Dispatcher) NotifyListings(ctx context.Context, ls []events.ListingDiscovered) {
	for _, l := range ls {
		for _, n := range d.Notifiers {
			if err := n.NotifyListing(ctx, l); err != nil {
				slog.Error("notify failed", "notifier", n.Name(), "listing", l.ExternalID, "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(d.Delay):
		}
	}
}

// NotifyText sends a plain message to every notifier.
func (d *Dispatcher) NotifyText(ctx context.Context, msg string) {
	for _, n := range d.Notifiers {
		if err := n.NotifyText(ctx, msg); err != nil {
			slog.Error("notify failed", "notifier", n.Name(), "err", err)
		}
	}
}
