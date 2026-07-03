package wda_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"protean-provider/internal/wda"
)

func TestWDA_ClientLifecycle(t *testing.T) {
	// Spin up test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": {"sessionId": "wda-session-123", "capabilities": {}}}`))
		case "/session/wda-session-123":
			w.WriteHeader(http.StatusOK)
		case "/session/wda-session-123/actions":
			w.WriteHeader(http.StatusOK)
		case "/session/wda-session-123/wda/keys":
			w.WriteHeader(http.StatusOK)
		case "/session/wda-session-123/screenshot", "/screenshot":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": "dGVzdA=="}`)) // base64 of "test"
		case "/session/wda-session-123/wda/apps/launch":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse server url: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	client := wda.NewClient(port)

	// Create Session
	sessionID, err := client.CreateSession(context.Background())
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	if sessionID != "wda-session-123" {
		t.Errorf("expected session ID wda-session-123, got %s", sessionID)
	}

	// Tap
	err = client.Tap(context.Background(), 100, 200)
	if err != nil {
		t.Errorf("tap failed: %v", err)
	}

	// Swipe
	err = client.Swipe(context.Background(), 100, 200, 300, 400, 1.0)
	if err != nil {
		t.Errorf("swipe failed: %v", err)
	}

	// SendKeys
	err = client.SendKeys(context.Background(), "hello")
	if err != nil {
		t.Errorf("sendkeys failed: %v", err)
	}

	// Screenshot
	img, err := client.Screenshot(context.Background())
	if err != nil {
		t.Errorf("screenshot failed: %v", err)
	}
	if string(img) != "test" {
		t.Errorf("expected screenshot data 'test', got %q", string(img))
	}

	// LaunchApp
	err = client.LaunchApp(context.Background(), "com.apple.Preferences")
	if err != nil {
		t.Errorf("launch app failed: %v", err)
	}

	// Delete Session
	err = client.DeleteSession(context.Background())
	if err != nil {
		t.Errorf("delete session failed: %v", err)
	}
}
