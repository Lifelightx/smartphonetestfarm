# 02 — Architecture

---

## 1. System Context Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                      Protean Platform                               │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                   protean-app (Angular UI)                   │   │
│  │         Browser-based device control & booking UI            │   │
│  └──────────────────────────┬───────────────────────────────────┘   │
│                             │  REST + WebSocket                     │
│  ┌──────────────────────────▼───────────────────────────────────┐   │
│  │                  protean-api (Go — future)                   │   │
│  │        REST gateway, JWT auth, WebSocket proxy               │   │
│  └──────────────────────────┬───────────────────────────────────┘   │
│                             │  gRPC (internal)                      │
│  ┌──────────────────────────▼───────────────────────────────────┐   │
│  │              protean-coordinator (Go — future)               │   │
│  │     Device pool, booking engine, heartbeat tracking          │   │
│  │                PostgreSQL  +  Redis                          │   │
│  └────────────────────────┬─────────────────────────────────────┘   │
│                           │  gRPC over mTLS                        │
└───────────────────────────┼─────────────────────────────────────────┘
                            │
         ┌──────────────────▼──────────────────────┐
         │        protean-provider-go (THIS REPO)   │
         │                                          │
         │  ┌──────────┐   ┌──────────┐             │
         │  │   ADB    │   │Supervisor│             │
         │  │ Tracker  │──►│+ Registry│             │
         │  └──────────┘   └────┬─────┘             │
         │                      │ spawns             │
         │               ┌──────▼──────┐            │
         │               │  Agent (×N) │            │
         │               │  per device │            │
         │               └──────┬──────┘            │
         │                      │                   │
         │           ┌──────────▼──────────┐        │
         │           │   Stream Manager    │        │
         │           │  MJPEG + Input Relay│        │
         │           └─────────────────────┘        │
         │                                          │
         │  USB / WiFi-ADB                          │
         └──────────┬───────────────────────────────┘
                    │
      ┌─────────────┼─────────────┐
      ▼             ▼             ▼
  [Device 1]   [Device 2]   [Device N]
  (Android)    (Android)    (Android)
```

---

## 2. Internal Component Architecture

```
cmd/provider/main.go
       │
       ▼
internal/app/app.go          ← Root: wires all deps, manages lifecycle
       │
       ├──► internal/config/     ← Load YAML + env, validate
       ├──► internal/logger/     ← Structured slog logger
       ├──► internal/metrics/    ← Prometheus HTTP server
       ├──► internal/grpc/       ← Provider's own gRPC health server
       │
       ├──► internal/coordinator/   ← gRPC CLIENT to the Coordinator
       │         └── reconnect.go   ← Exponential backoff reconnect
       │
       ├──► internal/adb/           ← ADB layer
       │         ├── client.go      ← go-adbkit wrapper
       │         ├── tracker.go     ← Watch() → DeviceEvent channel
       │         └── properties.go  ← getprop → DeviceInfo
       │
       ├──► internal/registry/      ← Thread-safe device store
       │
       ├──► internal/supervisor/    ← Watches tracker, spawns agents
       │         └── supervisor.go
       │
       ├──► internal/agent/         ← Per-device lifecycle FSM
       │         ├── agent.go
       │         └── fsm.go
       │
       └──► internal/stream/        ← Per-device screen + input
                 ├── manager.go
                 ├── mjpeg.go
                 └── relay.go
```

---

## 3. Data Flow — Device Connect

```
Step 1: ADB Tracker detects USB plug
        adb/tracker.go → emits DeviceEvent{Type: EventConnected, Serial: "ABC123"}

Step 2: Supervisor receives event
        supervisor.go → creates Agent for serial "ABC123"

Step 3: Agent transitions: idle → connecting
        agent.go → calls adb/properties.go to fetch DeviceInfo, DisplayInfo

Step 4: Agent calls Coordinator
        coordinator/client.go → RegisterDevice(Device{Serial, Info, Display, ...})

Step 5: Agent starts stream
        stream/manager.go → StartCapture(ctx, "ABC123")
        stream/mjpeg.go   → launches scrcpy subprocess, pipes JPEG frames

Step 6: Agent transitions: connecting → online
        Periodic heartbeat goroutine starts (every 10s)

Step 7: Coordinator marks device as available in its pool
        Frontend can now show device as bookable
```

---

## 4. Data Flow — Device Disconnect

```
Step 1: ADB Tracker detects USB unplug
        adb/tracker.go → emits DeviceEvent{Type: EventDisconnected, Serial: "ABC123"}

Step 2: Supervisor receives event
        supervisor.go → cancels Agent context for "ABC123"

Step 3: Agent context cancelled → cleanup triggers
        agent.go → StopCapture("ABC123")
                 → ReleaseDevice("ABC123") on Coordinator
                 → removes from Registry

Step 4: Coordinator marks device offline
        Frontend reflects device as unavailable
```

---

## 5. Data Flow — Graceful Shutdown (SIGTERM)

```
OS sends SIGTERM
       │
       ▼
main.go catches signal → cancels root context
       │
       ├── Supervisor: stops ADB Watch loop
       ├── All Agents: run cleanup (StopCapture + ReleaseDevice)
       ├── Coordinator client: Disconnect()
       ├── Metrics HTTP server: Shutdown(30s timeout)
       └── gRPC server: GracefulStop()
       │
       ▼
os.Exit(0)   [hard timeout: 30s]
```

---

## 6. Transport & Security

| Connection | Protocol | Auth |
|------------|----------|------|
| Provider → Coordinator | gRPC / HTTP2 | mTLS (mutual TLS) |
| Provider metrics endpoint | HTTP | None (internal network only) |
| Provider health gRPC server | gRPC | Optional: mTLS |
| ADB → Android device | ADB protocol | RSA key pair (ADB standard) |

### mTLS Certificate Layout
```
/etc/stf/certs/
├── ca.crt           ← CA certificate (shared across all services)
├── provider.crt     ← Provider's certificate (signed by CA)
└── provider.key     ← Provider's private key
```

---

## 7. Concurrency Model

```
main goroutine
  └── app.Start() → launches goroutines via errgroup

errgroup goroutines:
  ├── metrics HTTP server
  ├── gRPC health server
  ├── supervisor (watches ADB events)
  └── coordinator reconnect loop

per-device goroutines (spawned by supervisor):
  ├── agent.Run()          ← owns device FSM
  ├── agent.heartbeat()    ← ticker every 10s
  └── stream.capture()     ← scrcpy subprocess read loop
```

All goroutines share a single `context.Context`. Cancelling root context terminates everything in order.

---

## 8. Port Allocation

| Service | Port | Protocol |
|---------|------|----------|
| ADB daemon (local) | 5037 | TCP (ADB) |
| Provider metrics | 9090 | HTTP |
| Provider gRPC health | 9091 | gRPC |
| Device screen stream ports | 7400–7700 | TCP (MJPEG) |
| Coordinator (remote) | 9000 | gRPC |
