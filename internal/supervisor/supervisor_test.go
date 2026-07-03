package supervisor_test

import (
	"context"
	"testing"
	"time"

	"protean-provider/internal/domain"
	"protean-provider/internal/supervisor"
)

type mockStreamManager struct{}

func (m *mockStreamManager) StartCapture(ctx context.Context, serial string, port int) error {
	return nil
}
func (m *mockStreamManager) StopCapture(ctx context.Context, serial string) error {
	return nil
}
func (m *mockStreamManager) IsCapturing(serial string) bool {
	return false
}

func TestSupervisor_IOSLifecycle(t *testing.T) {
	sup, err := supervisor.New(
		context.Background(),
		"provider-test",
		nil,
		8000,
		8100,
		&mockStreamManager{},
	)
	if err != nil {
		t.Fatalf("failed to create supervisor: %v", err)
	}

	dev := &domain.Device{
		Serial:   "ios-test-udid",
		Platform: "ios",
		Info: domain.DeviceInfo{
			Manufacturer:   "Apple",
			Model:          "iPhone16,1",
			AndroidVersion: "17.4",
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = sup.OnDeviceConnected(ctx, dev)
	if err != nil {
		t.Fatalf("OnDeviceConnected failed: %v", err)
	}

	// Give it a brief moment to run and allocate port
	time.Sleep(100 * time.Millisecond)

	port, err := sup.PortOf("ios-test-udid")
	if err != nil {
		t.Fatalf("failed to get port: %v", err)
	}
	if port < 8000 || port > 8100 {
		t.Errorf("expected port in range [8000, 8100], got %d", port)
	}

	state, err := sup.StateOf("ios-test-udid")
	if err != nil {
		t.Fatalf("failed to get state: %v", err)
	}
	if state != supervisor.StateIdle {
		t.Errorf("expected StateIdle, got %s", state)
	}

	// Claim
	sessionID, err := sup.Claim(context.Background(), "ios-test-udid", "user-1")
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if sessionID == "" {
		t.Error("expected non-empty session ID")
	}

	state, err = sup.StateOf("ios-test-udid")
	if err != nil {
		t.Fatalf("failed to get state: %v", err)
	}
	if state != supervisor.StateClaimed {
		t.Errorf("expected StateClaimed, got %s", state)
	}

	// Activate
	err = sup.Activate(context.Background(), "ios-test-udid")
	if err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	state, err = sup.StateOf("ios-test-udid")
	if err != nil {
		t.Fatalf("failed to get state: %v", err)
	}
	if state != supervisor.StateBusy {
		t.Errorf("expected StateBusy, got %s", state)
	}

	// Release
	err = sup.Release(context.Background(), "ios-test-udid")
	if err != nil {
		t.Fatalf("release failed: %v", err)
	}

	state, err = sup.StateOf("ios-test-udid")
	if err != nil {
		t.Fatalf("failed to get state: %v", err)
	}
	if state != supervisor.StateIdle {
		t.Errorf("expected StateIdle, got %s", state)
	}

	// Disconnect
	sup.OnDeviceDisconnected("ios-test-udid")
}
