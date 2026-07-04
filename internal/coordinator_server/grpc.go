package coordinator_server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "protean-provider/pkg/protocol/coordinator"
	providerpb "protean-provider/pkg/protocol/provider"
)

// ── gRPC CoordinatorService Implementation ───────────────────────────────────

func (s *Server) RegisterProvider(ctx context.Context, req *pb.RegisterProviderRequest) (*pb.RegisterProviderResponse, error) {
	slog.Info("coordinator: registering provider", "id", req.ProviderId, "name", req.Name, "ip", req.Ip)

	err := s.db.RegisterProvider(
		req.ProviderId,
		req.Name,
		req.Host,
		int(req.MinPort),
		int(req.MaxPort),
		req.Version,
	)
	if err != nil {
		slog.Error("coordinator: failed to register provider", "id", req.ProviderId, "err", err)
		return &pb.RegisterProviderResponse{Accepted: false, Message: err.Error()}, nil
	}

	return &pb.RegisterProviderResponse{Accepted: true, Message: "Registered successfully"}, nil
}

func (s *Server) RegisterDevice(ctx context.Context, req *pb.RegisterDeviceRequest) (*pb.RegisterDeviceResponse, error) {
	slog.Info("coordinator: registering device", "serial", req.Serial, "provider", req.ProviderId)

	connectedAt := time.Now()
	if req.ConnectedAt != nil {
		connectedAt = req.ConnectedAt.AsTime()
	}

	err := s.db.RegisterDevice(
		req.ProviderId,
		req.Serial,
		req.Model,
		req.Manufacturer,
		req.Android,
		int(req.Sdk),
		req.Abi,
		req.RamMb,
		req.StorageMb,
		int(req.DisplayWidth),
		int(req.DisplayHeight),
		int(req.DisplayDpi),
		int(req.Battery),
		req.WifiSsid,
		req.Ip,
		connectedAt,
	)
	if err != nil {
		slog.Error("coordinator: failed to register device", "serial", req.Serial, "err", err)
		return &pb.RegisterDeviceResponse{Accepted: false, Message: err.Error()}, nil
	}

	if err == nil {
		s.broadcastFullList()
	}

	return &pb.RegisterDeviceResponse{Accepted: true, Message: "Device registered"}, nil
}

func (s *Server) ReleaseDevice(ctx context.Context, req *pb.ReleaseDeviceRequest) (*pb.ReleaseDeviceResponse, error) {
	slog.Info("coordinator: device disconnected/released by provider", "serial", req.Serial, "provider", req.ProviderId)

	err := s.db.ReleaseDevice(req.Serial)
	if err != nil {
		slog.Error("coordinator: failed to release device", "serial", req.Serial, "err", err)
		return &pb.ReleaseDeviceResponse{Ok: false}, nil
	}

	s.broadcastFullList()

	return &pb.ReleaseDeviceResponse{Ok: true}, nil
}

func (s *Server) UpdateDeviceState(ctx context.Context, req *pb.UpdateDeviceStateRequest) (*pb.UpdateDeviceStateResponse, error) {
	err := s.db.UpdateDeviceState(
		req.Serial,
		int(req.Battery),
		req.WifiSsid,
		req.FileSystemJson,
		req.InstalledBrowsersJson,
	)
	if err != nil {
		slog.Error("coordinator: failed to update device state", "serial", req.Serial, "err", err)
		return &pb.UpdateDeviceStateResponse{Success: false, Message: err.Error()}, nil
	}
	device, err2 := s.getDevice(req.Serial)
	if err2 == nil {
		s.wsManager.Broadcast("DEVICE_STATE_UPDATE", device)
	}
	return &pb.UpdateDeviceStateResponse{Success: true}, nil
}

func (s *Server) Heartbeat(stream pb.CoordinatorService_HeartbeatServer) error {
	// Read initial message to identify provider
	firstPing, err := stream.Recv()
	if err != nil {
		return err
	}

	providerID := firstPing.ProviderId
	slog.Info("coordinator: heartbeat stream established", "provider", providerID)

	_, cancel := context.WithCancel(stream.Context())
	defer cancel()

	s.mu.Lock()
	if oldCancel, exists := s.activeHBs[providerID]; exists {
		oldCancel() // Cancel any duplicate/stale stream
	}
	s.activeHBs[providerID] = cancel
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		if s.activeHBs[providerID] != nil {
			delete(s.activeHBs, providerID)
		}
		s.mu.Unlock()

		// Mark all devices of this provider as offline
		slog.Info("coordinator: heartbeat stream lost, marking provider devices offline", "provider", providerID)
		_, _ = s.db.RawDB().Exec("UPDATE devices SET status = 'offline' WHERE provider_ip = $1", providerID)
		s.broadcastFullList()
	}()

	// Handle initial ping snapshot
	s.processPing(firstPing)

	// Keep receiving pings and acking
	for {
		ping, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		s.processPing(ping)

		// Respond with regular ack
		pong := &pb.HeartbeatPong{
			ReceivedAt: timestamppb.Now(),
		}
		if err := stream.Send(pong); err != nil {
			return err
		}
	}
}

func (s *Server) processPing(ping *pb.HeartbeatPing) {
	// Update device timestamps and ensure their status is correctly active/idle
	for _, serial := range ping.DeviceSerials {
		_ = s.db.UpdateDeviceHeartbeat(serial)
	}
}

func (s *Server) getProviderClient(ip string, port int) (providerpb.ProviderServiceClient, *grpc.ClientConn, error) {
	addr := fmt.Sprintf("%s:%d", ip, port)
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	return providerpb.NewProviderServiceClient(conn), conn, nil
}
