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
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"protean-provider/internal/adb"
	"protean-provider/internal/domain"
)

const (
	// defaultRefreshInterval is how often the agent polls live device state.
	defaultRefreshInterval = 30 * time.Second
	// shellTimeout is the maximum time allowed for a single ADB shell command.
	shellTimeout = 15 * time.Second
)

// Agent manages the ADB bridge for a single claimed device.
type Agent struct {
	serial  string
	port    int      // allocated TCP port (host side)
	adb     adb.Client
	device  *domain.Device

	mu       sync.RWMutex
	forwards []ForwardRule
	stopped  bool

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

	slog.Info("agent: starting", "serial", a.serial, "port", a.port)

	// Establish the primary port forward.
	// Convention: forward local port → device port 8080 (the STF agent listener).
	// A real implementation would use the STF agent APK's actual port.
	if err := a.Forward(agentCtx, a.port, 8080); err != nil {
		slog.Warn("agent: primary forward failed (continuing without it)",
			"serial", a.serial, "err", err)
	}

	// Periodic state refresh.
	ticker := time.NewTicker(defaultRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-agentCtx.Done():
			a.teardown(context.Background())
			return nil

		case <-ticker.C:
			a.refreshState(agentCtx)
		}
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

// Forward adds a TCP port forward rule: host localPort → device remotePort.
// The rule is persisted in-memory and torn down when the agent stops.
func (a *Agent) Forward(ctx context.Context, localPort, remotePort int) error {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return ErrAgentStopped
	}
	a.mu.Unlock()

	args := []string{
		"-s", a.serial,
		"forward",
		fmt.Sprintf("tcp:%d", localPort),
		fmt.Sprintf("tcp:%d", remotePort),
	}

	if err := runADB(ctx, args...); err != nil {
		return fmt.Errorf("agent: forward tcp:%d→tcp:%d on %s: %w",
			localPort, remotePort, a.serial, err)
	}

	a.mu.Lock()
	a.forwards = append(a.forwards, ForwardRule{
		LocalPort:  localPort,
		RemotePort: remotePort,
		Serial:     a.serial,
	})
	a.mu.Unlock()

	slog.Info("agent: forward established",
		"serial", a.serial,
		"local", localPort,
		"remote", remotePort,
	)
	return nil
}

// RemoveForward removes a specific port forward rule.
func (a *Agent) RemoveForward(ctx context.Context, localPort int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	args := []string{
		"-s", a.serial,
		"forward", "--remove",
		fmt.Sprintf("tcp:%d", localPort),
	}
	if err := runADB(ctx, args...); err != nil {
		return fmt.Errorf("agent: remove forward tcp:%d on %s: %w", localPort, a.serial, err)
	}

	// Remove from in-memory list.
	filtered := a.forwards[:0]
	for _, f := range a.forwards {
		if f.LocalPort != localPort {
			filtered = append(filtered, f)
		}
	}
	a.forwards = filtered

	slog.Info("agent: forward removed", "serial", a.serial, "local", localPort)
	return nil
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

// ActiveForwards returns a snapshot of the currently active forward rules.
func (a *Agent) ActiveForwards() []ForwardRule {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]ForwardRule, len(a.forwards))
	copy(result, a.forwards)
	return result
}

// Device returns the most recent device snapshot the agent holds.
func (a *Agent) Device() *domain.Device {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.device
}

// ── internal ───────────────────────────────────────────────────────────────────

// refreshState re-fetches live battery and network state from the device and
// publishes the updated snapshot to StateUpdates.
func (a *Agent) refreshState(ctx context.Context) {
	refreshCtx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()

	// Re-fetch the full device including live state.
	updated, err := adb.FetchProperties(refreshCtx, a.adb, a.serial)
	if err != nil {
		slog.Warn("agent: state refresh failed", "serial", a.serial, "err", err)
		return
	}

	// Preserve immutable identity fields from the original device.
	a.mu.Lock()
	updated.ConnectedAt = a.device.ConnectedAt
	a.device = updated
	a.device.LastSeen = time.Now()
	a.mu.Unlock()

	// Publish non-blocking.
	select {
	case a.StateUpdates <- updated:
	default:
		// Consumer is slow; drop the update rather than block.
	}

	// slog.Debug("agent: state refreshed",
	// 	"serial", a.serial,
	// 	"battery", updated.State.Battery.Level,
	// 	"ip", updated.State.Network.IP,
	// )
}

// teardown removes all port forwards established by this agent.
func (a *Agent) teardown(ctx context.Context) {
	a.mu.Lock()
	forwards := make([]ForwardRule, len(a.forwards))
	copy(forwards, a.forwards)
	a.forwards = nil
	a.mu.Unlock()

	for _, f := range forwards {
		args := []string{
			"-s", a.serial,
			"forward", "--remove",
			fmt.Sprintf("tcp:%d", f.LocalPort),
		}
		if err := runADB(ctx, args...); err != nil {
			slog.Warn("agent: teardown forward failed",
				"serial", a.serial, "local", f.LocalPort, "err", err)
		} else {
			slog.Info("agent: forward removed on teardown",
				"serial", a.serial, "local", f.LocalPort)
		}
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
