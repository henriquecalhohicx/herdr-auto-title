// Command herdr-auto-title is the Herdr Auto Title plugin process.
//
// Herdr launches it once through a startup hook. It then stays alive, polls the
// session over the Herdr socket, and keeps every tab's title in step with what
// that tab is doing.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"herdr-auto-title/internal/app"
	"herdr-auto-title/internal/herdr"
	"herdr-auto-title/internal/resolver"
)

func main() {
	if err := run(); err != nil {
		slog.Error("auto title stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, warnings := app.LoadConfig()

	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	for _, warning := range warnings {
		log.Warn(warning)
	}
	log.Info("starting auto title", "poll", cfg.Poll, "max_length", cfg.MaxLength)

	// Terminate on the signals Herdr uses to stop a plugin.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client, err := herdr.New()
	if err != nil {
		return err
	}

	titles := resolver.Default(cfg.MaxLength, cfg.BranchMax)

	if err := app.New(cfg, log, titles).Run(ctx, client); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	return nil
}
