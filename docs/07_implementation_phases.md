# 07 — Implementation Phases

Step-by-step build plan. Each phase produces working, tested, committable code.

---

## Current State (Baseline)

```
✅ go.mod — exists (needs fix: go 1.25 → go 1.21)
✅ internal/domain/device.go — Device, DeviceInfo, DisplayInfo, BatteryInfo, NetworkInfo
✅ internal/domain/interfaces.go — DeviceTracker, StreamManager, CoordinatorClient (partial)
✅ internal/domain/provider.go — Provider struct (minimal)
✅ internal/adb/tracker.go — Tracker struct (stub, Watch() not implemented)
✅ internal/adb/client.go — Client interface (partial)
⬜ Everything else is empty or missing
```

---

## Phase 1 — Foundation (Week 1)

**Goal:** Project compiles, logs, reads config, exits gracefully.

### Tasks

#### 1.1 Fix `go.mod`
```bash
# Change go 1.25.0 to go 1.21.0
# Add missing direct dependencies
go mod tidy
```

Required additions to `go.mod`:
```
github.com/spf13/viper v1.18.0
github.com/go-playground/validator/v10 v10.22.0
golang.org/x/sync v0.7.0
github.com/google/uuid v1.6.0
```

#### 1.2 Complete Domain Package
- [ ] `internal/domain/events.go` — Add `EventType`, `DeviceEvent` with full fields
- [ ] `internal/domain/errors.go` — Sentinel errors
- [ ] `internal/domain/interfaces.go` — Add `IsCapturing()` to StreamManager; fix typo `StartCaputre` → `StartCapture`
- [ ] `internal/domain/provider.go` — Add `ID`, `Version` fields

#### 1.3 Config Package
- [ ] `internal/config/config.go` — Full `Config` struct + `Load()` with Viper
- [ ] `config/provider.yaml` — Fill in full schema (see `docs/05_configuration.md`)
- [ ] `internal/config/config_test.go` — Test valid config, missing fields, env override

#### 1.4 Logger Package
- [ ] `internal/logger/logger.go` — `New(cfg LoggingConfig) *slog.Logger`
- [ ] `internal/logger/context.go` — `WithLogger()`, `FromContext()`

#### 1.5 App Package
- [ ] `internal/app/app.go` — `App` struct, `New()`, `Run(ctx)`

#### 1.6 Main Entrypoint
- [ ] `cmd/provider/main.go` — flags, config load, signal handling, `app.Run()`

#### 1.7 Makefile
- [ ] `Makefile` — `build`, `run`, `test`, `lint`, `proto`, `certs`, `clean` targets

**Deliverable:** `make build && ./bin/provider --config config/provider.yaml` starts, logs version, and exits cleanly on Ctrl+C.

---

## Phase 2 — ADB Integration (Week 2)

**Goal:** Provider detects device connect/disconnect and logs events.

### Tasks

#### 2.1 ADB Client
- [ ] `internal/adb/client.go` — Implement concrete client wrapping `go-adbkit`
  - `ListDevices() ([]DeviceEntry, error)`
  - `Shell(serial, cmd string) (string, error)`

#### 2.2 ADB Device Properties
- [ ] `internal/adb/properties.go` — `FetchDeviceInfo(serial) (DeviceInfo, error)`
  - Use `adb shell getprop ro.product.model` etc.
  - Parse `wm size` output for display dimensions
  - Set `property_timeout` from config

#### 2.3 ADB Tracker
- [ ] `internal/adb/tracker.go` — Implement `Watch(ctx, ch chan<- DeviceEvent)`
  - Poll `ListDevices()` every `config.ADB.PollInterval`
  - Diff previous vs current list
  - Emit `EventConnected` for new devices
  - Emit `EventDisconnected` for removed devices
  - Reconnect if ADB daemon not reachable

#### 2.4 Device Registry
- [ ] `internal/registry/registry.go` — `sync.Map`-backed, thread-safe
- [ ] `internal/registry/registry_test.go` — Concurrent add/remove/list test

