# 08 — Testing Strategy

---

## 1. Principles

- **Tests live next to the code** they test (`foo_test.go` in the same package)
- **Table-driven tests** for all state machine and config logic
- **`go test -race`** must always pass — concurrent code must be race-free
- **No real ADB or Coordinator** in unit tests — always use mocks or stubs
- **Integration tests** use Docker Compose — real ADB, in-process mock coordinator
- **Coverage target:** ≥ 80% across all `internal/` packages

---

## 2. Test Layer by Package

| Package | Test Type | Approach |
|---------|-----------|----------|
| `internal/domain` | Unit | Pure — no mocks needed, just struct construction |
| `internal/config` | Unit | Load test YAML, test env override, test validation failures |
| `internal/adb` | Unit | Mock ADB server or stub `go-adbkit` responses |
| `internal/agent` | Unit | Mock coordinator + stream; table-driven FSM transition tests |
| `internal/registry` | Unit | Concurrent add/remove/list with `go test -race` |
| `internal/coordinator` | Unit | `bufconn` in-process gRPC server |
| `internal/stream` | Unit | Stub subprocess (`exec.Command` abstraction) |
| `internal/supervisor` | Unit | Mock tracker + mock agent factory |
| `internal/metrics` | Unit | Register metrics, call `httptest` server |
| Integration | Integration | Docker Compose: real ADB daemon + fake coordinator binary |

---

## 3. Domain Tests (Example)

```go
// internal/domain/device_test.go
func TestDeviceStatus(t *testing.T) {
    tests := []struct {
        status   DeviceStatus
        expected string
    }{
        {StatusOnline, "online"},
        {StatusOffline, "offline"},
        {StatusBusy, "busy"},
        {StatusUnauthorized, "unauthorized"},
    }
    for _, tt := range tests {
        t.Run(string(tt.status), func(t *testing.T) {
            assert.Equal(t, tt.expected, string(tt.status))
        })
    }
}
```

---

## 4. Config Tests (Example)

```go
// internal/config/config_test.go
func TestLoad_Valid(t *testing.T) {
    cfg, err := Load("testdata/valid.yaml")
    require.NoError(t, err)
    assert.Equal(t, "lab-provider-01", cfg.Provider.Name)
    assert.Equal(t, "coordinator:9000", cfg.Coordinator.Address)
}

func TestLoad_MissingRequired(t *testing.T) {
    _, err := Load("testdata/missing_name.yaml")
    assert.ErrorContains(t, err, "provider.name")
}

func TestLoad_EnvOverride(t *testing.T) {
    t.Setenv("PROVIDER_COORDINATOR_ADDRESS", "override:9999")
    cfg, err := Load("testdata/valid.yaml")
    require.NoError(t, err)
    assert.Equal(t, "override:9999", cfg.Coordinator.Address)
}
```

---

## 5. FSM Tests (Table-Driven)

```go
// internal/agent/fsm_test.go
func TestFSMTransitions(t *testing.T) {
    tests := []struct {
        name      string
        initial   State
        event     Event
        wantState State
        wantErr   bool
    }{
        {"idle→connecting on start",   StateIdle,       EventStart,      StateConnecting, false},
        {"connecting→online on success", StateConnecting, EventSuccess,   StateOnline,     false},
        {"connecting→offline on fail", StateConnecting, EventFailure,    StateOffline,    false},
        {"online→busy on allocate",    StateOnline,     EventAllocate,   StateBusy,       false},
        {"online→offline on disconnect", StateOnline,   EventDisconnect, StateOffline,    false},
        {"invalid: idle→busy",         StateIdle,       EventAllocate,   StateIdle,       true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            fsm := &FSM{current: tt.initial}
            state, err := fsm.Transition(tt.event)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                require.NoError(t, err)
                assert.Equal(t, tt.wantState, state)
            }
        })
    }
}
```

---

## 6. Coordinator Client Tests (bufconn)

```go
// internal/coordinator/client_test.go
func TestRegisterDevice(t *testing.T) {
    // Start in-process gRPC server
    lis := bufconn.Listen(1024 * 1024)
    srv := grpc.NewServer()
    stfpb.RegisterCoordinatorServiceServer(srv, &fakeCoordinator{})
    go srv.Serve(lis)
    defer srv.Stop()

    // Connect client
    conn, _ := grpc.DialContext(ctx, "bufnet",
        grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
            return lis.Dial()
        }),
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    client := NewClient(cfg, conn)

    // Test
    err := client.RegisterDevice(ctx, domain.Device{Serial: "TEST001"})
    assert.NoError(t, err)
}
```

---

## 7. Registry Concurrency Tests

```go
// internal/registry/registry_test.go
func TestRegistry_Concurrent(t *testing.T) {
    r := New()
    var wg sync.WaitGroup

    // 100 concurrent adds
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            r.Add(&domain.Device{Serial: fmt.Sprintf("DEVICE%03d", i)})
        }(i)
    }
    wg.Wait()

    assert.Equal(t, 100, r.Count())

    // 100 concurrent removes
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            r.Remove(fmt.Sprintf("DEVICE%03d", i))
        }(i)
    }
    wg.Wait()

    assert.Equal(t, 0, r.Count())
}
```

---

## 8. Mock Interfaces

Use `github.com/stretchr/testify/mock` for all domain interfaces.

```go
// internal/testutil/mocks.go

type MockCoordinatorClient struct {
    mock.Mock
}

func (m *MockCoordinatorClient) RegisterDevice(ctx context.Context, d domain.Device) error {
    args := m.Called(ctx, d)
    return args.Error(0)
}

func (m *MockCoordinatorClient) SendHeartbeat(ctx context.Context, serial string, state domain.DeviceState) error {
    args := m.Called(ctx, serial, state)
    return args.Error(0)
}
// ... etc
```

---

## 9. Running Tests

```bash
# All tests with race detector
make test
# or:
go test -race ./...

# With coverage
go test -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# Single package
go test -race -v ./internal/agent/...

# Run specific test
go test -run TestFSMTransitions ./internal/agent/...

# Integration tests only (requires docker-compose up)
go test -tags=integration ./...
```

---

## 10. Integration Test Setup

```yaml
# deploy/docker-compose.test.yml
services:
  adb-server:
    image: sorccu/adb:latest
    privileged: true

  mock-coordinator:
    build:
      context: ./testutil/mock-coordinator
    ports:
      - "9000:9000"

  provider:
    build: .
    depends_on:
      - adb-server
      - mock-coordinator
    environment:
      - PROVIDER_COORDINATOR_ADDRESS=mock-coordinator:9000
      - PROVIDER_ADB_HOST=adb-server
```

```bash
# Run integration tests
docker-compose -f deploy/docker-compose.test.yml up -d
go test -tags=integration -timeout=120s ./...
docker-compose -f deploy/docker-compose.test.yml down
```
