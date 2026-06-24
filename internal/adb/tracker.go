package adb

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	adbkit "github.com/codeskyblue/go-adbkit/adb"

	"protean-provider/internal/domain"
)

// ── Tracker ────────────────────────────────────────────────────────────────────
//
// How it works
// ────────────
// The ADB daemon supports a built-in push protocol: send "host:track-devices"
// and the daemon keeps the TCP connection open, writing a new length-prefixed
// device-list snapshot every time any device state changes.
//
// We open ONE persistent TCP connection to the ADB daemon. When that connection
// drops (daemon restart, network hiccup) we reconnect automatically using
// exponential backoff, then re-establish the stream.
//
// Compared to polling this means:
//   - Zero latency: event fires the instant the daemon sees the change.
//   - Zero CPU between events: the goroutine is blocked on a Read() call.
//   - One TCP connection total, not a new one every 2 seconds.

// Tracker watches the ADB daemon for device connection changes using the
// native `host:track-devices` push protocol. It implements domain.DeviceTracker.
type Tracker struct {
	inner       *adbkit.Client
	propTimeout time.Duration

	// reconnect backoff configuration
	backoffMin time.Duration
	backoffMax time.Duration
}

// TrackerConfig holds the tuning parameters for the Tracker.
type TrackerConfig struct {
	// PropertyTimeout is the per-device timeout for fetching hardware info.
	// Recommended: 5s.
	PropertyTimeout time.Duration

	// BackoffMin is the initial reconnect wait after a connection drop.
	// Recommended: 1s.
	BackoffMin time.Duration

	// BackoffMax caps the exponential backoff.
	// Recommended: 30s.
	BackoffMax time.Duration
}

// NewTracker returns a ready-to-use Tracker.
func NewTracker(client Client, cfg TrackerConfig) *Tracker {
	if cfg.PropertyTimeout == 0 {
		cfg.PropertyTimeout = 5 * time.Second
	}
	if cfg.BackoffMin == 0 {
		cfg.BackoffMin = 1 * time.Second
	}
	if cfg.BackoffMax == 0 {
		cfg.BackoffMax = 30 * time.Second
	}

	// We need the raw *adbkit.Client to call TrackDevices().
	// The concreteClient wraps it, so we construct one ourselves here.
	// The host/port are embedded in the adbkit Client we receive as Client
	// interface — we expose RawClient() for this purpose.
	c := client.(*concreteClient)
	return &Tracker{
		inner:       c.inner,
		propTimeout: cfg.PropertyTimeout,
		backoffMin:  cfg.BackoffMin,
		backoffMax:  cfg.BackoffMax,
	}
}

// Watch opens one persistent TCP connection to the ADB daemon, reads push
// updates, diffs device state, emits events on ch, and reconnects automatically
// if the connection drops.
//
// Watch blocks until ctx is cancelled and returns ctx.Err().
func (t *Tracker) Watch(ctx context.Context, ch chan<- domain.DeviceEvent) error {
	slog.Info("adb tracker: starting (mode=push, protocol=host:track-devices)")

	// adb Client interface used only for Shell (property fetch).
	shellClient := &concreteClient{inner: t.inner}

	backoff := t.backoffMin
	previous := map[string]string{} // serial → state

	for {
		// ── Open the persistent track-devices connection ───────────────────
		conn, err := t.openTrackConn(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Warn("adb tracker: connect failed, retrying",
				"err", err, "backoff", backoff)
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}
			backoff = minDur(backoff*2, t.backoffMax)
			continue
		}

		slog.Info("adb tracker: connected to ADB daemon — listening for device events")
		backoff = t.backoffMin // reset after successful connect

		// ── Stream updates until connection drops or ctx is cancelled ──────
		streamErr := t.stream(ctx, conn, shellClient, previous, ch)
		conn.Close()

		if ctx.Err() != nil {
			slog.Info("adb tracker: stopped")
			return ctx.Err()
		}

		// Connection dropped; emit Disconnected for all previously-online devices.
		slog.Warn("adb tracker: connection lost, will reconnect",
			"err", streamErr, "backoff", backoff)
		t.evictAll(previous, ch)

		if !sleep(ctx, backoff) {
			return ctx.Err()
		}
		backoff = minDur(backoff*2, t.backoffMax)
	}
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// openTrackConn establishes the `host:track-devices` persistent stream.
// It returns the raw net.Conn ready to be read as a sequence of
// length-prefixed device-list snapshots.
func (t *Tracker) openTrackConn(ctx context.Context) (net.Conn, error) {
	conn, err := t.inner.ConnectionContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("adb: dial: %w", err)
	}

	// Send the track-devices command.
	cmd := "host:track-devices"
	length := fmt.Sprintf("%04x", len(cmd))
	if _, err := conn.Write([]byte(length + cmd)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("adb: send track-devices: %w", err)
	}

	// Read the 4-byte OKAY/FAIL status.
	status := make([]byte, 4)
	if _, err := io.ReadFull(conn, status); err != nil {
		conn.Close()
		return nil, fmt.Errorf("adb: read status: %w", err)
	}
	if string(status) != "OKAY" {
		conn.Close()
		return nil, fmt.Errorf("adb: track-devices rejected (status=%s)", status)
	}

	return conn, nil
}

