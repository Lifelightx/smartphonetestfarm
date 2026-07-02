package automation

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestAndroidDriver(t *testing.T) {
	driver := NewAndroidDriver("TEST_SERIAL", nil)

	// Set up our mock runner
	var lastArgs []string
	var mockStdout string
	var mockErr error

	driver.runCmd = func(cmd *exec.Cmd) error {
		lastArgs = cmd.Args
		if mockErr != nil {
			return mockErr
		}
		// Write stdout if mockStdout is set
		if mockStdout != "" && cmd.Stdout != nil {
			_, _ = cmd.Stdout.Write([]byte(mockStdout))
		}
		return nil
	}

	ctx := context.Background()

	// 1. Test Launch
	lastArgs = nil
	mockStdout = ""
	mockErr = nil
	err := driver.Launch(ctx, "com.example.app")
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}
	expectedLaunch := []string{"adb", "-s", "TEST_SERIAL", "shell", "monkey", "-p", "com.example.app", "-c", "android.intent.category.LAUNCHER", "1"}
	if !equalSlices(lastArgs, expectedLaunch) {
		t.Errorf("Launch args mismatch.\nGot:  %v\nWant: %v", lastArgs, expectedLaunch)
	}

	// 2. Test Terminate
	lastArgs = nil
	err = driver.Terminate(ctx, "com.example.app")
	if err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}
	expectedTerminate := []string{"adb", "-s", "TEST_SERIAL", "shell", "am", "force-stop", "com.example.app"}
	if !equalSlices(lastArgs, expectedTerminate) {
		t.Errorf("Terminate args mismatch.\nGot:  %v\nWant: %v", lastArgs, expectedTerminate)
	}

	// 3. Test Tap
	// First it resolves dimensions. We need to mock "wm size" output
	lastArgs = nil
	mockStdout = "Physical size: 1080x1920\n"
	err = driver.Tap(ctx, 0.5, 0.25)
	if err != nil {
		t.Fatalf("Tap failed: %v", err)
	}
	// Tap coordinates should be: 0.5 * 1080 = 540, 0.25 * 1920 = 480
	expectedTap := []string{"adb", "-s", "TEST_SERIAL", "shell", "input", "tap", "540", "480"}
	if !equalSlices(lastArgs, expectedTap) {
		t.Errorf("Tap args mismatch.\nGot:  %v\nWant: %v", lastArgs, expectedTap)
	}

	// 4. Test Swipe
	// Dimensions should now be cached, so it won't run "wm size" again.
	lastArgs = nil
	mockStdout = ""
	err = driver.Swipe(ctx, 0.1, 0.2, 0.8, 0.9, 500)
	if err != nil {
		t.Fatalf("Swipe failed: %v", err)
	}
	// Start: 108, 384. End: 864, 1728
	expectedSwipe := []string{"adb", "-s", "TEST_SERIAL", "shell", "input", "swipe", "108", "384", "864", "1728", "500"}
	if !equalSlices(lastArgs, expectedSwipe) {
		t.Errorf("Swipe args mismatch.\nGot:  %v\nWant: %v", lastArgs, expectedSwipe)
	}

	// 5. Test Input
	lastArgs = nil
	err = driver.Input(ctx, "hello world & more")
	if err != nil {
		t.Fatalf("Input failed: %v", err)
	}
	expectedInput := []string{"adb", "-s", "TEST_SERIAL", "shell", "input", "text", "hello%sworld%s\\&%smore"}
	if !equalSlices(lastArgs, expectedInput) {
		t.Errorf("Input args mismatch.\nGot:  %v\nWant: %v", lastArgs, expectedInput)
	}

	// 6. Test Screenshot
	lastArgs = nil
	mockStdout = "\x89PNG\r\n\x1a\n..."
	data, err := driver.Screenshot(ctx)
	if err != nil {
		t.Fatalf("Screenshot failed: %v", err)
	}
	if string(data) != mockStdout {
		t.Errorf("Screenshot data mismatch. Got %q, want %q", string(data), mockStdout)
	}

	// 7. Test DumpUI
	lastArgs = nil
	mockStdout = "<hierarchy></hierarchy>"
	dump, err := driver.DumpUI(ctx)
	if err != nil {
		t.Fatalf("DumpUI failed: %v", err)
	}
	if dump != mockStdout {
		t.Errorf("DumpUI mismatch. Got %q, want %q", dump, mockStdout)
	}

	// 8. Test CurrentApp (dumpsys window success)
	lastArgs = nil
	mockStdout = "  mCurrentFocus=Window{4f20bc8 u0 com.android.settings/com.android.settings.Settings}\n"
	appInfo, err := driver.CurrentApp(ctx)
	if err != nil {
		t.Fatalf("CurrentApp failed: %v", err)
	}
	if appInfo.PackageName != "com.android.settings" || appInfo.Activity != "com.android.settings.Settings" {
		t.Errorf("CurrentApp mismatch. Got %+v", appInfo)
	}

	// 9. Test CurrentApp (dumpsys activity fallback)
	lastArgs = nil
	mockStdout = "junk\njunk\n" // dumpsys window fails to match
	driver.runCmd = func(cmd *exec.Cmd) error {
		lastArgs = cmd.Args
		// If running dumpsys window, return failure or no match
		if strings.Contains(strings.Join(cmd.Args, " "), "window") {
			_, _ = cmd.Stdout.Write([]byte("junk\n"))
			return nil
		}
		// If running dumpsys activity, return success with resumed activity
		_, _ = cmd.Stdout.Write([]byte("  mResumedActivity: ActivityRecord{9c12df8 u0 com.demo.app/.MainActivity t4}\n"))
		return nil
	}

	appInfo, err = driver.CurrentApp(ctx)
	if err != nil {
		t.Fatalf("CurrentApp fallback failed: %v", err)
	}
	if appInfo.PackageName != "com.demo.app" || appInfo.Activity != ".MainActivity" {
		t.Errorf("CurrentApp fallback mismatch. Got %+v", appInfo)
	}

	// 10. Test Install
	// Restore standard mock runner
	driver.runCmd = func(cmd *exec.Cmd) error {
		lastArgs = cmd.Args
		return nil
	}
	lastArgs = nil
	err = driver.Install(ctx, "/path/to/app.apk")
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	expectedInstall := []string{"adb", "-s", "TEST_SERIAL", "install", "-r", "-g", "/path/to/app.apk"}
	if !equalSlices(lastArgs, expectedInstall) {
		t.Errorf("Install args mismatch.\nGot:  %v\nWant: %v", lastArgs, expectedInstall)
	}

	// 11. Test Uninstall
	lastArgs = nil
	err = driver.Uninstall(ctx, "com.demo.app")
	if err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}
	expectedUninstall := []string{"adb", "-s", "TEST_SERIAL", "uninstall", "com.demo.app"}
	if !equalSlices(lastArgs, expectedUninstall) {
		t.Errorf("Uninstall args mismatch.\nGot:  %v\nWant: %v", lastArgs, expectedUninstall)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
