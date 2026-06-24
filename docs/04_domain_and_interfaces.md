# 04 — Domain Models & Interfaces

The `internal/domain` package is the **core contract** of the entire application.
It has zero external dependencies (stdlib only). Everything else depends on it.

---

## 1. Provider

```go
// provider.go
package domain

type Provider struct {
    ID      string  // UUID generated at startup
    Name    string  // Human-readable name, e.g. "lab-rack-01"
    Host    string  // Hostname of the provider machine
    IP      string  // IP address of the provider machine
    Version string  // Binary version, e.g. "1.2.3"
}
```

---

## 2. Device

```go
// device.go
package domain

import "time"

type DeviceStatus string

const (
    StatusOnline       DeviceStatus = "online"
    StatusOffline      DeviceStatus = "offline"
    StatusUnauthorized DeviceStatus = "unauthorized"
    StatusBusy         DeviceStatus = "busy"
)

type Device struct {
    Serial     string
    ProviderIP string

    Info    DeviceInfo
    Display DisplayInfo
    State   DeviceState

    ConnectedAt time.Time
    LastSeen    time.Time
}

type DeviceInfo struct {
    Model          string
    MarketName     string
    Manufacturer   string
    AndroidVersion string
    SDKVersion     int
    CPUABI         string
    RAMMB          int64
    StorageMB      int64
}

type DisplayInfo struct {
    Width    int32
    Height   int32
    Density  int32
    FPS      int32
    Rotation int32
    Size     float64
    XDPI     float64
    YDPI     float64
}

type BatteryInfo struct {
    Level      int
    IsCharging bool
    Health     string
}

type NetworkInfo struct {
    Connected  bool
    WiFiSSID   string
    IP         string
    MobileData bool
    Airplane   bool
}

type DeviceState struct {
    Status  DeviceStatus
    Battery BatteryInfo
    Network NetworkInfo
}
```

---

## 3. Events

```go
// events.go
package domain

import "time"

type EventType string

const (
    EventConnected    EventType = "connected"
    EventDisconnected EventType = "disconnected"
    EventStateChanged EventType = "state_changed"
)

type DeviceEvent struct {
    Serial    string
    Type      EventType
    State     string    // raw ADB state string (device, offline, unauthorized)
    Timestamp time.Time
}
```

---

## 4. Sentinel Errors

```go
// errors.go
package domain

import "errors"

var (
    ErrDeviceNotFound   = errors.New("device not found")
    ErrNotConnected     = errors.New("not connected to coordinator")
    ErrStreamFailed     = errors.New("stream failed to start")
    ErrDeviceBusy       = errors.New("device is busy")
    ErrInvalidSerial    = errors.New("invalid device serial")
)
```

---

## 5. Core Interfaces

All interfaces are defined here. Each is implemented in a concrete package under `internal/`.

```go
// interfaces.go
package domain

import "context"

// DeviceTracker watches ADB for device events.
// Implemented by: internal/adb.Tracker
type DeviceTracker interface {
    Watch(ctx context.Context, ch chan<- DeviceEvent) error
}

// StreamManager controls per-device screen capture and input relay.
// Implemented by: internal/stream.Manager
type StreamManager interface {
    StartCapture(ctx context.Context, serial string) error
    StopCapture(ctx context.Context, serial string) error
    IsCapturing(serial string) bool
}

// CoordinatorClient communicates with the central Protean Coordinator.
// Implemented by: internal/coordinator.Client
type CoordinatorClient interface {
    Connect(ctx context.Context) error
    RegisterProvider(ctx context.Context, p Provider) error
    RegisterDevice(ctx context.Context, d Device) error
    SendHeartbeat(ctx context.Context, serial string, state DeviceState) error
    ReleaseDevice(ctx context.Context, serial string) error
    Disconnect() error
}
```

---

## 6. Agent State Machine (FSM)

### States

| State | Meaning |
|-------|---------|
| `StateIdle` | Agent created, not yet started |
| `StateConnecting` | Fetching device properties, registering with Coordinator |
| `StateOnline` | Device registered, stream running, heartbeat active |
| `StateBusy` | Device allocated to a user session |
| `StateOffline` | Device disconnected or errored |

### Transitions

```
                    ┌──────────────────────┐
                    │                      │
             ┌──────▼──────┐        ┌──────▼──────┐
    start    │             │ success │             │  allocate
  ──────────►│ connecting  ├────────►│   online    ├──────────►┐
             │             │         │             │           │
             └──────┬──────┘         └──────┬──────┘    ┌──────▼──────┐
                    │ failure               │ disconnect │             │
                    │                       │            │    busy     │
                    ▼                       ▼            │             │
             ┌─────────────┐        ┌──────────────┐    └──────┬──────┘
             │             │        │              │           │
             │   offline   │◄───────│   offline    │◄──────────┘ release / disconnect
             │             │        │              │
             └─────────────┘        └──────────────┘
```

### FSM Implementation

```go
// internal/agent/fsm.go
type State string

const (
    StateIdle       State = "idle"
    StateConnecting State = "connecting"
    StateOnline     State = "online"
    StateBusy       State = "busy"
    StateOffline    State = "offline"
)

type Event string

const (
    EventStart      Event = "start"
    EventSuccess    Event = "success"
    EventFailure    Event = "failure"
    EventAllocate   Event = "allocate"
    EventRelease    Event = "release"
    EventDisconnect Event = "disconnect"
)

type FSM struct {
    current State
    mu      sync.Mutex
}

func (f *FSM) Transition(event Event) (State, error) {
    f.mu.Lock()
    defer f.mu.Unlock()
    // transition table lookup
    next, ok := transitions[f.current][event]
    if !ok {
        return f.current, fmt.Errorf("invalid transition: %s -[%s]-> ?", f.current, event)
    }
    f.current = next
    return next, nil
}
```
