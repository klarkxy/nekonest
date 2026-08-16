package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	corestore "github.com/klarkxy/nekonest/relaycore/store"
	"github.com/nekonest/server/internal/protocol"
	_ "modernc.org/sqlite"
)

// DB wraps the SQLite database connection.
type DB struct {
	conn                   *sql.DB
	preexistingApplication bool
}

// New creates and initializes a new database.
func New(dbPath string) (*DB, error) {
	return NewWithTransportMode(dbPath, "")
}

// NewWithTransportMode creates and initializes a database, then establishes
// its one persistent transport mode. requestedMode is only meaningful for the
// first initialization; a later mismatch is rejected rather than silently
// changing how a nest carries application data.
func NewWithTransportMode(dbPath, requestedMode string) (*DB, error) {
	if err := preparePrivateDatabase(dbPath); err != nil {
		return nil, err
	}
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
	hadApplicationTables, err := db.hasApplicationTables()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	db.preexistingApplication = hadApplicationTables
	if _, err := db.bootstrapTransportMode(requestedMode); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := db.migrate(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := tightenPrivateDatabaseArtifacts(dbPath); err != nil {
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
			sealed_envelope_json TEXT NOT NULL DEFAULT '',
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

		-- Schema version tracking (v1+)
		CREATE TABLE IF NOT EXISTS schema_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);

		-- Independent phone identities (v1)
		CREATE TABLE IF NOT EXISTS phone_identities (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			ed25519_public TEXT NOT NULL DEFAULT '',
			x25519_public TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			last_seen INTEGER NOT NULL,
			revoked_at INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_phone_token ON phone_identities(token_hash);

		-- Phone → host device grants (pairing result)
		CREATE TABLE IF NOT EXISTS phone_device_grants (
			phone_id TEXT NOT NULL,
			device_id TEXT NOT NULL,
			paired_at INTEGER NOT NULL,
			revoked_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (phone_id, device_id),
			FOREIGN KEY (phone_id) REFERENCES phone_identities(id),
			FOREIGN KEY (device_id) REFERENCES devices(id)
		);
		CREATE INDEX IF NOT EXISTS idx_grants_device ON phone_device_grants(device_id);

		-- E2E wrapped key packages (ciphertext only on server)
		CREATE TABLE IF NOT EXISTS key_packages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			phone_id TEXT NOT NULL,
			device_id TEXT NOT NULL,
			scope TEXT NOT NULL,
			session_id TEXT NOT NULL DEFAULT '',
			epoch INTEGER NOT NULL,
			wrapped_key TEXT NOT NULL,
			nonce TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			UNIQUE(phone_id, device_id, scope, session_id, epoch)
		);
		CREATE INDEX IF NOT EXISTS idx_key_packages_phone ON key_packages(phone_id, device_id);

		-- Sealed-safe attention routing events.  The server intentionally stores
		-- no prompt, answer, path, approval detail, or event class here.
		CREATE TABLE IF NOT EXISTS attention_events (
			device_id TEXT NOT NULL,
			event_id TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (device_id, event_id)
		);
		CREATE INDEX IF NOT EXISTS idx_attention_events_created_at
			ON attention_events(created_at);
	`)
	if err != nil {
		return err
	}
	if err := db.migratePushSubscriptions(); err != nil {
		return err
	}
	if err := db.migratePromptCommands(); err != nil {
		return err
	}
	if err := db.migratePushPhoneID(); err != nil {
		return err
	}
	if err := db.migrateDeviceIdentityColumns(); err != nil {
		return err
	}
	return db.ensureSchemaVersion()
}

// hasApplicationTables reports whether this database already contained
// NekoNest data before the current migration created its tables. schema_meta is
// deliberately excluded so a brand-new database remains distinguishable.
func (db *DB) hasApplicationTables() (bool, error) {
	const q = `SELECT 1 FROM sqlite_master
		WHERE type = 'table' AND name IN (
			'devices', 'pair_codes', 'user_tokens', 'session_messages',
			'push_subscriptions', 'prompt_commands', 'phone_identities',
			'phone_device_grants', 'key_packages', 'attention_events'
		) LIMIT 1`
	var one int
	err := db.conn.QueryRow(q).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// InitializeTransportMode returns the immutable mode for this nest. Existing
// mode metadata is authoritative. A legacy application database with no mode
// metadata is explicitly classified as open once; a genuinely new nest starts
// sealed unless an explicit first-run mode was supplied.
func (db *DB) InitializeTransportMode(requestedMode string) (protocol.TransportMode, error) {
	return initializeTransportMode(db.conn, db.preexistingApplication, requestedMode)
}

type transportModeStore interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

// bootstrapTransportMode creates and pins the immutable nest mode in one
// transaction before application tables are migrated. If startup is
// interrupted after this point, a new sealed database can never be mistaken
// for a legacy open database merely because some tables already exist.
func (db *DB) bootstrapTransportMode(requestedMode string) (protocol.TransportMode, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS schema_meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		return "", err
	}
	mode, err := initializeTransportMode(tx, db.preexistingApplication, requestedMode)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return mode, nil
}

func initializeTransportMode(store transportModeStore, preexistingApplication bool, requestedMode string) (protocol.TransportMode, error) {
	requestedMode = strings.TrimSpace(requestedMode)
	var requested protocol.TransportMode
	if requestedMode != "" {
		parsed, err := protocol.ParseTransportMode(requestedMode)
		if err != nil {
			return "", fmt.Errorf("invalid requested transport_mode: %w", err)
		}
		requested = parsed
	}

	var stored string
	err := store.QueryRow(`SELECT value FROM schema_meta WHERE key = 'transport_mode'`).Scan(&stored)
	if err == nil {
		mode, parseErr := protocol.ParseTransportMode(stored)
		if parseErr != nil {
			return "", fmt.Errorf("stored transport_mode is invalid: %w", parseErr)
		}
		if requested != "" && requested != mode {
			return "", fmt.Errorf("transport_mode mismatch: persisted %s, requested %s", mode, requested)
		}
		return mode, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	mode := protocol.TransportSealed
	if preexistingApplication {
		mode = protocol.TransportOpen
	}
	if requested != "" {
		if preexistingApplication && requested != protocol.TransportOpen {
			return "", fmt.Errorf("transport_mode mismatch: legacy nest is open; use the offline migration before sealed")
		}
		mode = requested
	}
	if _, err := store.Exec(`INSERT INTO schema_meta (key, value) VALUES ('transport_mode', ?)`, string(mode)); err != nil {
		return "", err
	}
	return mode, nil
}

// TransportMode reads the persistent mode. Callers must treat an error as a
// fail-closed startup condition rather than choosing a fallback relay mode.
func (db *DB) TransportMode() (protocol.TransportMode, error) {
	var raw string
	if err := db.conn.QueryRow(`SELECT value FROM schema_meta WHERE key = 'transport_mode'`).Scan(&raw); err != nil {
		return "", err
	}
	mode, err := protocol.ParseTransportMode(raw)
	if err != nil {
		return "", fmt.Errorf("stored transport_mode is invalid: %w", err)
	}
	return mode, nil
}

const attentionEventTTL = 24 * time.Hour

// AcceptAttentionEvent durably deduplicates an event across server instances.
// Only the routing identifiers and timestamp are persisted. Old event ids are
// removed opportunistically to bound the table.
func (db *DB) AcceptAttentionEvent(deviceID, eventID string, createdAt time.Time) (bool, error) {
	if strings.TrimSpace(deviceID) == "" || strings.TrimSpace(eventID) == "" {
		return false, fmt.Errorf("device_id and event_id required")
	}
	now := createdAt.Unix()
	tx, err := db.conn.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM attention_events WHERE created_at < ?`, now-int64(attentionEventTTL/time.Second)); err != nil {
		return false, err
	}
	result, err := tx.Exec(
		`INSERT INTO attention_events (device_id, event_id, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(device_id, event_id) DO NOTHING`,
		deviceID, eventID, now,
	)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return err == nil && n == 1, err
}

