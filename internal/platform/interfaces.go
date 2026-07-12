// Package platform implements cross-platform device managers and factory interfaces.
//
// File: interfaces.go
// This file contains implementation and helper structures for cross-platform device managers and factory interfaces.

package platform

import (
	"context"
	"protean-provider/internal/domain"
)

// Platform represents the operating system type of a mobile device.
type Platform string

const (
	Android Platform = "android"
	IOS     Platform = "ios"
)

// DeviceManager defines platform-agnostic operations for discovering and managing devices.
type DeviceManager interface {
	Discover(ctx context.Context) ([]*domain.Device, error)
	GetProperties(ctx context.Context, serial string) (*domain.Device, error)
}

// AppManager defines platform-agnostic operations for installing, launching, and managing apps.
type AppManager interface {
	Install(ctx context.Context, serial string, appPath string) error
	Uninstall(ctx context.Context, serial string, appID string) error
	Launch(ctx context.Context, serial string, appID string) error
	Stop(ctx context.Context, serial string, appID string) error
	List(ctx context.Context, serial string) ([]string, error)
}

// Streamer defines platform-agnostic operations for starting and stopping screen streaming.
type Streamer interface {
	Start(ctx context.Context, serial string, port int) error
	Stop(ctx context.Context, serial string) error
}
