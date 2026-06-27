// Package agent implements the per-device ADB bridge.
//
// An Agent is created by the DeviceSupervisor when a device is claimed.
// It is responsible for:
//   - Establishing ADB port forwards (local TCP port → device TCP port)
//   - Running ADB shell commands on behalf of the session
//   - Refreshing device state (battery, network) periodically
//   - Tearing down all forwards cleanly on release
//
// The Agent is intentionally stateless with respect to sessions — it only
// knows about the ADB-level connection. Session state lives in the supervisor.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"protean-provider/internal/adb"
	"protean-provider/internal/domain"
)

const (
	// shellTimeout is the maximum time allowed for a single ADB shell command.
	shellTimeout = 15 * time.Second
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow localhost internal connection
	},
}

// Agent manages the ADB bridge for a single claimed device.
type Agent struct {
	serial  string
	port    int      // allocated TCP port (host side for websocket reverse proxy)
	adb     adb.Client
	device  *domain.Device

	mu       sync.RWMutex
	stopped  bool
	wsConn   *websocket.Conn // Active WebSocket connection from Android APK

	cancel context.CancelFunc
	done   chan struct{}

	// StateUpdates receives periodic device state snapshots (battery, network, etc.)
	// Consumers should drain this channel; it is closed when the agent stops.
	StateUpdates chan *domain.Device
}

// New creates an Agent for the given device. Call Run to start it.
func New(device *domain.Device, port int, adbClient adb.Client) *Agent {
	return &Agent{
		serial:       device.Serial,
		port:         port,
		adb:          adbClient,
		device:       device,
		StateUpdates: make(chan *domain.Device, 4),
		done:         make(chan struct{}),
	}
}

// Run starts the agent's background work:
//  1. Establishes the primary port forward.
//  2. Starts the periodic state refresh loop.
//
// It blocks until ctx is cancelled or Stop is called.
func (a *Agent) Run(ctx context.Context) error {
	agentCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	defer close(a.done)
	defer cancel()

	slog.Info("agent: starting ws listener", "serial", a.serial, "port", a.port)

	// 1. Start the HTTP server to listen for the Android Agent's WebSocket connection
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", a.handleWS)

	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", a.port),
		Handler: mux,
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.ListenAndServe()
	}()

	// 2. Deploy the Agent APK which will setup adb reverse and connect back
	go func() {
		if err := adb.DeployAgent(a.serial, a.port); err != nil {
			slog.Warn("agent: failed to deploy apk", "serial", a.serial, "err", err)
		}
	}()

	select {
	case <-agentCtx.Done():
		a.teardown(context.Background())
		// Explicitly close active WS connection to unblock handleWS
		a.mu.Lock()
		if a.wsConn != nil {
			_ = a.wsConn.Close()
		}
		a.mu.Unlock()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 1*time.Second)
		_ = srv.Shutdown(shutdownCtx)
		shutdownCancel()
		return nil
	case err := <-serverErr:
		if err != http.ErrServerClosed {
			slog.Error("agent: ws server failed", "serial", a.serial, "err", err)
			return err
		}
	}
	return nil
}

func (a *Agent) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("agent: ws upgrade failed", "serial", a.serial, "err", err)
		return
	}

	a.mu.Lock()
	a.wsConn = conn
	a.mu.Unlock()

	slog.Info("agent: android apk connected via websocket", "serial", a.serial)

	defer func() {
		conn.Close()
		a.mu.Lock()
		if a.wsConn == conn {
			a.wsConn = nil
		}
		a.mu.Unlock()
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}
		a.handleIncomingEvent(message)
	}
}

// handleIncomingEvent parses JSON from the Agent APK and updates the device state
func (a *Agent) handleIncomingEvent(data []byte) {
	var event struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return
	}

	a.mu.Lock()
	updated := false
	switch event.Type {
	case "BATTERY_CHANGED":
		var batt struct {
			Level      int  `json:"level"`
			IsCharging bool `json:"is_charging"`
		}
		if err := json.Unmarshal(event.Data, &batt); err == nil {
			a.device.State.Battery.Level = batt.Level
			a.device.State.Battery.IsCharging = batt.IsCharging
			updated = true
		}
	case "NETWORK_CHANGED":
		var net struct {
			Connected bool   `json:"connected"`
			WiFiSSID  string `json:"wifi_ssid"`
		}
		if err := json.Unmarshal(event.Data, &net); err == nil {
			a.device.State.Network.Connected = net.Connected
			a.device.State.Network.WiFiSSID = net.WiFiSSID
			updated = true
		}
	}
	
	if updated {
		a.device.LastSeen = time.Now()
		deviceSnapshot := *a.device // Copy
		a.mu.Unlock()

		// Publish to StateUpdates
		select {
		case a.StateUpdates <- &deviceSnapshot:
		default:
		}
	} else {
		a.mu.Unlock()
	}
}

// Stop signals the agent to shut down and waits for it to finish.
func (a *Agent) Stop() {
	a.mu.Lock()
	a.stopped = true
	a.mu.Unlock()

	if a.cancel != nil {
		a.cancel()
	}
	<-a.done
	close(a.StateUpdates)
}

// teardown removes the ADB reverse tunnel.
func (a *Agent) teardown(ctx context.Context) {
	args := []string{
		"-s", a.serial,
		"reverse", "--remove",
		fmt.Sprintf("tcp:%d", a.port),
	}
	if err := runADB(ctx, args...); err != nil {
		slog.Warn("agent: teardown reverse failed", "serial", a.serial, "port", a.port, "err", err)
	} else {
		slog.Info("agent: reverse tunnel removed on teardown", "serial", a.serial, "port", a.port)
	}
	slog.Info("agent: stopped", "serial", a.serial)
}

// runADB executes the `adb` binary directly with the given arguments.
// This is used for operations that go-adbkit doesn't expose (e.g. forward).
func runADB(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "adb", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb %s: %w (output: %s)",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// parseExitCode attempts to extract an exit code from an adb shell output.
// ADB shell doesn't natively return exit codes, so we use the common trick:
// `cmd; echo "EXIT:$?"`.
func parseExitCode(output string) (string, int) {
	const marker = "EXIT:"
	idx := strings.LastIndex(output, marker)
	if idx == -1 {
		return output, -1
	}
	codeStr := strings.TrimSpace(output[idx+len(marker):])
	code, err := strconv.Atoi(codeStr)
	if err != nil {
		return output[:idx], -1
	}
	return strings.TrimSpace(output[:idx]), code
}

// Shell executes a single ADB shell command on the device and returns the output.
func (a *Agent) Shell(ctx context.Context, cmd string) (CommandResult, error) {
	a.mu.RLock()
	stopped := a.stopped
	a.mu.RUnlock()

	if stopped {
		return CommandResult{}, ErrAgentStopped
	}

	cmdCtx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()

	out, err := a.adb.Shell(cmdCtx, a.serial, cmd)
	if err != nil {
		return CommandResult{ExitCode: -1}, fmt.Errorf("agent: shell %q: %w", cmd, err)
	}

	return CommandResult{Output: out, ExitCode: 0}, nil
}

// ShellWithExitCode runs a shell command and captures the exit code using the
// `echo EXIT:$?` trick. Returns the cleaned output and exit code.
func (a *Agent) ShellWithExitCode(ctx context.Context, cmd string) (CommandResult, error) {
	wrapped := fmt.Sprintf("%s; echo EXIT:$?", cmd)
	result, err := a.Shell(ctx, wrapped)
	if err != nil {
		return result, err
	}
	clean, code := parseExitCode(result.Output)
	return CommandResult{Output: clean, ExitCode: code}, nil
}
