package grpc_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"protean-provider/internal/config"
	"protean-provider/internal/db"
	"protean-provider/internal/domain"
	inboundGRPC "protean-provider/internal/grpc"
	"protean-provider/internal/registry"
	"protean-provider/internal/supervisor"
	provider "protean-provider/pkg/protocol/provider"
)

func TestInboundServer_UnaryCalls(t *testing.T) {
	// Create temp directory for SQLite DB
	tempDir, err := os.MkdirTemp("", "provider-grpc-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	store, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	// Get a free port for gRPC server
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	_ = lis.Close() // Free the port so the server can bind to it

	cfg := &config.Config{
		GRPCServer: config.GRPCServerConfig{
			Enabled: true,
			Port:    port,
		},
		Provider: config.ProviderConfig{
			Name:    "test-provider",
			Host:    "127.0.0.1",
			MinPort: 7000,
			MaxPort: 7100,
		},
	}

	reg := registry.New()

	sup, err := supervisor.New(context.Background(), "provider-1", nil, store, 7000, 7100, &dummyStream{})
	if err != nil {
		t.Fatalf("failed to create supervisor: %v", err)
	}

	server := inboundGRPC.NewServer(cfg, sup, reg)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start gRPC server: %v", err)
	}
	defer server.Stop()

	// Connect client
	conn, err := grpc.Dial(fmt.Sprintf("127.0.0.1:%d", port), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect to gRPC server: %v", err)
	}
	defer conn.Close()

	client := provider.NewProviderServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Test HealthCheck
	healthResp, err := client.HealthCheck(ctx, &provider.HealthCheckRequest{})
	if err != nil {
		t.Errorf("HealthCheck failed: %v", err)
	}
	if !healthResp.Healthy {
		t.Errorf("expected healthy=true, got %v", healthResp.Healthy)
	}

	// Test GetVersion
	verResp, err := client.GetVersion(ctx, &provider.VersionRequest{})
	if err != nil {
		t.Errorf("GetVersion failed: %v", err)
	}
	if verResp.Version == "" {
		t.Errorf("expected non-empty version")
	}

	// Test ListDevices (should be empty initially)
	listResp, err := client.ListDevices(ctx, &provider.ListDevicesRequest{})
	if err != nil {
		t.Errorf("ListDevices failed: %v", err)
	}
	if len(listResp.Devices) != 0 {
		t.Errorf("expected 0 devices, got %d", len(listResp.Devices))
	}

	// Add dummy device to registry and supervisor
	dummyDev := &domain.Device{
		Serial: "TEST_SERIAL_123",
		Info: domain.DeviceInfo{
			Model:        "Pixel 6",
			Manufacturer: "Google",
		},
	}
	if err := reg.Add(dummyDev); err != nil {
		t.Fatalf("failed to add dummy device: %v", err)
	}
	if err := sup.OnDeviceConnected(context.Background(), dummyDev); err != nil {
		t.Fatalf("failed to supervise device: %v", err)
	}
	defer sup.OnDeviceDisconnected("TEST_SERIAL_123")

	// ListDevices should now return 1 device
	listResp, err = client.ListDevices(ctx, &provider.ListDevicesRequest{})
	if err != nil {
		t.Errorf("ListDevices failed after adding device: %v", err)
	}
	if len(listResp.Devices) != 1 {
		t.Errorf("expected 1 device, got %d", len(listResp.Devices))
	} else {
		d := listResp.Devices[0]
		if d.Serial != "TEST_SERIAL_123" {
			t.Errorf("expected serial TEST_SERIAL_123, got %s", d.Serial)
		}
		if d.Status != "idle" {
			t.Errorf("expected status idle, got %s", d.Status)
		}
	}

	// ClaimDevice
	claimResp, err := client.ClaimDevice(ctx, &provider.ClaimDeviceRequest{
		Serial:    "TEST_SERIAL_123",
		ClaimedBy: "test-user@domain.com",
	})
	if err != nil {
		t.Errorf("ClaimDevice failed: %v", err)
	}
	if !claimResp.Success {
		t.Errorf("expected success=true, got message: %s", claimResp.Message)
	}
	if claimResp.SessionId == "" {
		t.Errorf("expected non-empty session ID")
	}

	// ListDevices should now show claimed
	listResp, err = client.ListDevices(ctx, &provider.ListDevicesRequest{})
	if err == nil && len(listResp.Devices) == 1 {
		if listResp.Devices[0].Status != "claimed" {
			t.Errorf("expected status claimed, got %s", listResp.Devices[0].Status)
		}
	}

	// ReleaseDevice
	releaseResp, err := client.ReleaseDevice(ctx, &provider.ReleaseDeviceRequest{
		Serial: "TEST_SERIAL_123",
	})
	if err != nil {
		t.Errorf("ReleaseDevice failed: %v", err)
	}
	if !releaseResp.Success {
		t.Errorf("expected success=true, got message: %s", releaseResp.Message)
	}
}

type dummyStream struct{}

func (d *dummyStream) StartCapture(ctx context.Context, serial string, port int) error { return nil }
func (d *dummyStream) StopCapture(ctx context.Context, serial string) error          { return nil }
func (d *dummyStream) IsCapturing(serial string) bool                               { return false }