#### 2.5 Tests
- [ ] `internal/adb/tracker_test.go` — Mock device list → verify events emitted

**Deliverable:** Provider starts, connects to ADB, and logs `"device connected serial=XXXX"` when a device is plugged in.

---

## Phase 3 — Coordinator gRPC (Week 3)

**Goal:** Provider registers itself and its devices with the Coordinator.

### Tasks

#### 3.1 Protobuf Definition
- [ ] `pkg/protocol/stf/provider.proto` — Full definition (see `docs/06_grpc_and_proto.md`)
- [ ] `buf.yaml`, `buf.gen.yaml`
- [ ] `pkg/protocol/gen.go` — `//go:generate` directive
- [ ] Run `make proto` → generates `.pb.go` files

#### 3.2 Coordinator Client
- [ ] `internal/coordinator/client.go` — Implements `domain.CoordinatorClient`
  - `Connect()` with mTLS
  - `RegisterProvider()`
  - `RegisterDevice()`
  - `SendHeartbeat()`
  - `ReleaseDevice()`
  - `Disconnect()`

#### 3.3 Reconnect Loop
- [ ] `internal/coordinator/reconnect.go`
  - Exponential backoff: 1s, 2s, 4s, 8s … up to `reconnect_max_backoff`
  - Re-registers provider on every reconnect
  - Logs each attempt

#### 3.4 Tests
- [ ] `internal/coordinator/client_test.go` — Use `google.golang.org/grpc/test/bufconn` in-process server
  - Test RegisterProvider, RegisterDevice, Heartbeat
  - Test reconnect on connection drop

**Deliverable:** Provider registers with Coordinator on startup. Coordinator shows provider in its device list.

---

## Phase 4 — Agent & Supervisor (Week 4)

**Goal:** Full device lifecycle — connect → register → heartbeat → disconnect → release.

### Tasks

#### 4.1 FSM
- [ ] `internal/agent/fsm.go` — State machine with transition table
- [ ] All transitions defined (see `docs/04_domain_and_interfaces.md` section 6)

#### 4.2 Agent
- [ ] `internal/agent/agent.go` — `Agent.Run(ctx)`:
  1. Transition: idle → connecting
  2. Fetch `DeviceInfo` via `adb/properties.go`
  3. Call `coordinator.RegisterDevice()`
  4. Start heartbeat goroutine
  5. Transition: connecting → online
  6. Wait for context cancel (device disconnect or shutdown)
  7. Cleanup: `StopCapture()`, `coordinator.ReleaseDevice()`, remove from registry
- [ ] `internal/agent/agent_test.go` — FSM transition tests, mock coordinator

#### 4.3 Supervisor
- [ ] `internal/supervisor/supervisor.go`:
  - Calls `tracker.Watch()` in a goroutine
  - On `EventConnected`: `go agent.Run(agentCtx)`
  - On `EventDisconnected`: cancel agent context
  - On parent context cancel: cancel all agent contexts
- [ ] `internal/supervisor/supervisor_test.go`

#### 4.4 Wire Everything in App
- [ ] `internal/app/app.go` — Connect all pieces:
  ```
  tracker → supervisor → agent → coordinator + registry
  ```

**Deliverable:** Full lifecycle works end-to-end. Plug in a device → it appears in coordinator. Unplug → it disappears. SIGTERM → all devices released.

---

## Phase 5 — Screen Streaming (Week 5)

**Goal:** Live screen visible in the frontend.

### Tasks

#### 5.1 MJPEG Capture
- [ ] `internal/stream/mjpeg.go`:
  - Launch `scrcpy --video-codec=mjpeg --port=<N>` as subprocess
  - Read JPEG frame stream from subprocess stdout
  - Expose as HTTP MJPEG endpoint on assigned port
  - Handle subprocess crash: restart up to `config.Stream.MaxRestarts`

