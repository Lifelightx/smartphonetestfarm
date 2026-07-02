package domain

import (
	"context"
)

// AppInfo represents the package name and active activity of the running application.
type AppInfo struct {
	PackageName string `json:"packageName"`
	Activity    string `json:"activity"`
}

// Driver defines the cross-platform abstraction interface for automation execution.
// It is implemented by platform-specific drivers (e.g. AndroidDriver, IOSDriver).
type Driver interface {
	// Launch starts the application with the specified package name / identifier.
	Launch(ctx context.Context, appID string) error

	// Terminate force-stops the application with the specified package name / identifier.
	Terminate(ctx context.Context, appID string) error

	// Tap performs a single tap gesture at the normalized coordinates (X, Y) [0, 1].
	Tap(ctx context.Context, x, y float64) error

	// Swipe performs a swipe/drag gesture from normalized (startX, startY) to (endX, endY) with duration.
	Swipe(ctx context.Context, startX, startY, endX, endY float64, durationMs int) error

	// Input types text into the currently focused element.
	Input(ctx context.Context, text string) error

	// Screenshot captures a screenshot of the device display and returns the image bytes.
	Screenshot(ctx context.Context) ([]byte, error)

	// DumpUI dumps the current UI/Accessibility XML hierarchy tree as a string.
	DumpUI(ctx context.Context) (string, error)

	// CurrentApp retrieves the current package/app ID and active activity.
	CurrentApp(ctx context.Context) (*AppInfo, error)

	// Install installs an app package on the device given the path to the package file.
	Install(ctx context.Context, filepath string) error

	// Uninstall removes an app package from the device.
	Uninstall(ctx context.Context, appID string) error

	// ScreenSize retrieves the physical screen width and height of the device.
	ScreenSize(ctx context.Context) (width, height int32, err error)
}
