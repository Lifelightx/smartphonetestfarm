// Package supervisor manages per-device lifecycle goroutines for all devices
// connected to this provider.
//
// The Supervisor is the top-level manager. When a device connects it spawns
// a DeviceSupervisor; when it disconnects it tears it down. All DeviceSupervisors
// share a single PortAllocator and DB store.
package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"protean-provider/internal/adb"
	"protean-provider/internal/agent"
	"protean-provider/internal/db"
	"protean-provider/internal/domain"
)

const eventBufferSize = 128

// Supervisor manages all per-device supervisors.
type Supervisor struct {
	providerID string
	adbClient  adb.Client
	store      *db.DB
	ports      *PortAllocator
	streams    domain.StreamManager

	mu      sync.RWMutex
	devices map[string]*DeviceSupervisor // serial → supervisor

	// Events published by any DeviceSupervisor. Consumers can watch this channel.
	Events <-chan SupervisorEvent
	events chan SupervisorEvent
}

// New creates a Supervisor. The PortAllocator is initialised from the DB so
// existing allocations survive restarts.
func New(ctx context.Context, providerID string, adbClient adb.Client, store *db.DB, minPort, maxPort int, streams domain.StreamManager) (*Supervisor, error) {
	ports, err := NewPortAllocator(ctx, store, minPort, maxPort)
	if err != nil {
		return nil, fmt.Errorf("supervisor: %w", err)
	}

	events := make(chan SupervisorEvent, eventBufferSize)
	s := &Supervisor{
		providerID: providerID,
		adbClient:  adbClient,
		store:      store,
		ports:      ports,
		streams:    streams,
		devices:    make(map[string]*DeviceSupervisor),
		events:     events,
		Events:     events,
	}
	return s, nil
}

// OnDeviceConnected creates and starts a DeviceSupervisor for the given device.
// It is safe to call concurrently.
func (s *Supervisor) OnDeviceConnected(ctx context.Context, device *domain.Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.devices[device.Serial]; exists {
		slog.Warn("supervisor: device already supervised — ignoring duplicate connect",
			"serial", device.Serial)
		return nil
	}

	ds := newDeviceSupervisor(device, s.providerID, s.adbClient, s.store, s.ports, s.events, s.streams)
	s.devices[device.Serial] = ds

	go ds.Run(ctx)

	slog.Info("supervisor: device supervised", "serial", device.Serial, "total", len(s.devices))
	return nil
}

// OnDeviceDisconnected stops and removes the DeviceSupervisor for the serial.
func (s *Supervisor) OnDeviceDisconnected(serial string) {
	s.mu.Lock()
	ds, exists := s.devices[serial]
	if exists {
		delete(s.devices, serial)
	}
	s.mu.Unlock()

	if !exists {
		slog.Warn("supervisor: disconnect for unknown device", "serial", serial)
		return
	}

	ds.Stop()
	slog.Info("supervisor: device removed", "serial", serial, "total", s.count())
}

// Claim delegates to the DeviceSupervisor for the given serial.
func (s *Supervisor) Claim(ctx context.Context, serial, claimedBy string) (sessionID string, err error) {
	ds, err := s.get(serial)
	if err != nil {
		return "", err
	}
	return ds.Claim(ctx, claimedBy)
}

// Activate delegates to the DeviceSupervisor for the given serial.
func (s *Supervisor) Activate(ctx context.Context, serial string) error {
	ds, err := s.get(serial)
	if err != nil {
		return err
	}
	return ds.Activate(ctx)
}

// Release delegates to the DeviceSupervisor for the given serial.
func (s *Supervisor) Release(ctx context.Context, serial string) error {
	ds, err := s.get(serial)
	if err != nil {
		return err
	}
	return ds.Release(ctx)
}

// StateOf returns the current DeviceState for a serial.
func (s *Supervisor) StateOf(serial string) (DeviceState, error) {
	ds, err := s.get(serial)
	if err != nil {
		return StateIdle, err
	}
	return ds.State(), nil
}

// PortOf returns the allocated port for a serial.
func (s *Supervisor) PortOf(serial string) (int, error) {
	ds, err := s.get(serial)
	if err != nil {
		return 0, err
	}
	return ds.Port(), nil
}

// Agent returns the active Agent for a device serial, if one exists.
// Returns nil if the device has no active session.
func (s *Supervisor) Agent(serial string) *agent.Agent {
	s.mu.RLock()
	ds, ok := s.devices[serial]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	ds.mu.RLock()
	agt := ds.agt
	ds.mu.RUnlock()
	return agt
}

// StopAll gracefully stops all device supervisors.
// Called during provider shutdown.
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	serials := make([]string, 0, len(s.devices))
	for serial := range s.devices {
		serials = append(serials, serial)
	}
	s.mu.Unlock()

	for _, serial := range serials {
		s.OnDeviceDisconnected(serial)
	}
	slog.Info("supervisor: all devices stopped")
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (s *Supervisor) get(serial string) (*DeviceSupervisor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ds, ok := s.devices[serial]
	if !ok {
		return nil, fmt.Errorf("supervisor: no supervisor for device %s", serial)
	}
	return ds, nil
}

func (s *Supervisor) count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.devices)
}