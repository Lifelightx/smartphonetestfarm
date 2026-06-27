package grpc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"protean-provider/internal/config"
	"protean-provider/internal/domain"
	"protean-provider/internal/supervisor"
	provider "protean-provider/pkg/protocol/provider"
)

// Server implements the provider's inbound gRPC API.
type Server struct {
	cfg        *config.Config
	sup        *supervisor.Supervisor
	registry   domain.DeviceRegistry
	grpcServer *grpc.Server
}

// NewServer creates a new inbound gRPC server.
func NewServer(cfg *config.Config, sup *supervisor.Supervisor, registry domain.DeviceRegistry) *Server {
	s := &Server{
		cfg:      cfg,
		sup:      sup,
		registry: registry,
	}
	return s
}

// Start starts the gRPC server listening on the configured port.
// It runs in a background goroutine and returns immediately.
func (s *Server) Start() error {
	if !s.cfg.GRPCServer.Enabled {
		slog.Info("grpc: inbound server is disabled by config")
		return nil
	}

	addr := fmt.Sprintf(":%d", s.cfg.GRPCServer.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("grpc: listen failed on %s: %w", addr, err)
	}

	// Create gRPC server.
	s.grpcServer = grpc.NewServer()
	provider.RegisterProviderServiceServer(s.grpcServer, s)

	go func() {
		slog.Info("grpc: inbound server listening", "addr", addr)
		if err := s.grpcServer.Serve(listener); err != nil && err != grpc.ErrServerStopped {
			slog.Error("grpc: server error", "err", err)
		}
	}()

	return nil
}

// Stop gracefully stops the gRPC server.
func (s *Server) Stop() {
	if s.grpcServer != nil {
		slog.Info("grpc: stopping inbound server")
		s.grpcServer.Stop()
	}
}

// ── ProviderService Implementation ───────────────────────────────────────────

// HealthCheck reports the health of the provider.
func (s *Server) HealthCheck(ctx context.Context, req *provider.HealthCheckRequest) (*provider.HealthCheckResponse, error) {
	return &provider.HealthCheckResponse{
		Healthy: true,
		Message: "Provider is online and healthy",
	}, nil
}

// GetVersion returns the provider version details.
func (s *Server) GetVersion(ctx context.Context, req *provider.VersionRequest) (*provider.VersionResponse, error) {
	return &provider.VersionResponse{
		Version:   "dev",
		BuildDate: time.Now().Format(time.RFC3339),
	}, nil
}

// ListDevices returns all connected devices on this provider.
func (s *Server) ListDevices(ctx context.Context, req *provider.ListDevicesRequest) (*provider.ListDevicesResponse, error) {
	devices := s.registry.List()
	protos := make([]*provider.DeviceProto, len(devices))

	for i, d := range devices {
		state, _ := s.sup.StateOf(d.Serial)
		port, _ := s.sup.PortOf(d.Serial)

		protos[i] = &provider.DeviceProto{
			Serial:          d.Serial,
			Model:           d.Info.Model,
			Manufacturer:    d.Info.Manufacturer,
			AndroidVersion:  d.Info.AndroidVersion,
			SdkVersion:      int32(d.Info.SDKVersion),
			CpuAbi:          d.Info.CPUABI,
			RamMb:           d.Info.RAMMB,
			StorageMb:       d.Info.StorageMB,
			Status:          state.String(),
			Port:            int32(port),
			BatteryLevel:    int32(d.State.Battery.Level),
			IpAddress:       d.State.Network.IP,
			ConnectedAt:     timestamppb.New(d.ConnectedAt),
		}
	}

	return &provider.ListDevicesResponse{
		Devices: protos,
	}, nil
}

