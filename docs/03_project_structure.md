# 03 — Project Structure

Every file and folder in `protean-provider-go` — what it is, what it does, and why it exists.

---

## Full Tree

```
protean-provider-go/
│
├── cmd/
│   └── provider/
│       └── main.go
│
├── config/
│   └── provider.yaml
│
├── internal/
│   ├── app/
│   │   └── app.go
│   ├── adb/
│   │   ├── client.go
│   │   ├── tracker.go
│   │   ├── properties.go
│   │   └── tracker_test.go
│   ├── agent/
│   │   ├── agent.go
│   │   ├── fsm.go
│   │   └── agent_test.go
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   ├── coordinator/
│   │   ├── client.go
│   │   ├── reconnect.go
│   │   └── client_test.go
│   ├── domain/
│   │   ├── device.go
│   │   ├── events.go
│   │   ├── interfaces.go
│   │   ├── provider.go
│   │   └── errors.go
│   ├── grpc/
│   │   ├── server.go
│   │   ├── handler.go
│   │   └── middleware.go
│   ├── logger/
│   │   ├── logger.go
│   │   └── context.go
│   ├── metrics/
│   │   ├── metrics.go
│   │   └── server.go
│   ├── registry/
│   │   ├── registry.go
│   │   └── registry_test.go
│   ├── stream/
│   │   ├── manager.go
│   │   ├── mjpeg.go
│   │   ├── relay.go
│   │   └── manager_test.go
│   └── supervisor/
│       ├── supervisor.go
│       └── supervisor_test.go
│
├── pkg/
│   └── protocol/
│       ├── gen.go
│       └── stf/
│           ├── provider.proto
│           ├── provider.pb.go          ← generated
│           └── provider_grpc.pb.go     ← generated
│
├── scripts/
│   ├── gen-certs.sh
│   └── install-tools.sh
│
├── deploy/
│   ├── Dockerfile
│   ├── docker-compose.yml
│   └── provider.service
│
├── docs/                               ← YOU ARE HERE
│
├── .github/
│   └── workflows/
│       ├── ci.yml
│       └── release.yml
│
├── .golangci.yml
├── buf.gen.yaml
├── buf.yaml
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

---

## File-by-File Reference

### `cmd/provider/main.go`
**The binary entrypoint.**
- Parses CLI flags (`--config`, `--log-level`)
- Calls `internal/config` to load config
- Initialises logger
- Creates `internal/app.App` and calls `app.Run(ctx)`
- Sets up OS signal handling (SIGTERM, SIGINT) → context cancel

```
Role: Wiring only. No business logic here.
Pattern: Keep main.go < 50 lines.
```

---

### `config/provider.yaml`
**Default configuration file.**
- Shipped with the binary
- Can be overridden with `--config /path/to/custom.yaml`
- All values can be overridden with env vars
- See `docs/05_configuration.md` for full schema

---

### `internal/app/app.go`
**Application root — owns the lifecycle.**
- Constructs all dependencies in the right order
- Starts all goroutines using `golang.org/x/sync/errgroup`
- Returns an error if any goroutine fails fatally
- Handles ordered shutdown when context is cancelled

```go
type App struct {
    cfg         *config.Config
    log         *slog.Logger
    coordinator domain.CoordinatorClient
    tracker     domain.DeviceTracker
    registry    *registry.Registry
    supervisor  *supervisor.Supervisor
    metrics     *metrics.Server
    grpcServer  *grpc.Server
}
```

---

### `internal/adb/client.go`
**Concrete ADB client wrapping `go-adbkit`.**
- Satisfies the `domain.ADBClient` interface
- Exposes: `Shell(serial, cmd)` for running `adb shell` commands
- Used by `properties.go` for one-shot property fetches
- **NOT used for device list watching** — that is the Tracker's job via `track-devices`
- All calls include a `property_timeout` deadline from config

---

### `internal/adb/tracker.go`
**Watches ADB for device connect/disconnect — the event source.**
- Satisfies `domain.DeviceTracker` interface
- `Watch(ctx, ch)` uses ADB's **native `host:track-devices` push protocol** — NOT polling
- Opens a single persistent TCP connection to the ADB server (`:5037`)
- ADB server **pushes** an event the instant any device connects or disconnects
- Zero lag, zero CPU cost between events — goroutine sleeps until ADB sends data
- On ADB daemon crash/restart: reconnects with 2s backoff and resumes watching

```
How it works (wire protocol):
  1. TCP connect to ADB server :5037
  2. Send "host:track-devices"
  3. ADB replies "OKAY" then keeps connection open
  4. ADB pushes "<serial>\t<state>\n" the moment any device changes
  5. We map state → EventConnected / EventDisconnected and send to channel

