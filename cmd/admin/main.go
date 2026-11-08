// Command admin runs one-off maintenance: migrate, seed users, purge old data.
//
//	admin migrate
//	admin seed
//	admin check          (validate USERS_FILE, no database needed)
//	admin purge [-older-than 1440h]
//	admin ss-codebook [-only cars|flats]
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/A1nar-S/advertisiment-monitoring/internal/app"
	"github.com/A1nar-S/advertisiment-monitoring/internal/listings"
	"github.com/A1nar-S/advertisiment-monitoring/internal/notifications"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/config"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/db"
	"github.com/A1nar-S/advertisiment-monitoring/internal/scraping/ss"
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
		return fmt.Errorf("usage: admin migrate | seed | check | purge [-older-than 1440h] | ss-codebook [-only cars|flats]")
	}
	cmd, args := os.Args[1], os.Args[2:]
	if cmd == "ss-codebook" {
		return ssCodebook(args) // needs no database
	}
	if cmd == "check" {
		return check() // needs no database
	}

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
	subs := subscriptions.NewService(pool, app.KnownSources(0, 0), cfg.FeedCollapseAt)
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

// ssCodebook refreshes the ss.com codebook snapshots (cars, flats) from the
// live search forms: about one request per make or region, REQUEST_GAP_SECONDS
// apart.
func ssCodebook(args []string) error {
	fs := flag.NewFlagSet("ss-codebook", flag.ExitOnError)
	dir := fs.String("dir", "internal/scraping/ss/codebook", "where to write the snapshots")
	only := fs.String("only", "", "cars or flats; empty = both")
	_ = fs.Parse(args)

	sc, err := config.ScraperFromEnv()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c := ss.NewClient(sc.RequestTimeout)

	builds := map[string]func(context.Context, time.Duration) (*ss.Codebook, error){
		"cars": c.BuildCarsCodebook, "flats": c.BuildFlatsCodebook,
	}
	for _, name := range []string{"cars", "flats"} {
		if *only != "" && *only != name {
			continue
		}
		cb, err := builds[name](ctx, sc.RequestGap)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		raw, err := cb.MarshalIndent()
		if err != nil {
			return err
		}
		path := filepath.Join(*dir, name+".json")
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			return err
		}
		slog.Info("wrote codebook", "file", path, "fields", len(cb.Fields), "makes", len(cb.Makes), "regions", len(cb.Regions))
	}
	return nil
}

// check validates the users file: every search, with "did you mean" hints.
func check() error {
	path := os.Getenv("USERS_FILE")
	if path == "" {
		path = "config/users.yaml"
	}
	if err := subscriptions.ValidateFile(app.KnownSources(0, 0), path); err != nil {
		return err
	}
	slog.Info("users file is valid", "file", path)
	return nil
}
