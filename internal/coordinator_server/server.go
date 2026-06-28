package coordinator_server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "protean-provider/pkg/protocol/coordinator"
	providerpb "protean-provider/pkg/protocol/provider"
)

type Server struct {
	pb.UnimplementedCoordinatorServiceServer
	cfg Config
	db  *DB

	// Active heartbeats tracking: providerID -> cancelFunc
	mu         sync.Mutex
	activeHBs  map[string]context.CancelFunc
	grpcServer *grpc.Server
	httpServer *http.Server
	wsManager  *WSManager
}

func NewServer(cfg Config, db *DB) *Server {
	return &Server{
		cfg:       cfg,
		db:        db,
		activeHBs: make(map[string]context.CancelFunc),
		wsManager: NewWSManager(),
	}
}

// Start starts both the gRPC server and the HTTP REST API.
func (s *Server) Start() error {
	// 1. Listen for gRPC
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.GRPCPort))
	if err != nil {
		return fmt.Errorf("failed to listen on gRPC port %d: %w", s.cfg.GRPCPort, err)
	}

	s.grpcServer = grpc.NewServer()
	pb.RegisterCoordinatorServiceServer(s.grpcServer, s)

	go func() {
		slog.Info("coordinator: gRPC server listening", "port", s.cfg.GRPCPort)
		if err := s.grpcServer.Serve(lis); err != nil {
			slog.Error("coordinator: gRPC server error", "err", err)
		}
	}()

	// 2. Start HTTP server on port GRPCPort + 2 (e.g. 9002)
	httpPort := s.cfg.GRPCPort + 2
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/devices", s.handleListDevices)
	mux.HandleFunc("/api/v1/devices/ws", s.handleWS)
	mux.HandleFunc("/api/v1/devices/", s.handleDeviceAction)

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", httpPort),
		Handler: corsMiddleware(mux),
	}

	go func() {
		slog.Info("coordinator: HTTP API listening", "port", httpPort)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("coordinator: HTTP server error", "err", err)
		}
	}()

	// No more 2-second broadcast loop

	return nil
}

// corsMiddleware adds permissive CORS headers (suitable for local dev).
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Stop shuts down the gRPC and HTTP servers.
func (s *Server) Stop() {
	if s.grpcServer != nil {
		s.grpcServer.Stop()
	}
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(ctx)
	}
	slog.Info("coordinator: servers stopped")
}

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
		_, _ = s.db.db.Exec("UPDATE devices SET status = 'offline' WHERE provider_ip = $1", providerID)
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

// ── HTTP API Implementation ──────────────────────────────────────────────────

type DeviceJSON struct {
	Serial       string    `json:"serial"`
	ProviderID   string    `json:"provider_id"`
	Model        string    `json:"model"`
	Manufacturer string    `json:"manufacturer"`
	Android      string    `json:"android"`
	SDK          int       `json:"sdk"`
	ABI          string    `json:"abi"`
	RAM          int64     `json:"ram_mb"`
	Storage      int64     `json:"storage_mb"`
	Display      string    `json:"display"`
	Battery      int       `json:"battery"`
	WiFi         string    `json:"wifi_ssid"`
	IP           string    `json:"ip"`
	Status            string          `json:"status"`
	StreamPort        int             `json:"stream_port"`
	ConnectedAt       time.Time       `json:"connected_at"`
	FileSystem        json.RawMessage `json:"file_system,omitempty"`
	InstalledBrowsers json.RawMessage `json:"installed_browsers,omitempty"`
}

func (s *Server) getDevice(serial string) (*DeviceJSON, error) {
	row := s.db.db.QueryRow(`
		SELECT serial, provider_ip, model, manufacturer, android, sdk, abi, ram_mb, storage_mb,
		       display_width || 'x' || display_height || ' @ ' || display_dpi || 'dpi',
		       battery, wifi_ssid, ip, status, stream_port, connected_at,
		       file_system, installed_browsers
		FROM devices
		WHERE serial = $1
	`, serial)

	var d DeviceJSON
	var fsJson sql.NullString
	var brJson sql.NullString
	err := row.Scan(&d.Serial, &d.ProviderID, &d.Model, &d.Manufacturer, &d.Android, &d.SDK, &d.ABI, &d.RAM, &d.Storage, &d.Display, &d.Battery, &d.WiFi, &d.IP, &d.Status, &d.StreamPort, &d.ConnectedAt, &fsJson, &brJson)
	if err != nil {
		return nil, err
	}
	if fsJson.Valid {
		d.FileSystem = json.RawMessage(fsJson.String)
	}
	if brJson.Valid {
		d.InstalledBrowsers = json.RawMessage(brJson.String)
	}
	return &d, nil
}

