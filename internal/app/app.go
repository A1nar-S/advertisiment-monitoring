// Package app holds the wiring of each service: what cmd/<name> runs. It lives
// here, not in main, so integration tests can start the real services
// in-process against real Postgres and Kafka with fake sites and notifiers.
package app

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"log/slog"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/listings"
	"github.com/A1nar-S/advertisiment-monitoring/internal/notifications"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/config"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/db"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/kafka"
	"github.com/A1nar-S/advertisiment-monitoring/internal/scraping"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions"
)

const relayBatch = 100

// Scraper fetches the feeds users watch, for every registered source, and
// publishes each ad it sees. It does no deduplication; ingest does.
func Scraper(ctx context.Context, cfg config.Scraper, reg sources.Registry) error {
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	subs := subscriptions.NewService(pool, reg, cfg.FeedCollapseAt)
	if err := subs.Migrate(ctx); err != nil {
		return err
	}

	producer, err := kafka.NewProducer(cfg.KafkaBrokers)
	if err != nil {
		return err
	}
	defer producer.Close()

	alert := func(ctx context.Context, text string) {
		if cfg.AdminChannel.Target == "" {
			return
		}
		err := producer.Publish(ctx, cfg.Topic(events.TopicNotificationsRequested), "admin", events.NotificationRequested{
			Channel: events.Channel{Kind: cfg.AdminChannel.Kind, Target: cfg.AdminChannel.Target},
			Text:    text,
		})
		if err != nil {
			slog.Error("alert", "err", err)
		}
	}

	slog.Info("starting scraper", "sources", reg.Names(), "interval", cfg.CrawlInterval, "max_pages", cfg.MaxPages)
	alert(ctx, fmt.Sprintf("🚀 **Scraper started**\nSources: %v, every %s", reg.Names(), cfg.CrawlInterval))

	var wg sync.WaitGroup
	for _, name := range reg.Names() {
		src := reg[name]
		runner := &scraping.Runner{
			Source: src,
			Feeds:  subs,
			Publish: func(ctx context.Context, l events.ListingDiscovered) error {
				return producer.Publish(ctx, cfg.Topic(events.TopicListingsDiscovered), l.Source+"/"+l.ExternalID, l)
			},
			Alert:    alert,
			Interval: cfg.CrawlInterval,
			Gap:      cfg.RequestGap,
			MaxPages: cfg.MaxPages,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			runner.Run(ctx)
		}()
	}
	wg.Wait()

	slog.Info("stopping scraper")
	// ctx is cancelled; use a fresh one so the goodbye message can go out.
	bye, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	alert(bye, "⏹️ **Scraper stopped**")
	return nil
}

// Ingest stores discovered listings and announces the new ones. It owns the
// listings table and relays its outbox to Kafka.
func Ingest(ctx context.Context, cfg config.Ingest) error {
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	store := listings.NewService(pool, cfg.SeedWindow)
	if err := store.Migrate(ctx); err != nil {
		return err
	}

	producer, err := kafka.NewProducer(cfg.KafkaBrokers)
	if err != nil {
		return err
	}
	defer producer.Close()

	ctx, stop := context.WithCancel(ctx)
	defer stop()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		relayLoop(ctx, store, producer, cfg.Topic(events.TopicListingsNew))
	}()

	slog.Info("starting ingest")
	err = kafka.Consume(ctx, cfg.KafkaBrokers, cfg.Group("ingest"), cfg.Topic(events.TopicListingsDiscovered),
		func(ctx context.Context, l events.ListingDiscovered) error {
			_, err := store.Ingest(ctx, l)
			return err
		})
	stop()
	wg.Wait()
	return err
}

// relayLoop publishes outbox entries as they appear. Polling is plenty at this
// volume and avoids LISTEN/NOTIFY plumbing.
func relayLoop(ctx context.Context, store *listings.Service, p *kafka.Producer, topic string) {
	publish := func(ctx context.Context, l events.ListingDiscovered) error {
		return p.Publish(ctx, topic, l.Source+"/"+l.ExternalID, l)
	}
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		for {
			n, err := store.RelayOutbox(ctx, relayBatch, publish)
			if err != nil && ctx.Err() == nil {
				slog.Error("relay outbox", "err", err)
			}
			if err != nil || n < relayBatch {
				break
			}
		}
	}
}

// Matcher decides which users care about each new listing and asks the
// notifier to tell them. It owns users, channels and searches.
func Matcher(ctx context.Context, cfg config.Matcher, reg sources.Registry) error {
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	subs := subscriptions.NewService(pool, reg, cfg.FeedCollapseAt)
	if err := subs.Migrate(ctx); err != nil {
		return err
	}
	if cfg.UsersFile != "" {
		if err := subs.SeedFromFile(ctx, cfg.UsersFile); err != nil {
			return err
		}
		slog.Info("seeded users", "file", cfg.UsersFile)
	}

	producer, err := kafka.NewProducer(cfg.KafkaBrokers)
	if err != nil {
		return err
	}
	defer producer.Close()

	slog.Info("starting matcher")
	return kafka.Consume(ctx, cfg.KafkaBrokers, cfg.Group("matcher"), cfg.Topic(events.TopicListingsNew),
		func(ctx context.Context, l events.ListingDiscovered) error {
			ns, err := subs.Notifications(ctx, l)
			if err != nil {
				return err
			}
			for _, n := range ns {
				key := strconv.FormatInt(n.UserID, 10) // one user's messages stay in order
				if err := producer.Publish(ctx, cfg.Topic(events.TopicNotificationsRequested), key, n.NotificationRequested); err != nil {
					return err
				}
			}
			slog.Info("matched", "listing", l.ExternalID, "notifications", len(ns))
			return subs.Record(ctx, ns)
		})
}

// Notifier delivers notification requests through the given channels.
func Notifier(ctx context.Context, cfg config.Notifier, notifiers ...notifications.Notifier) error {
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	svc := notifications.NewService(pool, cfg.Delay, notifiers...)
	if err := svc.Migrate(ctx); err != nil {
		return err
	}

	slog.Info("starting notifier")
	return kafka.Consume(ctx, cfg.KafkaBrokers, cfg.Group("notifier"), cfg.Topic(events.TopicNotificationsRequested), svc.Handle)
}
