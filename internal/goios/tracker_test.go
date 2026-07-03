package goios_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"protean-provider/internal/domain"
	"protean-provider/internal/goios"
)

func TestTracker_Watch(t *testing.T) {
	discoverCount := 0
	mock := &mockCLIClient{
		runNoUDIDFunc: func(ctx context.Context, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "list" {
				discoverCount++
				if discoverCount == 1 {
					return []byte(`[{"udid": "ios-device-1", "name": "iPhone 15", "type": "iPhone16,1", "version": "17.4"}]`), nil
				}
				return []byte(`[]`), nil
			}
			return nil, errors.New("unexpected command")
		},
		runFunc: func(ctx context.Context, udid string, args ...string) ([]byte, error) {
			return []byte(`{"ProductType": "iPhone16,1", "ProductVersion": "17.4"}`), nil
		},
	}

	dm := goios.NewDeviceManager(mock)
	tracker := goios.NewTracker(dm, 10*time.Millisecond)

	ch := make(chan domain.DeviceEvent, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = tracker.Watch(ctx, ch)
	}()

	// Verify EventConnected
	select {
	case eventConnected := <-ch:
		if eventConnected.Type != domain.EventConnected {
			t.Errorf("expected EventConnected, got %s", eventConnected.Type)
		}
		if eventConnected.Serial != "ios-device-1" {
			t.Errorf("expected serial ios-device-1, got %s", eventConnected.Serial)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for EventConnected")
	}

	// Verify EventDisconnected
	select {
	case eventDisconnected := <-ch:
		if eventDisconnected.Type != domain.EventDisconnected {
			t.Errorf("expected EventDisconnected, got %s", eventDisconnected.Type)
		}
		if eventDisconnected.Serial != "ios-device-1" {
			t.Errorf("expected serial ios-device-1, got %s", eventDisconnected.Serial)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for EventDisconnected")
	}
}