func (s *Server) getDevicesList() ([]DeviceJSON, error) {
	rows, err := s.db.db.Query(`
		SELECT serial, provider_ip, model, manufacturer, android, sdk, abi, ram_mb, storage_mb,
		       display_width || 'x' || display_height || ' @ ' || display_dpi || 'dpi',
		       battery, wifi_ssid, ip, status, stream_port, connected_at,
		       file_system, installed_browsers
		FROM devices
		ORDER BY CASE status
		    WHEN 'idle' THEN 1
		    WHEN 'claimed' THEN 2
		    WHEN 'busy' THEN 2
		    WHEN 'offline' THEN 3
		    ELSE 4
		END, connected_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []DeviceJSON
	for rows.Next() {
		var d DeviceJSON
		var fsJson sql.NullString
		var brJson sql.NullString
		err := rows.Scan(&d.Serial, &d.ProviderID, &d.Model, &d.Manufacturer, &d.Android, &d.SDK, &d.ABI, &d.RAM, &d.Storage, &d.Display, &d.Battery, &d.WiFi, &d.IP, &d.Status, &d.StreamPort, &d.ConnectedAt, &fsJson, &brJson)
		if err != nil {
			return nil, err
		}
		if fsJson.Valid {
			d.FileSystem = json.RawMessage(fsJson.String)
		}
		if brJson.Valid {
			d.InstalledBrowsers = json.RawMessage(brJson.String)
		}
		list = append(list, d)
	}
	if list == nil {
		list = []DeviceJSON{}
	}
	return list, nil
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	list, err := s.getDevicesList()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func (s *Server) broadcastFullList() {
	list, err := s.getDevicesList()
	if err == nil {
		s.wsManager.Broadcast("DEVICE_LIST_UPDATE", list)
	} else {
		slog.Error("coordinator: failed to get device list for broadcast", "err", err)
	}
}

func (s *Server) handleDeviceAction(w http.ResponseWriter, r *http.Request) {
	// Trim prefix "/api/v1/devices/"
	relPath := strings.TrimPrefix(r.URL.Path, "/api/v1/devices/")
	parts := strings.Split(relPath, "/")
	if len(parts) < 2 || parts[0] == "" || (parts[1] != "claim" && parts[1] != "release" && parts[1] != "control") {
		http.Error(w, "Invalid path structure. Use /api/v1/devices/{serial}/[claim|release|control]", http.StatusBadRequest)
		return
	}
	serial := parts[0]
	action := parts[1]

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if action == "claim" {
		claimedBy := r.URL.Query().Get("user")
		if claimedBy == "" {
			claimedBy = "admin@apmosys.com" // Default to admin if user query param is empty
		}

		sessionID := uuidString()
		slog.Info("coordinator: claim requested", "serial", serial, "by", claimedBy)

		// 1. Transactionally update DB
		if err := s.db.CreateSession(sessionID, serial, claimedBy); err != nil {
			slog.Error("coordinator: claim failed", "serial", serial, "err", err)
			http.Error(w, fmt.Sprintf("Claim DB state transition failed: %v", err), http.StatusConflict)
			return
		}

		// 2. Fetch provider IP/port
		providerIP, _, err := s.db.GetDeviceProvider(serial)
		if err != nil {
			// Rollback DB state
			_ = s.db.CloseSession(serial)
			http.Error(w, "Failed to get provider details for device", http.StatusInternalServerError)
			return
		}

		// 3. Dial Provider over gRPC to trigger FSM & Streaming
		pClient, conn, err := s.getProviderClient(providerIP, 9091) // Default to 9091
		if err != nil {
			_ = s.db.CloseSession(serial)
			http.Error(w, fmt.Sprintf("Failed to connect to provider: %v", err), http.StatusBadGateway)
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		resp, err := pClient.ClaimDevice(ctx, &providerpb.ClaimDeviceRequest{
			Serial:    serial,
			ClaimedBy: claimedBy,
		})
		if err != nil || !resp.Success {
			_ = s.db.CloseSession(serial)
			msg := "Provider claim call failed"
			if err != nil {
				msg = fmt.Sprintf("Provider claim call error: %v", err)
			} else {
				msg = fmt.Sprintf("Provider claim rejected: %s", resp.Message)
			}
			http.Error(w, msg, http.StatusBadGateway)
			return
		}

		// Update device's stream_port in database for view stream support
		if dbErr := s.db.UpdateDeviceStreamPort(serial, int(resp.Port)); dbErr != nil {
			slog.Error("coordinator: failed to update device stream port in DB", "serial", serial, "port", resp.Port, "err", dbErr)
		} else {
			slog.Info("coordinator: device claimed successfully and stream port stored", "serial", serial, "port", resp.Port)
		}

		// Return success response with stream port
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    true,
			"session_id": sessionID,
			"port":       resp.Port,
			"message":    "Device claimed successfully",
		})

		s.wsManager.Broadcast("DEVICE_CLAIMED", map[string]interface{}{
			"serial":     serial,
			"session_id": sessionID,
			"port":       resp.Port,
			"claimed_by": claimedBy,
		})

	} else if action == "release" {
		slog.Info("coordinator: release requested", "serial", serial)

		// 1. Fetch provider details
		providerIP, _, err := s.db.GetDeviceProvider(serial)
		if err != nil && err != sql.ErrNoRows {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// 2. Transactionally update DB
		if err := s.db.CloseSession(serial); err != nil {
			http.Error(w, fmt.Sprintf("Release DB transition failed: %v", err), http.StatusInternalServerError)
			return
		}

		if providerIP != "" {
			// 3. Inform Provider over gRPC
			pClient, conn, err := s.getProviderClient(providerIP, 9091)
			if err == nil {
				defer conn.Close()
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				defer cancel()
				_, _ = pClient.ReleaseDevice(ctx, &providerpb.ReleaseDeviceRequest{
					Serial: serial,
				})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Device released successfully",
		})

		s.wsManager.Broadcast("DEVICE_RELEASED", map[string]interface{}{
			"serial": serial,
		})
	} else if action == "control" {
		var reqBody struct {
			Type    string `json:"type"`
			KeyCode int32  `json:"keycode,omitempty"`
			Text    string `json:"text,omitempty"`
			Command string `json:"command,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		providerIP, _, err := s.db.GetDeviceProvider(serial)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		pClient, conn, err := s.getProviderClient(providerIP, 9091)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		stream, err := pClient.ControlDevice(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var req *providerpb.ControlRequest
		switch reqBody.Type {
		case "key":
			req = &providerpb.ControlRequest{
				Serial: serial,
				Event: &providerpb.ControlRequest_Key{
					Key: &providerpb.KeyEvent{
						Action:  providerpb.KeyEvent_DOWN,
						KeyCode: reqBody.KeyCode,
					},
				},
			}
		case "text":
			req = &providerpb.ControlRequest{
				Serial: serial,
				Event: &providerpb.ControlRequest_Text{
					Text: &providerpb.TextEvent{
						Text: reqBody.Text,
					},
				},
			}
		case "rotate":
			req = &providerpb.ControlRequest{
				Serial: serial,
				Event: &providerpb.ControlRequest_Rotate{
					Rotate: &providerpb.RotateEvent{
						Rotation: reqBody.KeyCode,
					},
				},
			}
		case "shell":
			req = &providerpb.ControlRequest{
				Serial: serial,
				Event: &providerpb.ControlRequest_Shell{
					Shell: &providerpb.ShellCommandRequest{
						Command: reqBody.Command,
					},
				},
			}
		default:
			http.Error(w, "Unsupported control type", http.StatusBadRequest)
			return
		}

		if err := stream.Send(req); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := stream.Recv()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": resp.Success,
			"message": resp.Message,
		})
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

func uuidString() string {
	return uuid.New().String()
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("coordinator: failed to upgrade to websocket", "err", err)
		return
	}

	s.wsManager.AddClient(conn)
	defer s.wsManager.RemoveClient(conn)

	// Send initial full list
	list, err := s.getDevicesList()
	if err == nil {
		msg := WSEvent{
			Event: "DEVICE_LIST_UPDATE",
			Data:  list,
		}
		b, _ := json.Marshal(msg)
		conn.WriteMessage(websocket.TextMessage, b)
	}

	// Keep connection alive
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}
