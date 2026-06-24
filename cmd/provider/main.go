package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"protean-provider/internal/app"
	"protean-provider/internal/config"
	"protean-provider/internal/logger"
)

// Version is injected at build time via -ldflags "-X main.Version=v1.0.0".
var Version = "dev"

func main() {
	// ── CLI flags ─────────────────────────────────────────────────────────────
	configPath := flag.String("config", "config/provider.yaml", "Path to provider config file")
	logLevel := flag.String("log-level", "", "Override log level (debug|info|warn|error)")
	version := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *version {
		fmt.Printf("protean-provider %s\n", Version)
		os.Exit(0)
	}

	// ── Config ────────────────────────────────────────────────────────────────
	cfg, err := config.Load(*configPath)
	if err != nil {
		// We don't have a logger yet, so use fmt.
		fmt.Fprintf(os.Stderr, "fatal: load config: %v\n", err)
		os.Exit(1)
	}

	// Apply CLI log-level override.
	if *logLevel != "" {
		cfg.Logging.Level = *logLevel
	}

	// ── Logger ────────────────────────────────────────────────────────────────
	log := logger.New(cfg.Logging)
	log.Info("protean-provider", "version", Version, "config", *configPath)

	// ── App ───────────────────────────────────────────────────────────────────
	application, err := app.New(cfg)
	if err != nil {
		slog.Error("failed to initialize application", "err", err)
		os.Exit(1)
	}

	// ── Signal handling ───────────────────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ── Run ───────────────────────────────────────────────────────────────────
	if err := application.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("application exited with error", "err", err)
		os.Exit(1)
	}

	slog.Info("goodbye")
}