-- ─────────────────────────────────────────────────────────────────────────────
-- Migration 001 — Initial schema
-- ─────────────────────────────────────────────────────────────────────────────

PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

-- ── Port Allocations ─────────────────────────────────────────────────────────
-- Tracks which TCP port is bound to which device.
-- Persists across restarts so we never double-allocate a port.
CREATE TABLE IF NOT EXISTS port_allocations (
    serial      TEXT    NOT NULL PRIMARY KEY,
    port        INTEGER NOT NULL UNIQUE,
    allocated_at TEXT   NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);


-- ── Device Events ─────────────────────────────────────────────────────────────
-- Append-only audit log of every meaningful device lifecycle event.
CREATE TABLE IF NOT EXISTS device_events (
    id          INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    serial      TEXT    NOT NULL,
    event_type  TEXT    NOT NULL,  -- connected|disconnected|claimed|released|error
    detail      TEXT    NOT NULL DEFAULT '',
    occurred_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_device_events_serial ON device_events(serial);
CREATE INDEX IF NOT EXISTS idx_device_events_type   ON device_events(event_type);
