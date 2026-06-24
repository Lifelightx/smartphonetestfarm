# 12 — Dependencies

All Go packages used in `protean-provider-go`, with version recommendations and justification.

---

## 1. Runtime Dependencies

| Package | Version | Purpose | Why this one |
|---------|---------|---------|--------------|
| `github.com/codeskyblue/go-adbkit` | `v0.3.0` | ADB communication — connect to ADB daemon, list devices, shell commands | Already in use; pure Go, no CGO |
| `google.golang.org/grpc` | `v1.64.0` | gRPC transport for Coordinator communication | Official Google gRPC for Go |
| `google.golang.org/protobuf` | `v1.34.0` | Protobuf serialization / deserialization | Required by gRPC |
| `google.golang.org/genproto/googleapis/rpc` | `latest` | Well-known gRPC status types | Required by gRPC |
| `github.com/spf13/viper` | `v1.18.0` | YAML config loading + env var override | De-facto standard; supports all config sources |
| `github.com/go-playground/validator/v10` | `v10.22.0` | Struct tag-based config validation | Most complete Go validator |
| `github.com/prometheus/client_golang` | `v1.19.0` | Prometheus metrics | Official Prometheus client |
| `golang.org/x/sync` | `v0.7.0` | `errgroup` for goroutine lifecycle | Standard Go extended library |
| `github.com/google/uuid` | `v1.6.0` | UUID generation for provider ID and correlation IDs | Widely used, zero dependencies |
| `golang.org/x/net` | `v0.26.0` | HTTP/2 support (needed by gRPC) | Required by gRPC |
| `golang.org/x/sys` | `v0.22.0` | OS-level syscalls (signal handling) | Required by dependencies |
| `golang.org/x/text` | `v0.16.0` | Text encoding | Required by dependencies |

---

## 2. Test-Only Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/stretchr/testify` | `v1.9.0` | `assert`, `require`, `mock` — test assertions |
| `github.com/stretchr/objx` | `v0.5.2` | Required by `testify/mock` |
| `google.golang.org/grpc/test/bufconn` | (part of grpc) | In-process gRPC server for coordinator tests |

---

## 3. Development Tools (not in go.mod)

Install with `make install-tools`:

| Tool | Install Command | Purpose |
|------|----------------|---------|
| `buf` | `go install github.com/bufbuild/buf/cmd/buf@latest` | Protobuf code generation (replaces protoc) |
| `golangci-lint` | `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest` | Multi-linter runner |
| `protoc-gen-go` | `go install google.golang.org/protobuf/cmd/protoc-gen-go@latest` | Go protobuf generator (used by buf) |
| `protoc-gen-go-grpc` | `go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest` | gRPC service generator (used by buf) |

---

## 4. go.mod (Target State)

```go
module protean-provider

go 1.21.0

require (
    github.com/codeskyblue/go-adbkit v0.3.0
    github.com/go-playground/validator/v10 v10.22.0
    github.com/google/uuid v1.6.0
    github.com/prometheus/client_golang v1.19.0
    github.com/spf13/viper v1.18.0
    github.com/stretchr/testify v1.9.0
    golang.org/x/sync v0.7.0
    google.golang.org/grpc v1.64.0
    google.golang.org/protobuf v1.34.0
)

require (
    // indirect deps — managed by go mod tidy
    github.com/stretchr/objx v0.5.2 // indirect
    golang.org/x/net v0.26.0 // indirect
    golang.org/x/sys v0.22.0 // indirect
    golang.org/x/text v0.16.0 // indirect
    google.golang.org/genproto/googleapis/rpc v0.0.0-20240610135401-a8a62080eff3 // indirect
)
```

---

## 5. Why NOT These Packages

| Package | Why Avoided |
|---------|-------------|
| `github.com/pebbe/zmq4` | Requires CGO + libzmq C library — breaks pure Go binary |
| `github.com/gin-gonic/gin` | Not needed (provider has no REST API) |
| `github.com/sirupsen/logrus` | Superseded by stdlib `log/slog` in Go 1.21 |
| `go.uber.org/zap` | `log/slog` is sufficient; avoids extra dependency |
| `gorm.io/gorm` | Provider has no database — belongs in coordinator |
| `github.com/gorilla/websocket` | Not needed in provider — belongs in API gateway |
| `github.com/rs/zerolog` | `log/slog` preferred for stdlib alignment |

---

## 6. Dependency Update Policy

- Review dependency updates monthly
- Use `go get -u ./...` to check for updates
- Never update `google.golang.org/grpc` and `google.golang.org/protobuf` independently — they must be compatible versions
- Pin exact versions in `go.mod` — no `latest` or ranges
- Run `go mod tidy` after any update
- Run `go test -race ./...` after any update before committing
