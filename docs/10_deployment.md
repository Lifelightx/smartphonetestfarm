# 10 — Deployment

---

## 1. Dockerfile (Dockerfile.provider)

```dockerfile
# Stage 1: Build the provider binary
FROM golang:1.26-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev curl

WORKDIR /app

# Copy dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Fetch the scrcpy-server dependency
RUN make fetch-deps

# Build the provider
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -ldflags "-s -w" -o bin/provider ./cmd/provider

# Install go-ios tool
RUN GOBIN=/app/bin go install github.com/danielpaulus/go-ios@latest

# Stage 2: Create a minimal runner image
FROM alpine:latest

# Install runtime dependencies (such as adb client)
RUN apk add --no-cache ca-certificates tzdata curl android-tools-adb

WORKDIR /app

# Copy the compiled binaries and config
COPY --from=builder /app/bin/provider /app/provider
COPY --from=builder /app/bin/go-ios /usr/local/bin/ios
COPY --from=builder /app/config/provider.yaml /app/config/provider.yaml

# Expose metrics and gRPC server ports
EXPOSE 9090 9091

# Run the provider binary
ENTRYPOINT ["/app/provider", "--config", "/app/config/provider.yaml", "--log-level", "debug"]
```

### Build the Provider

```bash
docker build -t protean-provider:latest -f Dockerfile.provider .
```

---

## 2. Docker Compose Configurations

The Protean system uses a decoupled multi-compose deployment model to support distributed deployments.

### 2.1 Central Coordinator Stack (`docker-compose.yml`)

Deploys the database (PostgreSQL), the coordinator server, and the static web frontend.

```yaml
version: "3.9"

services:
  # 1. PostgreSQL Database Service
  db:
    image: postgres:16
    container_name: protean-db
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: "123456"
      POSTGRES_DB: protean
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres -d protean"]
      interval: 5s
      timeout: 5s
      retries: 5

  # 2. Go Coordinator Server (REST HTTP + gRPC)
  coordinator:
    build:
      context: .
      dockerfile: Dockerfile.coordinator
    image: go-coordinator:v1
    container_name: protean-coordinator
    environment:
      - COORDINATOR_POSTGRES_URI=postgres://postgres:123456@db:5432/protean?sslmode=disable
      - COORDINATOR_GRPC_PORT=9000
      - COORDINATOR_JWT_SECRET=protean-default-secret-key-change-me-123456
      - BYPASS_AUTH_IN_DEV=false
    ports:
      - "9000:9000" # gRPC
      - "9002:9002" # HTTP API / WebSockets
    extra_hosts:
      - "host.docker.internal:host-gateway"
    depends_on:
      db:
        condition: service_healthy

  # 3. Nginx Static Frontend Service
  frontend:
    build:
      context: .
      dockerfile: Dockerfile.frontend
    container_name: protean-frontend
    image: go-frontend:v1
    ports:
      - "5173:80"
    depends_on:
      - coordinator

volumes:
  postgres_data:
```

### 2.2 Standalone Remote Provider Node (`docker-compose.provider.yml`)

Designed to be run on edge nodes containing physical device racks. Uses host networking for low-latency TCP communication and shares local USB interfaces.

```yaml
version: "3.9"

services:
  provider:
    build:
      context: .
      dockerfile: Dockerfile.provider
    container_name: protean-provider-node
    image: go-provider:v1
    environment:
      - PROVIDER_COORDINATOR_ADDRESS=coordinator.your-domain.com:9000 # Set to the coordinator's endpoint
      - PROVIDER_ADB_HOST=127.0.0.1                  # Connects to the host's local ADB daemon
      - PROVIDER_ADB_PORT=5037
    network_mode: "host"                             # Shares host network for direct port/ADB communication
    volumes:
      - /var/run/usbmuxd:/var/run/usbmuxd            # Share usbmuxd socket for iOS discovery
    restart: on-failure
```

---

## 3. systemd Unit (Bare Metal / VM)

```ini
# deploy/provider.service
# Install to: /etc/systemd/system/protean-provider.service

[Unit]
Description=Protean Provider — Android Device Edge Node
Documentation=https://github.com/your-org/protean-provider-go
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=protean
Group=protean
ExecStart=/usr/local/bin/provider --config /etc/stf/provider.yaml
Restart=on-failure
RestartSec=5s
TimeoutStopSec=30s

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=protean-provider

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/protean-provider

# Environment (or use EnvironmentFile=/etc/stf/provider.env)
Environment=PROVIDER_LOGGING_FORMAT=json

[Install]
WantedBy=multi-user.target
```

### Install & Enable

