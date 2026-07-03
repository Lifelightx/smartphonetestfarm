package platform

import (
	"fmt"
	"sync"
)

// PlatformManager keeps track of the registered platform-specific managers and streamers.
type PlatformManager struct {
	mu         sync.RWMutex
	deviceMgrs map[Platform]DeviceManager
	appMgrs    map[Platform]AppManager
	streamers  map[Platform]Streamer
}

// NewManager initializes a new PlatformManager registry.
func NewManager() *PlatformManager {
	return &PlatformManager{
		deviceMgrs: make(map[Platform]DeviceManager),
		appMgrs:    make(map[Platform]AppManager),
		streamers:  make(map[Platform]Streamer),
	}
}

// RegisterDeviceManager registers a DeviceManager for a specific platform.
func (m *PlatformManager) RegisterDeviceManager(p Platform, dm DeviceManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deviceMgrs[p] = dm
}

// RegisterAppManager registers an AppManager for a specific platform.
func (m *PlatformManager) RegisterAppManager(p Platform, am AppManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appMgrs[p] = am
}

// RegisterStreamer registers a Streamer for a specific platform.
func (m *PlatformManager) RegisterStreamer(p Platform, s Streamer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streamers[p] = s
}

// DeviceManager returns the DeviceManager for the given platform.
func (m *PlatformManager) DeviceManager(p Platform) (DeviceManager, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dm, ok := m.deviceMgrs[p]
	if !ok {
		return nil, fmt.Errorf("device manager not registered for platform %s", p)
	}
	return dm, nil
}

// AppManager returns the AppManager for the given platform.
func (m *PlatformManager) AppManager(p Platform) (AppManager, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	am, ok := m.appMgrs[p]
	if !ok {
		return nil, fmt.Errorf("app manager not registered for platform %s", p)
	}
	return am, nil
}

// Streamer returns the Streamer for the given platform.
func (m *PlatformManager) Streamer(p Platform) (Streamer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.streamers[p]
	if !ok {
		return nil, fmt.Errorf("streamer not registered for platform %s", p)
	}
	return s, nil
}
