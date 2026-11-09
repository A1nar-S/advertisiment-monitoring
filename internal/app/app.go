// Package app holds the wiring of each service: what cmd/<name> runs. It lives
// here, not in main, so integration tests can start the real services
// in-process against real Postgres and Kafka with fake sites and notifiers.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"log/slog"

	"github.com/A1nar-S/advertisiment-monitoring/internal/bot"
	"github.com/A1nar-S/advertisiment-monitoring/internal/bot/discordbot"
	"github.com/A1nar-S/advertisiment-monitoring/internal/dashboard"
	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/listings"
	"github.com/A1nar-S/advertisiment-monitoring/internal/notifications"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/config"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/db"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/kafka"
	"github.com/A1nar-S/advertisiment-monitoring/internal/scraping"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions"
	"github.com/A1nar-S/advertisiment-monitoring/internal/web"
)

const relayBatch = 100

// digestCheckEvery is how often the notifier looks for digests that are due.
const digestCheckEvery = 30 * time.Second

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
	// The scraper only reads which ads are stored (to know where it left off);
	// ingest owns writing them. Migrating here too is safe: it takes a lock.
	history := listings.NewService(pool, listings.DefaultSeedWindow)
	if err := history.Migrate(ctx); err != nil {
		return err
	}

	producer, err := kafka.NewProducer(cfg.KafkaBrokers)
	if err != nil {
		return err
	}
	defer producer.Close()

	// Status messages go where /setup status-channel said, else to the .env
	// channel. Looked up per message: they are rare and the setting may change.
	alert := func(ctx context.Context, text string) {
		ch := events.Channel{Kind: cfg.AdminChannel.Kind, Target: cfg.AdminChannel.Target}
		if set, ok, err := subs.StatusChannel(ctx); err != nil {
			slog.Warn("status channel setting", "err", err)
		} else if ok {
			ch = set
		}
		if ch.Target == "" {
			return
		}
		err := producer.Publish(ctx, cfg.Topic(events.TopicNotificationsRequested), "admin", events.NotificationRequested{
			Channel: ch,
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
			Source:  src,
			Feeds:   subs,
			History: history,
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
	// Ad pages the matcher reads are saved, so the dashboard can show a
	// car's region, gearbox, ... without reading the page again.
	store := listings.NewService(pool, listings.DefaultSeedWindow)
	subs := subscriptions.NewService(pool, reg, cfg.FeedCollapseAt).WithAdPageHook(adPageSaver(store))
	if err := subs.Migrate(ctx); err != nil {
		return err
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

// adPageSaver stores what an ad page added to a listing. Failing to save is
// logged, not fatal: the alert matters more than the copy for the dashboard.
func adPageSaver(store *listings.Service) subscriptions.AdPageHook {
	return func(ctx context.Context, before, enriched events.ListingDiscovered, gone bool) {
		var err error
		if gone {
			err = store.MarkGone(ctx, before.Source, before.ExternalID)
		} else {
			err = store.SaveDetails(ctx, before, enriched)
		}
		if err != nil {
			slog.Warn("could not save ad page details", "listing", before.ExternalID, "err", err)
		}
	}
}

// Notifier delivers notification requests through the given channels.
func Notifier(ctx context.Context, cfg config.Notifier, notifiers ...notifications.Notifier) error {
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	svc := notifications.NewService(pool, cfg.Delay, notifiers...).WithBurst(cfg.Burst, cfg.BurstWindow)
	if err := svc.Migrate(ctx); err != nil {
		return err
	}
	if cfg.Burst > 0 {
		go svc.RunDigests(ctx, min(digestCheckEvery, max(cfg.BurstWindow/2, time.Second)))
	}

	slog.Info("starting notifier", "burst", cfg.Burst, "burst_window", cfg.BurstWindow)
	return kafka.Consume(ctx, cfg.KafkaBrokers, cfg.Group("notifier"), cfg.Topic(events.TopicNotificationsRequested), svc.Handle)
}

// Bot runs the chat bot users manage their searches with, on every
// platform it has an adapter for (Discord now).
func Bot(ctx context.Context, cfg config.Bot, reg sources.Registry) error {
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	discord, err := discordbot.New(cfg.DiscordBotToken)
	if err != nil {
		return err
	}
	// The Discord app's owners are admins too, so /setup works without
	// ADMIN_USERS.
	admins := slices.Clone(cfg.Admins)
	if owners, err := discord.Owners(); err != nil {
		slog.Warn("cannot read the Discord app's owners; only ADMIN_USERS are admins", "err", err)
	} else {
		for _, id := range owners {
			admins = append(admins, discordbot.Platform+":"+id)
		}
	}
	subs := subscriptions.NewService(pool, reg, cfg.FeedCollapseAt).WithLimits(cfg.MaxSearches, admins)
	store := listings.NewService(pool, listings.DefaultSeedWindow) // to show /matches
	for _, m := range []func(context.Context) error{subs.Migrate, store.Migrate} {
		if err := m(ctx); err != nil {
			return err
		}
	}

	core := bot.New(discord, subs, botAds{store}, cfg.PublicURL, cfg.MaxSearches)
	slog.Info("starting bot", "platform", discordbot.Platform, "max_searches", cfg.MaxSearches, "admins", len(admins))
	return discord.Serve(ctx, core, cfg.GuildID)
}

// botAds shows the bot stored ads without the bot importing the listings slice.
type botAds struct{ store *listings.Service }

func (a botAds) Ads(ctx context.Context, source string, ids []string) (map[string]bot.Ad, error) {
	found, err := a.store.Summaries(ctx, source, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bot.Ad, len(found))
	for id, s := range found {
		out[id] = bot.Ad{Title: s.Title, URL: s.URL, Price: s.Price, Location: s.Location}
	}
	return out, nil
}

// API serves the dashboard and its JSON API.
func API(ctx context.Context, cfg config.API, reg sources.Registry) error {
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	subs := subscriptions.NewService(pool, reg, cfg.FeedCollapseAt).WithLimits(cfg.MaxSearches, cfg.Admins)
	store := listings.NewService(pool, listings.DefaultSeedWindow)
	for _, m := range []func(context.Context) error{subs.Migrate, store.Migrate} {
		if err := m(ctx); err != nil {
			return err
		}
	}
	webDir := cfg.WebDir
	if _, err := os.Stat(filepath.Join(webDir, "index.html")); err != nil {
		slog.Warn("no built dashboard; serving the API only (run bun run build in web/)", "dir", webDir)
		webDir = ""
	}
	server := &http.Server{
		Addr: cfg.Addr,
		Handler: web.New(web.Config{
			Searches: subs, Listings: store, Overview: dashboard.NewHandler(dashboard.Load(pool)),
			Sources: reg, WebDir: webDir, PublicURL: cfg.PublicURL, MaxSearches: cfg.MaxSearches,
		}),
		ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: time.Minute,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.Error("shutdown", "err", err)
		}
	}()
	slog.Info("dashboard", "listen", cfg.Addr, "public_url", cfg.PublicURL)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
