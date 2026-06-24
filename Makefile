BINARY        := bin/provider
COORD_BIN     := bin/coordinator
CMD           := ./cmd/provider
COORD_CMD     := ./cmd/coordinator
CONFIG        := config/provider.yaml
VERSION       := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS       := -ldflags "-X main.Version=$(VERSION) -s -w"

# scrcpy-server: version of the official scrcpy release to embed.
# The file is a ZIP/JAR archive with no extension in the upstream release.
SCRCPY_VERSION  := 4.0
SCRCPY_SERVER   := internal/stream/scrcpy-server.jar
SCRCPY_URL      := https://github.com/Genymobile/scrcpy/releases/download/v$(SCRCPY_VERSION)/scrcpy-server-v$(SCRCPY_VERSION)

.PHONY: all build build-coordinator build-all run run-coordinator test lint clean proto fetch-deps help

all: build-all

## fetch-deps: Download the scrcpy-server binary required for go:embed (idempotent)
fetch-deps:
	@if [ ! -f $(SCRCPY_SERVER) ]; then \
		echo "→ downloading scrcpy-server v$(SCRCPY_VERSION)..."; \
		curl -fsSL $(SCRCPY_URL) -o $(SCRCPY_SERVER); \
		echo "✔  saved $(SCRCPY_SERVER) ($$(du -sh $(SCRCPY_SERVER) | cut -f1))"; \
	else \
		echo "✔  $(SCRCPY_SERVER) already present, skipping download"; \
	fi

## build: Compile the provider binary into ./bin/ (downloads scrcpy-server if missing)
build: fetch-deps
	@mkdir -p bin
	go build -buildvcs=false $(LDFLAGS) -o $(BINARY) $(CMD)
	@cp $(SCRCPY_SERVER) bin/scrcpy-server.jar
	@echo "✔  built $(BINARY) (version=$(VERSION))"

## build-coordinator: Compile the coordinator binary into ./bin/
build-coordinator:
	@mkdir -p bin
	go build -buildvcs=false $(LDFLAGS) -o $(COORD_BIN) $(COORD_CMD)
	@echo "✔  built $(COORD_BIN) (version=$(VERSION))"

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

## lint: Run golangci-lint (install from https://golangci-lint.run)
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

## help: Show this help
help:
	@grep -E '^##' Makefile | sed 's/## /  /'
