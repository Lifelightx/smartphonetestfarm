package automation

import (
	"context"
	"fmt"

	"protean-provider/internal/domain"
	"protean-provider/internal/wda"
)

// IOSDriver implements the domain.Driver interface for iOS devices using WebDriverAgent.
type IOSDriver struct {
	client *wda.Client
}

// NewIOSDriver creates a new instance of IOSDriver.
func NewIOSDriver(wdaPort int) *IOSDriver {
	return &IOSDriver{
		client: wda.NewClient(wdaPort),
	}
}

// NewIOSDriverWithClient creates a new instance of IOSDriver with an existing client.
func NewIOSDriverWithClient(client *wda.Client) *IOSDriver {
	return &IOSDriver{
		client: client,
	}
}

// ensureSession makes sure a valid WDA session exists.
func (d *IOSDriver) ensureSession(ctx context.Context) error {
	return d.client.EnsureSession(ctx)
}

// Launch starts the application with the specified bundle ID.
func (d *IOSDriver) Launch(ctx context.Context, appID string) error {
	if err := d.ensureSession(ctx); err != nil {
		return err
	}
	return d.client.LaunchApp(ctx, appID)
}

// Terminate force-stops the application with the specified bundle ID.
func (d *IOSDriver) Terminate(ctx context.Context, appID string) error {
	if err := d.ensureSession(ctx); err != nil {
		return err
	}
	return d.client.TerminateApp(ctx, appID)
}

// Tap performs a single tap gesture at the normalized coordinates (X, Y) [0, 1].
func (d *IOSDriver) Tap(ctx context.Context, x, y float64) error {
	if err := d.ensureSession(ctx); err != nil {
		return err
	}
	w, h, err := d.ScreenSize(ctx)
	if err != nil {
		return err
	}
	absX := x * float64(w)
	absY := y * float64(h)
	return d.client.Tap(ctx, absX, absY)
}

// Swipe performs a swipe/drag gesture from normalized (startX, startY) to (endX, endY) with duration.
func (d *IOSDriver) Swipe(ctx context.Context, startX, startY, endX, endY float64, durationMs int) error {
	if err := d.ensureSession(ctx); err != nil {
		return err
	}
	w, h, err := d.ScreenSize(ctx)
	if err != nil {
		return err
	}
	x1 := startX * float64(w)
	y1 := startY * float64(h)
	x2 := endX * float64(w)
	y2 := endY * float64(h)

	durationSec := float64(durationMs) / 1000.0
	if durationSec <= 0 {
		durationSec = 0.5
	}
	return d.client.Swipe(ctx, x1, y1, x2, y2, durationSec)
}

// Input types text into the currently focused element.
func (d *IOSDriver) Input(ctx context.Context, text string) error {
	if err := d.ensureSession(ctx); err != nil {
		return err
	}
	return d.client.SendKeys(ctx, text)
}

// Screenshot captures a screenshot of the device display and returns the image bytes.
func (d *IOSDriver) Screenshot(ctx context.Context) ([]byte, error) {
	if err := d.ensureSession(ctx); err != nil {
		return nil, err
	}
	return d.client.Screenshot(ctx)
}

// DumpUI dumps the current UI/Accessibility XML hierarchy tree as a string.
func (d *IOSDriver) DumpUI(ctx context.Context) (string, error) {
	if err := d.ensureSession(ctx); err != nil {
		return "", err
	}
	var resp struct {
		Value string `json:"value"`
	}
	urlPath := fmt.Sprintf("/session/%s/source?format=xml", d.client.SessionID())
	if err := d.client.Request(ctx, "GET", urlPath, nil, &resp); err != nil {
		// Fallback to sessionless source
		if fbErr := d.client.Request(ctx, "GET", "/source?format=xml", nil, &resp); fbErr != nil {
			return "", fmt.Errorf("wda source request failed: %w (fallback error: %v)", err, fbErr)
		}
	}
	return resp.Value, nil
}

// CurrentApp retrieves the current package/app ID and active activity.
func (d *IOSDriver) CurrentApp(ctx context.Context) (*domain.AppInfo, error) {
	// iOS doesn't map easily to Android's activity stack. We return springboard/placeholder
	return &domain.AppInfo{
		PackageName: "com.apple.springboard",
		Activity:    "SpringBoard",
	}, nil
}

// Install installs an app package on the device.
func (d *IOSDriver) Install(ctx context.Context, filepath string) error {
	// Handled externally or by installer helpers. Placeholder to satisfy interface.
	return nil
}

// Uninstall removes an app package from the device.
func (d *IOSDriver) Uninstall(ctx context.Context, appID string) error {
	// Handled externally or by installer helpers. Placeholder to satisfy interface.
	return nil
}

// ScreenSize retrieves the physical screen width and height of the device.
func (d *IOSDriver) ScreenSize(ctx context.Context) (width, height int32, err error) {
	if err := d.ensureSession(ctx); err != nil {
		return 0, 0, err
	}
	var resp struct {
		Value struct {
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		} `json:"value"`
	}
	urlPath := fmt.Sprintf("/session/%s/window/size", d.client.SessionID())
	if err := d.client.Request(ctx, "GET", urlPath, nil, &resp); err != nil {
		// Return standard fallback
		return 390, 844, nil
	}
	return int32(resp.Value.Width), int32(resp.Value.Height), nil
}
