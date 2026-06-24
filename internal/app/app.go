package app

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"protean-provider/internal/adb"
	"protean-provider/internal/config"
	"protean-provider/internal/coordinator"
	"protean-provider/internal/db"
	"protean-provider/internal/domain"
	inboundGRPC "protean-provider/internal/grpc"
	"protean-provider/internal/registry"
	"protean-provider/internal/stream"
	"protean-provider/internal/supervisor"
)

const eventBufferSize = 64

// App is the top-level application object that wires all components together.
type App struct {
	cfg           *config.Config
	provider      *domain.Provider
	adbClient     adb.Client
	tracker       *adb.Tracker
	registry      domain.DeviceRegistry
	coordinator   *coordinator.Client
	db            *db.DB
	supervisor    *supervisor.Supervisor
	inboundServer *inboundGRPC.Server
}

// New creates an App from a loaded Config.
func New(cfg *config.Config) (*App, error) {
	// ── Provider identity ────────────────────────────────────────────────────
	detectedIP := detectProviderIP(cfg.Provider.IP)
	provider := &domain.Provider{
		ID:      detectedIP,
		Name:    cfg.Provider.Name,
		Host:    cfg.Provider.Host,
		IP:      detectedIP,
		MinPort: cfg.Provider.MinPort,
		MaxPort: cfg.Provider.MaxPort,
		Version: "dev",
	}

	// ── ADB Client ──────────────────────────────────────────────────────────
	adbClient, err := adb.NewClient(cfg.ADB.Host, cfg.ADB.Port)
	if err != nil {
		return nil, fmt.Errorf("app: adb client: %w", err)
	}

	// ── ADB Tracker ─────────────────────────────────────────────────────────
	tracker := adb.NewTracker(adbClient, adb.TrackerConfig{
		PropertyTimeout: cfg.ADB.PropertyTimeout,
		BackoffMin:      1 * time.Second,
		BackoffMax:      30 * time.Second,
	})

	// ── Device Registry ──────────────────────────────────────────────────────
	reg := registry.New()

	// ── Coordinator client ───────────────────────────────────────────────────
	coord := coordinator.New(cfg.Coordinator, provider.ID)

	// ── SQLite DB ────────────────────────────────────────────────────────────
	if err := os.MkdirAll(filepath.Dir(cfg.DB.Path), 0o755); err != nil {
		return nil, fmt.Errorf("app: create db dir: %w", err)
	}
	store, err := db.Open(cfg.DB.Path)
	if err != nil {
		return nil, fmt.Errorf("app: open db: %w", err)
	}

	// ── Stream Manager ──────────────────────────────────────────────────────
	var sup *supervisor.Supervisor
	streamMgr := stream.NewManager(cfg)

	// ── Supervisor (initialised but not started yet) ──────────────────────────
	// We pass a background context for the port allocator's DB restore;
	// the real context is passed in Run().
	sup, err = supervisor.New(
		context.Background(),
		provider.ID,
		adbClient,
		store,
		cfg.Provider.MinPort,
		cfg.Provider.MaxPort,
		streamMgr,
	)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("app: supervisor: %w", err)
	}

	inboundServer := inboundGRPC.NewServer(cfg, sup, reg)

	return &App{
		cfg:           cfg,
		provider:      provider,
		adbClient:     adbClient,
		tracker:       tracker,
		registry:      reg,
		coordinator:   coord,
		db:            store,
		supervisor:    sup,
		inboundServer: inboundServer,
	}, nil
}

