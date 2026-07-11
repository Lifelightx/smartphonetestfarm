package goios_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danielpaulus/go-ios/ios"
	"protean-provider/internal/domain"
	"protean-provider/internal/goios"
)

func TestTracker_Watch(t *testing.T) {
	mock := &mockCLIClient{
		runFunc: func(ctx context.Context, udid string, args ...string) ([]byte, error) {
			return []byte(`{"ProductType": "iPhone16,1", "ProductVersion": "17.4"}`), nil
		},
	}

	eventChan := make(chan ios.AttachedMessage, 5)
	eventChan <- ios.AttachedMessage{
		MessageType: "Attached",
		Properties: ios.DeviceProperties{
			SerialNumber: "ios-device-1",
		},
	}
	eventChan <- ios.AttachedMessage{
		MessageType: "Detached",
		Properties: ios.DeviceProperties{
			SerialNumber: "ios-device-1",
		},
	}

	mockListen := func() (func() (ios.AttachedMessage, error), func() error, error) {
		listenFunc := func() (ios.AttachedMessage, error) {
			select {
			case msg := <-eventChan:
				return msg, nil
			case <-time.After(500 * time.Millisecond):
				return ios.AttachedMessage{}, errors.New("no more events")
			}
		}
		closeFunc := func() error {
			return nil
		}
		return listenFunc, closeFunc, nil
	}

	dm := goios.NewDeviceManager(mock)
	tracker := goios.NewTracker(dm, 10*time.Millisecond)
	tracker.ListenFn = mockListen

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
	case <-time.After(1000 * time.Millisecond):
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
	case <-time.After(1000 * time.Millisecond):
		t.Fatal("timeout waiting for EventDisconnected")
	}
}
