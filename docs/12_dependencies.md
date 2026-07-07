# 12 — Dependencies

All Go packages used in the `protean-provider` and `protean-coordinator` repository, with their purpose and justification.

---

## 1. Runtime Dependencies

| Package | Version | Purpose | Why this one |
|---------|---------|---------|--------------|
| `github.com/lib/pq` | `v1.12.3` | PostgreSQL database driver | Standard, pure Go PostgreSQL driver. Used for coordinator users, groups, and automation reporting. |
| `github.com/golang-jwt/jwt/v5` | `v5.3.1` | JSON Web Token (JWT) library | Standard Go library for generating and validating claims and cryptographic signatures. |
| `golang.org/x/crypto` | `v0.48.0` | Cryptographic algorithms (bcrypt) | Used for hashing user passwords securely. |
| `github.com/gorilla/websocket` | `v1.5.3` | WebSocket communication | Used by the coordinator server to broadcast device states and relay interactive commands/streams. |
| `github.com/Eyevinn/mp4ff` | `v0.52.0` | MP4/H.264 parser & multiplexer | Used to parse, modify, and stream raw H.264 video streams from iOS WebDriverAgent connections. |
| `github.com/codeskyblue/go-adbkit` | `v0.3.0` | ADB protocol library | Pure Go implementation of the ADB host client protocol to list and manage Android devices. |
| `google.golang.org/grpc` | `v1.81.1` | gRPC framework | Remote procedure call transport linking provider nodes to the central coordinator. |
| `google.golang.org/protobuf` | `v1.36.11` | Protobuf serialization | Binary protocol compiler runtime for gRPC messages. |
| `github.com/spf13/viper` | `v1.18.2` | Configuration loader | Handles reading, parsing, and env-var overriding of YAML configs. |
| `github.com/go-playground/validator/v10` | `v10.22.0` | Struct field validation | Used for validator tag assertions on configuration load. |
| `github.com/google/uuid` | `v1.6.0` | UUID generation | Generates unique IDs for sessions, scripts, reports, and providers. |

---

## 2. Development and Test Tools

| Tool / Package | Install Method | Purpose |
|----------------|----------------|---------|
| `buf` | `go install github.com/bufbuild/buf/cmd/buf@latest` | Fast, robust Protobuf/gRPC generation tool. |
| `golangci-lint` | `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest` | Aggregate linter utility. |
| `scrcpy-server` | `make fetch-deps` | Official Genymobile scrcpy jar (v4.0) embedded into the provider. |
| `go-ios` | `go install github.com/danielpaulus/go-ios@latest` | Command line wrapper for iOS connection and tunnel assembly. |

---

## 3. Dependency Update Policy

- **Version Lock**: Pin exact versions in `go.mod` (avoiding tags like `latest` or ranges) to ensure build reproducibility.
- **Coordination**: Coordinate gRPC and protobuf libraries to remain fully compatible.
- **Security Scans**: Periodically audit dependencies via `go list -m -u all` and address CVE vulnerability alerts.

