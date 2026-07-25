package db

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
	"github.com/nekonest/server/internal/protocol"
)

// DB wraps the SQLite database connection.
type DB struct {
	conn *sql.DB
}

// New creates and initializes a new database.
func New(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, err
	}
	return db, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) migrate() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS devices (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			os TEXT NOT NULL DEFAULT 'windows',
			token_hash TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			last_seen INTEGER NOT NULL,
			active_agents INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS pair_codes (
			code TEXT PRIMARY KEY,
			device_id TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			used INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (device_id) REFERENCES devices(id)
		);

		CREATE TABLE IF NOT EXISTS user_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token_hash TEXT NOT NULL,
			device_id TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			FOREIGN KEY (device_id) REFERENCES devices(id)
		);

		-- P2-A: Session message history
		CREATE TABLE IF NOT EXISTS session_messages (
			id TEXT NOT NULL,
			device_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'assistant',
			content TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL DEFAULT 'text',
			timestamp INTEGER NOT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}',
			PRIMARY KEY (id, device_id, session_id)
		);
		CREATE INDEX IF NOT EXISTS idx_messages_device_session
			ON session_messages(device_id, session_id, timestamp);

		-- P2-C: Push notification subscriptions
		CREATE TABLE IF NOT EXISTS push_subscriptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL,
			endpoint TEXT NOT NULL UNIQUE,
			p256dh TEXT NOT NULL DEFAULT '',
			auth TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_push_device ON push_subscriptions(device_id);
	`)
	return err
}

// RegisterDevice registers a new device and returns its token.
func (db *DB) RegisterDevice(id, name string) (string, error) {
	token := generateToken()
	tokenHash := hashToken(token)
	now := time.Now().Unix()

	_, err := db.conn.Exec(
		`INSERT INTO devices (id, name, os, token_hash, created_at, last_seen) VALUES (?, ?, 'windows', ?, ?, ?)`,
		id, name, tokenHash, now, now,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// GetDevice retrieves a device by ID.
func (db *DB) GetDevice(id string) (*protocol.Device, error) {
	row := db.conn.QueryRow(`SELECT id, name, os, last_seen FROM devices WHERE id = ?`, id)
	var d protocol.Device
	if err := row.Scan(&d.ID, &d.Name, &d.OS, &d.LastSeen); err != nil {
		return nil, err
	}
	d.Status = "offline" // default, updated by connection manager
	return &d, nil
}

// UpdateDeviceLastSeen updates the last seen timestamp.
func (db *DB) UpdateDeviceLastSeen(id string) error {
	_, err := db.conn.Exec(`UPDATE devices SET last_seen = ? WHERE id = ?`, time.Now().Unix(), id)
	return err
}

// ListDevices returns all registered devices.
func (db *DB) ListDevices() ([]*protocol.Device, error) {
	rows, err := db.conn.Query(`SELECT id, name, os, last_seen, active_agents FROM devices ORDER BY last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*protocol.Device
	for rows.Next() {
		d := &protocol.Device{}
		if err := rows.Scan(&d.ID, &d.Name, &d.OS, &d.LastSeen, &d.ActiveAgents); err != nil {
			return nil, err
		}
		d.Status = "offline"
		devices = append(devices, d)
	}
	return devices, nil
}

// ValidateDeviceToken checks if a device token is valid.
func (db *DB) ValidateDeviceToken(deviceID, token string) bool {
	tokenHash := hashToken(token)
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM devices WHERE id = ? AND token_hash = ?`, deviceID, tokenHash).Scan(&count)
	return err == nil && count > 0
}

// CreatePairCode generates a temporary pairing code.
func (db *DB) CreatePairCode(code, deviceID string, expiresAt time.Time) error {
	_, err := db.conn.Exec(
		`INSERT INTO pair_codes (code, device_id, expires_at) VALUES (?, ?, ?)`,
		code, deviceID, expiresAt.Unix(),
	)
	return err
}

// ConsumePairCode validates and marks a pair code as used.
func (db *DB) ConsumePairCode(code string) (string, error) {
	var deviceID string
	var expiresAt int64
	var used int
	err := db.conn.QueryRow(
		`SELECT device_id, expires_at, used FROM pair_codes WHERE code = ?`, code,
	).Scan(&deviceID, &expiresAt, &used)
	if err != nil {
		return "", err
	}
	if used != 0 {
		return "", sql.ErrNoRows
	}
	if time.Now().Unix() > expiresAt {
		return "", sql.ErrNoRows
	}

	_, err = db.conn.Exec(`UPDATE pair_codes SET used = 1 WHERE code = ?`, code)
	if err != nil {
		return "", err
	}
	return deviceID, nil
}

// UpdateDeviceSessions updates the active agent count for a device.
func (db *DB) UpdateDeviceSessions(id string, count int) error {
	_, err := db.conn.Exec(`UPDATE devices SET active_agents = ?, last_seen = ? WHERE id = ?`, count, time.Now().Unix(), id)
	return err
}