// stream reads push snapshots from conn until the connection breaks or ctx is
// cancelled. Each snapshot is a length-prefixed string of lines:
//
//	SERIAL1\tSTATE1\nSERIAL2\tSTATE2\n...
//
// It diffs each snapshot against `previous` and emits events on ch.
func (t *Tracker) stream(
	ctx context.Context,
	conn net.Conn,
	shellClient Client,
	previous map[string]string,
	ch chan<- domain.DeviceEvent,
) error {
	// Cancel the blocking Read when ctx is done.
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	reader := bufio.NewReader(conn)

	for {
		// ── Read 4-byte hex length prefix ─────────────────────────────────
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(reader, lenBuf); err != nil {
			if ctx.Err() != nil {
				return nil // clean shutdown
			}
			return fmt.Errorf("read length prefix: %w", err)
		}

		var payloadLen int64
		if _, err := fmt.Sscanf(string(lenBuf), "%04x", &payloadLen); err != nil {
			return fmt.Errorf("parse length prefix %q: %w", string(lenBuf), err)
		}

		// ── Read payload ──────────────────────────────────────────────────
		payload := make([]byte, payloadLen)
		if payloadLen > 0 {
			if _, err := io.ReadFull(reader, payload); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("read payload: %w", err)
			}
		}

		// ── Parse device list snapshot ────────────────────────────────────
		current := parseDeviceSnapshot(string(payload))

		// ── Diff and emit events ──────────────────────────────────────────
		t.diff(ctx, current, previous, shellClient, ch)

		// Update previous snapshot.
		for k := range previous {
			delete(previous, k)
		}
		for k, v := range current {
			previous[k] = v
		}
	}
}

// parseDeviceSnapshot converts the raw payload ("SERIAL\tSTATE\n...") into a map.
func parseDeviceSnapshot(payload string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(payload), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The format is either "SERIAL\tSTATE" or "SERIAL STATE"
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

// diff compares current vs previous snapshots and emits events for changes.
func (t *Tracker) diff(
	ctx context.Context,
	current, previous map[string]string,
	shellClient Client,
	ch chan<- domain.DeviceEvent,
) {
	// Additions and state changes.
	for serial, state := range current {
		prevState, existed := previous[serial]

		switch state {
		case "device":
			if !existed || prevState != "device" {
				slog.Info("adb: device connected", "serial", serial)
				t.emitConnected(ctx, serial, shellClient, ch)
			}
		case "unauthorized":
			if !existed || prevState != "unauthorized" {
				slog.Warn("adb: device unauthorized — enable USB debugging", "serial", serial)
				t.emitSimple(ch, serial, domain.EventUnauthorized)
			}
		case "offline":
			if !existed || prevState != "offline" {
				slog.Warn("adb: device offline", "serial", serial)
				t.emitSimple(ch, serial, domain.EventOffline)
			}
		}
	}

	// Removals.
	for serial, prevState := range previous {
		if _, still := current[serial]; !still {
			if prevState == "device" {
				slog.Info("adb: device disconnected", "serial", serial)
			} else {
				slog.Info("adb: device removed", "serial", serial, "prev_state", prevState)
			}
			t.emitSimple(ch, serial, domain.EventDisconnected)
		}
	}
}

// evictAll emits EventDisconnected for every currently-known device.
// Called when the tracking connection itself drops.
func (t *Tracker) evictAll(previous map[string]string, ch chan<- domain.DeviceEvent) {
	for serial, state := range previous {
		if state == "device" {
			slog.Warn("adb: emitting disconnect due to daemon connection loss", "serial", serial)
			t.emitSimple(ch, serial, domain.EventDisconnected)
		}
	}
	for k := range previous {
		delete(previous, k)
	}
}

// emitConnected fetches device properties and sends EventConnected on ch.
// If shellClient is nil (test path) the property fetch is skipped.
func (t *Tracker) emitConnected(ctx context.Context, serial string, shellClient Client, ch chan<- domain.DeviceEvent) {
	var device *domain.Device

	if shellClient != nil && t.propTimeout > 0 {
		propCtx, cancel := context.WithTimeout(ctx, t.propTimeout)
		defer cancel()

		var err error
		device, err = FetchProperties(propCtx, shellClient, serial)
		if err != nil {
			slog.Error("adb: property fetch failed", "serial", serial, "err", err)
			// Still emit the event — callers can retry or log.
		}
	}

	select {
	case ch <- domain.DeviceEvent{
		Serial:    serial,
		Type:      domain.EventConnected,
		Device:    device,
		Timestamp: time.Now(),
	}:
	case <-ctx.Done():
	}
}

// emitSimple sends a state-change event with no Device payload.
func (t *Tracker) emitSimple(ch chan<- domain.DeviceEvent, serial string, evtType domain.EventType) {
	select {
	case ch <- domain.DeviceEvent{
		Serial:    serial,
		Type:      evtType,
		Timestamp: time.Now(),
	}:
	default:
		slog.Warn("adb: event channel full, dropping event", "serial", serial, "type", evtType)
	}
}

// ── Utilities ─────────────────────────────────────────────────────────────────

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}