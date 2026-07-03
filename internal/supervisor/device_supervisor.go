package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"protean-provider/internal/adb"
	"protean-provider/internal/agent"
	"protean-provider/internal/domain"
	"protean-provider/internal/wda"
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
	ports      *PortAllocator
	events     chan<- SupervisorEvent

	mu        sync.RWMutex
	state     DeviceState
	sessionID string
	port      int
	agt       *agent.Agent // non-nil while a session is active
	iosWorker *IOSWorker   // non-nil for iOS devices
	wdaClient *wda.Client  // non-nil for iOS devices
	streams   domain.StreamManager

	cancel context.CancelFunc
	done   chan struct{}
}

// newDeviceSupervisor creates a DeviceSupervisor. Call Run to start it.
func newDeviceSupervisor(
	device *domain.Device,
	providerID string,
	adbClient adb.Client,
	ports *PortAllocator,
	events chan<- SupervisorEvent,
	streams domain.StreamManager,
) *DeviceSupervisor {
	return &DeviceSupervisor{
		device:     device,
		providerID: providerID,
		adbClient:  adbClient,
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

	// Spawn the Agent or IOS Worker for this device (using port + 3000 for WDA / WebSocket proxy)
	if strings.EqualFold(ds.device.Platform, "ios") {
		iosWorker := NewIOSWorker(ds.device.Serial, port+3000)
		ds.mu.Lock()
		ds.iosWorker = iosWorker
		ds.wdaClient = wda.NewClient(port + 3000)
		ds.mu.Unlock()

		if err := iosWorker.Start(dsCtx); err != nil {
			slog.Warn("device supervisor: ios worker start failed",
				"serial", ds.device.Serial, "err", err)
		}
	} else {
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

		// Listen to state updates from the agent (telemetry changes like battery/network)
		go func() {
			for {
				select {
				case <-dsCtx.Done():
					return
				case dev, ok := <-agt.StateUpdates:
					if !ok {
						return
					}
					ds.mu.RLock()
					state := ds.state
					sessionID := ds.sessionID
					ds.mu.RUnlock()

					ds.emit(SupervisorEvent{
						Serial:    ds.device.Serial,
						OldState:  state,
						NewState:  state,
						SessionID: sessionID,
						Device:    dev,
					})
				}
			}
		}()
	}

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

	ds.emit(SupervisorEvent{
		Serial:    ds.device.Serial,
		OldState:  old,
		NewState:  StateBusy,
		SessionID: ds.sessionID,
	})

	slog.Info("device supervisor: activated", "serial", ds.device.Serial, "session", ds.sessionID)
	return nil
}

// WDAClient returns the WDA Client instance for this device.
func (ds *DeviceSupervisor) WDAClient() *wda.Client {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.wdaClient
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
		ds.sessionID = ""
	}

	// Stop screen stream capture.
	_ = ds.streams.StopCapture(ctx, ds.device.Serial)

	// Clean up WDA session
	if ds.wdaClient != nil {
		go func(c *wda.Client) {
			delCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = c.DeleteSession(delCtx)
		}(ds.wdaClient)
	}

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
	iosWorker := ds.iosWorker
	ds.iosWorker = nil
	wdaClient := ds.wdaClient
	ds.wdaClient = nil
	if ds.sessionID != "" {
		ds.sessionID = ""
	}
	ds.mu.Unlock()

	// Stop agent outside the lock (it has its own sync).
	if agt != nil {
		agt.Stop()
	}

	// Stop iOS worker outside the lock.
	if iosWorker != nil {
		iosWorker.Stop()
	}

	// Clean up WDA session
	if wdaClient != nil {
		go func(c *wda.Client) {
			delCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = c.DeleteSession(delCtx)
		}(wdaClient)
	}

	// Stop screen stream capture on teardown.
	_ = ds.streams.StopCapture(ctx, ds.device.Serial)

	ds.ports.Free(ctx, ds.device.Serial)
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