// SchemaVersion is the current server schema generation.
const SchemaVersion = "1"

func (db *DB) ensureSchemaVersion() error {
	var v string
	err := db.conn.QueryRow(`SELECT value FROM schema_meta WHERE key = 'version'`).Scan(&v)
	if err == nil && v != "" {
		return nil
	}
	_, err = db.conn.Exec(
		`INSERT INTO schema_meta (key, value) VALUES ('version', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		SchemaVersion,
	)
	return err
}

// SchemaVersion returns the stored schema version string.
func (db *DB) GetSchemaVersion() string {
	var v string
	if err := db.conn.QueryRow(`SELECT value FROM schema_meta WHERE key = 'version'`).Scan(&v); err != nil {
		return ""
	}
	return v
}

// migratePushPhoneID adds optional phone_id to push_subscriptions for v1 scoping.
func (db *DB) migratePushPhoneID() error {
	var schema string
	if err := db.conn.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'push_subscriptions'`,
	).Scan(&schema); err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(schema), "phone_id") {
		return nil
	}
	_, err := db.conn.Exec(`ALTER TABLE push_subscriptions ADD COLUMN phone_id TEXT NOT NULL DEFAULT ''`)
	if err != nil {
		return fmt.Errorf("migrate push phone_id: %w", err)
	}
	return nil
}

