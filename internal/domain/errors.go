package domain

import "errors"

// Sentinel errors used throughout the provider.
// Use errors.Is() to check for these.
var (
	// ErrDeviceNotFound is returned when a device with the given serial is not in the registry.
	ErrDeviceNotFound = errors.New("device not found")

	// ErrDeviceAlreadyRegistered is returned when trying to add a device that already exists.
	ErrDeviceAlreadyRegistered = errors.New("device already registered")

	// ErrADBUnavailable is returned when the local ADB daemon cannot be reached.
	ErrADBUnavailable = errors.New("adb daemon unavailable")

	// ErrPropertyFetchTimeout is returned when fetching device properties exceeds the configured timeout.
	ErrPropertyFetchTimeout = errors.New("device property fetch timed out")

	// ErrCoordinatorUnreachable is returned when the coordinator gRPC endpoint is unreachable.
	ErrCoordinatorUnreachable = errors.New("coordinator unreachable")

	// ErrStreamAlreadyActive is returned when StartCapture is called for an already-streaming device.
	ErrStreamAlreadyActive = errors.New("stream already active for device")

	// ErrStreamNotActive is returned when StopCapture is called for a device that is not streaming.
	ErrStreamNotActive = errors.New("stream not active for device")

	// ErrInvalidConfig is returned when configuration validation fails.
	ErrInvalidConfig = errors.New("invalid configuration")
)
