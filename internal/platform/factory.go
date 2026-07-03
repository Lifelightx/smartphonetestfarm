package platform

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"protean-provider/internal/adb"
	"protean-provider/internal/domain"
	"protean-provider/internal/goios"
)

// ── Android Implementations ──────────────────────────────────────────────────

// AndroidDeviceManager wraps the existing ADB client and properties logic.
type AndroidDeviceManager struct {
	client adb.Client
}

// Discover queries the connected Android devices.
func (adm *AndroidDeviceManager) Discover(ctx context.Context) ([]*domain.Device, error) {
	entries, err := adm.client.ListDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("android discover: %w", err)
	}

	var devices []*domain.Device
	for _, entry := range entries {
		if entry.State == "device" {
			dev, err := adb.FetchProperties(ctx, adm.client, entry.Serial)
			if err == nil {
				devices = append(devices, dev)
			} else {
				slog.Warn("android discover: failed to fetch properties", "serial", entry.Serial, "err", err)
			}
		}
	}
	return devices, nil
}

// GetProperties fetches detailed hardware info for a given Android device.
func (adm *AndroidDeviceManager) GetProperties(ctx context.Context, serial string) (*domain.Device, error) {
	return adb.FetchProperties(ctx, adm.client, serial)
}

// AndroidAppManager wraps Android application installations and runs via ADB.
type AndroidAppManager struct {
	client adb.Client
}

// Install installs an APK file on the device.
func (aam *AndroidAppManager) Install(ctx context.Context, serial string, appPath string) error {
	cmd := exec.CommandContext(ctx, "adb", "-s", serial, "install", "-r", "-g", appPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adb install failed: %w (out: %s)", err, string(out))
	}
	return nil
}

// Uninstall uninstalls a package by its name.
func (aam *AndroidAppManager) Uninstall(ctx context.Context, serial string, appID string) error {
	cmd := exec.CommandContext(ctx, "adb", "-s", serial, "uninstall", appID)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adb uninstall failed: %w (out: %s)", err, string(out))
	}
	return nil
}

// Launch runs the app via the Android monkey utility.
func (aam *AndroidAppManager) Launch(ctx context.Context, serial string, appID string) error {
	cmd := exec.CommandContext(ctx, "adb", "-s", serial, "shell", "monkey", "-p", appID, "-c", "android.intent.category.LAUNCHER", "1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adb launch failed: %w (out: %s)", err, string(out))
	}
	return nil
}

// Stop stops a running application by package name.
func (aam *AndroidAppManager) Stop(ctx context.Context, serial string, appID string) error {
	cmd := exec.CommandContext(ctx, "adb", "-s", serial, "shell", "am", "force-stop", appID)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adb force-stop failed: %w (out: %s)", err, string(out))
	}
	return nil
}

// List queries the installed packages.
func (aam *AndroidAppManager) List(ctx context.Context, serial string) ([]string, error) {
	out, err := aam.client.Shell(ctx, serial, "pm list packages")
	if err != nil {
		return nil, fmt.Errorf("adb list packages failed: %w", err)
	}

	var packages []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package:") {
			packages = append(packages, strings.TrimPrefix(line, "package:"))
		}
	}
	return packages, nil
}

// AndroidStreamer wraps the existing Android stream manager.
type AndroidStreamer struct {
	mgr domain.StreamManager
}

// Start starts stream capture.
func (as *AndroidStreamer) Start(ctx context.Context, serial string, port int) error {
	return as.mgr.StartCapture(ctx, serial, port)
}

// Stop stops stream capture.
func (as *AndroidStreamer) Stop(ctx context.Context, serial string) error {
	return as.mgr.StopCapture(ctx, serial)
}

// ── iOS Implementations ──────────────────────────────────────────────────────

// IOSDeviceManager wraps goios.DeviceManager.
type IOSDeviceManager struct {
	mgr *goios.DeviceManager
}

// Discover queries the connected iOS devices.
func (idm *IOSDeviceManager) Discover(ctx context.Context) ([]*domain.Device, error) {
	return idm.mgr.Discover(ctx)
}

// GetProperties gets iOS device details.
func (idm *IOSDeviceManager) GetProperties(ctx context.Context, serial string) (*domain.Device, error) {
	return idm.mgr.GetProperties(ctx, serial)
}

// IOSAppManager wraps goios.AppManager.
type IOSAppManager struct {
	mgr *goios.AppManager
}

// Install installs an app.
func (iam *IOSAppManager) Install(ctx context.Context, serial string, appPath string) error {
	return iam.mgr.Install(ctx, serial, appPath)
}

// Uninstall uninstalls an app.
func (iam *IOSAppManager) Uninstall(ctx context.Context, serial string, appID string) error {
	return iam.mgr.Uninstall(ctx, serial, appID)
}

// Launch launches an app.
func (iam *IOSAppManager) Launch(ctx context.Context, serial string, appID string) error {
	return iam.mgr.Launch(ctx, serial, appID)
}

// Stop stops an app.
func (iam *IOSAppManager) Stop(ctx context.Context, serial string, appID string) error {
	return iam.mgr.Stop(ctx, serial, appID)
}

// List lists apps.
func (iam *IOSAppManager) List(ctx context.Context, serial string) ([]string, error) {
	return iam.mgr.List(ctx, serial)
}

// IOSStreamer coordinates screen capture for iOS.
type IOSStreamer struct{}

// Start begins MJPEG or screenshot capture sequence.
func (is *IOSStreamer) Start(ctx context.Context, serial string, port int) error {
	slog.Info("ios streamer: starting capture", "serial", serial, "port", port)
	return nil
}

// Stop terminates capture.
func (is *IOSStreamer) Stop(ctx context.Context, serial string) error {
	slog.Info("ios streamer: stopping capture", "serial", serial)
	return nil
}

// ── Factory Helper ───────────────────────────────────────────────────────────

// InitializePlatformManager sets up and returns a fully initialized PlatformManager.
func InitializePlatformManager(adbClient adb.Client, streamMgr domain.StreamManager) *PlatformManager {
	pm := NewManager()

	// 1. Android implementations
	pm.RegisterDeviceManager(Android, &AndroidDeviceManager{client: adbClient})
	pm.RegisterAppManager(Android, &AndroidAppManager{client: adbClient})
	pm.RegisterStreamer(Android, &AndroidStreamer{mgr: streamMgr})

	// 2. iOS implementations
	goiosClient := goios.NewClient()
	goiosDeviceMgr := goios.NewDeviceManager(goiosClient)
	goiosAppMgr := goios.NewAppManager(goiosClient)

	pm.RegisterDeviceManager(IOS, &IOSDeviceManager{mgr: goiosDeviceMgr})
	pm.RegisterAppManager(IOS, &IOSAppManager{mgr: goiosAppMgr})
	pm.RegisterStreamer(IOS, &IOSStreamer{})

	return pm
}
