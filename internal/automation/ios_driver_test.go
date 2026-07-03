package automation_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"protean-provider/internal/automation"
)

func TestIOSDriver(t *testing.T) {
	// Spin up test HTTP server to mock WebDriverAgent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": {"sessionId": "wda-session-ios-123", "capabilities": {}}}`))
		case "/session/wda-session-ios-123/wda/apps/launch":
			w.WriteHeader(http.StatusOK)
		case "/session/wda-session-ios-123/wda/apps/terminate":
			w.WriteHeader(http.StatusOK)
		case "/session/wda-session-ios-123/wda/tap":
			w.WriteHeader(http.StatusOK)
		case "/session/wda-session-ios-123/wda/dragfromtoforduration":
			w.WriteHeader(http.StatusOK)
		case "/session/wda-session-ios-123/wda/keys":
			w.WriteHeader(http.StatusOK)
		case "/session/wda-session-ios-123/screenshot":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": "dGVzdA=="}`)) // base64 of "test"
		case "/session/wda-session-ios-123/source":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": "<XCUIElementTypeApplication name=\"springboard\" />"}`))
		case "/session/wda-session-ios-123/window/size":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": {"width": 375, "height": 812}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse url: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	driver := automation.NewIOSDriver(port)

	ctx := context.Background()

	if err := driver.Launch(ctx, "com.apple.Preferences"); err != nil {
		t.Errorf("Launch failed: %v", err)
	}

	if err := driver.Tap(ctx, 0.5, 0.5); err != nil {
		t.Errorf("Tap failed: %v", err)
	}

	if err := driver.Swipe(ctx, 0.1, 0.2, 0.3, 0.4, 200); err != nil {
		t.Errorf("Swipe failed: %v", err)
	}

	if err := driver.Input(ctx, "hello"); err != nil {
		t.Errorf("Input failed: %v", err)
	}

	img, err := driver.Screenshot(ctx)
	if err != nil {
		t.Errorf("Screenshot failed: %v", err)
	}
	if string(img) != "test" {
		t.Errorf("expected screenshot data 'test', got %s", string(img))
	}

	xml, err := driver.DumpUI(ctx)
	if err != nil {
		t.Errorf("DumpUI failed: %v", err)
	}
	if xml != "<XCUIElementTypeApplication name=\"springboard\" />" {
		t.Errorf("expected UI dump, got %s", xml)
	}

	if err := driver.Terminate(ctx, "com.apple.Preferences"); err != nil {
		t.Errorf("Terminate failed: %v", err)
	}
}
