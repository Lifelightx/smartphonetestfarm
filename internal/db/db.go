package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"

	"protean-provider/internal/domain"
)

type DB struct {
	db *sql.DB
}

func (d *DB) RawDB() *sql.DB {
	return d.db
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
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS platform TEXT NOT NULL DEFAULT 'android';`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS os_version TEXT NOT NULL DEFAULT '';`,
		`UPDATE devices SET os_version = android WHERE os_version = '';`,
		`UPDATE devices SET platform = 'ios' WHERE manufacturer = 'Apple';`,
		`CREATE TABLE IF NOT EXISTS automation_scripts (
			id UUID PRIMARY KEY,
			name TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS automation_reports (
			id UUID PRIMARY KEY,
			script_id UUID REFERENCES automation_scripts(id) ON DELETE CASCADE,
			serial TEXT NOT NULL REFERENCES devices(serial) ON DELETE CASCADE,
			success BOOLEAN NOT NULL,
			start_time TIMESTAMP NOT NULL,
			end_time TIMESTAMP NOT NULL,
			results JSON NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			password_hash TEXT,
			role TEXT NOT NULL DEFAULT 'user',
			auth_provider TEXT NOT NULL DEFAULT 'local',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS groups (
			id UUID PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			description TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS user_groups (
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			PRIMARY KEY (user_id, group_id)
		);`,
		`CREATE TABLE IF NOT EXISTS device_groups (
			serial TEXT NOT NULL REFERENCES devices(serial) ON DELETE CASCADE,
			group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			PRIMARY KEY (serial, group_id)
		);`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id UUID PRIMARY KEY,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			token_hash TEXT UNIQUE NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			expires_at TIMESTAMP
		);`,
		`ALTER TABLE automation_scripts ADD COLUMN IF NOT EXISTS group_id UUID REFERENCES groups(id) ON DELETE SET NULL;`,
		`ALTER TABLE automation_scripts ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES users(id) ON DELETE SET NULL;`,
		`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE SET NULL;`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP;`,
		`INSERT INTO groups (id, name, description, created_at) VALUES ('00000000-0000-0000-0000-000000000001', 'Public', 'Default public group', NOW()) ON CONFLICT (name) DO NOTHING;`,
		`INSERT INTO device_groups (serial, group_id) SELECT serial, (SELECT id FROM groups WHERE name = 'Public') FROM devices ON CONFLICT DO NOTHING;`,
		`INSERT INTO user_groups (user_id, group_id) SELECT u.id, g.id FROM users u CROSS JOIN groups g WHERE u.role = 'admin' ON CONFLICT DO NOTHING;`,
	}

	for _, q := range queries {
		if _, err := d.db.Exec(q); err != nil {
			return err
		}
	}
	slog.Info("coordinator db: migrations applied successfully")

	// Seed default admin user if none exists
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return fmt.Errorf("check users count: %w", err)
	}

	if count == 0 {
		slog.Info("No users found in database, seeding default admin user")
		password := "Welcome@2026"
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("failed to hash default password: %w", err)
		}

		userID := "00000000-0000-0000-0000-000000000000"
		_, err = d.db.Exec(`
			INSERT INTO users (id, email, password_hash, role, auth_provider, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW(), NOW())`,
			userID, "admin@domain.com", string(hash), "admin", "local")
		if err != nil {
			return fmt.Errorf("failed to seed default admin: %w", err)
		}

		// Create a default group "Public" and add the user to it
		groupID := "11111111-1111-1111-1111-111111111111"
		_, _ = d.db.Exec(`
			INSERT INTO groups (id, name, description, created_at)
			VALUES ($1, $2, $3, NOW()) ON CONFLICT (name) DO NOTHING`,
			groupID, "Public", "Default Public Group")

		var actualGroupID string
		err = d.db.QueryRow("SELECT id FROM groups WHERE name = 'Public'").Scan(&actualGroupID)
		if err == nil {
			_, _ = d.db.Exec(`
				INSERT INTO user_groups (user_id, group_id)
				VALUES ($1, $2) ON CONFLICT DO NOTHING`,
				userID, actualGroupID)
		}
	}

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
	platform := "android"
	if manufacturer == "Apple" || sdk == 0 {
		platform = "ios"
	}
	osVersion := android

	query := `
		INSERT INTO devices (
			serial, provider_ip, model, manufacturer, android, sdk, abi, ram_mb, storage_mb,
			display_width, display_height, display_dpi, battery, wifi_ssid, ip, status, connected_at, updated_at,
			platform, os_version
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, 'idle', $16, NOW(), $17, $18)
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
			updated_at = NOW(),
			platform = EXCLUDED.platform,
			os_version = EXCLUDED.os_version;`
	_, err := d.db.Exec(query, serial, providerIP, model, manufacturer, android, sdk, abi, ram, storage, width, height, dpi, battery, wifi, ip, connectedAt, platform, osVersion)
	if err != nil {
		return err
	}

	// Make sure the "Public" group exists
	var publicGroupID string
	err = d.db.QueryRow("SELECT id FROM groups WHERE name = 'Public'").Scan(&publicGroupID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			publicGroupID = uuid.New().String()
			_, err = d.db.Exec("INSERT INTO groups (id, name, description, created_at) VALUES ($1, 'Public', 'Default public group', NOW()) ON CONFLICT (name) DO NOTHING", publicGroupID)
			if err != nil {
				_ = d.db.QueryRow("SELECT id FROM groups WHERE name = 'Public'").Scan(&publicGroupID)
			}
		} else {
			return err
		}
	}

	// Allocate the device to the Public group
	_, _ = d.db.Exec("INSERT INTO device_groups (serial, group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", serial, publicGroupID)
	return nil
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

	_, err = tx.Exec(`
		UPDATE devices
		SET status = CASE WHEN status = 'offline' THEN 'offline' ELSE 'idle' END,
		    stream_port = 0,
		    updated_at = NOW()
		WHERE serial = $1
	`, serial)
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