#### 5.2 Input Relay
- [ ] `internal/stream/relay.go`:
  - Receives gRPC stream of `InputEvent` from Coordinator
  - Touch: `adb shell input tap X Y`
  - Swipe: `adb shell input swipe X1 Y1 X2 Y2`
  - Key: `adb shell input keyevent CODE`
  - Text: `adb shell input text "TEXT"`

#### 5.3 Stream Manager
- [ ] `internal/stream/manager.go` — Implements `domain.StreamManager`:
  - `StartCapture(ctx, serial)` → start MJPEG + input relay
  - `StopCapture(ctx, serial)` → terminate subprocess
  - `IsCapturing(serial) bool`
- [ ] `internal/stream/manager_test.go` — Mock subprocess

#### 5.4 Wire into Agent
- [ ] `internal/agent/agent.go` — Call `stream.StartCapture()` after successful registration
- [ ] On cleanup: call `stream.StopCapture()`

**Deliverable:** Device screen visible in browser through Coordinator/API stream proxy.

---

## Phase 6 — Observability (Week 6)

**Goal:** Production-grade metrics, logging, and health checks.

### Tasks

#### 6.1 Prometheus Metrics
- [ ] `internal/metrics/metrics.go` — Register all collectors
- [ ] `internal/metrics/server.go` — HTTP server on `:9090`
- [ ] Wire into `app.go`
- [ ] Instrument: agent state changes, heartbeat errors, ADB events, stream starts/stops

#### 6.2 Provider gRPC Server
- [ ] `internal/grpc/server.go` — gRPC server on `:9091`
- [ ] `internal/grpc/handler.go` — Implement HealthCheck, GetVersion, ListDevices
- [ ] `internal/grpc/middleware.go` — Logging + recovery interceptors

#### 6.3 Linter Configuration
- [ ] `.golangci.yml` — Enable `errcheck`, `govet`, `staticcheck`, `revive`, `gocyclo`
- [ ] Fix all lint warnings: `make lint`

#### 6.4 Test Coverage
- [ ] Run `go test -race -coverprofile=coverage.out ./...`
- [ ] Target: ≥ 80% coverage
- [ ] Add missing tests to reach target

**Deliverable:** `curl localhost:9090/metrics` returns Prometheus metrics. All lints pass.

---

## Phase 7 — Deployment & CI/CD (Week 7–8)

**Goal:** One-command deploy, automated CI on every push.

### Tasks

#### 7.1 Dockerfile
- [ ] `deploy/Dockerfile` — Multi-stage build (see `docs/10_deployment.md`)
- [ ] Verify: `docker build -t protean-provider:latest -f deploy/Dockerfile .`
- [ ] Verify image size < 50 MB

#### 7.2 systemd Unit
- [ ] `deploy/provider.service`

#### 7.3 Docker Compose Dev Stack
- [ ] `deploy/docker-compose.yml` — Provider + mock coordinator for local integration testing

#### 7.4 TLS Cert Script
- [ ] `scripts/gen-certs.sh` — Generates CA + provider cert for dev

#### 7.5 GitHub Actions
- [ ] `.github/workflows/ci.yml` — On push/PR: vet, test, lint, build
- [ ] `.github/workflows/release.yml` — On tag: build + push Docker image

#### 7.6 README
- [ ] `README.md` — Quickstart, prerequisites, config reference, architecture diagram

**Deliverable:** Push to GitHub → CI runs and passes. `docker pull protean-provider:v1.0.0` works.

---

## Summary Timeline

| Week | Phase | Key Deliverable |
|------|-------|----------------|
| 1 | Foundation | Binary starts, reads config, exits cleanly |
| 2 | ADB Integration | Device plug/unplug logged |
| 3 | Coordinator gRPC | Provider registers with Coordinator |
| 4 | Agent & Supervisor | Full device lifecycle |
| 5 | Streaming | Live screen in browser |
| 6 | Observability | Metrics + health check + linting |
| 7–8 | Deployment | Docker image + CI/CD pipeline |
