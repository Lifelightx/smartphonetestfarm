package goios_test

import (
	"context"
	"errors"
	"testing"

	"protean-provider/internal/goios"
)

type mockCLIClient struct {
	runFunc       func(ctx context.Context, udid string, args ...string) ([]byte, error)
	runNoUDIDFunc func(ctx context.Context, args ...string) ([]byte, error)
}

func (m *mockCLIClient) Run(ctx context.Context, udid string, args ...string) ([]byte, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, udid, args...)
	}
	return nil, errors.New("unimplemented")
}

func (m *mockCLIClient) RunNoUDID(ctx context.Context, args ...string) ([]byte, error) {
	if m.runNoUDIDFunc != nil {
		return m.runNoUDIDFunc(ctx, args...)
	}
	return nil, errors.New("unimplemented")
}

func TestDeviceManager_Discover(t *testing.T) {
	mock := &mockCLIClient{
		runNoUDIDFunc: func(ctx context.Context, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "list" {
				return []byte(`[{"udid": "12345-67890", "name": "iPhone 15", "type": "iPhone16,1", "version": "17.4"}]`), nil
			}
			return nil, errors.New("unexpected command")
		},
	}

	dm := goios.NewDeviceManager(mock)
	devices, err := dm.Discover(context.Background())
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}

	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}

	if devices[0].Serial != "12345-67890" {
		t.Errorf("expected serial 12345-67890, got %s", devices[0].Serial)
	}

	if devices[0].Info.Model != "iPhone 15 Pro" {
		t.Errorf("expected model iPhone 15 Pro, got %s", devices[0].Info.Model)
	}

	if devices[0].Info.AndroidVersion != "17.4" {
		t.Errorf("expected version 17.4, got %s", devices[0].Info.AndroidVersion)
	}
}

func TestDeviceManager_GetProperties(t *testing.T) {
	mock := &mockCLIClient{
		runFunc: func(ctx context.Context, udid string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "info" {
				if len(args) > 1 && args[1] == "display" {
					return []byte(`{"width": 1170, "height": 2532}`), nil
				}
				return []byte(`{"ProductType": "iPhone16,1", "ProductVersion": "17.4"}`), nil
			}
			return nil, errors.New("unexpected command")
		},
	}

	dm := goios.NewDeviceManager(mock)
	dev, err := dm.GetProperties(context.Background(), "12345-67890")
	if err != nil {
		t.Fatalf("get properties failed: %v", err)
	}

	if dev.Display.Width != 1170 || dev.Display.Height != 2532 {
		t.Errorf("expected display 1170x2532, got %dx%d", dev.Display.Width, dev.Display.Height)
	}
}
