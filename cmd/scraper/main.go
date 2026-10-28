// Command scraper fetches the feeds users are watching and publishes every ad it sees to Kafka.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/A1nar-S/advertisiment-monitoring/internal/app"
	"github.com/A1nar-S/advertisiment-monitoring/internal/platform/config"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load() // optional

	cfg, err := config.ScraperFromEnv()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return app.Scraper(ctx, cfg, app.KnownSources(cfg.RequestTimeout))
}