// SaveScript saves or updates an automation script in the database.
func (d *DB) SaveScript(id, name, content string) error {
	query := `
		INSERT INTO automation_scripts (id, name, content, created_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			content = EXCLUDED.content;`
	_, err := d.db.Exec(query, id, name, content)
	return err
}

// GetScript retrieves an automation script by ID.
func (d *DB) GetScript(id string) (name, content string, err error) {
	query := `SELECT name, content FROM automation_scripts WHERE id = $1`
	err = d.db.QueryRow(query, id).Scan(&name, &content)
	return
}

// DeleteScript deletes an automation script by ID.
func (d *DB) DeleteScript(id string) error {
	query := `DELETE FROM automation_scripts WHERE id = $1`
	_, err := d.db.Exec(query, id)
	return err
}

// SaveReport stores a new execution report in the database.
func (d *DB) SaveReport(id, scriptID, serial string, success bool, startTime, endTime time.Time, resultsJSON string) error {
	query := `
		INSERT INTO automation_reports (id, script_id, serial, success, start_time, end_time, results)
		VALUES ($1, $2, $3, $4, $5, $6, CAST($7 AS JSON))`
	_, err := d.db.Exec(query, id, scriptID, serial, success, startTime, endTime, resultsJSON)
	return err
}

// GetReport retrieves an execution report by ID.
func (d *DB) GetReport(id string) (scriptID, serial string, success bool, startTime, endTime time.Time, resultsJSON string, err error) {
	query := `SELECT script_id, serial, success, start_time, end_time, results FROM automation_reports WHERE id = $1`
	err = d.db.QueryRow(query, id).Scan(&scriptID, &serial, &success, &startTime, &endTime, &resultsJSON)
	return
}

type ScriptDB struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

