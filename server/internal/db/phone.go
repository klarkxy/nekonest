package db

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	corestore "github.com/klarkxy/nekonest/relaycore/store"
)

// PhoneIdentity is an independent phone client identity.
type PhoneIdentity = corestore.PhoneIdentity

// PhoneAuth is the result of validating a phone bearer token.
type PhoneAuth = corestore.PhoneAuth

var (
	ErrPhoneNotFound     = corestore.ErrPhoneNotFound
	ErrPhoneRevoked      = corestore.ErrPhoneRevoked
	ErrPhoneTokenInvalid = corestore.ErrPhoneTokenInvalid
	ErrGrantNotFound     = errors.New("device grant not found")
	ErrGrantRevoked      = errors.New("device grant revoked")
)

// CreatePhoneIdentity mints a new phone identity and returns the plaintext token once.
func (db *DB) CreatePhoneIdentity(name string) (phoneID, token string, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Phone"
	}
	token = generateToken()
	phoneID = "phone_" + generateToken()[:16]
	now := time.Now().Unix()
	_, err = db.conn.Exec(
		`INSERT INTO phone_identities (id, name, token_hash, ed25519_public, x25519_public, created_at, last_seen, revoked_at)
		 VALUES (?, ?, ?, '', '', ?, ?, 0)`,
		phoneID, name, hashToken(token), now, now,
	)
	if err != nil {
		return "", "", err
	}
	return phoneID, token, nil
}

// ValidatePhoneToken returns phone auth for an active token.
func (db *DB) ValidatePhoneToken(token string) (*PhoneAuth, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrPhoneTokenInvalid
	}
	row := db.conn.QueryRow(
		`SELECT id, name, token_hash, revoked_at FROM phone_identities WHERE token_hash = ?`,
		hashToken(token),
	)
	var id, name, tokenHash string
	var revokedAt int64
	if err := row.Scan(&id, &name, &tokenHash, &revokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPhoneTokenInvalid
		}
		return nil, err
	}
	// Constant-time compare of hashes (already looked up by hash; still guards timing).
	if subtle.ConstantTimeCompare([]byte(tokenHash), []byte(hashToken(token))) != 1 {
		return nil, ErrPhoneTokenInvalid
	}
	if revokedAt > 0 {
		return nil, ErrPhoneRevoked
	}
	_, _ = db.conn.Exec(`UPDATE phone_identities SET last_seen = ? WHERE id = ?`, time.Now().Unix(), id)
	return &PhoneAuth{PhoneID: id, Name: name}, nil
}

// GetPhone returns a phone identity by id.
func (db *DB) GetPhone(id string) (*PhoneIdentity, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, ed25519_public, x25519_public, created_at, last_seen, revoked_at
		 FROM phone_identities WHERE id = ?`, id,
	)
	var p PhoneIdentity
	if err := row.Scan(&p.ID, &p.Name, &p.Ed25519Public, &p.X25519Public, &p.CreatedAt, &p.LastSeen, &p.RevokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPhoneNotFound
		}
		return nil, err
	}
	return &p, nil
}

// ListPhones returns all phone identities (including revoked).
func (db *DB) ListPhones() ([]*PhoneIdentity, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, ed25519_public, x25519_public, created_at, last_seen, revoked_at
		 FROM phone_identities ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PhoneIdentity
	for rows.Next() {
		var p PhoneIdentity
		if err := rows.Scan(&p.ID, &p.Name, &p.Ed25519Public, &p.X25519Public, &p.CreatedAt, &p.LastSeen, &p.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// SetPhonePublicKeys stores E2E public keys for a phone.
func (db *DB) SetPhonePublicKeys(phoneID, ed25519Pub, x25519Pub string) error {
	res, err := db.conn.Exec(
		`UPDATE phone_identities SET ed25519_public = ?, x25519_public = ? WHERE id = ? AND revoked_at = 0`,
		ed25519Pub, x25519Pub, phoneID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrPhoneNotFound
	}
	return nil
}

// RevokePhone marks a phone revoked and revokes all its device grants.
func (db *DB) RevokePhone(phoneID string) error {
	now := time.Now().Unix()
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE phone_identities SET revoked_at = ? WHERE id = ? AND revoked_at = 0`,
		now, phoneID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Already revoked or missing — check existence
		var exists int
		if err := tx.QueryRow(`SELECT 1 FROM phone_identities WHERE id = ?`, phoneID).Scan(&exists); err != nil {
			return ErrPhoneNotFound
		}
		return nil
	}
	if _, err := tx.Exec(
		`UPDATE phone_device_grants SET revoked_at = ? WHERE phone_id = ? AND revoked_at = 0`,
		now, phoneID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM key_packages WHERE phone_id = ?`, phoneID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM push_subscriptions WHERE phone_id = ?`, phoneID); err != nil {
		return err
	}
	return tx.Commit()
}

