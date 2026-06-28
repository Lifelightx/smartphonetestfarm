package domain

import "context"

// DeviceTracker watches the ADB daemon for device connect/disconnect events.
// Implementations must emit one event per state change and never block the caller.
type DeviceTracker interface {
	// Watch blocks until ctx is cancelled. Events are written to ch.
	// ch should be a buffered channel to avoid dropping events.
	Watch(ctx context.Context, ch chan<- DeviceEvent) error
}

// DeviceRegistry stores the live set of connected devices.
// All methods must be safe for concurrent use.
type DeviceRegistry interface {
	Add(device *Device) error
	Remove(serial string) error
	Get(serial string) (*Device, error)
	List() []*Device
	Count() int
}

// PropertyFetcher retrieves hardware/software information from a device via ADB shell.
type PropertyFetcher interface {
	Fetch(ctx context.Context, serial string) (*Device, error)
}

// StreamManager controls per-device screen capture and input relay.
type StreamManager interface {
	StartCapture(ctx context.Context, serial string, port int) error
	StopCapture(ctx context.Context, serial string) error
	IsCapturing(serial string) bool
}

// CoordinatorClient communicates with the central STF Coordinator over gRPC.
type CoordinatorClient interface {
	Connect(ctx context.Context) error
	RegisterProvider(ctx context.Context, p *Provider) error
	RegisterDevice(ctx context.Context, d *Device) error
	UpdateDeviceState(ctx context.Context, d *Device) error
	SendHeartbeat(ctx context.Context, serial string) error
	ReleaseDevice(ctx context.Context, serial string) error
	Disconnect(ctx context.Context) error
}