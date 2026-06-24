package coordinator_server

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "protean-provider/pkg/protocol/coordinator"
)

// mockCoordinatorServiceServer is a minimal mock for testing connection and registration handlers
type mockCoordinatorServiceServer struct {
	pb.UnimplementedCoordinatorServiceServer
	registeredProviders []string
	registeredDevices   []string
}

func (m *mockCoordinatorServiceServer) RegisterProvider(ctx context.Context, req *pb.RegisterProviderRequest) (*pb.RegisterProviderResponse, error) {
	m.registeredProviders = append(m.registeredProviders, req.ProviderId)
	return &pb.RegisterProviderResponse{Accepted: true, Message: "Mock success"}, nil
}

func (m *mockCoordinatorServiceServer) RegisterDevice(ctx context.Context, req *pb.RegisterDeviceRequest) (*pb.RegisterDeviceResponse, error) {
	m.registeredDevices = append(m.registeredDevices, req.Serial)
	return &pb.RegisterDeviceResponse{Accepted: true, Message: "Mock device success"}, nil
}

func TestCoordinatorRegister(t *testing.T) {
	// 1. Setup in-memory gRPC listener
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()

	mockSrv := &mockCoordinatorServiceServer{}
	grpcServer := grpc.NewServer()
	pb.RegisterCoordinatorServiceServer(grpcServer, mockSrv)

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	// 2. Connect to the mock gRPC server
	conn, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial mock server: %v", err)
	}
	defer conn.Close()

	client := pb.NewCoordinatorServiceClient(conn)

	// 3. Test RegisterProvider
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.RegisterProvider(ctx, &pb.RegisterProviderRequest{
		ProviderId: "mock-provider-01",
		Name:       "Mock Provider",
		Host:       "localhost",
		Ip:         "127.0.0.1",
		MinPort:    7400,
		MaxPort:    7500,
		Version:    "1.0.0",
	})
	if err != nil {
		t.Fatalf("RegisterProvider failed: %v", err)
	}
	if !resp.Accepted {
		t.Errorf("expected Accepted=true, got %v", resp.Accepted)
	}
	if len(mockSrv.registeredProviders) != 1 || mockSrv.registeredProviders[0] != "mock-provider-01" {
		t.Errorf("unexpected registered providers: %v", mockSrv.registeredProviders)
	}

	// 4. Test RegisterDevice
	respDev, err := client.RegisterDevice(ctx, &pb.RegisterDeviceRequest{
		ProviderId:   "mock-provider-01",
		Serial:       "MOCKSERIAL123",
		Model:        "Pixel 7",
		Manufacturer: "Google",
		Android:      "14",
		Sdk:          34,
	})
	if err != nil {
		t.Fatalf("RegisterDevice failed: %v", err)
	}
	if !respDev.Accepted {
		t.Errorf("expected device Accepted=true, got %v", respDev.Accepted)
	}
	if len(mockSrv.registeredDevices) != 1 || mockSrv.registeredDevices[0] != "MOCKSERIAL123" {
		t.Errorf("unexpected registered devices: %v", mockSrv.registeredDevices)
	}
}
