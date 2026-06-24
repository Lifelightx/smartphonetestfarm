package registry_test

import (
	"errors"
	"sync"
	"testing"

	"protean-provider/internal/domain"
	"protean-provider/internal/registry"
)

func makeDevice(serial string) *domain.Device {
	return &domain.Device{Serial: serial}
}

func TestRegistry_AddAndGet(t *testing.T) {
	r := registry.New()
	d := makeDevice("SERIAL001")

	if err := r.Add(d); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := r.Get("SERIAL001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Serial != "SERIAL001" {
		t.Errorf("got serial %q, want %q", got.Serial, "SERIAL001")
	}
}

func TestRegistry_AddDuplicate(t *testing.T) {
	r := registry.New()
	d := makeDevice("DUP001")

	_ = r.Add(d)
	err := r.Add(d)
	if !errors.Is(err, domain.ErrDeviceAlreadyRegistered) {
		t.Errorf("expected ErrDeviceAlreadyRegistered, got %v", err)
	}
}

func TestRegistry_Remove(t *testing.T) {
	r := registry.New()
	_ = r.Add(makeDevice("REM001"))

	if err := r.Remove("REM001"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	_, err := r.Get("REM001")
	if !errors.Is(err, domain.ErrDeviceNotFound) {
		t.Errorf("expected ErrDeviceNotFound after remove, got %v", err)
	}
}

func TestRegistry_RemoveNotFound(t *testing.T) {
	r := registry.New()
	err := r.Remove("GHOST001")
	if !errors.Is(err, domain.ErrDeviceNotFound) {
		t.Errorf("expected ErrDeviceNotFound, got %v", err)
	}
}

func TestRegistry_Count(t *testing.T) {
	r := registry.New()
	for i := 0; i < 5; i++ {
		_ = r.Add(makeDevice(string(rune('A' + i))))
	}
	if r.Count() != 5 {
		t.Errorf("Count() = %d, want 5", r.Count())
	}
}

func TestRegistry_List(t *testing.T) {
	r := registry.New()
	_ = r.Add(makeDevice("X1"))
	_ = r.Add(makeDevice("X2"))
	_ = r.Add(makeDevice("X3"))

	list := r.List()
	if len(list) != 3 {
		t.Errorf("List() length = %d, want 3", len(list))
	}
}

// TestRegistry_Concurrent exercises the registry under heavy concurrent load.
func TestRegistry_Concurrent(t *testing.T) {
	r := registry.New()
	var wg sync.WaitGroup
	const goroutines = 50

	// Concurrent adds.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			serial := string([]byte{byte('A' + n%26), byte('0' + n/26)})
			_ = r.Add(makeDevice(serial))
		}(i)
	}
	wg.Wait()

	// Concurrent reads.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.List()
			_ = r.Count()
		}()
	}
	wg.Wait()
}