// ClaimDevice claims a device.
func (s *Server) ClaimDevice(ctx context.Context, req *provider.ClaimDeviceRequest) (*provider.ClaimDeviceResponse, error) {
	if req.Serial == "" {
		return nil, status.Error(codes.InvalidArgument, "serial is required")
	}

	slog.Info("grpc: claim device request", "serial", req.Serial, "claimed_by", req.ClaimedBy)

	sessionID, err := s.sup.Claim(ctx, req.Serial, req.ClaimedBy)
	if err != nil {
		return &provider.ClaimDeviceResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	port, _ := s.sup.PortOf(req.Serial)
	return &provider.ClaimDeviceResponse{
		Success:   true,
		SessionId: sessionID,
		Port:      int32(port),
		Message:   "Device claimed successfully",
	}, nil
}

// ReleaseDevice releases a device.
func (s *Server) ReleaseDevice(ctx context.Context, req *provider.ReleaseDeviceRequest) (*provider.ReleaseDeviceResponse, error) {
	if req.Serial == "" {
		return nil, status.Error(codes.InvalidArgument, "serial is required")
	}

	slog.Info("grpc: release device request", "serial", req.Serial)

	if err := s.sup.Release(ctx, req.Serial); err != nil {
		return &provider.ReleaseDeviceResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	return &provider.ReleaseDeviceResponse{
		Success: true,
		Message: "Device released successfully",
	}, nil
}

// ControlDevice handles bidirectional screen inputs and shell execution.
func (s *Server) ControlDevice(stream provider.ProviderService_ControlDeviceServer) error {
	var activeSerial string
	var lastX, lastY int32
	var touchDownTime time.Time

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		serial := req.Serial
		if serial == "" {
			serial = activeSerial
		}
		if serial == "" {
			return status.Error(codes.InvalidArgument, "device serial not specified")
		}
		activeSerial = serial

		agt := s.sup.Agent(serial)
		if agt == nil {
			return status.Errorf(codes.FailedPrecondition, "no active agent session found for device %s", serial)
		}

		ctx := stream.Context()
		var resp *provider.ControlResponse

		switch event := req.Event.(type) {
		case *provider.ControlRequest_Touch:
			t := event.Touch
			// Convert streaming coordinates sequence to tap or swipe command.
			switch t.Action {
			case provider.TouchEvent_DOWN:
				lastX = t.X
				lastY = t.Y
				touchDownTime = time.Now()
				resp = &provider.ControlResponse{Success: true}
			case provider.TouchEvent_MOVE:
				lastX = t.X
				lastY = t.Y
				resp = &provider.ControlResponse{Success: true}
			case provider.TouchEvent_UP:
				duration := time.Since(touchDownTime).Milliseconds()
				if duration < 250 && abs(lastX-t.X) < 10 && abs(lastY-t.Y) < 10 {
					// Tap event
					cmd := fmt.Sprintf("input tap %d %d", t.X, t.Y)
					_, shellErr := agt.Shell(ctx, cmd)
					if shellErr != nil {
						resp = &provider.ControlResponse{Success: false, Message: shellErr.Error()}
					} else {
						resp = &provider.ControlResponse{Success: true}
					}
				} else {
					// Swipe event
					if duration == 0 {
						duration = 100
					}
					cmd := fmt.Sprintf("input swipe %d %d %d %d %d", lastX, lastY, t.X, t.Y, duration)
					_, shellErr := agt.Shell(ctx, cmd)
					if shellErr != nil {
						resp = &provider.ControlResponse{Success: false, Message: shellErr.Error()}
					} else {
						resp = &provider.ControlResponse{Success: true}
					}
				}
			}

		case *provider.ControlRequest_Key:
			k := event.Key
			// Execute keyevent command.
			// Action keycode mapping. Key actions in ADB are usually sent simply as a single keyevent.
			if k.Action == provider.KeyEvent_DOWN {
				cmd := fmt.Sprintf("input keyevent %d", k.KeyCode)
				_, shellErr := agt.Shell(ctx, cmd)
				if shellErr != nil {
					resp = &provider.ControlResponse{Success: false, Message: shellErr.Error()}
				} else {
					resp = &provider.ControlResponse{Success: true}
				}
			} else {
				resp = &provider.ControlResponse{Success: true}
			}

		case *provider.ControlRequest_Text:
			t := event.Text
			// Escape text for shell injection safety.
			escaped := strings.ReplaceAll(t.Text, "'", "'\\''")
			cmd := fmt.Sprintf("input text '%s'", escaped)
			_, shellErr := agt.Shell(ctx, cmd)
			if shellErr != nil {
				resp = &provider.ControlResponse{Success: false, Message: shellErr.Error()}
			} else {
				resp = &provider.ControlResponse{Success: true}
			}

		case *provider.ControlRequest_Rotate:
			r := event.Rotate
			// Rotate values: 0 -> 0 degrees, 1 -> 90 degrees, 2 -> 180 degrees, 3 -> 270 degrees.
			rotationVal := 0
			switch r.Rotation {
			case 90:
				rotationVal = 1
			case 180:
				rotationVal = 2
			case 270:
				rotationVal = 3
			}
			cmd := fmt.Sprintf("settings put system accelerometer_rotation 0 && settings put system user_rotation %d", rotationVal)
			_, shellErr := agt.Shell(ctx, cmd)
			if shellErr != nil {
				resp = &provider.ControlResponse{Success: false, Message: shellErr.Error()}
			} else {
				resp = &provider.ControlResponse{Success: true}
			}

		case *provider.ControlRequest_Shell:
			sh := event.Shell
			res, shellErr := agt.ShellWithExitCode(ctx, sh.Command)
			if shellErr != nil {
				resp = &provider.ControlResponse{
					Success: false,
					Message: shellErr.Error(),
				}
			} else {
				resp = &provider.ControlResponse{
					Success: true,
					Response: &provider.ControlResponse_ShellResponse{
						ShellResponse: &provider.ShellCommandResponse{
							Output:   res.Output,
							ExitCode: int32(res.ExitCode),
						},
					},
				}
			}
		}

		if resp != nil {
			if sendErr := stream.Send(resp); sendErr != nil {
				return sendErr
			}
		}
	}
}

func abs(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
