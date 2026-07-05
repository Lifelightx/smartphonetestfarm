package coordinator_server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"google.golang.org/grpc"

	pb "protean-provider/pkg/protocol/coordinator"

	"protean-provider/internal/auth"
	"protean-provider/internal/automation"
)

type Server struct {
	pb.UnimplementedCoordinatorServiceServer
	cfg Config
	db  *DB

	// Active heartbeats tracking: providerID -> cancelFunc
	mu          sync.Mutex
	activeHBs   map[string]context.CancelFunc
	grpcServer  *grpc.Server
	httpServer  *http.Server
	wsManager   *WSManager
	scheduler   *automation.Scheduler
	authService *auth.AuthService
}

func NewServer(cfg Config, db *DB) *Server {
	jwtMgr := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.OIDCJWKSURL)
	authSrv := auth.NewAuthService(db, jwtMgr)

	return &Server{
		cfg:         cfg,
		db:          db,
		activeHBs:   make(map[string]context.CancelFunc),
		wsManager:   NewWSManager(),
		scheduler:   automation.NewScheduler(),
		authService: authSrv,
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
	mux.HandleFunc("/api/v1/automation/scripts", s.handleScripts)
	mux.HandleFunc("/api/v1/automation/scripts/", s.handleScriptByID)
	mux.HandleFunc("/api/v1/automation/run", s.handleRunScript)
	mux.HandleFunc("/api/v1/automation/reports", s.handleReports)
	mux.HandleFunc("/api/v1/automation/reports/", s.handleReportByID)

	// Admin management endpoints
	mux.HandleFunc("/api/v1/admin/users", s.handleAdminUsers)
	mux.HandleFunc("/api/v1/admin/groups", s.handleAdminGroups)
	mux.HandleFunc("/api/v1/admin/groups/", s.handleAdminGroupAction)

	// Register auth HTTP endpoints
	s.authService.RegisterHandlers(mux)

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", httpPort),
		Handler: corsMiddleware(s.authService.AuthMiddleware(s.cfg.BypassAuthInDev)(mux)),
	}

	go func() {
		slog.Info("coordinator: HTTP API listening", "port", httpPort)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("coordinator: HTTP server error", "err", err)
		}
	}()

	return nil
}

// corsMiddleware adds permissive CORS headers (suitable for local dev).
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
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
