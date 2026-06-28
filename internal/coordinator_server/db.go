package coordinator_server

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/lib/pq"
)

type DB struct {
	db *sql.DB
}

func OpenDB(postgresURI string) (*DB, error) {
	db, err := sql.Open("postgres", postgresURI)
	if err != nil {
		return nil, err
	}

	// Verify connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("postgres ping: %w", err)
	}

	d := &DB{db: db}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("postgres migrate: %w", err)
	}

	return d, nil
}

func (d *DB) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS providers (
			ip TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
			min_port INT NOT NULL,
			max_port INT NOT NULL,
			version TEXT NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS devices (
			serial TEXT PRIMARY KEY,
			provider_ip TEXT NOT NULL REFERENCES providers(ip) ON DELETE CASCADE,
			model TEXT NOT NULL,
			manufacturer TEXT NOT NULL,
			android TEXT NOT NULL,
			sdk INT NOT NULL,
			abi TEXT NOT NULL,
			ram_mb BIGINT NOT NULL,
			storage_mb BIGINT NOT NULL,
			display_width INT NOT NULL,
			display_height INT NOT NULL,
			display_dpi INT NOT NULL,
			battery INT NOT NULL,
			wifi_ssid TEXT NOT NULL,
			ip TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'idle',
			stream_port INT NOT NULL DEFAULT 0,
			connected_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id UUID PRIMARY KEY,
			serial TEXT NOT NULL REFERENCES devices(serial) ON DELETE CASCADE,
			claimed_by TEXT NOT NULL,
			claimed_at TIMESTAMP NOT NULL DEFAULT NOW(),
			released_at TIMESTAMP,
			status TEXT NOT NULL DEFAULT 'active'
		);`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS stream_port INT NOT NULL DEFAULT 0;`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS file_system JSON;`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS installed_browsers JSON;`,
	}

	for _, q := range queries {
		if _, err := d.db.Exec(q); err != nil {
			return err
		}
	}
	slog.Info("coordinator db: migrations applied successfully")
	return nil
}

func (d *DB) RegisterProvider(ip, name, host string, minPort, maxPort int, version string) error {
	query := `
		INSERT INTO providers (ip, name, host, min_port, max_port, version, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (ip) DO UPDATE SET
			name = EXCLUDED.name,
			host = EXCLUDED.host,
			min_port = EXCLUDED.min_port,
			max_port = EXCLUDED.max_port,
			version = EXCLUDED.version,
			updated_at = NOW();`
	_, err := d.db.Exec(query, ip, name, host, minPort, maxPort, version)
	return err
}

func (d *DB) RegisterDevice(providerIP, serial, model, manufacturer, android string, sdk int, abi string, ram, storage int64, width, height, dpi, battery int, wifi, ip string, connectedAt time.Time) error {
	query := `
		INSERT INTO devices (
			serial, provider_ip, model, manufacturer, android, sdk, abi, ram_mb, storage_mb,
			display_width, display_height, display_dpi, battery, wifi_ssid, ip, status, connected_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, 'idle', $16, NOW())
		ON CONFLICT (serial) DO UPDATE SET
			provider_ip = EXCLUDED.provider_ip,
			model = EXCLUDED.model,
			manufacturer = EXCLUDED.manufacturer,
			android = EXCLUDED.android,
			sdk = EXCLUDED.sdk,
			abi = EXCLUDED.abi,
			ram_mb = EXCLUDED.ram_mb,
			storage_mb = EXCLUDED.storage_mb,
			display_width = EXCLUDED.display_width,
			display_height = EXCLUDED.display_height,
			display_dpi = EXCLUDED.display_dpi,
			battery = EXCLUDED.battery,
			wifi_ssid = EXCLUDED.wifi_ssid,
			ip = EXCLUDED.ip,
			status = CASE WHEN devices.status = 'offline' THEN 'idle' ELSE devices.status END,
			connected_at = EXCLUDED.connected_at,
			updated_at = NOW();`
	_, err := d.db.Exec(query, serial, providerIP, model, manufacturer, android, sdk, abi, ram, storage, width, height, dpi, battery, wifi, ip, connectedAt)
	return err
}

func (d *DB) ReleaseDevice(serial string) error {
	query := `UPDATE devices SET status = 'offline', updated_at = NOW() WHERE serial = $1`
	_, err := d.db.Exec(query, serial)
	return err
}

func (d *DB) UpdateDeviceHeartbeat(serial string) error {
	query := `UPDATE devices SET status = CASE WHEN status = 'offline' THEN 'idle' ELSE status END, updated_at = NOW() WHERE serial = $1`
	_, err := d.db.Exec(query, serial)
	return err
}

func (d *DB) UpdateDeviceState(serial string, battery int, wifi, fileSystemJSON, installedBrowsersJSON string) error {
	query := `
		UPDATE devices SET
			battery = $2,
			wifi_ssid = $3,
			file_system = CASE WHEN $4 = '' THEN file_system ELSE CAST($4 AS JSON) END,
			installed_browsers = CASE WHEN $5 = '' THEN installed_browsers ELSE CAST($5 AS JSON) END,
			updated_at = NOW()
		WHERE serial = $1`
	_, err := d.db.Exec(query, serial, battery, wifi, fileSystemJSON, installedBrowsersJSON)
	return err
}

func (d *DB) CreateSession(sessionID, serial, claimedBy string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	err = tx.QueryRow("SELECT status FROM devices WHERE serial = $1 FOR UPDATE", serial).Scan(&status)
	if err != nil {
		return fmt.Errorf("device check: %w", err)
	}
	if status == "claimed" {
		return fmt.Errorf("device is already claimed")
	}

	_, err = tx.Exec("UPDATE devices SET status = 'claimed', updated_at = NOW() WHERE serial = $1", serial)
	if err != nil {
		return err
	}

	_, err = tx.Exec("INSERT INTO sessions (id, serial, claimed_by, claimed_at, status) VALUES ($1, $2, $3, NOW(), 'active')", sessionID, serial, claimedBy)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (d *DB) CloseSession(serial string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("UPDATE devices SET status = 'idle', stream_port = 0, updated_at = NOW() WHERE serial = $1", serial)
	if err != nil {
		return err
	}

	_, err = tx.Exec("UPDATE sessions SET status = 'released', released_at = NOW() WHERE serial = $1 AND status = 'active'", serial)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (d *DB) UpdateDeviceStreamPort(serial string, port int) error {
	query := `UPDATE devices SET stream_port = $1, updated_at = NOW() WHERE serial = $2`
	_, err := d.db.Exec(query, port, serial)
	return err
}

func (d *DB) GetDeviceProvider(serial string) (string, string, error) {
	var ip string
	query := `
		SELECT provider_ip
		FROM devices
		WHERE serial = $1`
	err := d.db.QueryRow(query, serial).Scan(&ip)
	if err != nil {
		return "", "", err
	}
	return ip, ip, nil
}
