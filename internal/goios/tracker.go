package goios

import (
	"context"
	"log/slog"
	"time"

	"protean-provider/internal/domain"
)

// Tracker implements domain.DeviceTracker for iOS.
// It polls the connected iOS device list at regular intervals.
type Tracker struct {
	manager      *DeviceManager
	pollInterval time.Duration
}

// NewTracker creates a new iOS device Tracker.
func NewTracker(manager *DeviceManager, pollInterval time.Duration) *Tracker {
	if pollInterval == 0 {
		pollInterval = 3 * time.Second
	}
	return &Tracker{
		manager:      manager,
		pollInterval: pollInterval,
	}
}

// Watch blocks and streams DeviceEvents (EventConnected, EventDisconnected) into the channel.
func (t *Tracker) Watch(ctx context.Context, ch chan<- domain.DeviceEvent) error {
	slog.Info("ios tracker: starting polling loop", "interval", t.pollInterval)
	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()

	connected := make(map[string]*domain.Device)

	for {
		select {
		case <-ctx.Done():
			slog.Info("ios tracker: stopped")
			return ctx.Err()
		case <-ticker.C:
			devices, err := t.manager.Discover(ctx)
			if err != nil {
				slog.Debug("ios tracker: discover failed", "err", err)
				continue
			}

			current := make(map[string]bool)
			for _, dev := range devices {
				current[dev.Serial] = true
				if _, ok := connected[dev.Serial]; !ok {
					slog.Info("ios tracker: device connected", "serial", dev.Serial)
					fullDev, err := t.manager.GetProperties(ctx, dev.Serial)
					if err != nil {
						slog.Warn("ios tracker: failed to fetch properties", "serial", dev.Serial, "err", err)
						fullDev = dev
					}
					connected[dev.Serial] = fullDev

					select {
					case ch <- domain.DeviceEvent{
						Serial:    dev.Serial,
						Type:      domain.EventConnected,
						Device:    fullDev,
						Timestamp: time.Now(),
					}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}

			// Identify disconnected devices
			for serial := range connected {
				if !current[serial] {
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
	}
}
