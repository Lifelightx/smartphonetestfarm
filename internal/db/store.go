package db

import (
	"context"
	"fmt"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Port Allocations
// ─────────────────────────────────────────────────────────────────────────────

// AllocatedPort holds a serial → port mapping from the DB.
type AllocatedPort struct {
	Serial      string
	Port        int
	AllocatedAt time.Time
}

// GetAllocatedPorts returns all currently allocated port mappings.
// Used by the port allocator at startup to restore its state.
func (db *DB) GetAllocatedPorts(ctx context.Context) ([]AllocatedPort, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT serial, port, allocated_at FROM port_allocations ORDER BY port`,
	)
	if err != nil {
		return nil, fmt.Errorf("db: GetAllocatedPorts: %w", err)
	}
	defer rows.Close()

	var ports []AllocatedPort
	for rows.Next() {
		var p AllocatedPort
		var ts string
		if err := rows.Scan(&p.Serial, &p.Port, &ts); err != nil {
			return nil, fmt.Errorf("db: GetAllocatedPorts scan: %w", err)
		}
		p.AllocatedAt, _ = time.Parse(time.RFC3339, ts)
		ports = append(ports, p)
	}
	return ports, rows.Err()
}

// AllocatePort persists a serial → port binding.
func (db *DB) AllocatePort(ctx context.Context, serial string, port int) error {
	_, err := db.sql.ExecContext(ctx,
		`INSERT INTO port_allocations (serial, port) VALUES (?, ?)
		 ON CONFLICT(serial) DO UPDATE SET port = excluded.port`,
		serial, port,
	)
	if err != nil {
		return fmt.Errorf("db: AllocatePort %s=%d: %w", serial, port, err)
	}
	return nil
}

// FreePort removes the port binding for a serial.
func (db *DB) FreePort(ctx context.Context, serial string) error {
	_, err := db.sql.ExecContext(ctx,
		`DELETE FROM port_allocations WHERE serial = ?`, serial,
	)
	if err != nil {
		return fmt.Errorf("db: FreePort %s: %w", serial, err)
	}
	return nil
}



// ─────────────────────────────────────────────────────────────────────────────
// Device Events (audit log)
// ─────────────────────────────────────────────────────────────────────────────

// LogEvent appends a device lifecycle event to the audit log.
func (db *DB) LogEvent(ctx context.Context, serial, eventType, detail string) error {
	_, err := db.sql.ExecContext(ctx,
		`INSERT INTO device_events (serial, event_type, detail) VALUES (?, ?, ?)`,
		serial, eventType, detail,
	)
	if err != nil {
		return fmt.Errorf("db: LogEvent %s/%s: %w", serial, eventType, err)
	}
	return nil
}

// GetEvents returns the last n events for a serial, newest first.
func (db *DB) GetEvents(ctx context.Context, serial string, limit int) ([]DeviceEvent, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT id, serial, event_type, detail, occurred_at
		 FROM device_events WHERE serial = ?
		 ORDER BY occurred_at DESC LIMIT ?`,
		serial, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("db: GetEvents %s: %w", serial, err)
	}
	defer rows.Close()

	var events []DeviceEvent
	for rows.Next() {
		var e DeviceEvent
		var ts string
		if err := rows.Scan(&e.ID, &e.Serial, &e.EventType, &e.Detail, &ts); err != nil {
			return nil, fmt.Errorf("db: GetEvents scan: %w", err)
		}
		e.OccurredAt, _ = time.Parse(time.RFC3339, ts)
		events = append(events, e)
	}
	return events, rows.Err()
}

// DeviceEvent is a row from the device_events table.
type DeviceEvent struct {
	ID         int64
	Serial     string
	EventType  string
	Detail     string
	OccurredAt time.Time
}


