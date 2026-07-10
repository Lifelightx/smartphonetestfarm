package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
	resetSerial := flag.String("reset", "", "Reset/Restart the supervisor for a particular device serial")
	flag.Parse()

	if *version {
		fmt.Printf("protean-provider %s\n", Version)
		os.Exit(0)
	}

	// Support subcommand "reset <serial>" or flag "--reset <serial>"
	serialToReset := *resetSerial
	if serialToReset == "" && len(flag.Args()) >= 2 && flag.Arg(0) == "reset" {
		serialToReset = flag.Arg(1)
	}

	if serialToReset != "" {
		socketPath := "scratch/provider.sock"
		conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
		if err != nil {
			fmt.Printf("Error: could not connect to provider admin socket: %v\n", err)
			fmt.Println("Is the provider daemon process running in this workspace?")
			os.Exit(1)
		}
		defer conn.Close()

		_, err = fmt.Fprintf(conn, "reset %s\n", serialToReset)
		if err != nil {
			fmt.Printf("Error: failed to send command to provider daemon: %v\n", err)
			os.Exit(1)
		}

		scanner := bufio.NewScanner(conn)
		if scanner.Scan() {
			response := scanner.Text()
			fmt.Println(response)
			if strings.HasPrefix(response, "SUCCESS:") {
				os.Exit(0)
			}
		} else {
			if err := scanner.Err(); err != nil {
				fmt.Printf("Error: failed to read response: %v\n", err)
			} else {
				fmt.Println("Error: no response received from provider daemon")
			}
		}
		os.Exit(1)
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