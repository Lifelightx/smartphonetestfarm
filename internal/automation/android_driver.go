package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"protean-provider/internal/agent"
	"protean-provider/internal/domain"
)

var (
	currentFocusRe    = regexp.MustCompile(`mCurrentFocus=Window\{[^{}]*?\s*([^/ \t{}]+)/([^/ \t{}]+)\}`)
	resumedActivityRe = regexp.MustCompile(`mResumedActivity:\s*ActivityRecord\{[^{}]*?\s*([^/ \t{}]+)/([^/ \t{}]+)`)
)

// AndroidDriver implements the domain.Driver interface for Android devices.
// It directly invokes the local `adb` CLI to control the device.
type AndroidDriver struct {
	serial string
	agent  *agent.Agent
	mu     sync.RWMutex
	width  int32
	height int32
	runCmd func(cmd *exec.Cmd) error
}

// NewAndroidDriver creates a new instance of AndroidDriver for the given device serial.
func NewAndroidDriver(serial string, agt *agent.Agent) *AndroidDriver {
	return &AndroidDriver{
		serial: serial,
		agent:  agt,
		runCmd: func(cmd *exec.Cmd) error {
			return cmd.Run()
		},
	}
}

// execute executes an ADB command with the device's serial and returns the trimmed output.
func (d *AndroidDriver) execute(ctx context.Context, args ...string) (string, error) {
	fullArgs := append([]string{"-s", d.serial}, args...)
	cmd := exec.CommandContext(ctx, "adb", fullArgs...)
	
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := d.runCmd(cmd)
	if err != nil {
		combined := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		return "", fmt.Errorf("adb command execution failed: %w (output: %s)", err, combined)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// resolveDimensions fetches the physical dimensions of the screen if not cached yet.
func (d *AndroidDriver) resolveDimensions(ctx context.Context) (int32, int32, error) {
	d.mu.RLock()
	w, h := d.width, d.height
	d.mu.RUnlock()

	if w > 0 && h > 0 {
		return w, h, nil
	}

	out, err := d.execute(ctx, "shell", "wm", "size")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get screen size: %w", err)
	}

	re := regexp.MustCompile(`size:\s*(\d+)x(\d+)`)
	m := re.FindStringSubmatch(out)
	if len(m) < 3 {
		return 0, 0, fmt.Errorf("invalid screen size output from wm size: %s", out)
	}

	width, _ := strconv.ParseInt(m[1], 10, 32)
	height, _ := strconv.ParseInt(m[2], 10, 32)

	d.mu.Lock()
	d.width = int32(width)
	d.height = int32(height)
	d.mu.Unlock()

	return int32(width), int32(height), nil
}

// Launch starts the application with the specified package name.
func (d *AndroidDriver) Launch(ctx context.Context, appID string) error {
	_, err := d.execute(ctx, "shell", "monkey", "-p", appID, "-c", "android.intent.category.LAUNCHER", "1")
	if err != nil {
		return fmt.Errorf("launch app: %w", err)
	}
	return nil
}

// Terminate force-stops the application with the specified package name.
func (d *AndroidDriver) Terminate(ctx context.Context, appID string) error {
	_, err := d.execute(ctx, "shell", "am", "force-stop", appID)
	if err != nil {
		return fmt.Errorf("terminate app: %w", err)
	}
	return nil
}

// Tap performs a single tap gesture at the normalized coordinates (X, Y) [0, 1].
func (d *AndroidDriver) Tap(ctx context.Context, x, y float64) error {
	if d.agent != nil {
		w, h, err := d.resolveDimensions(ctx)
		if err == nil {
			px := float32(x * float64(w))
			py := float32(y * float64(h))
			
			payload := map[string]interface{}{
				"x": px,
				"y": py,
			}
			respBytes, err := d.agent.SendCommand(ctx, "TAP", payload, 5*time.Second)
			if err == nil {
				var resp struct {
					Success bool   `json:"success"`
					Error   string `json:"error"`
				}
				if jsonErr := json.Unmarshal(respBytes, &resp); jsonErr == nil {
					if resp.Success {
						return nil
					}
					if resp.Error != "" {
						return fmt.Errorf("agent tap error: %s", resp.Error)
					}
				}
			}
		}
	}

	width, height, err := d.resolveDimensions(ctx)
	if err != nil {
		return err
	}

	absX := int(x * float64(width))
	absY := int(y * float64(height))

	_, err = d.execute(ctx, "shell", "input", "tap", strconv.Itoa(absX), strconv.Itoa(absY))
	if err != nil {
		return fmt.Errorf("tap at (%d, %d): %w", absX, absY, err)
	}
	return nil
}

// Swipe performs a swipe/drag gesture from normalized (startX, startY) to (endX, endY) with duration.
func (d *AndroidDriver) Swipe(ctx context.Context, startX, startY, endX, endY float64, durationMs int) error {
	if d.agent != nil {
		w, h, err := d.resolveDimensions(ctx)
		if err == nil {
			sx := float32(startX * float64(w))
			sy := float32(startY * float64(h))
			ex := float32(endX * float64(w))
			ey := float32(endY * float64(h))
			
			payload := map[string]interface{}{
				"startX":     sx,
				"startY":     sy,
				"endX":       ex,
				"endY":       ey,
				"durationMs": durationMs,
			}
			respBytes, err := d.agent.SendCommand(ctx, "SWIPE", payload, 10*time.Second)
			if err == nil {
				var resp struct {
					Success bool   `json:"success"`
					Error   string `json:"error"`
				}
				if jsonErr := json.Unmarshal(respBytes, &resp); jsonErr == nil {
					if resp.Success {
						return nil
					}
					if resp.Error != "" {
						return fmt.Errorf("agent swipe error: %s", resp.Error)
					}
				}
			}
		}
	}

	width, height, err := d.resolveDimensions(ctx)
	if err != nil {
		return err
	}

	absStartX := int(startX * float64(width))
	absStartY := int(startY * float64(height))
	absEndX := int(endX * float64(width))
	absEndY := int(endY * float64(height))

	args := []string{
		"shell", "input", "swipe",
		strconv.Itoa(absStartX), strconv.Itoa(absStartY),
		strconv.Itoa(absEndX), strconv.Itoa(absEndY),
	}
	if durationMs > 0 {
		args = append(args, strconv.Itoa(durationMs))
	}

	_, err = d.execute(ctx, args...)
	if err != nil {
		return fmt.Errorf("swipe from (%d, %d) to (%d, %d): %w", absStartX, absStartY, absEndX, absEndY, err)
	}
	return nil
}

// Input types text into the currently focused element.
func (d *AndroidDriver) Input(ctx context.Context, text string) error {
	if d.agent != nil {
		payload := map[string]interface{}{
			"text": text,
		}
		respBytes, err := d.agent.SendCommand(ctx, "INPUT", payload, 5*time.Second)
		if err == nil {
			var resp struct {
				Success bool   `json:"success"`
				Error   string `json:"error"`
			}
			if jsonErr := json.Unmarshal(respBytes, &resp); jsonErr == nil {
				if resp.Success {
					return nil
				}
				if resp.Error != "" {
					return fmt.Errorf("agent input error: %s", resp.Error)
				}
			}
		}
	}

	// adb input text requires spaces to be passed as %s
	escaped := strings.ReplaceAll(text, " ", "%s")

	// Escape shell characters
	escaped = strings.ReplaceAll(escaped, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	escaped = strings.ReplaceAll(escaped, "`", "\\`")
	escaped = strings.ReplaceAll(escaped, "$", "\\$")
	escaped = strings.ReplaceAll(escaped, "&", "\\&")
	escaped = strings.ReplaceAll(escaped, "|", "\\|")
	escaped = strings.ReplaceAll(escaped, ";", "\\;")
	escaped = strings.ReplaceAll(escaped, "<", "\\<")
	escaped = strings.ReplaceAll(escaped, ">", "\\>")
	escaped = strings.ReplaceAll(escaped, "(", "\\(")
	escaped = strings.ReplaceAll(escaped, ")", "\\)")

	_, err := d.execute(ctx, "shell", "input", "text", escaped)
	if err != nil {
		return fmt.Errorf("input text: %w", err)
	}
	return nil
}

// Screenshot captures a screenshot of the device display and returns the image bytes.
func (d *AndroidDriver) Screenshot(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "adb", "-s", d.serial, "exec-out", "screencap", "-p")
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := d.runCmd(cmd); err != nil {
		return nil, fmt.Errorf("screenshot failed: %w (stderr: %s)", err, strings.TrimSpace(stderrBuf.String()))
	}
	return stdoutBuf.Bytes(), nil
}

// DumpUI dumps the current UI/Accessibility XML hierarchy tree as a string.
func (d *AndroidDriver) DumpUI(ctx context.Context) (string, error) {
	if d.agent != nil {
		respBytes, err := d.agent.SendCommand(ctx, "DUMP_UI", nil, 10*time.Second)
		if err == nil {
			var resp struct {
				Success bool   `json:"success"`
				XML     string `json:"xml"`
				Error   string `json:"error"`
			}
			if jsonErr := json.Unmarshal(respBytes, &resp); jsonErr == nil {
				if resp.Success {
					return resp.XML, nil
				}
				if resp.Error != "" {
					return "", fmt.Errorf("agent dump UI error: %s", resp.Error)
				}
			}
		}
	}

	const remotePath = "/data/local/tmp/uidump.xml"
	_, err := d.execute(ctx, "shell", "uiautomator", "dump", remotePath)
	if err != nil {
		return "", fmt.Errorf("dump UI: failed to generate dump: %w", err)
	}

	dump, err := d.execute(ctx, "shell", "cat", remotePath)
	if err != nil {
		return "", fmt.Errorf("dump UI: failed to read dump: %w", err)
	}

	// Clean up the temp XML file
	cleanupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	_, _ = d.execute(cleanupCtx, "shell", "rm", "-f", remotePath)
	cancel()

	return dump, nil
}

// CurrentApp retrieves the current package name and active activity.
func (d *AndroidDriver) CurrentApp(ctx context.Context) (*domain.AppInfo, error) {
	// 1. Try dumpsys window first
	out, err := d.execute(ctx, "shell", "dumpsys", "window", "windows")
	if err == nil {
		lines := strings.Split(out, "\n")
		for _, line := range lines {
			if strings.Contains(line, "mCurrentFocus") {
				m := currentFocusRe.FindStringSubmatch(line)
				if len(m) >= 3 {
					return &domain.AppInfo{
						PackageName: m[1],
						Activity:    m[2],
					}, nil
				}
			}
		}
	}

	// 2. Try dumpsys activity activities as fallback
	out, err = d.execute(ctx, "shell", "dumpsys", "activity", "activities")
	if err == nil {
		lines := strings.Split(out, "\n")
		for _, line := range lines {
			if strings.Contains(line, "mResumedActivity") {
				m := resumedActivityRe.FindStringSubmatch(line)
				if len(m) >= 3 {
					return &domain.AppInfo{
						PackageName: m[1],
						Activity:    m[2],
					}, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("failed to retrieve current package and activity")
}

// Install installs an app package on the device given the path to the package file.
func (d *AndroidDriver) Install(ctx context.Context, filepath string) error {
	_, err := d.execute(ctx, "install", "-r", "-g", filepath)
	if err != nil {
		return fmt.Errorf("install apk: %w", err)
	}
	return nil
}

// Uninstall removes an app package from the device.
func (d *AndroidDriver) Uninstall(ctx context.Context, appID string) error {
	_, err := d.execute(ctx, "uninstall", appID)
	if err != nil {
		return fmt.Errorf("uninstall package: %w", err)
	}
	return nil
}

// ScreenSize retrieves the physical screen width and height of the device.
func (d *AndroidDriver) ScreenSize(ctx context.Context) (width, height int32, err error) {
	return d.resolveDimensions(ctx)
}