// Run starts the application event loop and blocks until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	slog.Info("protean-provider starting",
		"id", a.provider.ID,
		"name", a.cfg.Provider.Name,
		"host", a.cfg.Provider.Host,
		"adb", fmt.Sprintf("%s:%d", a.cfg.ADB.Host, a.cfg.ADB.Port),
		"db", a.cfg.DB.Path,
	)

	// ── Start inbound gRPC server ───────────────────────────────────────────
	if err := a.inboundServer.Start(); err != nil {
		return fmt.Errorf("app: start inbound grpc server: %w", err)
	}

	// ── Connect to coordinator ───────────────────────────────────────────────
	if err := a.coordinator.Connect(ctx); err != nil {
		slog.Warn("coordinator: initial connect failed (will retry via heartbeat)", "err", err)
	} else {
		if err := a.coordinator.RegisterProvider(ctx, a.provider); err != nil {
			slog.Warn("coordinator: provider registration failed", "err", err)
		}
		go a.coordinator.RunHeartbeat(ctx)
	}

	// ── ADB Tracker ─────────────────────────────────────────────────────────
	events := make(chan domain.DeviceEvent, eventBufferSize)

	trackerErr := make(chan error, 1)
	go func() {
		trackerErr <- a.tracker.Watch(ctx, events)
	}()

	slog.Info("adb tracker running, waiting for devices…")

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutdown signal received, stopping…")
			a.drainAndCleanup()
			return ctx.Err()

		case err := <-trackerErr:
			if err != nil && err != context.Canceled {
				return fmt.Errorf("adb tracker: %w", err)
			}
			return nil

		case event := <-events:
			a.handleEvent(event)
		}
	}
}

// handleEvent dispatches a single DeviceEvent from the tracker.
func (a *App) handleEvent(event domain.DeviceEvent) {
	switch event.Type {
	case domain.EventConnected:
		a.onDeviceConnected(event)

	case domain.EventDisconnected:
		a.onDeviceDisconnected(event)

	case domain.EventUnauthorized:
		slog.Warn("device requires USB debugging authorization",
			"serial", event.Serial,
			"hint", "enable USB debugging and accept the RSA fingerprint on the device",
		)

	case domain.EventOffline:
		slog.Warn("device is offline", "serial", event.Serial)
	}
}

func (a *App) onDeviceConnected(event domain.DeviceEvent) {
	if event.Device == nil {
		slog.Error("device connected but property fetch failed — skipping registration",
			"serial", event.Serial,
		)
		return
	}

	d := event.Device
	if err := a.registry.Add(d); err != nil {
		slog.Warn("registry: could not add device (already present?)",
			"serial", d.Serial, "err", err,
		)
		return
	}

	slog.Info("device registered",
		"serial", d.Serial,
		"model", d.Info.Model,
		"manufacturer", d.Info.Manufacturer,
		"android", d.Info.AndroidVersion,
		"sdk", d.Info.SDKVersion,
		"abi", d.Info.CPUABI,
		"ram_mb", d.Info.RAMMB,
		"storage_mb", d.Info.StorageMB,
		"display", fmt.Sprintf("%dx%d @ %ddpi", d.Display.Width, d.Display.Height, d.Display.Density),
		"battery", d.State.Battery.Level,
		"wifi", d.State.Network.WiFiSSID,
		"ip", d.State.Network.IP,
		"total_devices", a.registry.Count(),
	)


	// Install/push scrcpy-server.jar to device
	if err := a.installScrcpyServer(context.Background(), d.Serial); err != nil {
		slog.Warn("failed to install scrcpy-server.jar on device", "serial", d.Serial, "err", err)
	}

	// Start a supervisor for this device.
	if err := a.supervisor.OnDeviceConnected(context.Background(), d); err != nil {
		slog.Warn("supervisor: failed to start device supervisor", "serial", d.Serial, "err", err)
	}

	// Notify coordinator (best-effort).
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Coordinator.CallTimeout)
		defer cancel()
		if err := a.coordinator.RegisterDevice(ctx, d); err != nil {
			slog.Warn("coordinator: failed to register device", "serial", d.Serial, "err", err)
		}
	}()
}

