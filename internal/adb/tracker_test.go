package adb_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"protean-provider/internal/adb"
	"protean-provider/internal/domain"
)

// ── Fake ADB Server ────────────────────────────────────────────────────────────
//
// We simulate the ADB track-devices push stream using net.Pipe().
//
// After OKAY is exchanged (via openTrackConn / acceptTrackDevices), the daemon
// repeatedly sends:
//   <4-hex-len><payload>
// where payload is "SERIAL STATE\n" lines.
//
// When using StreamFromConnForTest we bypass the OKAY handshake and feed raw
// snapshots directly — so the fake server should NOT send OKAY in that path.

type fakeADBServer struct {
	serverConn net.Conn
}

func newFakeADBPair(t *testing.T) (*fakeADBServer, net.Conn) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	return &fakeADBServer{serverConn: serverConn}, clientConn
}

// Push sends a device-list snapshot (raw, no OKAY prefix).
// This is what goes AFTER the handshake — directly into stream().
// entries format: []string{"SERIAL1 device", "SERIAL2 offline"}
func (f *fakeADBServer) Push(t *testing.T, entries []string) {
	t.Helper()
	body := ""
	for _, e := range entries {
		body += e + "\n"
	}
	header := fmt.Sprintf("%04x", len(body))
	if _, err := f.serverConn.Write([]byte(header + body)); err != nil {
		t.Logf("fake ADB: push failed (conn may be closed): %v", err)
	}
}

func (f *fakeADBServer) Close() { f.serverConn.Close() }

// ── Drain helper ──────────────────────────────────────────────────────────────

func drainEvents(ch <-chan domain.DeviceEvent) []domain.DeviceEvent {
	var events []domain.DeviceEvent
	for {
		select {
		case e := <-ch:
			events = append(events, e)
		default:
			return events
		}
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestPushProtocol_ConnectedEvent verifies that a device appearing in a snapshot
// emits exactly one EventConnected with the correct serial.
func TestPushProtocol_ConnectedEvent(t *testing.T) {
	server, clientConn := newFakeADBPair(t)
	defer server.Close()
	defer clientConn.Close()

	ch := make(chan domain.DeviceEvent, 16)
	previous := map[string]string{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Push(t, []string{"ABC123 device"})
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// StreamFromConnForTest bypasses openTrackConn — receives raw snapshots.
	adb.StreamFromConnForTest(ctx, clientConn, ch, previous)

	events := drainEvents(ch)
	if len(events) == 0 {
		t.Fatal("expected at least one event, got none")
	}
	if events[0].Type != domain.EventConnected {
		t.Errorf("want EventConnected, got %s", events[0].Type)
	}
	if events[0].Serial != "ABC123" {
		t.Errorf("want serial ABC123, got %s", events[0].Serial)
	}
	if events[0].Timestamp.IsZero() {
		t.Error("want non-zero Timestamp")
	}
}

// TestPushProtocol_DisconnectedEvent verifies that a device disappearing from
// a snapshot emits EventDisconnected.
func TestPushProtocol_DisconnectedEvent(t *testing.T) {
	server, clientConn := newFakeADBPair(t)
	defer server.Close()
	defer clientConn.Close()

	ch := make(chan domain.DeviceEvent, 16)
	// Seed previous: DEV001 was already online.
	previous := map[string]string{"DEV001": "device"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Push(t, []string{}) // empty snapshot → DEV001 gone
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	adb.StreamFromConnForTest(ctx, clientConn, ch, previous)

	events := drainEvents(ch)
	if len(events) == 0 {
		t.Fatal("expected EventDisconnected, got none")
	}
	if events[0].Type != domain.EventDisconnected {
		t.Errorf("want EventDisconnected, got %s", events[0].Type)
	}
	if events[0].Serial != "DEV001" {
		t.Errorf("want serial DEV001, got %s", events[0].Serial)
	}
}

// TestPushProtocol_NoDuplicates verifies that two identical snapshots do not
// produce two EventConnected events.
func TestPushProtocol_NoDuplicates(t *testing.T) {
	server, clientConn := newFakeADBPair(t)
	defer server.Close()
	defer clientConn.Close()

	ch := make(chan domain.DeviceEvent, 16)
	previous := map[string]string{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Push(t, []string{"STABLE device"})
		server.Push(t, []string{"STABLE device"}) // same snapshot again
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	adb.StreamFromConnForTest(ctx, clientConn, ch, previous)

	events := drainEvents(ch)
	connected := 0
	for _, e := range events {
		if e.Type == domain.EventConnected {
			connected++
		}
	}
	if connected != 1 {
		t.Errorf("want exactly 1 EventConnected, got %d", connected)
	}
}

// TestPushProtocol_Unauthorized verifies that an unauthorized device emits
// EventUnauthorized, not EventConnected.
func TestPushProtocol_Unauthorized(t *testing.T) {
	server, clientConn := newFakeADBPair(t)
	defer server.Close()
	defer clientConn.Close()

	ch := make(chan domain.DeviceEvent, 16)
	previous := map[string]string{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Push(t, []string{"UNAUTH01 unauthorized"})
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	adb.StreamFromConnForTest(ctx, clientConn, ch, previous)

	events := drainEvents(ch)
	if len(events) == 0 {
		t.Fatal("expected EventUnauthorized, got none")
	}
	if events[0].Type != domain.EventUnauthorized {
		t.Errorf("want EventUnauthorized, got %s", events[0].Type)
	}
}

// TestPushProtocol_MultipleDevices verifies handling of a snapshot with
// multiple devices in different states simultaneously.
func TestPushProtocol_MultipleDevices(t *testing.T) {
	server, clientConn := newFakeADBPair(t)
	defer server.Close()
	defer clientConn.Close()

	ch := make(chan domain.DeviceEvent, 16)
	previous := map[string]string{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Push(t, []string{
			"DEV001 device",
			"DEV002 unauthorized",
			"DEV003 offline",
		})
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	adb.StreamFromConnForTest(ctx, clientConn, ch, previous)

	events := drainEvents(ch)
	typesSeen := make(map[domain.EventType]int)
	for _, e := range events {
		typesSeen[e.Type]++
	}

	if typesSeen[domain.EventConnected] != 1 {
		t.Errorf("want 1 EventConnected, got %d", typesSeen[domain.EventConnected])
	}
	if typesSeen[domain.EventUnauthorized] != 1 {
		t.Errorf("want 1 EventUnauthorized, got %d", typesSeen[domain.EventUnauthorized])
	}
	if typesSeen[domain.EventOffline] != 1 {
		t.Errorf("want 1 EventOffline, got %d", typesSeen[domain.EventOffline])
	}
}

// TestPushProtocol_ConnectionDrop verifies that when the connection drops,
// the stream exits cleanly (no panic, no hang).
func TestPushProtocol_ConnectionDrop(t *testing.T) {
	server, clientConn := newFakeADBPair(t)
	defer clientConn.Close()

	ch := make(chan domain.DeviceEvent, 16)
	previous := map[string]string{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Push(t, []string{"DEV001 device"})
		time.Sleep(30 * time.Millisecond)
		// Abruptly close the server side — simulates ADB daemon crash.
		server.Close()
	}()

	// stream() should return (non-nil error) after the pipe closes.
	adb.StreamFromConnForTest(ctx, clientConn, ch, previous)
	// If we get here without deadlock, the test passes.
}
