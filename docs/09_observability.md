# 09 — Observability (Logging, Metrics, Health)

---

## 1. Logging

### Library
`log/slog` — Go 1.21+ standard library. No external dependency.

### Logger Initialization

```go
// internal/logger/logger.go

func New(cfg config.LoggingConfig) *slog.Logger {
    var handler slog.Handler

    opts := &slog.HandlerOptions{
        Level: parseLevel(cfg.Level),
        ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
            if a.Key == slog.TimeKey {
                a.Value = slog.StringValue(a.Value.Time().Format(time.RFC3339))
            }
            return a
        },
    }

    if cfg.Format == "json" {
        handler = slog.NewJSONHandler(os.Stdout, opts)
    } else {
        handler = slog.NewTextHandler(os.Stdout, opts)
    }

    return slog.New(handler).With(
        "service", "protean-provider",
        "version", buildinfo.Version,
    )
}
```

### Log Format (JSON Production Output)

```json
{
  "time": "2026-06-16T10:30:00Z",
  "level": "INFO",
  "msg": "device registered",
  "service": "protean-provider",
  "version": "1.2.3",
  "provider": "lab-provider-01",
  "serial": "R3CX107ABCD",
  "model": "Galaxy S23",
  "status": "online"
}
```

### Logging Standards

| When | What to log | Level |
|------|-------------|-------|
| Provider starts | version, config path, coordinator address | INFO |
| Provider registered | provider ID from coordinator | INFO |
| Device connected | serial, model, android version | INFO |
| Device registered | serial, coordinator response | INFO |
| Heartbeat error | serial, error, attempt number | WARN |
| Reconnecting to coordinator | attempt, wait duration | WARN |
| Device unauthorized | serial | WARN |
| Stream process crashed | serial, restart count | WARN |
| Stream failed (max restarts) | serial, error | ERROR |
| Coordinator unreachable | address, last error | ERROR |
| ADB daemon not found | host, port | ERROR |
| Device released | serial, reason | INFO |
| Provider shutting down | reason | INFO |

### Context Logger Pattern

```go
// Pass logger via context to avoid threading logger through every function signature
ctx = logger.WithLogger(ctx, log.With("serial", device.Serial))

// Retrieve anywhere:
log := logger.FromContext(ctx)
log.Info("heartbeat sent", "battery", state.Battery.Level)
```

---

## 2. Prometheus Metrics

### Metric Definitions

```go
// internal/metrics/metrics.go

var (
    DevicesConnectedTotal = promauto.NewCounter(prometheus.CounterOpts{
        Name: "provider_devices_connected_total",
        Help: "Total number of device connections since startup",
    })

    DevicesOnline = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "provider_devices_online",
        Help: "Number of devices currently in online state",
    })

    DevicesBusy = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "provider_devices_busy",
        Help: "Number of devices currently allocated to a session",
    })

    HeartbeatErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
        Name: "provider_heartbeat_errors_total",
        Help: "Total number of failed heartbeat sends",
    })

    ADBEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "provider_adb_events_total",
        Help: "Total ADB device events received",
    }, []string{"type"}) // type: connected | disconnected

    StreamActive = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "provider_stream_active",
        Help: "Number of active screen capture streams",
    })

    StreamRestarts = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "provider_stream_restarts_total",
        Help: "Total stream subprocess restart count",
    }, []string{"serial"})

    GRPCCallDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "provider_grpc_call_duration_seconds",
        Help:    "gRPC call duration",
        Buckets: prometheus.DefBuckets,
    }, []string{"method", "status"})

    CoordinatorReconnectsTotal = promauto.NewCounter(prometheus.CounterOpts{
        Name: "provider_coordinator_reconnects_total",
        Help: "Total number of coordinator reconnect attempts",
    })
)
```

### Instrumentation Points

```go
// In agent.go — when device state changes:
metrics.DevicesOnline.Inc()         // on → online
metrics.DevicesOnline.Dec()         // on → offline
metrics.DevicesBusy.Inc()           // on → busy
metrics.DevicesBusy.Dec()           // busy → online or offline
metrics.DevicesConnectedTotal.Inc() // on EventConnected

// In adb/tracker.go:
metrics.ADBEventsTotal.WithLabelValues("connected").Inc()
metrics.ADBEventsTotal.WithLabelValues("disconnected").Inc()

// In coordinator/client.go:
metrics.HeartbeatErrorsTotal.Inc()  // on heartbeat failure
metrics.GRPCCallDuration.WithLabelValues(method, status).Observe(duration)
```

### Metrics HTTP Server

```go
// internal/metrics/server.go

func NewServer(cfg config.MetricsConfig) *Server {
    mux := http.NewServeMux()
    mux.Handle(cfg.Path, promhttp.Handler())
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("ok"))
    })
    return &Server{
        srv: &http.Server{
            Addr:    fmt.Sprintf(":%d", cfg.Port),
            Handler: mux,
        },
    }
}
```

---

## 3. Health Checks

### HTTP Health Endpoint
Available on the metrics server:

| Path | Returns | Purpose |
|------|---------|---------|
| `GET /healthz` | `200 ok` | Basic liveness — process is running |
| `GET /readyz` | `200 ok` / `503` | Readiness — connected to coordinator |
| `GET /metrics` | Prometheus text | All metrics |

### gRPC Health Check
The provider's gRPC server (`:9091`) implements the standard `grpc.health.v1.Health` service:

```protobuf
service Health {
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
  rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse);
}
```

Status values returned:
- `SERVING` — provider connected to coordinator, ADB reachable
- `NOT_SERVING` — coordinator disconnected or ADB daemon down

### Kubernetes Probe Config (if deployed on K8s)
```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 9090
  initialDelaySeconds: 5
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /readyz
    port: 9090
  initialDelaySeconds: 10
  periodSeconds: 5
```

---

## 4. Tracing (Future)

When/if distributed tracing is needed:
- Library: `go.opentelemetry.io/otel`
- Exporter: Jaeger or OTLP
- Instrument: gRPC calls (auto via otelgrpc interceptor)
- Propagate: trace context via gRPC metadata

Not required for Phase 1–7. Add in a future observability phase.
