// Command notifier delivers notification requests to Discord (later Telegram).
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
	"github.com/A1nar-S/advertisiment-monitoring/internal/notifications/discord"
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

	cfg, err := config.NotifierFromEnv()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dc, err := discord.New(cfg.DiscordBotToken)
	if err != nil {
		return err
	}
	return app.Notifier(ctx, cfg, dc)
}
