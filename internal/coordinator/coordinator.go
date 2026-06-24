// Package coordinator implements the gRPC client that connects this provider
// to the central STF Coordinator.
//
// Responsibilities:
//   - Dial the coordinator with optional mTLS.
//   - Register the provider on startup.
//   - Maintain a long-lived bi-directional heartbeat stream, reconnecting on
//     failure with exponential back-off.
//   - Announce device connect / disconnect events.
//   - Cleanly shut down when the context is cancelled.
package coordinator

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"protean-provider/internal/config"
	"protean-provider/internal/domain"
	pb "protean-provider/pkg/protocol/coordinator"
)

// Client is a live, reconnecting gRPC connection to the STF Coordinator.
// It satisfies domain.CoordinatorClient.
type Client struct {
	cfg        config.CoordinatorConfig
	providerID string

	mu   sync.Mutex
	conn *grpc.ClientConn
	stub pb.CoordinatorServiceClient

	// heartbeat state
	hbCancel context.CancelFunc
	hbStream pb.CoordinatorService_HeartbeatClient
	hbMu     sync.Mutex

	// serialsSnapshot is sent on each heartbeat ping.
	snapshotMu sync.RWMutex
	serials    []string
}

// New constructs a Client. Call Connect to actually dial.
func New(cfg config.CoordinatorConfig, providerID string) *Client {
	return &Client{
		cfg:        cfg,
		providerID: providerID,
	}
}

// ── domain.CoordinatorClient implementation ───────────────────────────────────

// Connect dials the coordinator. It is idempotent — calling it while already
// connected is a no-op.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil && c.conn.GetState() != connectivity.Shutdown {
		return nil // already connected
	}

	dialOpts, err := c.buildDialOptions()
	if err != nil {
		return fmt.Errorf("coordinator: build dial options: %w", err)
	}

	slog.Info("coordinator: dialing", "address", c.cfg.Address)
	conn, err := grpc.NewClient(c.cfg.Address, dialOpts...)
	if err != nil {
		return fmt.Errorf("coordinator: dial %q: %w", c.cfg.Address, domain.ErrCoordinatorUnreachable)
	}

	c.conn = conn
	c.stub = pb.NewCoordinatorServiceClient(conn)
	slog.Info("coordinator: connected", "address", c.cfg.Address)
	return nil
}

// RegisterProvider announces this provider instance to the coordinator.
func (c *Client) RegisterProvider(ctx context.Context, p *domain.Provider) error {
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
	defer cancel()

	req := &pb.RegisterProviderRequest{
		ProviderId: p.ID,
		Name:       p.Name,
		Host:       p.Host,
		Ip:         p.IP,
		MinPort:    int32(p.MinPort),
		MaxPort:    int32(p.MaxPort),
		Version:    p.Version,
	}

	resp, err := c.stub.RegisterProvider(callCtx, req)
	if err != nil {
		return fmt.Errorf("coordinator: RegisterProvider: %w", grpcErr(err))
	}
	if !resp.Accepted {
		return fmt.Errorf("coordinator: provider registration rejected: %s", resp.Message)
	}

	slog.Info("coordinator: provider registered", "id", p.ID, "name", p.Name)
	return nil
}

// RegisterDevice tells the coordinator a device is now online.
func (c *Client) RegisterDevice(ctx context.Context, d *domain.Device) error {
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
	defer cancel()

	req := &pb.RegisterDeviceRequest{
		ProviderId:    c.providerID,
		Serial:        d.Serial,
		Model:         d.Info.Model,
		Manufacturer:  d.Info.Manufacturer,
		Android:       d.Info.AndroidVersion,
		Sdk:           int32(d.Info.SDKVersion),
		Abi:           d.Info.CPUABI,
		RamMb:         d.Info.RAMMB,
		StorageMb:     d.Info.StorageMB,
		DisplayWidth:  d.Display.Width,
		DisplayHeight: d.Display.Height,
		DisplayDpi:    d.Display.Density,
		Battery:       int32(d.State.Battery.Level),
		WifiSsid:      d.State.Network.WiFiSSID,
		Ip:            d.State.Network.IP,
		ConnectedAt:   timestamppb.New(d.ConnectedAt),
	}

	resp, err := c.stub.RegisterDevice(callCtx, req)
	if err != nil {
		return fmt.Errorf("coordinator: RegisterDevice %s: %w", d.Serial, grpcErr(err))
	}
	if !resp.Accepted {
		return fmt.Errorf("coordinator: device %s rejected: %s", d.Serial, resp.Message)
	}

	slog.Info("coordinator: device registered", "serial", d.Serial)
	c.addSerial(d.Serial)
	return nil
}

// SendHeartbeat is a no-op here — heartbeats are driven by the RunHeartbeat
// goroutine. This satisfies the interface for callers that want manual pings.
func (c *Client) SendHeartbeat(ctx context.Context, serial string) error {
	return c.sendPing(ctx)
}

// ReleaseDevice informs the coordinator that a device has disconnected.
func (c *Client) ReleaseDevice(ctx context.Context, serial string) error {
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
	defer cancel()

	_, err := c.stub.ReleaseDevice(callCtx, &pb.ReleaseDeviceRequest{
		ProviderId: c.providerID,
		Serial:     serial,
	})
	if err != nil {
		return fmt.Errorf("coordinator: ReleaseDevice %s: %w", serial, grpcErr(err))
	}

	slog.Info("coordinator: device released", "serial", serial)
	c.removeSerial(serial)
	return nil
}

