# 02 — Architecture

---

## 1. System Context Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                      Protean Platform Core                          │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                protean-frontend (React Vite UI)              │   │
│  │       Sleek dashboard, user login & live device interaction   │   │
│  └──────────────────────────┬───────────────────────────────────┘   │
│                             │  REST HTTP + WebSockets (Port 5173)   │
│  ┌──────────────────────────▼───────────────────────────────────┐   │
│  │         protean-coordinator (Go REST/gRPC Server)            │   │
│  │   JWT/OIDC Auth & RBAC, Booking Engine, WS Manager, Scheduler│   │
│  └──────┬─────────────────────────────────────────────────┬─────┘   │
│         │                                                 │         │
│         │ gRPC / HTTP (mTLS)                              │ SQL     │
│         │                                                 │         │
│         │                                        ┌────────▼───────┐ │
│         │                                        │  PostgreSQL 16 │ │
│         │                                        │  User/Device/  │ │
│         │                                        │  Group Store   │ │
│         │                                        └────────────────┘ │
└─────────┼───────────────────────────────────────────────────────────┘
          │
          ├─────────────────────────────────────────────────┐
          │ gRPC over host network / mTLS (Port 9000/9091)   │
          ▼                                                 ▼
┌──────────────────────────────────┐               ┌──────────────────────────────────┐
│  protean-provider (Android Node) │               │   protean-provider (iOS Node)    │
│                                  │               │                                  │
│  ┌──────────┐      ┌──────────┐  │               │  ┌──────────┐      ┌──────────┐  │
│  │   ADB    │      │Supervisor│  │               │  │  go-ios  │      │Supervisor│  │
│  │ Tracker  │─────►│+ Registry│  │               │  │ Tracker  │─────►│+ Registry│  │
│  └──────────┘      └────┬─────┘  │               │  └──────────┘      └────┬─────┘  │
│                         │ spawns │               │                         │ spawns │
│                  ┌──────▼──────┐ │               │                  ┌──────▼──────┐ │
│                  │  Agent (×N) │ │               │                  │  Agent (×N) │ │
│                  │  per device │ │               │                  │  per device │ │
│                  └──────┬──────┘ │               │                  └──────┬──────┘ │
│                         │        │               │                         │        │
│              ┌──────────▼────────┐               │              ┌──────────▼────────┐
│              │   Stream Manager  │               │              │   Stream Manager  │
│              │MJPEG/H264 & Input │               │              │WDA Stream & Input │
│              └───────────────────┘               │              └───────────────────┘
│                                  │               │                                  │
│  USB / WiFi-ADB                  │               │  USB (usbmuxd)                   │
└─────────┬────────────────────────┘               └─────────┬────────────────────────┘
          │                                                  │
    ┌─────┴─────┐                                      ┌─────┴─────┐
    ▼           ▼                                      ▼           ▼
[Device 1]  [Device N]                             [Device 1]  [Device N]
(Android)   (Android)                              (iOS)       (iOS)
```


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
| Frontend → Coordinator REST | HTTPS / HTTP | JWT (bearer token) or OIDC |
| Frontend → Coordinator WS | WebSockets | WS authorization token / JWT |
| Client → Coordinator (CI/CD) | HTTPS / HTTP | Pre-shared API Key (`X-API-Key`) |
| Coordinator → PostgreSQL | TCP / Postgres protocol | Password (or SSL client certificates) |
| Provider metrics endpoint | HTTP | None (internal network only) |
| Provider health gRPC server | gRPC | Optional: mTLS |
| ADB → Android device | ADB protocol | RSA key pair (ADB standard) |
| Provider → iOS device | HTTP (WebDriverAgent) | None (isolated local network) |

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
  ├── supervisor (watches ADB / iOS USB events)
  └── coordinator reconnect loop

per-device goroutines (spawned by supervisor):
  ├── agent.Run()          ← owns device FSM (Android / iOS)
  ├── agent.heartbeat()    ← ticker every 10s
  └── stream.capture()     ← scrcpy or WebDriverAgent stream read loop
```

All goroutines share a single `context.Context`. Cancelling the root context terminates everything in order.

---

## 8. Port Allocation

| Service | Port | Protocol | Description |
|---------|------|----------|-------------|
| ADB daemon (local) | 5037 | TCP | Local Android Debug Bridge Daemon |
| usbmuxd (local socket) | Unix Socket | IPC | iOS Device Discovery Bridge |
| Provider metrics | 9090 | HTTP | Prometheus metrics scraping |
| Provider gRPC health | 9091 | gRPC | Coordinator-to-Provider health check |
| Device screen stream ports | 7400–7700 | TCP | Individual MJPEG streams (Android / iOS) |
| Coordinator gRPC | 9000 | gRPC | Bi-directional provider registration/heartbeats |
| Coordinator REST / WS | 9002 | HTTP / WS | Frontend REST endpoints and WebSocket stream broker |
| PostgreSQL Database | 5432 | TCP | User, Group, API key, and schema storage |
| Frontend Web UI | 5173 | HTTP | React Web application dashboard (served via Nginx) |

