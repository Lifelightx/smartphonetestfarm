package goios

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielpaulus/go-ios/ios"
	"protean-provider/internal/domain"
)

// Tracker implements domain.DeviceTracker for iOS.
// It subscribes to usbmuxd connection/disconnection events.
type Tracker struct {
	manager      *DeviceManager
	pollInterval time.Duration
	// ListenFn allows mocking the usbmuxd listener in tests.
	ListenFn func() (func() (ios.AttachedMessage, error), func() error, error)
}

// NewTracker creates a new iOS device Tracker.
func NewTracker(manager *DeviceManager, pollInterval time.Duration) *Tracker {
	if pollInterval == 0 {
		pollInterval = 3 * time.Second
	}
	return &Tracker{
		manager:      manager,
		pollInterval: pollInterval,
		ListenFn:     ios.Listen,
	}
}

// Watch blocks and streams DeviceEvents (EventConnected, EventDisconnected) into the channel.
func (t *Tracker) Watch(ctx context.Context, ch chan<- domain.DeviceEvent) error {
	slog.Info("ios tracker: starting usbmuxd subscription listener")

	backoffMin := 1 * time.Second
	backoffMax := 30 * time.Second
	backoff := backoffMin

	connected := make(map[string]*domain.Device)

	// Helper to evict all connected devices when the listener drops.
	evictAll := func() {
		for serial := range connected {
			slog.Info("ios tracker: usbmuxd connection lost, evicting device", "serial", serial)
			select {
			case ch <- domain.DeviceEvent{
				Serial:    serial,
				Type:      domain.EventDisconnected,
				Timestamp: time.Now(),
			}:
			case <-ctx.Done():
				return
			}
		}
		connected = make(map[string]*domain.Device)
	}

	for {
		listenFunc, closeFunc, err := t.ListenFn()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Warn("ios tracker: usbmuxd listen failed, retrying", "err", err, "backoff", backoff)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = minDur(backoff*2, backoffMax)
			continue
		}

		slog.Info("ios tracker: connected to usbmuxd — listening for device events")
		backoff = backoffMin // reset backoff

		// Stop goroutine to close the listener when context is done.
		stopChan := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				_ = closeFunc()
			case <-stopChan:
			}
		}()

		// Loop to read events from the listener function.
		streamErr := func() error {
			for {
				msg, err := listenFunc()
				if err != nil {
					return err
				}

				serial := msg.DeviceEntry().Properties.SerialNumber
				if serial == "" {
					continue
				}

				if msg.DeviceAttached() {
					if _, ok := connected[serial]; !ok {
						slog.Info("ios tracker: device connected", "serial", serial)

						fullDev, err := t.manager.GetProperties(ctx, serial)
						if err != nil {
							slog.Warn("ios tracker: failed to fetch properties, using basic info", "serial", serial, "err", err)
							fullDev = &domain.Device{
								Serial:      serial,
								Platform:    "ios",
								ProviderIP:  "127.0.0.1",
								ConnectedAt: time.Now(),
								LastSeen:    time.Now(),
							}
						}
						connected[serial] = fullDev

						select {
						case ch <- domain.DeviceEvent{
							Serial:    serial,
							Type:      domain.EventConnected,
							Device:    fullDev,
							Timestamp: time.Now(),
						}:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				} else if msg.DeviceDetached() {
					if _, ok := connected[serial]; ok {
						slog.Info("ios tracker: device disconnected", "serial", serial)
						delete(connected, serial)

						select {
						case ch <- domain.DeviceEvent{
							Serial:    serial,
							Type:      domain.EventDisconnected,
							Timestamp: time.Now(),
						}:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				}
			}
		}()

		close(stopChan)
		_ = closeFunc()

		if ctx.Err() != nil {
			slog.Info("ios tracker: stopped")
			return ctx.Err()
		}

		slog.Warn("ios tracker: connection lost, will reconnect", "err", streamErr, "backoff", backoff)
		evictAll()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = minDur(backoff*2, backoffMax)
	}
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
