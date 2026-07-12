// Package goios implements native iOS communication using the go-ios library.
//
// File: client.go
// This file contains implementation and helper structures for native iOS communication using the go-ios library.

package goios

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/danielpaulus/go-ios/ios"
)

// Client defines the interface to interact with iOS device services.
type Client interface {
	Run(ctx context.Context, udid string, args ...string) ([]byte, error)
	RunNoUDID(ctx context.Context, args ...string) ([]byte, error)
}

// CLIClient implements Client by invoking the `ios` binary.
type CLIClient struct {
	binPath string
}

// Run executes the `ios` command targeting a specific device.
func (c *CLIClient) Run(ctx context.Context, udid string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"--udid=" + udid}, args...)
	return c.execute(ctx, cmdArgs...)
}

// RunNoUDID executes the `ios` command without specifying a target device (e.g., for listing devices).
func (c *CLIClient) RunNoUDID(ctx context.Context, args ...string) ([]byte, error) {
	return c.execute(ctx, args...)
}

// execute performs the execute operation.
func (c *CLIClient) execute(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.binPath, args...)
	slog.Debug("goios CLI: running command", "bin", c.binPath, "args", strings.Join(args, " "))

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("goios CLI command failed: %w (stderr: %s)", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("goios CLI run failed: %w", err)
	}

	return out, nil
}

// LibraryClient implements Client by calling go-ios Go library functions.
type LibraryClient struct {
	cliClient *CLIClient
}

// NewClient creates a Client implementation. We default to LibraryClient for performance.
func NewClient() Client {
	return &LibraryClient{
		cliClient: &CLIClient{binPath: "ios"},
	}
}

// RunNoUDID executes the `ios` command without specifying a target device.
func (l *LibraryClient) RunNoUDID(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "list" {
		slog.Debug("goios library: ListDevices")
		deviceList, err := ios.ListDevices()
		if err != nil {
			return nil, fmt.Errorf("library list devices failed: %w", err)
		}

		var entries []IOSDeviceEntry
		for _, dev := range deviceList.DeviceList {
			entry := IOSDeviceEntry{
				UDID: dev.Properties.SerialNumber,
			}

			// Try to retrieve detailed properties.
			values, err := ios.GetValues(dev)
			if err == nil {
				entry.DeviceName = values.Value.DeviceName
				entry.ProductType = values.Value.ProductType
				entry.ProductVersion = values.Value.ProductVersion
			} else {
				slog.Debug("goios library: failed to get details for device", "udid", dev.Properties.SerialNumber, "err", err)
			}
			entries = append(entries, entry)
		}

		return json.Marshal(entries)
	}

	// Fallback to CLI for any other commands
	return l.cliClient.RunNoUDID(ctx, args...)
}

// Run executes the `ios` command targeting a specific device.
func (l *LibraryClient) Run(ctx context.Context, udid string, args ...string) ([]byte, error) {
	if len(args) > 0 {
		switch args[0] {
		case "info":
			slog.Debug("goios library: info", "udid", udid, "args", args)
			device, err := ios.GetDevice(udid)
			if err != nil {
				return nil, fmt.Errorf("library get device failed: %w", err)
			}

			if len(args) > 1 && args[1] == "display" {
				// info display command queries MobileGestalt. We fall back to CLI.
				break
			}

			values, err := ios.GetValues(device)
			if err != nil {
				return nil, fmt.Errorf("library get values failed: %w", err)
			}

			return json.Marshal(values.Value)

		case "diskspace":
			slog.Debug("goios library: diskspace", "udid", udid)
			device, err := ios.GetDevice(udid)
			if err != nil {
				return nil, fmt.Errorf("library get device failed: %w", err)
			}

			lockdownConn, err := ios.ConnectLockdownWithSession(device)
			if err != nil {
				return nil, fmt.Errorf("library connect lockdown failed: %w", err)
			}
			defer lockdownConn.Close()

			val, err := lockdownConn.GetValueForDomain("", "com.apple.disk_usage")
			if err != nil {
				return nil, fmt.Errorf("library get diskspace value failed: %w", err)
			}

			return json.Marshal(val)

		case "batterycheck":
			slog.Debug("goios library: batterycheck", "udid", udid)
			device, err := ios.GetDevice(udid)
			if err != nil {
				return nil, fmt.Errorf("library get device failed: %w", err)
			}

			lockdownConn, err := ios.ConnectLockdownWithSession(device)
			if err != nil {
				return nil, fmt.Errorf("library connect lockdown failed: %w", err)
			}
			defer lockdownConn.Close()

			val, err := lockdownConn.GetValueForDomain("", "com.apple.mobile.battery")
			if err != nil {
				return nil, fmt.Errorf("library get battery value failed: %w", err)
			}

			return json.Marshal(val)
		}
	}

	// Fallback to CLI for any other commands (e.g. apps, launch, install, etc.)
	return l.cliClient.Run(ctx, udid, args...)
}
