package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/nekonest/server/internal/protocol"
	_ "modernc.org/sqlite"
)

// DB wraps the SQLite database connection.
type DB struct {
	conn *sql.DB
}

// New creates and initializes a new database.
func New(dbPath string) (*DB, error) {
	// modernc.org/sqlite applies connection-local PRAGMAs through repeated
	// _pragma query parameters. The similarly named _journal_mode and
	// _busy_timeout parameters are not recognized by this driver, which leaves
	// concurrent WebSocket handlers vulnerable to immediate SQLITE_BUSY errors.
	conn, err := sql.Open(
		"sqlite",
		dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
	)
	if err != nil {
		return nil, err
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		_ = conn.Close()
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
			endpoint TEXT NOT NULL,
			p256dh TEXT NOT NULL DEFAULT '',
			auth TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			UNIQUE(endpoint, device_id)
		);
		CREATE INDEX IF NOT EXISTS idx_push_device ON push_subscriptions(device_id);

		-- Durable phone -> daemon command state. A client-generated id is scoped
		-- to one device and is never forwarded twice.
		CREATE TABLE IF NOT EXISTS prompt_commands (
			device_id TEXT NOT NULL,
			client_msg_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			prompt TEXT NOT NULL,
			attachments_json TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'registered'
				CHECK(status IN ('registered', 'pending', 'accepted', 'failed', 'indeterminate')),
			error TEXT NOT NULL DEFAULT '',
			outcome TEXT NOT NULL DEFAULT '',
			retry_allowed INTEGER NOT NULL DEFAULT 0,
			commit_sent INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (device_id, client_msg_id)
		);
		CREATE INDEX IF NOT EXISTS idx_prompt_commands_status
			ON prompt_commands(status, updated_at);
	`)
	if err != nil {
		return err
	}
	if err := db.migratePushSubscriptions(); err != nil {
		return err
	}
	return db.migratePromptCommands()
}

// migratePushSubscriptions upgrades the original UNIQUE(endpoint) schema to a
// per-device mapping. Browsers intentionally reuse one PushSubscription for
// every device selected in the same PWA.
func (db *DB) migratePushSubscriptions() error {
	var schema string
	if err := db.conn.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'push_subscriptions'`,
	).Scan(&schema); err != nil {
		return err
	}
	compact := strings.Join(strings.Fields(strings.ToLower(schema)), "")
	if strings.Contains(compact, "unique(endpoint,device_id)") {
		return nil
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		CREATE TABLE push_subscriptions_v2 (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL,
			endpoint TEXT NOT NULL,
			p256dh TEXT NOT NULL DEFAULT '',
			auth TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			UNIQUE(endpoint, device_id)
		);
		INSERT OR REPLACE INTO push_subscriptions_v2
			(id, device_id, endpoint, p256dh, auth, created_at)
			SELECT id, device_id, endpoint, p256dh, auth, created_at
			FROM push_subscriptions;
		DROP TABLE push_subscriptions;
		ALTER TABLE push_subscriptions_v2 RENAME TO push_subscriptions;
		CREATE INDEX idx_push_device ON push_subscriptions(device_id);
	`); err != nil {
		return fmt.Errorf("migrate push subscriptions: %w", err)
	}
	return tx.Commit()
}

// migratePromptCommands adds the non-retryable indeterminate terminal state
// used when the daemon cannot prove whether an external CLI accepted a prompt.
func (db *DB) migratePromptCommands() error {
	var schema string
	if err := db.conn.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'prompt_commands'`,
	).Scan(&schema); err != nil {
		return err
	}
	compact := strings.Join(strings.Fields(strings.ToLower(schema)), "")
	if strings.Contains(compact, "'registered'") &&
		strings.Contains(compact, "'indeterminate'") &&
		strings.Contains(compact, "retry_allowed") &&
		strings.Contains(compact, "outcome") &&
		strings.Contains(compact, "commit_sent") {
		return nil
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		CREATE TABLE prompt_commands_v2 (
			device_id TEXT NOT NULL,
			client_msg_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			prompt TEXT NOT NULL,
			attachments_json TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'registered'
				CHECK(status IN ('registered', 'pending', 'accepted', 'failed', 'indeterminate')),
			error TEXT NOT NULL DEFAULT '',
			outcome TEXT NOT NULL DEFAULT '',
			retry_allowed INTEGER NOT NULL DEFAULT 0,
			commit_sent INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (device_id, client_msg_id)
		);
		INSERT INTO prompt_commands_v2
			(device_id, client_msg_id, session_id, prompt, attachments_json,
			 status, error, outcome, retry_allowed, commit_sent, created_at, updated_at)
			SELECT device_id, client_msg_id, session_id, prompt, attachments_json,
			       status, error,
			       CASE status
			           WHEN 'accepted' THEN 'accepted'
			           WHEN 'failed' THEN 'failed'
			           ELSE ''
			       END,
			       CASE status WHEN 'failed' THEN 1 ELSE 0 END,
			       0,
			       created_at, updated_at
			FROM prompt_commands;
		DROP TABLE prompt_commands;
		ALTER TABLE prompt_commands_v2 RENAME TO prompt_commands;
		CREATE INDEX idx_prompt_commands_status
			ON prompt_commands(status, updated_at);
	`); err != nil {
		return fmt.Errorf("migrate prompt commands: %w", err)
	}
	return tx.Commit()
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

// DeviceExists reports whether a subscription target is registered.
func (db *DB) DeviceExists(id string) bool {
	if id == "" {
		return false
	}
	var exists int
	err := db.conn.QueryRow(`SELECT 1 FROM devices WHERE id = ? LIMIT 1`, id).Scan(&exists)
	return err == nil && exists == 1
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

// ConsumePairCode validates and marks a pair code as used (atomic single-winner).
func (db *DB) ConsumePairCode(code string) (string, error) {
	now := time.Now().Unix()
	res, err := db.conn.Exec(
		`UPDATE pair_codes SET used = 1 WHERE code = ? AND used = 0 AND expires_at >= ?`,
		code, now,
	)
	if err != nil {
		return "", err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", sql.ErrNoRows
	}
	var deviceID string
	err = db.conn.QueryRow(`SELECT device_id FROM pair_codes WHERE code = ?`, code).Scan(&deviceID)
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