// migrateDeviceIdentityColumns adds E2E public key fields on devices.
func (db *DB) migrateDeviceIdentityColumns() error {
	cols := []struct {
		name string
		ddl  string
	}{
		{"ed25519_public", `ALTER TABLE devices ADD COLUMN ed25519_public TEXT NOT NULL DEFAULT ''`},
		{"x25519_public", `ALTER TABLE devices ADD COLUMN x25519_public TEXT NOT NULL DEFAULT ''`},
		{"identity_fingerprint", `ALTER TABLE devices ADD COLUMN identity_fingerprint TEXT NOT NULL DEFAULT ''`},
	}
	for _, c := range cols {
		var schema string
		if err := db.conn.QueryRow(
			`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'devices'`,
		).Scan(&schema); err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(schema), strings.ToLower(c.name)) {
			continue
		}
		if _, err := db.conn.Exec(c.ddl); err != nil {
			return fmt.Errorf("migrate devices.%s: %w", c.name, err)
		}
	}
	return nil
}

// SetDevicePublicKeys stores daemon E2E public keys (base64url) and fingerprint.
func (db *DB) SetDevicePublicKeys(deviceID, ed25519Pub, x25519Pub, fingerprint string) error {
	res, err := db.conn.Exec(
		`UPDATE devices SET ed25519_public = ?, x25519_public = ?, identity_fingerprint = ? WHERE id = ?`,
		ed25519Pub, x25519Pub, fingerprint, deviceID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("device not found")
	}
	return nil
}

// DevicePublicKeys is the public E2E material for a host daemon.
type DevicePublicKeys = corestore.DevicePublicKeys

// GetDevicePublicKeys returns stored daemon public keys (may be empty).
func (db *DB) GetDevicePublicKeys(deviceID string) (*DevicePublicKeys, error) {
	row := db.conn.QueryRow(
		`SELECT ed25519_public, x25519_public, identity_fingerprint FROM devices WHERE id = ?`,
		deviceID,
	)
	var k DevicePublicKeys
	if err := row.Scan(&k.Ed25519Public, &k.X25519Public, &k.Fingerprint); err != nil {
		return nil, err
	}
	return &k, nil
}

// ClearPlaintextContentForV1 wipes server-held plaintext application content
// after a verified backup. Preserves devices (ids + token hashes) and schema.
// Phones must re-login/re-pair; native agent stores on hosts are untouched.
func (db *DB) ClearPlaintextContentForV1() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM session_messages`,
		`DELETE FROM prompt_commands`,
		`DELETE FROM pair_codes`,
		`DELETE FROM push_subscriptions`,
		`DELETE FROM key_packages`,
		`DELETE FROM phone_device_grants`,
		`DELETE FROM phone_identities`,
		`DELETE FROM user_tokens`,
	} {
		if _, err := tx.Exec(q); err != nil {
			// Table may not exist on very old DBs — ignore.
			if !strings.Contains(err.Error(), "no such table") {
				return fmt.Errorf("%s: %w", q, err)
			}
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_meta (key, value) VALUES ('version', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		SchemaVersion,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_meta (key, value) VALUES ('migrated_v1_at', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		fmt.Sprintf("%d", time.Now().Unix()),
	); err != nil {
		return err
	}
	// This routine is reachable only from the offline migrator after a verified
	// backup and plaintext cleanup. Make the sealed cutover part of the same
	// database transaction; normal startup can never switch an existing nest.
	if _, err := tx.Exec(
		`INSERT INTO schema_meta (key, value) VALUES ('transport_mode', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		string(protocol.TransportSealed),
	); err != nil {
		return err
	}
	return tx.Commit()
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
		strings.Contains(compact, "commit_sent") &&
		strings.Contains(compact, "sealed_envelope_json") {
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
			sealed_envelope_json TEXT NOT NULL DEFAULT '',
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
			(device_id, client_msg_id, session_id, prompt, attachments_json, sealed_envelope_json,
			 status, error, outcome, retry_allowed, commit_sent, created_at, updated_at)
			SELECT device_id, client_msg_id, session_id, prompt, attachments_json, '',
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
// osName should be "windows" or "linux" (v1 formal hosts); empty defaults to windows.
func (db *DB) RegisterDevice(id, name string, osName ...string) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	tokenHash := hashToken(token)
	now := time.Now().Unix()
	osVal := "windows"
	if len(osName) > 0 {
		switch strings.ToLower(strings.TrimSpace(osName[0])) {
		case "linux":
			osVal = "linux"
		case "windows", "":
			osVal = "windows"
		default:
			// Keep unknown values for forward compatibility (e.g. future darwin).
			if s := strings.TrimSpace(osName[0]); s != "" {
				osVal = s
			}
		}
	}

	_, err = db.conn.Exec(
		`INSERT INTO devices (id, name, os, token_hash, created_at, last_seen) VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, osVal, tokenHash, now, now,
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

// UpdateDeviceSessions updates the session-count hint stored in active_agents.
func (db *DB) UpdateDeviceSessions(id string, count int) error {
	_, err := db.conn.Exec(`UPDATE devices SET active_agents = ?, last_seen = ? WHERE id = ?`, count, time.Now().Unix(), id)
	return err
}
