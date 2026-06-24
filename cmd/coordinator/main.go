package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"protean-provider/internal/config"
	"protean-provider/internal/coordinator_server"
	"protean-provider/internal/logger"
)

func main() {
	// Initialize default configuration
	cfg := coordinator_server.LoadConfig()

	// Initialize basic slog logger
	// In production, we'd use config-driven logging, but a direct logger.New with defaults is fine.
	log := logger.New(config.LoggingConfig{
		Format: "text",
		Level:  "debug",
	})
	slog.SetDefault(log)

	slog.Info("protean-coordinator starting", "grpc_port", cfg.GRPCPort, "postgres_uri", cfg.PostgresURI)

	// 1. Open PostgreSQL Database
	db, err := coordinator_server.OpenDB(cfg.PostgresURI)
	if err != nil {
		slog.Error("failed to open postgres database", "err", err)
		os.Exit(1)
	}

	// 2. Initialize and Start Server
	server := coordinator_server.NewServer(cfg, db)
	if err := server.Start(); err != nil {
		slog.Error("failed to start coordinator server", "err", err)
		os.Exit(1)
	}

	// 3. Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	slog.Info("shutdown signal received, stopping coordinator…")
	server.Stop()
	slog.Info("goodbye")
}
