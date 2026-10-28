// Command admin runs one-off maintenance: migrate, seed users, purge old data.
//
//	admin migrate
//	admin seed
//	admin purge [-older-than 168h]
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/A1nar-S/advertisiment-monitoring/internal/app"
	"github.com/A1nar-S/advertisiment-monitoring/internal/listings"
	"github.com/A1nar-S/advertisiment-monitoring/internal/notifications"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/config"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/db"
	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load() // optional

	if len(os.Args) < 2 {
		return fmt.Errorf("usage: admin migrate | seed | purge [-older-than 168h]")
	}
	cmd, args := os.Args[1], os.Args[2:]

	cfg, err := config.AdminFromEnv()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	store := listings.NewService(pool, listings.DefaultSeedWindow)
	subs := subscriptions.NewService(pool, app.KnownSources(0), cfg.FeedCollapseAt)
	// No Notifier is needed for maintenance, only the deliveries table.
	notify := notifications.NewService(pool, 0)
	for _, m := range []func(context.Context) error{store.Migrate, subs.Migrate, notify.Migrate} {
		if err := m(ctx); err != nil {
			return err
		}
	}

	switch cmd {
	case "migrate":
		slog.Info("migrated")
		return nil

	case "seed":
		if err := subs.SeedFromFile(ctx, cfg.UsersFile); err != nil {
			return err
		}
		slog.Info("seeded", "file", cfg.UsersFile)
		return nil

	case "purge":
		fs := flag.NewFlagSet("purge", flag.ExitOnError)
		olderThan := fs.Duration("older-than", cfg.Retention, "delete data not seen for this long")
		_ = fs.Parse(args)

		n, err := store.Purge(ctx, *olderThan)
		if err != nil {
			return fmt.Errorf("purge listings: %w", err)
		}
		// Matches and deliveries outlive listings so a re-scraped old ad is not
		// announced twice; hence the 2x.
		m, err := subs.PurgeMatches(ctx, 2**olderThan)
		if err != nil {
			return fmt.Errorf("purge matches: %w", err)
		}
		d, err := notify.PurgeDeliveries(ctx, 2**olderThan)
		if err != nil {
			return fmt.Errorf("purge deliveries: %w", err)
		}
		slog.Info("purged", "listings", n, "matches", m, "deliveries", d, "older_than", olderThan.Round(time.Second))
		return nil
	}
	return fmt.Errorf("unknown command %q", cmd)
}
