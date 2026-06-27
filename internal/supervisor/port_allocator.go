package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// PortAllocator manages a pool of TCP ports within a configured range.
type PortAllocator struct {
	mu       sync.Mutex
	minPort  int
	maxPort  int
	used     map[int]string // port → serial
	bySerial map[string]int // serial → port
}

// NewPortAllocator creates a PortAllocator.
func NewPortAllocator(ctx context.Context, minPort, maxPort int) (*PortAllocator, error) {
	if minPort >= maxPort {
		return nil, fmt.Errorf("port allocator: minPort (%d) must be < maxPort (%d)", minPort, maxPort)
	}

	pa := &PortAllocator{
		minPort:  minPort,
		maxPort:  maxPort,
		used:     make(map[int]string),
		bySerial: make(map[string]int),
	}

	slog.Info("port allocator: ready",
		"range", fmt.Sprintf("%d-%d", minPort, maxPort),
		"used", len(pa.used),
		"free", maxPort-minPort+1-len(pa.used),
	)
	return pa, nil
}

// Allocate reserves the next free port for the given serial.
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

			slog.Debug("port allocator: allocated", "serial", serial, "port", port)
			return port, nil
		}
	}

	return 0, fmt.Errorf("port allocator: pool exhausted (range %d-%d, used %d)",
		pa.minPort, pa.maxPort, len(pa.used))
}

// Free releases the port for the given serial.
func (pa *PortAllocator) Free(ctx context.Context, serial string) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	port, ok := pa.bySerial[serial]
	if !ok {
		return // nothing to do
	}

	delete(pa.used, port)
	delete(pa.bySerial, serial)

	slog.Debug("port allocator: freed", "serial", serial, "port", port)
}

// PortFor returns the currently allocated port for a serial, or 0 if none.
func (pa *PortAllocator) PortFor(serial string) int {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	return pa.bySerial[serial]
}
