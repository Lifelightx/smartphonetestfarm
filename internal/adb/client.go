package adb

import (
	"context"
	"fmt"
	"strings"

	adbkit "github.com/codeskyblue/go-adbkit/adb"
)

// DeviceEntry is a minimal representation of an ADB device list entry.
type DeviceEntry struct {
	Serial string
	State  string // "device", "offline", "unauthorized", "recovery", etc.
}

// Client wraps the go-adbkit ADB client with a clean, context-aware interface.
// All methods are safe for concurrent use.
type Client interface {
	ListDevices(ctx context.Context) ([]DeviceEntry, error)
	Shell(ctx context.Context, serial, cmd string) (string, error)
}

// concreteClient is the real implementation backed by go-adbkit.
type concreteClient struct {
	inner *adbkit.Client
}

// NewClient creates an ADB client that talks to the local ADB daemon.
// host is typically "127.0.0.1", port is typically 5037.
func NewClient(host string, port int) (Client, error) {
	c := adbkit.NewClientWithOptions(adbkit.ClientOptions{
		Host: host,
		Port: port,
	})
	return &concreteClient{inner: c}, nil
}

// ListDevices returns every device currently known to the ADB daemon,
// regardless of state (online, offline, unauthorized, …).
func (c *concreteClient) ListDevices(ctx context.Context) ([]DeviceEntry, error) {
	// go-adbkit's ListDevices does not take a context; we can't propagate
	// cancellation into it, but we check ctx.Err() before and after.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	devices, err := c.inner.ListDevices()
	if err != nil {
		return nil, fmt.Errorf("adb: list devices: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries := make([]DeviceEntry, 0, len(devices))
	for _, d := range devices {
		entries = append(entries, DeviceEntry{
			Serial: d.Serial,
			State:  d.State,
		})
	}
	return entries, nil
}

// Shell runs a single shell command on the device and returns stdout as a
// trimmed string. Returns an error if the command cannot be dispatched
// (e.g. device is offline or unauthorized).
func (c *concreteClient) Shell(ctx context.Context, serial, cmd string) (string, error) {
	device := c.inner.Device(adbkit.DeviceWithSerial(serial))
	out, err := device.RunCommandContext(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("adb shell %q on %s: %w", cmd, serial, err)
	}
	return strings.TrimSpace(out), nil
}