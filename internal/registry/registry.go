package registry

import (
	"fmt"
	"sync"

	"protean-provider/internal/domain"
)

// Registry is a thread-safe, in-memory store of connected devices.
// It implements domain.DeviceRegistry.
type Registry struct {
	mu      sync.RWMutex
	devices map[string]*domain.Device
}

// New returns an empty, ready-to-use Registry.
func New() *Registry {
	return &Registry{
		devices: make(map[string]*domain.Device),
	}
}

// Add stores a device. Returns domain.ErrDeviceAlreadyRegistered if the serial
// is already present.
func (r *Registry) Add(device *domain.Device) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.devices[device.Serial]; exists {
		return fmt.Errorf("registry: %w (serial=%s)", domain.ErrDeviceAlreadyRegistered, device.Serial)
	}
	r.devices[device.Serial] = device
	return nil
}

// Remove deletes a device by serial. Returns domain.ErrDeviceNotFound if absent.
func (r *Registry) Remove(serial string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.devices[serial]; !exists {
		return fmt.Errorf("registry: %w (serial=%s)", domain.ErrDeviceNotFound, serial)
	}
	delete(r.devices, serial)
	return nil
}

// Get returns the device for the given serial.
// Returns domain.ErrDeviceNotFound if the serial is unknown.
func (r *Registry) Get(serial string) (*domain.Device, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	d, ok := r.devices[serial]
	if !ok {
		return nil, fmt.Errorf("registry: %w (serial=%s)", domain.ErrDeviceNotFound, serial)
	}
	return d, nil
}

// List returns a snapshot of all currently registered devices.
// The returned slice is safe to iterate after the call returns.
func (r *Registry) List() []*domain.Device {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*domain.Device, 0, len(r.devices))
	for _, d := range r.devices {
		out = append(out, d)
	}
	return out
}

// Count returns the number of registered devices.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.devices)
}