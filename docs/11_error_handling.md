# 11 — Error Handling

---

## 1. Principles

1. **Wrap errors with context** — `fmt.Errorf("RegisterDevice: %w", err)`
2. **Sentinel errors** — defined in `internal/domain/errors.go`, used for `errors.Is()` checks
3. **Never swallow errors silently** — always log or return
4. **Distinguish fatal vs recoverable** — fatal = `os.Exit`, recoverable = retry or log+continue
5. **gRPC status codes** — map to appropriate retry behaviour

---

## 2. Error Scenarios & Recovery Strategy

### 2.1 Startup Errors (Fatal — exit immediately)

| Error | Cause | Action |
|-------|-------|--------|
| Config file not found | Wrong `--config` path | Log error + `os.Exit(1)` |
| Config validation failed | Missing required field | Log which field + `os.Exit(1)` |
| Cannot load TLS certs | Wrong cert path or corrupt file | Log error + `os.Exit(1)` |
| Cannot bind metrics port | Port already in use | Log error + `os.Exit(1)` |

```go
cfg, err := config.Load(*configPath)
if err != nil {
    slog.Error("failed to load config", "error", err)
    os.Exit(1)
}
```

---

### 2.2 ADB Errors (Recoverable — retry)

| Error | Cause | Action |
|-------|-------|--------|
| ADB daemon not running | `adb start-server` not called | Retry every `poll_interval`, log WARN |
| Device unauthorized | No ADB RSA key approved | Emit `StatusUnauthorized` event, skip registration |
| `getprop` timeout | Device slow or unresponsive | Retry once, then mark device as degraded |
| ADB connection lost mid-operation | ADB daemon crashed | Re-establish, continue polling |

```go
func (t *Tracker) Watch(ctx context.Context, ch chan<- domain.DeviceEvent) error {
    for {
        devices, err := t.client.ListDevices(ctx)
        if err != nil {
            slog.Warn("adb list devices failed, retrying", "error", err)
            select {
            case <-ctx.Done():
                return nil
            case <-time.After(t.cfg.PollInterval):
                continue // retry
            }
        }
        // process devices ...
    }
}
```

---

### 2.3 Coordinator gRPC Errors

| gRPC Code | Meaning | Action |
|-----------|---------|--------|
| `Unavailable` | Coordinator down | Trigger reconnect loop |
| `DeadlineExceeded` | Call timed out | Log WARN, will retry on next heartbeat |
| `Unauthenticated` | Bad TLS cert | Log ERROR, exit (cert misconfiguration) |
| `InvalidArgument` | Bug in our request | Log ERROR with request details, skip device |
| `AlreadyExists` | Device already registered | Log INFO (idempotent), treat as success |
| `NotFound` | Device not found on release | Log WARN (already released), treat as success |

```go
func handleGRPCError(err error, operation string) error {
    st, ok := status.FromError(err)
    if !ok {
        return fmt.Errorf("%s: %w", operation, err)
    }

    switch st.Code() {
    case codes.Unavailable, codes.DeadlineExceeded:
        // Transient — caller should retry
        return fmt.Errorf("%s transient error: %w", operation, err)

    case codes.AlreadyExists, codes.NotFound:
        // Idempotent — not a real error
        slog.Info(operation+" idempotent", "code", st.Code())
        return nil

    case codes.InvalidArgument:
        // Bug — log and don't retry
        slog.Error(operation+" invalid argument", "details", st.Message())
        return fmt.Errorf("%s bug: %w", operation, err)

    case codes.Unauthenticated:
        // Config error — exit
        slog.Error("authentication failed — check TLS certificates", "error", st.Message())
        os.Exit(1)

    default:
        return fmt.Errorf("%s: %w", operation, err)
    }
    return nil
}
```

---

### 2.4 Stream Errors (Retry with backoff)

| Error | Cause | Action |
|-------|-------|--------|
| `scrcpy` binary not found | Not installed on host | Log ERROR, mark stream as unavailable |
| Stream process exits unexpectedly | scrcpy crash | Restart up to `max_restarts` times |
| Stream port conflict | Port in range already used | Try next port in range |
| Max restarts exceeded | Repeated crashes | Log ERROR, mark device as degraded, notify coordinator |

```go
func (m *MJPEGCapture) start(ctx context.Context) error {
    for attempt := 0; attempt <= m.maxRestarts; attempt++ {
        if attempt > 0 {
            wait := time.Duration(attempt) * 2 * time.Second
            slog.Warn("restarting stream", "serial", m.serial, "attempt", attempt, "wait", wait)
            select {
            case <-ctx.Done():
                return nil
            case <-time.After(wait):
            }
        }

        err := m.runCapture(ctx)
        if err == nil || ctx.Err() != nil {
            return nil // clean exit
        }
        slog.Error("stream process failed", "serial", m.serial, "error", err)
        metrics.StreamRestarts.WithLabelValues(m.serial).Inc()
    }
    return fmt.Errorf("stream failed after %d restarts", m.maxRestarts)
}
```

---

### 2.5 Coordinator Reconnect (Exponential Backoff)

```go
// internal/coordinator/reconnect.go

func (c *Client) reconnectLoop(ctx context.Context) {
    backoff := time.Second
    maxBackoff := c.cfg.ReconnectMaxBackoff

    for {
        select {
        case <-ctx.Done():
            return
        case <-time.After(backoff):
        }

        metrics.CoordinatorReconnectsTotal.Inc()
        slog.Warn("reconnecting to coordinator", "address", c.cfg.Address, "backoff", backoff)

        if err := c.Connect(ctx); err != nil {
            backoff = min(backoff*2, maxBackoff)
            slog.Error("reconnect failed", "error", err, "next_attempt", backoff)
            continue
        }

        // Re-register provider after reconnect
        if err := c.RegisterProvider(ctx, c.provider); err != nil {
            slog.Error("re-registration failed after reconnect", "error", err)
            continue
        }

        slog.Info("reconnected to coordinator")
        backoff = time.Second // reset backoff on success
        return
    }
}
```

---

### 2.6 Graceful Shutdown Errors

On SIGTERM, all cleanup operations are attempted even if some fail:

```go
func (a *App) shutdown(ctx context.Context) {
    // 30 second hard timeout
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Release all devices — best effort, log but don't fatal
    for _, agent := range a.supervisor.Agents() {
        if err := agent.Release(shutdownCtx); err != nil {
            slog.Error("failed to release device on shutdown",
                "serial", agent.Serial(),
                "error", err,
            )
        }
    }

    // Disconnect from coordinator
    if err := a.coordinator.Disconnect(); err != nil {
        slog.Warn("coordinator disconnect error", "error", err)
    }
}
```

---

## 3. Error Wrapping Pattern

Always wrap errors with context at each layer boundary:

```go
// internal/adb/properties.go
func FetchDeviceInfo(serial string) (domain.DeviceInfo, error) {
    out, err := shell(serial, "getprop ro.product.model")
    if err != nil {
        return domain.DeviceInfo{}, fmt.Errorf("getprop ro.product.model: %w", err)
    }
    // ...
}

// internal/agent/agent.go
info, err := a.adbProps.FetchDeviceInfo(a.serial)
if err != nil {
    return fmt.Errorf("enrich device %s: %w", a.serial, err)
}

// internal/supervisor/supervisor.go
if err := agent.Run(ctx); err != nil {
    slog.Error("agent exited with error", "serial", serial, "error", err)
    // errors.Is(err, domain.ErrDeviceNotFound) for specific handling
}
```

The full error chain for a getprop failure would be:
```
agent exited with error: enrich device ABC123: getprop ro.product.model: exit status 1
```
