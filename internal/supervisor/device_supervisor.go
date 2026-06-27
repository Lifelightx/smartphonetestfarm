package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"protean-provider/internal/adb"
	"protean-provider/internal/agent"
	"protean-provider/internal/db"
	"protean-provider/internal/domain"
)

// DeviceSupervisor manages the full lifecycle of a single connected device.
// It owns the state machine, the session record, and the port allocation for
// that device.
//
// One DeviceSupervisor is created per connected device and torn down when the
// device disconnects.
type DeviceSupervisor struct {
	device     *domain.Device
	providerID string
	adbClient  adb.Client
	store      *db.DB
	ports      *PortAllocator
	events     chan<- SupervisorEvent

	mu        sync.RWMutex
	state     DeviceState
	sessionID string
	port      int
	agt       *agent.Agent // non-nil while a session is active
	streams   domain.StreamManager

	cancel context.CancelFunc
	done   chan struct{}
}

// newDeviceSupervisor creates a DeviceSupervisor. Call Run to start it.
func newDeviceSupervisor(
	device *domain.Device,
	providerID string,
	adbClient adb.Client,
	store *db.DB,
	ports *PortAllocator,
	events chan<- SupervisorEvent,
	streams domain.StreamManager,
) *DeviceSupervisor {
	return &DeviceSupervisor{
		device:     device,
		providerID: providerID,
		adbClient:  adbClient,
		store:      store,
		ports:      ports,
		events:     events,
		streams:    streams,
		state:      StateIdle,
		done:       make(chan struct{}),
	}
}

// Run starts the supervisor. It blocks until ctx is cancelled or Stop is called.
func (ds *DeviceSupervisor) Run(ctx context.Context) {
	dsCtx, cancel := context.WithCancel(ctx)
	ds.cancel = cancel
	defer close(ds.done)
	defer cancel()

	serial := ds.device.Serial
	slog.Info("device supervisor: started", "serial", serial)

	// Allocate a port for this device.
	port, err := ds.ports.Allocate(dsCtx, serial)
	if err != nil {
		slog.Error("device supervisor: port allocation failed",
			"serial", serial, "err", err)
		return
	}

	ds.mu.Lock()
	ds.port = port
	ds.mu.Unlock()

	slog.Info("device supervisor: port allocated", "serial", serial, "port", port)

	// Spawn the Protean Agent for this device (using port + 3000 for the WebSocket reverse proxy)
	// (Note: port+2000 is used by scrcpy-server for adb forward)
	agt := agent.New(ds.device, port+3000, ds.adbClient)
	ds.mu.Lock()
	ds.agt = agt
	ds.mu.Unlock()

	go func() {
		if err := agt.Run(dsCtx); err != nil {
			slog.Warn("device supervisor: agent exited with error",
				"serial", ds.device.Serial, "err", err)
		}
	}()

	// Log the connected event.
	_ = ds.store.LogEvent(dsCtx, serial, "connected",
		fmt.Sprintf("port=%d model=%s", port, ds.device.Info.Model))

	// The supervisor now sits idle, processing commands sent via Claim/Release/Activate.
	<-dsCtx.Done()

	slog.Info("device supervisor: stopping", "serial", serial)
	ds.teardown(context.Background())
}

// Stop signals the supervisor to shut down and waits for it to finish.
func (ds *DeviceSupervisor) Stop() {
	if ds.cancel != nil {
		ds.cancel()
	}
	<-ds.done
}

// State returns the current device state (safe for concurrent use).
func (ds *DeviceSupervisor) State() DeviceState {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.state
}

// Port returns the allocated TCP port for this device.
func (ds *DeviceSupervisor) Port() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.port
}

// ── State transitions ─────────────────────────────────────────────────────────

// Claim transitions the device from Idle → Claimed and creates a session record.
// claimedBy identifies the client (user ID, session token, etc.).
func (ds *DeviceSupervisor) Claim(ctx context.Context, claimedBy string) (string, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.state != StateIdle {
		return "", fmt.Errorf("device %s is not idle (current state: %s)", ds.device.Serial, ds.state)
	}

	sessionID := uuid.New().String()
	old := ds.state

	// Start screen stream capture.
	if err := ds.streams.StartCapture(ctx, ds.device.Serial, ds.port); err != nil {
		return "", fmt.Errorf("device supervisor: start capture failed: %w", err)
	}

	ds.state = StateClaimed
	ds.sessionID = sessionID

	_ = ds.store.LogEvent(ctx, ds.device.Serial, "claimed",
		fmt.Sprintf("session=%s by=%s", sessionID, claimedBy))

	ds.emit(SupervisorEvent{
		Serial:    ds.device.Serial,
		OldState:  old,
		NewState:  StateClaimed,
		SessionID: sessionID,
	})

	slog.Info("device supervisor: claimed",
		"serial", ds.device.Serial,
		"session", sessionID,
		"by", claimedBy,
	)
	return sessionID, nil
}

// Activate transitions the device from Claimed → Busy.
func (ds *DeviceSupervisor) Activate(ctx context.Context) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.state != StateClaimed {
		return fmt.Errorf("device %s must be claimed before activating (current: %s)",
			ds.device.Serial, ds.state)
	}

	old := ds.state
	ds.state = StateBusy

	_ = ds.store.LogEvent(ctx, ds.device.Serial, "activated",
		fmt.Sprintf("session=%s", ds.sessionID))

	ds.emit(SupervisorEvent{
		Serial:    ds.device.Serial,
		OldState:  old,
		NewState:  StateBusy,
		SessionID: ds.sessionID,
	})

	slog.Info("device supervisor: activated", "serial", ds.device.Serial, "session", ds.sessionID)
	return nil
}

// Release transitions the device from Claimed|Busy → Idle and closes the session.
func (ds *DeviceSupervisor) Release(ctx context.Context) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.state == StateIdle || ds.state == StateReleasing {
		return nil // already idle
	}

	old := ds.state
	ds.state = StateReleasing

	if ds.sessionID != "" {
		_ = ds.store.LogEvent(ctx, ds.device.Serial, "released",
			fmt.Sprintf("session=%s", ds.sessionID))
		ds.sessionID = ""
	}

	// Stop screen stream capture.
	_ = ds.streams.StopCapture(ctx, ds.device.Serial)

	ds.state = StateIdle
	ds.emit(SupervisorEvent{
		Serial:   ds.device.Serial,
		OldState: old,
		NewState: StateIdle,
	})

	slog.Info("device supervisor: released", "serial", ds.device.Serial)
	return nil
}

// teardown is called when the device disconnects. It releases any active session
// and frees the port.
func (ds *DeviceSupervisor) teardown(ctx context.Context) {
	ds.mu.Lock()
	agt := ds.agt
	ds.agt = nil
	if ds.sessionID != "" {
		ds.sessionID = ""
	}
	ds.mu.Unlock()

	// Stop agent outside the lock (it has its own sync).
	if agt != nil {
		agt.Stop()
	}

	// Stop screen stream capture on teardown.
	_ = ds.streams.StopCapture(ctx, ds.device.Serial)

	ds.ports.Free(ctx, ds.device.Serial)
	_ = ds.store.LogEvent(ctx, ds.device.Serial, "disconnected", "supervisor teardown")
	slog.Info("device supervisor: torn down", "serial", ds.device.Serial)
}

// emit sends a SupervisorEvent without blocking.
func (ds *DeviceSupervisor) emit(e SupervisorEvent) {
	select {
	case ds.events <- e:
	default:
		slog.Warn("device supervisor: event channel full, dropping event",
			"serial", e.Serial, "state", e.NewState)
	}
}