// Disconnect closes the gRPC connection and stops the heartbeat loop.
func (c *Client) Disconnect(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.hbCancel != nil {
		c.hbCancel()
	}
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			return fmt.Errorf("coordinator: close: %w", err)
		}
		c.conn = nil
		c.stub = nil
	}
	slog.Info("coordinator: disconnected")
	return nil
}

// ── Heartbeat loop ─────────────────────────────────────────────────────────────

// RunHeartbeat starts the long-lived heartbeat stream and keeps it alive,
// reconnecting with exponential back-off up to cfg.ReconnectMaxBackoff.
// It blocks until ctx is cancelled.
func (c *Client) RunHeartbeat(ctx context.Context) {
	backoff := 1 * time.Second
	max := c.cfg.ReconnectMaxBackoff
	if max <= 0 {
		max = 2 * time.Minute
	}

	for {
		if ctx.Err() != nil {
			return
		}

		if err := c.runHeartbeatOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return // clean shutdown
			}
			slog.Warn("coordinator: heartbeat stream lost, retrying",
				"err", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, max)
			continue
		}
		// Clean exit (context cancelled).
		return
	}
}

// runHeartbeatOnce opens a single Heartbeat bidi-stream and keeps sending
// pings at cfg.HeartbeatInterval until an error or ctx cancellation.
func (c *Client) runHeartbeatOnce(ctx context.Context) error {
	hbCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	c.hbMu.Lock()
	c.hbCancel = cancel
	c.hbMu.Unlock()

	stream, err := c.stub.Heartbeat(hbCtx)
	if err != nil {
		return fmt.Errorf("open heartbeat stream: %w", err)
	}

	c.hbMu.Lock()
	c.hbStream = stream
	c.hbMu.Unlock()

	slog.Info("coordinator: heartbeat stream open")

	// Fan-in: ticker sends pings; stream.Recv reads coordinator commands.
	ticker := time.NewTicker(c.cfg.HeartbeatInterval)
	defer ticker.Stop()

	recvErr := make(chan error, 1)
	go func() {
		for {
			pong, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			c.handlePong(pong)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			_ = stream.CloseSend()
			return nil

		case err := <-recvErr:
			if err == io.EOF {
				return fmt.Errorf("coordinator closed the heartbeat stream")
			}
			return fmt.Errorf("recv: %w", err)

		case <-ticker.C:
			if err := c.sendPing(ctx); err != nil {
				return fmt.Errorf("send ping: %w", err)
			}
		}
	}
}

func (c *Client) sendPing(ctx context.Context) error {
	c.hbMu.Lock()
	stream := c.hbStream
	c.hbMu.Unlock()

	if stream == nil {
		return nil // stream not yet open; harmless
	}

	c.snapshotMu.RLock()
	serials := make([]string, len(c.serials))
	copy(serials, c.serials)
	c.snapshotMu.RUnlock()

	ping := &pb.HeartbeatPing{
		ProviderId:    c.providerID,
		SentAt:        timestamppb.Now(),
		DeviceSerials: serials,
	}
	if err := stream.Send(ping); err != nil {
		return err
	}
	// slog.Debug("coordinator: heartbeat sent", "devices", len(serials))
	return nil
}

func (c *Client) handlePong(pong *pb.HeartbeatPong) {
	switch cmd := pong.Command.(type) {
	case *pb.HeartbeatPong_Reconnect:
		slog.Warn("coordinator: reconnect requested", "reason", cmd.Reconnect.Reason)
		// The outer RunHeartbeat loop will reconnect after this stream closes.
		c.hbMu.Lock()
		if c.hbCancel != nil {
			c.hbCancel()
		}
		c.hbMu.Unlock()

	case *pb.HeartbeatPong_Shutdown:
		slog.Warn("coordinator: shutdown requested", "reason", cmd.Shutdown.Reason)
		// Signal the provider process to exit via the heartbeat cancel.
		c.hbMu.Lock()
		if c.hbCancel != nil {
			c.hbCancel()
		}
		c.hbMu.Unlock()

	default:
		// Just a regular ack — nothing to do.
	}
}

// ── Serial snapshot helpers ───────────────────────────────────────────────────

func (c *Client) addSerial(serial string) {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	for _, s := range c.serials {
		if s == serial {
			return
		}
	}
	c.serials = append(c.serials, serial)
}

func (c *Client) removeSerial(serial string) {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	for i, s := range c.serials {
		if s == serial {
			c.serials = append(c.serials[:i], c.serials[i+1:]...)
			return
		}
	}
}

// ── Dial helpers ──────────────────────────────────────────────────────────────

func (c *Client) buildDialOptions() ([]grpc.DialOption, error) {
	if !c.cfg.TLS.Enabled {
		return []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}, nil
	}

	// mTLS
	cert, err := tls.LoadX509KeyPair(c.cfg.TLS.ClientCert, c.cfg.TLS.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("load client cert/key: %w", err)
	}

	caCert, err := os.ReadFile(c.cfg.TLS.CACert)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("invalid CA cert")
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
	}

	return []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
	}, nil
}

// ── Error normalisation ───────────────────────────────────────────────────────

func grpcErr(err error) error {
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
			return fmt.Errorf("%w: %s", domain.ErrCoordinatorUnreachable, s.Message())
		}
	}
	return err
}

// min returns the smaller of two durations (stdlib min not available pre-Go 1.21).
func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}