# 06 — gRPC & Protobuf Contract

---

## 1. Overview

The provider communicates with the Coordinator using **gRPC over HTTP/2 with mTLS**.
Protobuf is used for message serialization.

| Direction | Who calls whom | Service |
|-----------|---------------|---------|
| Provider → Coordinator | Provider calls Coordinator | `CoordinatorService` |
| Coordinator → Provider | Coordinator calls Provider | `ProviderService` |

---

## 2. Proto Definition (`pkg/protocol/stf/provider.proto`)

```protobuf
syntax = "proto3";
package stf.provider.v1;
option go_package = "protean-provider/pkg/protocol/stf;stfpb";

import "google/protobuf/timestamp.proto";

// =============================================================================
// Provider → Coordinator: CoordinatorService
// Provider calls these RPCs on the Coordinator
// =============================================================================
service CoordinatorService {
  // Called once at startup: register this provider
  rpc RegisterProvider(RegisterProviderRequest) returns (RegisterProviderResponse);

  // Called when a device connects
  rpc RegisterDevice(RegisterDeviceRequest) returns (RegisterDeviceResponse);

  // Called every heartbeat_interval per device
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);

  // Called when a device disconnects or on shutdown
  rpc ReleaseDevice(ReleaseDeviceRequest) returns (ReleaseDeviceResponse);

  // Bidirectional: provider streams events, coordinator streams commands
  rpc DeviceEventStream(stream DeviceEventMessage) returns (stream CoordinatorCommand);
}

// =============================================================================
// Coordinator → Provider: ProviderService
// Coordinator calls these RPCs on the Provider's gRPC server
// =============================================================================
service ProviderService {
  // Health check
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);

  // Get provider info
  rpc GetVersion(VersionRequest) returns (VersionResponse);

  // List all currently known devices
  rpc ListDevices(ListDevicesRequest) returns (ListDevicesResponse);
}

// =============================================================================
// Messages
// =============================================================================

message ProviderProto {
  string id      = 1;
  string name    = 2;
  string host    = 3;
  string ip      = 4;
  string version = 5;
}

message DeviceProto {
  string serial           = 1;
  string provider_ip      = 2;
  string model            = 3;
  string market_name      = 4;
  string manufacturer     = 5;
  string android_version  = 6;
  int32  sdk_version      = 7;
  string cpu_abi          = 8;
  int64  ram_mb           = 9;
  int64  storage_mb       = 10;
  string status           = 11;  // online | offline | unauthorized | busy
  int32  battery_level    = 12;
  bool   is_charging      = 13;
  string wifi_ssid        = 14;
  string ip_address       = 15;
  DisplayProto display    = 16;
  google.protobuf.Timestamp connected_at = 17;
  google.protobuf.Timestamp last_seen    = 18;
}

message DisplayProto {
  int32  width    = 1;
  int32  height   = 2;
  int32  density  = 3;
  int32  fps      = 4;
  int32  rotation = 5;
}

// --- RegisterProvider ---
message RegisterProviderRequest  { ProviderProto provider = 1; }
message RegisterProviderResponse {
  string provider_id = 1;  // coordinator-assigned ID
  bool   accepted    = 2;
  string message     = 3;
}

// --- RegisterDevice ---
message RegisterDeviceRequest  { DeviceProto device = 1; }
message RegisterDeviceResponse {
  bool   accepted = 1;
  string message  = 2;
}

// --- Heartbeat ---
message HeartbeatRequest {
  string serial        = 1;
  string status        = 2;
  int32  battery_level = 3;
  bool   is_charging   = 4;
}
message HeartbeatResponse { bool ok = 1; }

// --- ReleaseDevice ---
message ReleaseDeviceRequest  { string serial = 1; string reason = 2; }
message ReleaseDeviceResponse { bool ok = 1; }

// --- DeviceEventStream ---
message DeviceEventMessage {
  string    serial     = 1;
  string    event_type = 2;  // connected | disconnected | state_changed
  string    state      = 3;
  google.protobuf.Timestamp timestamp = 4;
}

message CoordinatorCommand {
  string command = 1;  // allocate | release | reboot | ping
  string serial  = 2;
  string payload = 3;  // JSON, command-specific
}

// --- ProviderService messages ---
message HealthCheckRequest  {}
message HealthCheckResponse { bool healthy = 1; string message = 2; }

message VersionRequest  {}
message VersionResponse { string version = 1; string build_date = 2; }

message ListDevicesRequest  {}
message ListDevicesResponse { repeated DeviceProto devices = 1; }
```

---

## 3. Code Generation

### `buf.yaml`
```yaml
version: v1
breaking:
  use:
    - FILE
lint:
  use:
    - DEFAULT
```

### `buf.gen.yaml`
```yaml
version: v1
plugins:
  - plugin: go
    out: .
    opt: paths=source_relative
  - plugin: go-grpc
    out: .
    opt: paths=source_relative,require_unimplemented_servers=false
```

### `pkg/protocol/gen.go`
```go
package protocol

//go:generate buf generate
```

### Generate command
```bash
make proto
# or directly:
cd pkg/protocol && buf generate
```

---

## 4. Client Usage Example

```go
// internal/coordinator/client.go

import (
    stfpb "protean-provider/pkg/protocol/stf"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials"
)

func (c *Client) Connect(ctx context.Context) error {
    tlsCreds, err := credentials.NewClientTLSFromFile(c.cfg.TLS.CACert, "")
    // ... or load mTLS cert pair

    conn, err := grpc.DialContext(ctx, c.cfg.Address,
        grpc.WithTransportCredentials(tlsCreds),
        grpc.WithUnaryInterceptor(c.loggingInterceptor),
        grpc.WithBlock(),
    )
    if err != nil {
        return fmt.Errorf("dial coordinator: %w", err)
    }
    c.conn   = conn
    c.client = stfpb.NewCoordinatorServiceClient(conn)
    return nil
}

func (c *Client) RegisterDevice(ctx context.Context, d domain.Device) error {
    ctx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
    defer cancel()

    resp, err := c.client.RegisterDevice(ctx, &stfpb.RegisterDeviceRequest{
        Device: toProto(d),
    })
    if err != nil {
        return fmt.Errorf("RegisterDevice: %w", err)
    }
    if !resp.Accepted {
        return fmt.Errorf("coordinator rejected device: %s", resp.Message)
    }
    return nil
}
```

---

## 5. Error Handling in gRPC Calls

All gRPC calls must:
1. Use `context.WithTimeout` — never call without deadline
2. Check `status.Code(err)` for gRPC-specific errors
3. Distinguish `codes.Unavailable` (retry) from `codes.InvalidArgument` (bug, don't retry)

```go
import "google.golang.org/grpc/codes"
import "google.golang.org/grpc/status"

if st, ok := status.FromError(err); ok {
    switch st.Code() {
    case codes.Unavailable:
        // coordinator down — trigger reconnect
    case codes.InvalidArgument:
        // bug in our request — log and skip, don't retry
    case codes.DeadlineExceeded:
        // timeout — log warning, will retry on next heartbeat
    }
}
```