func (d *DB) ListScripts() ([]ScriptDB, error) {
	rows, err := d.db.Query(`SELECT id, name, content, created_at FROM automation_scripts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ScriptDB
	for rows.Next() {
		var s ScriptDB
		if err := rows.Scan(&s.ID, &s.Name, &s.Content, &s.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	if list == nil {
		list = []ScriptDB{}
	}
	return list, nil
}

type ReportDB struct {
	ID        string    `json:"id"`
	ScriptID  string    `json:"script_id"`
	Serial    string    `json:"serial"`
	Success   bool      `json:"success"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Results   string    `json:"results,omitempty"`
}

func (d *DB) ListReports() ([]ReportDB, error) {
	rows, err := d.db.Query(`SELECT id, script_id, serial, success, start_time, end_time, results FROM automation_reports ORDER BY start_time DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ReportDB
	for rows.Next() {
		var r ReportDB
		if err := rows.Scan(&r.ID, &r.ScriptID, &r.Serial, &r.Success, &r.StartTime, &r.EndTime, &r.Results); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	if list == nil {
		list = []ReportDB{}
	}
	return list, nil
}

// User methods

func (d *DB) CreateUser(u *domain.User) error {
	query := `
		INSERT INTO users (id, email, password_hash, role, auth_provider, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (email) DO UPDATE SET
			password_hash = COALESCE(EXCLUDED.password_hash, users.password_hash),
			role = EXCLUDED.role,
			auth_provider = EXCLUDED.auth_provider,
			updated_at = NOW();`
	if _, err := d.db.Exec(query, u.ID, u.Email, u.PasswordHash, string(u.Role), u.AuthProvider, u.CreatedAt, u.UpdatedAt); err != nil {
		return err
	}

	// If the user is an admin, automatically add them to all existing groups
	if u.Role == domain.RoleAdmin {
		_, _ = d.db.Exec(`
			INSERT INTO user_groups (user_id, group_id)
			SELECT $1, id FROM groups
			ON CONFLICT DO NOTHING`, u.ID)
	}

	return nil
}

func (d *DB) GetUserByEmail(email string) (*domain.User, error) {
	var u domain.User
	query := `SELECT id, email, password_hash, role, auth_provider, created_at, updated_at FROM users WHERE email = $1`
	err := d.db.QueryRow(query, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.AuthProvider, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (d *DB) GetUserByID(id string) (*domain.User, error) {
	var u domain.User
	query := `SELECT id, email, password_hash, role, auth_provider, created_at, updated_at FROM users WHERE id = $1`
	err := d.db.QueryRow(query, id).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.AuthProvider, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (d *DB) UpdateUserRole(id string, role domain.UserRole) error {
	query := `UPDATE users SET role = $1, updated_at = NOW() WHERE id = $2`
	if _, err := d.db.Exec(query, string(role), id); err != nil {
		return err
	}

	if role == domain.RoleAdmin {
		_, _ = d.db.Exec(`
			INSERT INTO user_groups (user_id, group_id)
			SELECT $1, id FROM groups
			ON CONFLICT DO NOTHING`, id)
	}
	return nil
}

func (d *DB) DeleteUser(id string) error {
	query := `DELETE FROM users WHERE id = $1`
	_, err := d.db.Exec(query, id)
	return err
}

func (d *DB) ListUsers() ([]domain.User, error) {
	query := `SELECT id, email, role, auth_provider, created_at, updated_at FROM users ORDER BY email ASC`
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.AuthProvider, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	if list == nil {
		list = []domain.User{}
	}
	return list, nil
}

// Group methods

func (d *DB) CreateGroup(g *domain.Group) error {
	query := `INSERT INTO groups (id, name, description, created_at, expires_at) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (name) DO NOTHING`
	if _, err := d.db.Exec(query, g.ID, g.Name, g.Description, g.CreatedAt, g.ExpiresAt); err != nil {
		return err
	}

	// Automatically add all admin users to the new group
	_, _ = d.db.Exec(`
		INSERT INTO user_groups (user_id, group_id)
		SELECT id, $1 FROM users WHERE role = 'admin'
		ON CONFLICT DO NOTHING`, g.ID)

	return nil
}

func (d *DB) GetGroup(id string) (*domain.Group, error) {
	var g domain.Group
	query := `SELECT id, name, description, created_at, expires_at FROM groups WHERE id = $1`
	err := d.db.QueryRow(query, id).Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (d *DB) ListGroups() ([]domain.Group, error) {
	rows, err := d.db.Query(`SELECT id, name, description, created_at, expires_at FROM groups ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.Group
	for rows.Next() {
		var g domain.Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.ExpiresAt); err != nil {
			return nil, err
		}
		list = append(list, g)
	}
	if list == nil {
		list = []domain.Group{}
	}
	return list, nil
}

func (d *DB) DeleteGroup(id string) error {
	query := `DELETE FROM groups WHERE id = $1`
	_, err := d.db.Exec(query, id)
	return err
}

func (d *DB) AddUserToGroup(userID, groupID string) error {
	var groupName string
	err := d.db.QueryRow("SELECT name FROM groups WHERE id = $1", groupID).Scan(&groupName)
	if err != nil {
		return err
	}

	query := `INSERT INTO user_groups (user_id, group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	if _, err := d.db.Exec(query, userID, groupID); err != nil {
		return err
	}

	if groupName != "Public" {
		var publicGroupID string
		err := d.db.QueryRow("SELECT id FROM groups WHERE name = 'Public'").Scan(&publicGroupID)
		if err == nil && publicGroupID != "" {
			_, _ = d.db.Exec("DELETE FROM user_groups WHERE user_id = $1 AND group_id = $2", userID, publicGroupID)
		}
	}
	return nil
}

func (d *DB) RemoveUserFromGroup(userID, groupID string) error {
	query := `DELETE FROM user_groups WHERE user_id = $1 AND group_id = $2`
	_, err := d.db.Exec(query, userID, groupID)
	return err
}

func (d *DB) AddDeviceToGroup(serial, groupID string) error {
	query := `INSERT INTO device_groups (serial, group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := d.db.Exec(query, serial, groupID)
	return err
}

func (d *DB) RemoveDeviceFromGroup(serial, groupID string) error {
	query := `DELETE FROM device_groups WHERE serial = $1 AND group_id = $2`
	_, err := d.db.Exec(query, serial, groupID)
	return err
}

func (d *DB) GetUserGroups(userID string) ([]domain.Group, error) {
	query := `
		SELECT g.id, g.name, g.description, g.created_at, g.expires_at
		FROM groups g
		INNER JOIN user_groups ug ON g.id = ug.group_id
		WHERE ug.user_id = $1
		ORDER BY g.name ASC`
	rows, err := d.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.Group
	for rows.Next() {
		var g domain.Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.ExpiresAt); err != nil {
			return nil, err
		}
		list = append(list, g)
	}
	if list == nil {
		list = []domain.Group{}
	}
	return list, nil
}

func (d *DB) GetDeviceGroups(serial string) ([]domain.Group, error) {
	query := `
		SELECT g.id, g.name, g.description, g.created_at, g.expires_at
		FROM groups g
		INNER JOIN device_groups dg ON g.id = dg.group_id
		WHERE dg.serial = $1
		ORDER BY g.name ASC`
	rows, err := d.db.Query(query, serial)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.Group
	for rows.Next() {
		var g domain.Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.ExpiresAt); err != nil {
			return nil, err
		}
		list = append(list, g)
	}
	if list == nil {
		list = []domain.Group{}
	}
	return list, nil
}

// API Key methods

func (d *DB) CreateApiKey(k *domain.ApiKey) error {
	query := `INSERT INTO api_keys (id, user_id, name, token_hash, created_at, expires_at) VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := d.db.Exec(query, k.ID, k.UserID, k.Name, k.TokenHash, k.CreatedAt, k.ExpiresAt)
	return err
}

func (d *DB) GetApiKeyByHash(hash string) (*domain.ApiKey, error) {
	var k domain.ApiKey
	query := `SELECT id, user_id, name, token_hash, created_at, expires_at FROM api_keys WHERE token_hash = $1`
	err := d.db.QueryRow(query, hash).Scan(&k.ID, &k.UserID, &k.Name, &k.TokenHash, &k.CreatedAt, &k.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (d *DB) DeleteApiKey(id string) error {
	query := `DELETE FROM api_keys WHERE id = $1`
	_, err := d.db.Exec(query, id)
	return err
}

func (d *DB) ListApiKeys(userID string) ([]domain.ApiKey, error) {
	query := `SELECT id, user_id, name, token_hash, created_at, expires_at FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`
	rows, err := d.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.ApiKey
	for rows.Next() {
		var k domain.ApiKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.TokenHash, &k.CreatedAt, &k.ExpiresAt); err != nil {
			return nil, err
		}
		list = append(list, k)
	}
	if list == nil {
		list = []domain.ApiKey{}
	}
	return list, nil
}

func (d *DB) GetGroupUsers(groupID string) ([]domain.User, error) {
	query := `
		SELECT u.id, u.email, u.role, u.auth_provider, u.created_at, u.updated_at
		FROM users u
		INNER JOIN user_groups ug ON u.id = ug.user_id
		WHERE ug.group_id = $1
		ORDER BY u.email ASC`
	rows, err := d.db.Query(query, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.AuthProvider, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	if list == nil {
		list = []domain.User{}
	}
	return list, nil
}

func (d *DB) GetGroupDevices(groupID string) ([]string, error) {
	query := `
		SELECT serial
		FROM device_groups
		WHERE group_id = $1
		ORDER BY serial ASC`
	rows, err := d.db.Query(query, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []string
	for rows.Next() {
		var serial string
		if err := rows.Scan(&serial); err != nil {
			return nil, err
		}
		list = append(list, serial)
	}
	if list == nil {
		list = []string{}
	}
	return list, nil
}