```bash
# Copy binary
sudo cp bin/provider /usr/local/bin/provider
sudo chmod +x /usr/local/bin/provider

# Copy config
sudo mkdir -p /etc/stf
sudo cp config/provider.yaml /etc/stf/provider.yaml

# Copy certs
sudo cp certs/ /etc/stf/certs/

# Create user
sudo useradd -r -s /sbin/nologin protean

# Install service
sudo cp deploy/provider.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable protean-provider
sudo systemctl start protean-provider

# Check status
sudo systemctl status protean-provider
sudo journalctl -u protean-provider -f
```

---

## 4. GitHub Actions — CI Pipeline

```yaml
# .github/workflows/ci.yml

name: CI

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.21'
          cache: true

      - name: Verify dependencies
        run: go mod verify

      - name: Vet
        run: go vet ./...

      - name: Test (with race detector)
        run: go test -race -coverprofile=coverage.out ./...

      - name: Coverage report
        run: go tool cover -func=coverage.out

      - name: Upload coverage
        uses: codecov/codecov-action@v4
        with:
          files: coverage.out

  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.21'
      - uses: golangci/golangci-lint-action@v4
        with:
          version: latest

  build:
    name: Build
    runs-on: ubuntu-latest
    needs: [test, lint]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.21'
      - name: Build binary
        run: CGO_ENABLED=0 go build -o bin/provider ./cmd/provider
      - name: Build Docker image
        run: docker build -f deploy/Dockerfile -t protean-provider:ci .
```

---

## 5. GitHub Actions — Release Pipeline

```yaml
# .github/workflows/release.yml

name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.21'

      - name: Set version
        run: echo "VERSION=${GITHUB_REF#refs/tags/}" >> $GITHUB_ENV

      - name: Build binary
        run: |
          CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
          go build -ldflags="-s -w -X main.Version=${{ env.VERSION }}" \
          -o bin/provider-linux-amd64 ./cmd/provider

      - name: Login to registry
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build and push Docker image
        uses: docker/build-push-action@v5
        with:
          context: .
          file: deploy/Dockerfile
          push: true
          tags: |
            ghcr.io/${{ github.repository }}:${{ env.VERSION }}
            ghcr.io/${{ github.repository }}:latest

      - name: Create GitHub Release
        uses: softprops/action-gh-release@v1
        with:
          files: bin/provider-linux-amd64
          generate_release_notes: true
```

---

## 6. Makefile Reference

```makefile
# Makefile

BINARY        := bin/provider
COORD_BIN     := bin/coordinator
CMD           := ./cmd/provider
COORD_CMD     := ./cmd/coordinator
CONFIG        := config/provider.yaml
VERSION       := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS       := -ldflags "-X main.Version=$(VERSION) -s -w"

# scrcpy-server download metadata
SCRCPY_VERSION  := 4.0
SCRCPY_SERVER   := internal/stream/scrcpy-server.jar
SCRCPY_URL      := https://github.com/Genymobile/scrcpy/releases/download/v$(SCRCPY_VERSION)/scrcpy-server-v$(SCRCPY_VERSION)

.PHONY: all build build-coordinator build-all run run-coordinator run-frontend test lint clean proto fetch-deps help

all: build-all

## fetch-deps: Download the scrcpy-server binary required for go:embed (idempotent)
fetch-deps:
	@if [ ! -f $(SCRCPY_SERVER) ]; then \
		echo "→ downloading scrcpy-server v$(SCRCPY_VERSION)..."; \
		curl -fsSL $(SCRCPY_URL) -o $(SCRCPY_SERVER); \
		echo "✔  saved $(SCRCPY_SERVER) ($$(du -sh $(SCRCPY_SERVER) | cut -f1))"; \
	fi

## build: Compile the provider binary into ./bin/
build: fetch-deps
	@mkdir -p bin
	go build -buildvcs=false $(LDFLAGS) -o $(BINARY) $(CMD)

## build-coordinator: Compile the coordinator binary into ./bin/
build-coordinator:
	@mkdir -p bin
	go build -buildvcs=false $(LDFLAGS) -o $(COORD_BIN) $(COORD_CMD)

## build-all: Compile both provider and coordinator binaries
build-all: build build-coordinator

## run: Build and run the provider with default config
run: build
	./$(BINARY) --config $(CONFIG) --log-level debug

## run-coordinator: Build and run the coordinator with default settings
run-coordinator: build-coordinator
	./$(COORD_BIN)

## run-frontend: Run the Vite React developer server for the user interface
run-frontend:
	cd frontend && npm run dev

## test: Run all tests with race detector
test:
	go test -race -cover ./...

## test-v: Run all tests with verbose output
test-v:
	go test -race -v -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

## lint: Run golangci-lint
lint:
	golangci-lint run ./...

## tidy: Tidy and verify go.mod / go.sum
tidy:
	go mod tidy
	go mod verify

## clean: Remove build artefacts
clean:
	rm -rf bin/ coverage.out

## proto: Generate Go code from .proto files (requires buf)
proto:
	buf generate
```

