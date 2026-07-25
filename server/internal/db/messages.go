package db

import (
	"database/sql"
	"time"

	"github.com/nekonest/server/internal/protocol"
)

// SaveMessage stores a session message in the database.
func (db *DB) SaveMessage(deviceID, sessionID string, msg *protocol.SessionMessage) error {
	_, err := db.conn.Exec(`
		INSERT INTO session_messages (id, device_id, session_id, role, content, type, timestamp, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, deviceID, sessionID, msg.Role, msg.Content, msg.Type, msg.Timestamp,
		metadataToJSON(msg.Metadata),
	)
	return err
}

// GetMessages retrieves messages for a session, ordered by timestamp.
// limit=0 means no limit.
func (db *DB) GetMessages(deviceID, sessionID string, limit int) ([]*protocol.SessionMessage, error) {
	query := `SELECT id, role, content, type, timestamp, metadata_json
		FROM session_messages
		WHERE device_id = ? AND session_id = ?
		ORDER BY timestamp ASC`

	if limit > 0 {
		query = `SELECT id, role, content, type, timestamp, metadata_json
			FROM session_messages
			WHERE device_id = ? AND session_id = ?
			ORDER BY timestamp DESC
			LIMIT ?`
	}

	var rows *sql.Rows
	var err error

	if limit > 0 {
		rows, err = db.conn.Query(query, deviceID, sessionID, limit)
	} else {
		rows, err = db.conn.Query(query, deviceID, sessionID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*protocol.SessionMessage
	for rows.Next() {
		msg := &protocol.SessionMessage{}
		var metadataJSON sql.NullString
		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &msg.Type, &msg.Timestamp, &metadataJSON); err != nil {
			return nil, err
		}
		if metadataJSON.Valid {
			msg.Metadata = jsonToMetadata(metadataJSON.String)
		}
		messages = append(messages, msg)
	}

	// If we used LIMIT, reverse to get chronological order
	if limit > 0 {
		for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
			messages[i], messages[j] = messages[j], messages[i]
		}
	}

	return messages, nil
}

// GetMessageCount returns the number of messages for a session.
func (db *DB) GetMessageCount(deviceID, sessionID string) (int, error) {
	var count int
	err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM session_messages WHERE device_id = ? AND session_id = ?`,
		deviceID, sessionID,
	).Scan(&count)
	return count, err
}

// DeleteOldMessages removes messages older than the given timestamp.
func (db *DB) DeleteOldMessages(before time.Time) (int64, error) {
	result, err := db.conn.Exec(
		`DELETE FROM session_messages WHERE timestamp < ?`,
		before.Unix(),
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DeleteSessionMessages removes all messages for a session.
func (db *DB) DeleteSessionMessages(deviceID, sessionID string) error {
	_, err := db.conn.Exec(
		`DELETE FROM session_messages WHERE device_id = ? AND session_id = ?`,
		deviceID, sessionID,
	)
	return err
}

// ListSessionsWithMessages returns session IDs that have stored messages for a device.
func (db *DB) ListSessionsWithMessages(deviceID string) ([]string, error) {
	rows, err := db.conn.Query(
		`SELECT session_id FROM session_messages WHERE device_id = ? GROUP BY session_id ORDER BY MAX(timestamp) DESC`,
		deviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessionIDs []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		sessionIDs = append(sessionIDs, sid)
	}
	return sessionIDs, nil
}

// metadataToJSON converts metadata map to a JSON string for storage.
func metadataToJSON(m map[string]any) string {
	if m == nil {
		return "{}"
	}
	// Simple JSON serialization for metadata
	data, err := marshalJSON(m)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// jsonToMetadata parses a JSON string back to metadata map.
func jsonToMetadata(s string) map[string]any {
	if s == "" || s == "{}" {
		return nil
	}
	var m map[string]any
	if err := unmarshalJSON([]byte(s), &m); err != nil {
		return nil
	}
	return m
}
