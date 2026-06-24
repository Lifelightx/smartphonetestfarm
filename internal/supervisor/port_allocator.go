package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"protean-provider/internal/db"
)

// PortAllocator manages a pool of TCP ports within a configured range.
// It pre-loads existing allocations from SQLite on startup so it survives
// provider restarts without ever double-allocating a port.
type PortAllocator struct {
	mu       sync.Mutex
	minPort  int
	maxPort  int
	used     map[int]string // port → serial
	bySerial map[string]int // serial → port
	store    *db.DB
}

// NewPortAllocator creates a PortAllocator and restores any existing
// allocations from the database.
func NewPortAllocator(ctx context.Context, store *db.DB, minPort, maxPort int) (*PortAllocator, error) {
	if minPort >= maxPort {
		return nil, fmt.Errorf("port allocator: minPort (%d) must be < maxPort (%d)", minPort, maxPort)
	}

	pa := &PortAllocator{
		minPort:  minPort,
		maxPort:  maxPort,
		used:     make(map[int]string),
		bySerial: make(map[string]int),
		store:    store,
	}

	// Restore existing allocations from the DB.
	existing, err := store.GetAllocatedPorts(ctx)
	if err != nil {
		return nil, fmt.Errorf("port allocator: load existing: %w", err)
	}
	for _, a := range existing {
		if a.Port >= minPort && a.Port <= maxPort {
			pa.used[a.Port] = a.Serial
			pa.bySerial[a.Serial] = a.Port
			slog.Debug("port allocator: restored", "serial", a.Serial, "port", a.Port)
		}
	}

	slog.Info("port allocator: ready",
		"range", fmt.Sprintf("%d-%d", minPort, maxPort),
		"used", len(pa.used),
		"free", maxPort-minPort+1-len(pa.used),
	)
	return pa, nil
}

// Allocate reserves the next free port for the given serial and persists it.
// Returns the allocated port or an error if the pool is exhausted.
func (pa *PortAllocator) Allocate(ctx context.Context, serial string) (int, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	// Already allocated for this serial — return existing.
	if port, ok := pa.bySerial[serial]; ok {
		return port, nil
	}

	// Find first free port in range.
	for port := pa.minPort; port <= pa.maxPort; port++ {
		if _, taken := pa.used[port]; !taken {
			pa.used[port] = serial
			pa.bySerial[serial] = port

			if err := pa.store.AllocatePort(ctx, serial, port); err != nil {
				// Roll back in-memory allocation on DB failure.
				delete(pa.used, port)
				delete(pa.bySerial, serial)
				return 0, fmt.Errorf("port allocator: persist %s=%d: %w", serial, port, err)
			}

			slog.Debug("port allocator: allocated", "serial", serial, "port", port)
			return port, nil
		}
	}

	return 0, fmt.Errorf("port allocator: pool exhausted (range %d-%d, used %d)",
		pa.minPort, pa.maxPort, len(pa.used))
}

// Free releases the port for the given serial and removes it from the DB.
func (pa *PortAllocator) Free(ctx context.Context, serial string) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	port, ok := pa.bySerial[serial]
	if !ok {
		return // nothing to do
	}

	delete(pa.used, port)
	delete(pa.bySerial, serial)

	if err := pa.store.FreePort(ctx, serial); err != nil {
		slog.Warn("port allocator: failed to remove port from DB",
			"serial", serial, "port", port, "err", err)
	}
	slog.Debug("port allocator: freed", "serial", serial, "port", port)
}

// PortFor returns the currently allocated port for a serial, or 0 if none.
func (pa *PortAllocator) PortFor(serial string) int {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	return pa.bySerial[serial]
}