// GrantPhoneDevice creates or reactivates a phone→device grant.
func (db *DB) GrantPhoneDevice(phoneID, deviceID string) error {
	if phoneID == "" || deviceID == "" {
		return fmt.Errorf("phone_id and device_id required")
	}
	// Ensure phone is active
	p, err := db.GetPhone(phoneID)
	if err != nil {
		return err
	}
	if p.RevokedAt > 0 {
		return ErrPhoneRevoked
	}
	if !db.DeviceExists(deviceID) {
		return fmt.Errorf("unknown device")
	}
	now := time.Now().Unix()
	_, err = db.conn.Exec(`
		INSERT INTO phone_device_grants (phone_id, device_id, paired_at, revoked_at)
		VALUES (?, ?, ?, 0)
		ON CONFLICT(phone_id, device_id) DO UPDATE SET
			paired_at = excluded.paired_at,
			revoked_at = 0
	`, phoneID, deviceID, now)
	return err
}

// RevokePhoneDeviceGrant revokes one phone→device grant.
func (db *DB) RevokePhoneDeviceGrant(phoneID, deviceID string) error {
	now := time.Now().Unix()
	res, err := db.conn.Exec(
		`UPDATE phone_device_grants SET revoked_at = ? WHERE phone_id = ? AND device_id = ? AND revoked_at = 0`,
		now, phoneID, deviceID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrGrantNotFound
	}
	_, _ = db.conn.Exec(
		`DELETE FROM key_packages WHERE phone_id = ? AND device_id = ?`,
		phoneID, deviceID,
	)
	_, _ = db.conn.Exec(
		`DELETE FROM push_subscriptions WHERE phone_id = ? AND device_id = ?`,
		phoneID, deviceID,
	)
	return nil
}

// PhoneHasDeviceGrant reports whether phone may access device.
func (db *DB) PhoneHasDeviceGrant(phoneID, deviceID string) bool {
	if phoneID == "" || deviceID == "" {
		return false
	}
	var n int
	err := db.conn.QueryRow(
		`SELECT 1 FROM phone_device_grants
		 WHERE phone_id = ? AND device_id = ? AND revoked_at = 0 LIMIT 1`,
		phoneID, deviceID,
	).Scan(&n)
	return err == nil && n == 1
}

// ListPhoneDeviceIDs returns active device grants for a phone.
func (db *DB) ListPhoneDeviceIDs(phoneID string) ([]string, error) {
	rows, err := db.conn.Query(
		`SELECT device_id FROM phone_device_grants
		 WHERE phone_id = ? AND revoked_at = 0 ORDER BY paired_at DESC`,
		phoneID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// UpsertKeyPackage stores a wrapped key package for a phone/device/scope.
func (db *DB) UpsertKeyPackage(phoneID, deviceID, scope, sessionID string, epoch uint64, wrappedKey, nonce string) error {
	now := time.Now().Unix()
	_, err := db.conn.Exec(`
		INSERT INTO key_packages (phone_id, device_id, scope, session_id, epoch, wrapped_key, nonce, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(phone_id, device_id, scope, session_id, epoch) DO UPDATE SET
			wrapped_key = excluded.wrapped_key,
			nonce = excluded.nonce,
			created_at = excluded.created_at
	`, phoneID, deviceID, scope, sessionID, epoch, wrappedKey, nonce, now)
	return err
}

// PhoneGrant describes an active phone→device grant with optional E2E pubs.
type PhoneGrant = corestore.PhoneGrant

// ListPhoneGrantsForDevice returns active grants for a host (daemon key wrap).
func (db *DB) ListPhoneGrantsForDevice(deviceID string) ([]*PhoneGrant, error) {
	rows, err := db.conn.Query(`
		SELECT g.phone_id, g.device_id, p.ed25519_public, p.x25519_public, g.paired_at
		FROM phone_device_grants g
		JOIN phone_identities p ON p.id = g.phone_id
		WHERE g.device_id = ? AND g.revoked_at = 0 AND p.revoked_at = 0
		ORDER BY g.paired_at DESC`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PhoneGrant
	for rows.Next() {
		var g PhoneGrant
		if err := rows.Scan(&g.PhoneID, &g.DeviceID, &g.Ed25519Public, &g.X25519Public, &g.PairedAt); err != nil {
			return nil, err
		}
		out = append(out, &g)
	}
	return out, rows.Err()
}

// ListKeyPackages returns key packages for a phone (optionally filtered by device).
func (db *DB) ListKeyPackages(phoneID, deviceID string) ([]map[string]any, error) {
	q := `SELECT phone_id, device_id, scope, session_id, epoch, wrapped_key, nonce, created_at
	      FROM key_packages WHERE phone_id = ?`
	args := []any{phoneID}
	if deviceID != "" {
		q += ` AND device_id = ?`
		args = append(args, deviceID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := db.conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var pid, did, scope, sid, wk, nonce string
		var epoch uint64
		var created int64
		if err := rows.Scan(&pid, &did, &scope, &sid, &epoch, &wk, &nonce, &created); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"phone_id":    pid,
			"device_id":   did,
			"scope":       scope,
			"session_id":  sid,
			"epoch":       epoch,
			"wrapped_key": wk,
			"nonce":       nonce,
			"created_at":  created,
		})
	}
	return out, rows.Err()
}