Key: This is the entry point for ALL device lifecycle events.
Key: One persistent connection handles ALL devices on this machine.
```

---

### `internal/adb/properties.go`
**Fetches detailed device properties via `adb shell getprop`.**
- Called once when a device first connects
- Parses `ro.product.model`, `ro.product.manufacturer`, `ro.build.version.release`, etc.
- Returns a fully populated `domain.DeviceInfo` and `domain.DisplayInfo`

Key properties fetched:
```
ro.product.model          → DeviceInfo.Model
ro.product.manufacturer   → DeviceInfo.Manufacturer
ro.build.version.release  → DeviceInfo.AndroidVersion
ro.build.version.sdk      → DeviceInfo.SDKVersion
ro.product.cpu.abi        → DeviceInfo.CPUABI
wm size                   → DisplayInfo.Width, Height
wm density                → DisplayInfo.Density
```

---

### `internal/agent/fsm.go`
**Device lifecycle state machine.**

States:
```
idle → connecting → online → busy → offline
            ↓           ↓
          error       disconnect
            ↓           ↓
          offline ←────┘
```

Transitions:
| From | Event | To | Action |
|------|-------|-----|--------|
| idle | connect | connecting | fetch properties |
| connecting | success | online | register + start stream |
| connecting | failure | offline | log error, emit offline |
| online | allocate | busy | notify coordinator |
| online | disconnect | offline | stop stream, release |
| busy | release | online | notify coordinator |
| busy | disconnect | offline | stop stream, release |

---

### `internal/agent/agent.go`
**Per-device goroutine — owns the full device lifecycle.**
- One Agent per connected device, managed by Supervisor
- On start: runs FSM, fetches metadata, registers with Coordinator
- Runs heartbeat ticker (configurable interval)
- On context cancel: cleans up stream, calls ReleaseDevice on Coordinator

```go
type Agent struct {
    serial      string
    cfg         *config.Config
    log         *slog.Logger
    coordinator domain.CoordinatorClient
    stream      domain.StreamManager
    registry    *registry.Registry
    fsm         *FSM
}
```

---

### `internal/config/config.go`
**Configuration loader.**
- Uses `github.com/spf13/viper`
- Loads YAML from `--config` flag path
- Auto-binds environment variables with `PROVIDER_` prefix
- Validates required fields on startup
- Returns typed `Config` struct (no raw maps in the app)

---

### `internal/coordinator/client.go`
**gRPC client to the Protean Coordinator.**
- Satisfies `domain.CoordinatorClient` interface
- Manages a single persistent gRPC connection
- Uses `google.golang.org/grpc` with mTLS credentials
- All calls have a deadline from config

Methods:
```go
Connect(ctx) error
RegisterProvider(ctx, Provider) error
RegisterDevice(ctx, Device) error
SendHeartbeat(ctx, serial, DeviceState) error
ReleaseDevice(ctx, serial) error
Disconnect() error
```

---

### `internal/coordinator/reconnect.go`
**Exponential backoff reconnect loop.**
- Watches connection state changes via gRPC connectivity API
- On disconnect: waits with exponential backoff (min 1s, max 2m)
- Re-calls `Connect()` and `RegisterProvider()` after reconnect
- Logs every reconnect attempt with attempt number and wait duration

---

### `internal/domain/`
**Pure domain models and interfaces — no external dependencies.**

| File | Contents |
|------|----------|
| `device.go` | `Device`, `DeviceInfo`, `DisplayInfo`, `BatteryInfo`, `NetworkInfo`, `DeviceStatus` |
| `events.go` | `DeviceEvent`, `EventType` constants |
| `interfaces.go` | `DeviceTracker`, `StreamManager`, `CoordinatorClient` interfaces |
| `provider.go` | `Provider` struct |
| `errors.go` | Sentinel errors: `ErrDeviceNotFound`, `ErrNotConnected`, `ErrStreamFailed` |

This package has **zero imports** outside stdlib. It is the contract the entire app is built around.

---

### `internal/grpc/server.go`
**The provider's own gRPC server (inbound).**
- Lets the Coordinator call back into the provider
- Exposes: `HealthCheck()`, `GetVersion()`, `ListDevices()`
- Used by Coordinator to verify provider liveness independently of heartbeat

---

### `internal/grpc/middleware.go`
**gRPC interceptors (middleware).**
- Logging interceptor: logs every RPC call with duration
- Recovery interceptor: catches panics, returns gRPC Internal error
- Auth interceptor: validates incoming gRPC metadata tokens

---

### `internal/logger/logger.go`
**Structured logger using `log/slog` (stdlib Go 1.21+).**
- JSON format in production, human-readable text in dev
- Returns `*slog.Logger` configured from `config.LoggingConfig`
- Default attributes: `service=protean-provider`, `version=<build version>`

---

### `internal/logger/context.go`
**Logger stored in and retrieved from `context.Context`.**
```go
func WithLogger(ctx context.Context, l *slog.Logger) context.Context
func FromContext(ctx context.Context) *slog.Logger
```
Allows any function to log without passing logger explicitly.

---

### `internal/metrics/metrics.go`
**Prometheus metrics definitions.**

All metrics use the `provider_` namespace:
```
provider_devices_connected_total    counter
provider_devices_online             gauge
provider_devices_busy               gauge
provider_heartbeat_errors_total     counter
provider_adb_events_total           counter   labels: type=[connected|disconnected]
provider_stream_active              gauge
provider_grpc_calls_total           counter   labels: method, status
```

---

### `internal/metrics/server.go`
**HTTP server exposing `/metrics`.**
- Listens on `config.Metrics.Port` (default 9090)
- Serves Prometheus text format
- Gracefully shuts down on context cancel

---

### `internal/registry/registry.go`
**Thread-safe in-memory device store.**
- Backed by `sync.Map`
- Operations: `Add(device)`, `Remove(serial)`, `Get(serial)`, `List()`, `Count()`
- `List()` returns a snapshot — safe to iterate without lock

---

### `internal/stream/manager.go`
**Lifecycle manager for per-device streams.**
- Satisfies `domain.StreamManager` interface
- `StartCapture(ctx, serial)` → starts MJPEG capture subprocess
- `StopCapture(ctx, serial)` → terminates subprocess gracefully
- Tracks active streams in a map keyed by serial

---

### `internal/stream/mjpeg.go`
**MJPEG screen capture via scrcpy or minicap.**
- Launches `scrcpy` as a subprocess in server mode
- Reads JPEG frames from subprocess stdout
- Pipes frames to a per-device HTTP endpoint or gRPC stream
- Restarts subprocess on crash (up to 3 times with backoff)

---

### `internal/stream/relay.go`
**Input relay — translates commands to ADB shell input.**
- Receives touch/key events from Coordinator (via gRPC stream)
- Translates to `adb shell input tap X Y` or `adb shell input keyevent CODE`
- Processes events sequentially per device

---

### `internal/supervisor/supervisor.go`
**Manages all per-device agents.**
- Calls `tracker.Watch()` to get the event stream
- On `EventConnected`: creates and starts new `Agent` for that serial
- On `EventDisconnected`: cancels that agent's context
- Maintains a map of `serial → *Agent` for cleanup on shutdown

---

### `pkg/protocol/stf/provider.proto`
**Protobuf definition for the Provider ↔ Coordinator contract.**
- Defines all gRPC services and message types
- The generated `.pb.go` files are checked into source control
- Regenerate with `make proto`
- See `docs/06_grpc_and_proto.md` for full definition

---

### `scripts/gen-certs.sh`
**Generates self-signed mTLS certificates for local development.**
- Creates `ca.crt`, `provider.crt`, `provider.key`
- Outputs to `./certs/` directory
- For production, replace with real certificates

---

### `deploy/Dockerfile`
**Multi-stage Docker build.**
- Stage 1: `golang:1.21-alpine` — compiles binary
- Stage 2: `alpine:3.19` — runtime, installs `android-tools` (for adb)
- Final image size target: < 50 MB

---

### `deploy/provider.service`
**Systemd unit for bare-metal / VM deployment.**
- `After=network.target`
- `Restart=on-failure` with 5s delay
- `ExecStart=/usr/local/bin/provider --config /etc/stf/provider.yaml`
- Logs go to journald

---

### `.golangci.yml`
**Linter configuration for `golangci-lint`.**

Enabled linters:
- `errcheck` — all errors must be handled
- `govet` — Go vet checks
- `staticcheck` — static analysis
- `revive` — style rules
- `gocyclo` — cyclomatic complexity limit
- `misspell` — spelling in comments
- `gocritic` — code style suggestions
