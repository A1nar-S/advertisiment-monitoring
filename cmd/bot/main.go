// Command bot is the walking skeleton: every slice wired together in one
// process. It is replaced by separate scheduler/scraper/ingest/notifier
// services once Kafka is in.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/A1nar-S/advertisiment-monitoring/internal/listings"
	"github.com/A1nar-S/advertisiment-monitoring/internal/notifications"
	"github.com/A1nar-S/advertisiment-monitoring/internal/notifications/discord"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/config"
	"github.com/A1nar-S/advertisiment-monitoring/internal/scraping/ss"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load() // optional

	cfg, err := config.FromEnv()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("database: %w", err)
	}

	store := listings.NewService(pool)
	if err := store.Migrate(ctx); err != nil {
		return err
	}

	dc, err := discord.New(cfg.DiscordBotToken, cfg.DiscordChannelID)
	if err != nil {
		return err
	}
	notify := &notifications.Dispatcher{Notifiers: []notifications.Notifier{dc}, Delay: cfg.NotifyDelay}

	scraper := ss.NewClient(cfg.RequestTimeout)
	form := ss.ParseFormData(cfg.SSFormData)

	slog.Info("starting", "interval", cfg.CrawlInterval, "url", cfg.SSURL)
	notify.NotifyText(ctx, fmt.Sprintf("🚀 **SS.com Crawler Started**\nCrawling every %s", cfg.CrawlInterval))

	crawl := func() {
		err := func() error {
			found, err := scraper.Fetch(ctx, cfg.SSURL, form)
			if err != nil {
				return err
			}
			fresh, err := store.Ingest(ctx, found)
			notify.NotifyListings(ctx, fresh) // also covers listings stored before a mid-batch error
			return err
		}()
		if err != nil && ctx.Err() == nil {
			slog.Error("crawl failed", "err", err)
			notify.NotifyText(ctx, "❌ **Crawl Error**\n"+err.Error())
		}
	}

	crawl()
	ticker := time.NewTicker(cfg.CrawlInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("stopping")
			// ctx is cancelled; use a fresh one so the goodbye message can go out.
			bye, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			notify.NotifyText(bye, "⏹️ **SS.com Crawler Stopped**")
			return nil
		case <-ticker.C:
			crawl()
		}
	}
}