func (a *App) onDeviceDisconnected(event domain.DeviceEvent) {
	if err := a.registry.Remove(event.Serial); err != nil {
		slog.Warn("registry: device not found on disconnect", "serial", event.Serial)
	}

	// Stop the supervisor for this device.
	a.supervisor.OnDeviceDisconnected(event.Serial)

	slog.Info("device unregistered",
		"serial", event.Serial,
		"total_devices", a.registry.Count(),
	)

	// Notify coordinator (best-effort).
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Coordinator.CallTimeout)
		defer cancel()
		if err := a.coordinator.ReleaseDevice(ctx, event.Serial); err != nil {
			slog.Warn("coordinator: failed to release device", "serial", event.Serial, "err", err)
		}
	}()
}

// drainAndCleanup performs graceful shutdown of all subsystems.
func (a *App) drainAndCleanup() {
	slog.Info("stopping inbound grpc server…")
	a.inboundServer.Stop()

	slog.Info("shutting down supervisors…")
	a.supervisor.StopAll()

	slog.Info("disconnecting from coordinator…")
	_ = a.coordinator.Disconnect(context.Background())

	slog.Info("closing database…")
	if err := a.db.Close(); err != nil {
		slog.Warn("db: close error", "err", err)
	}

	slog.Info("shutdown complete", "remaining_devices", a.registry.Count())
}

// detectProviderIP determines the host IP address of the provider.
func detectProviderIP(cfgIP string) string {
	// 1. If configured explicitly in YAML/env, use it!
	if cfgIP != "" {
		return cfgIP
	}

	// 2. Try standard environment variables often set in containers
	for _, env := range []string{"HOST_IP", "PROVIDER_IP", "PROVIDER_PROVIDER_IP"} {
		if val := os.Getenv(env); val != "" {
			return val
		}
	}

	// 3. Try to get the outbound IP by dialing a public IP (does not send packets, instant)
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		ip := localAddr.IP.String()
		if ip != "" && ip != "127.0.0.1" {
			return ip
		}
	}

	// 4. Try scanning interfaces for first non-loopback IPv4 address (local network)
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String()
				}
			}
		}
	}

	// 5. Fallback: try to resolve host.docker.internal (with a strict 200ms timeout to prevent blocking)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	dockerAddrs, dnsErr := net.DefaultResolver.LookupHost(ctx, "host.docker.internal")
	cancel()
	if dnsErr == nil && len(dockerAddrs) > 0 {
		return dockerAddrs[0]
	}

	return "127.0.0.1"
}

// scrcpyServerJar returns the path to the scrcpy-server.jar file.
// It looks next to the running executable first (for deployed builds),
// then falls back to the source-tree location (for local dev / go run).
func scrcpyServerJar() (string, error) {
	// 1. Next to the binary: <exe-dir>/scrcpy-server.jar
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "scrcpy-server.jar")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// 2. Source-tree location: internal/stream/scrcpy-server.jar
	// (works when running via `go run` or in the project root)
	p := filepath.Join("internal", "stream", "scrcpy-server.jar")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("scrcpy-server.jar not found next to binary or in internal/stream/ — run `make build` to copy it")
}

// installScrcpyServer pushes scrcpy-server.jar to /data/local/tmp/ on the device.
// The file is read directly from disk — no embedding or temp-file extraction needed.
func (a *App) installScrcpyServer(ctx context.Context, serial string) error {
	jarPath, err := scrcpyServerJar()
	if err != nil {
		return fmt.Errorf("installScrcpyServer: %w", err)
	}

	slog.Info("adb: pushing scrcpy-server to device", "serial", serial, "src", jarPath)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "adb", "-s", serial, "push", jarPath, "/data/local/tmp/scrcpy-server.jar")
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("installScrcpyServer: adb push: %w (stderr: %q)", err, stderr.String())
	}

	// Ensure the file is readable by the app on-device.
	chmodCmd := exec.CommandContext(ctx, "adb", "-s", serial, "shell", "chmod", "644", "/data/local/tmp/scrcpy-server.jar")
	if err := chmodCmd.Run(); err != nil {
		slog.Warn("adb: chmod 644 failed for scrcpy-server.jar", "serial", serial, "err", err)
	}

	slog.Info("adb: scrcpy-server successfully installed on device", "serial", serial)
	return nil
}
