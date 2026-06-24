# 10 — Deployment

---

## 1. Dockerfile (Multi-Stage)

```dockerfile
# deploy/Dockerfile

# =============================================================================
# Stage 1: Builder
# =============================================================================
FROM golang:1.21-alpine AS builder

# Install build tools
RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Download dependencies first (layer cache optimization)
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags="-s -w -X main.Version=$(git describe --tags --always)" \
    -o /provider \
    ./cmd/provider

# =============================================================================
# Stage 2: Runtime
# =============================================================================
FROM alpine:3.19

# adb is needed for ADB communication with devices
RUN apk add --no-cache \
    android-tools \
    ca-certificates \
    tzdata

# Non-root user
RUN addgroup -S provider && adduser -S provider -G provider

WORKDIR /app

# Copy binary
COPY --from=builder /provider /app/provider

# Copy default config (can be overridden by volume mount)
COPY config/provider.yaml /etc/stf/provider.yaml

# Switch to non-root
USER provider

# Ports: metrics=9090, grpc-health=9091, device-streams=7400-7700
EXPOSE 9090 9091

ENTRYPOINT ["/app/provider"]
CMD ["--config", "/etc/stf/provider.yaml"]
```

### Build & Push

```bash
# Build
docker build -t protean-provider:latest -f deploy/Dockerfile .

# Tag for registry
docker tag protean-provider:latest registry.example.com/protean/provider:v1.0.0

# Push
docker push registry.example.com/protean/provider:v1.0.0
```

---

## 2. Docker Compose (Local Dev / Integration)

```yaml
# deploy/docker-compose.yml

version: "3.9"

services:
  provider:
    build:
      context: ..
      dockerfile: deploy/Dockerfile
    container_name: protean-provider
    privileged: true          # needed for USB device access
    volumes:
      - /dev/bus/usb:/dev/bus/usb   # USB device passthrough
      - ./certs:/etc/stf/certs:ro   # mTLS certificates
      - ../config/provider.yaml:/etc/stf/provider.yaml:ro
    environment:
      - PROVIDER_COORDINATOR_ADDRESS=coordinator:9000
      - PROVIDER_LOGGING_LEVEL=debug
    ports:
      - "9090:9090"    # metrics
      - "9091:9091"    # gRPC health
    restart: unless-stopped
    depends_on:
      - coordinator
    networks:
      - protean

  # Mock coordinator for local testing
  coordinator:
    image: protean-mock-coordinator:latest
    container_name: mock-coordinator
    ports:
      - "9000:9000"
    networks:
      - protean

networks:
  protean:
    driver: bridge
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

## 6. Makefile

```makefile
# Makefile

BINARY     := bin/provider
CMD        := ./cmd/provider
VERSION    := $(shell git describe --tags --always --dirty)
BUILD_FLAGS := -ldflags="-s -w -X main.Version=$(VERSION)"

.PHONY: all build run test lint proto certs docker-build docker-run clean install-tools

all: build

## Build the provider binary
build:
	@mkdir -p bin
	CGO_ENABLED=0 go build $(BUILD_FLAGS) -o $(BINARY) $(CMD)

## Run the provider locally
run: build
	$(BINARY) --config config/provider.yaml

## Run all tests with race detector
test:
	go test -race ./...

## Run tests with coverage report
cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## Run linter
lint:
	golangci-lint run ./...

## Generate protobuf code
proto:
	cd pkg/protocol && buf generate

## Generate development TLS certificates
certs:
	./scripts/gen-certs.sh

## Build Docker image
docker-build:
	docker build -t protean-provider:$(VERSION) -f deploy/Dockerfile .
	docker tag protean-provider:$(VERSION) protean-provider:latest

## Run with Docker Compose
docker-run:
	docker-compose -f deploy/docker-compose.yml up

## Install dev tools
install-tools:
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

## Clean build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html
```
