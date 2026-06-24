package domain

import "time"

// EventType classifies what happened to a device.
type EventType string

const (
	EventConnected    EventType = "connected"
	EventDisconnected EventType = "disconnected"
	EventUnauthorized EventType = "unauthorized"
	EventOffline      EventType = "offline"
)

// DeviceEvent is emitted by the ADB tracker whenever a device state changes.
type DeviceEvent struct {
	Serial    string
	Type      EventType
	Device    *Device   // populated on EventConnected (after property fetch); nil otherwise
	Timestamp time.Time
}