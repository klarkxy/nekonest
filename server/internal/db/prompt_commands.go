package db

import (
	"database/sql"
	"errors"
	"time"
)

const (
	PromptRegistered    = "registered"
	PromptPending       = "pending"
	PromptAccepted      = "accepted"
	PromptFailed        = "failed"
	PromptIndeterminate = "indeterminate"
)

var ErrPromptCommandConflict = errors.New("client_msg_id already belongs to a different prompt")

// PromptCommand is the durable idempotency record for one phone prompt.
type PromptCommand struct {
	DeviceID           string
	ClientMsgID        string
	SessionID          string
	Prompt             string
	AttachmentsJSON    string
	SealedEnvelopeJSON string
	Status             string
	Error              string
	Outcome            string
	RetryAllowed       bool
	CommitSent         bool
	CreatedAt          int64
	UpdatedAt          int64
}

// RegisterPromptCommand inserts a new registered command. When retryFailed is
// explicitly true, a terminal failed command with the exact same immutable
// payload is atomically claimed for one retry. The bool reports whether the
// caller owns the forward.
func (db *DB) RegisterPromptCommand(cmd *PromptCommand, retryFailed bool) (*PromptCommand, bool, error) {
	if cmd == nil || cmd.DeviceID == "" || cmd.ClientMsgID == "" || cmd.SessionID == "" {
		return nil, false, errors.New("device_id, client_msg_id and session_id are required")
	}
	now := time.Now().Unix()
	result, err := db.conn.Exec(`
		INSERT OR IGNORE INTO prompt_commands
			(device_id, client_msg_id, session_id, prompt, attachments_json, sealed_envelope_json,
			 status, error, outcome, retry_allowed, commit_sent, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'registered', '', '', 0, 0, ?, ?)`,
		cmd.DeviceID, cmd.ClientMsgID, cmd.SessionID, cmd.Prompt, cmd.AttachmentsJSON, cmd.SealedEnvelopeJSON, now, now,
	)
	if err != nil {
		return nil, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	stored, err := db.GetPromptCommand(cmd.DeviceID, cmd.ClientMsgID)
	if err != nil {
		return nil, false, err
	}
	if affected == 0 &&
		(stored.SessionID != cmd.SessionID ||
			stored.Prompt != cmd.Prompt ||
			stored.AttachmentsJSON != cmd.AttachmentsJSON ||
			stored.SealedEnvelopeJSON != cmd.SealedEnvelopeJSON) {
		return stored, false, ErrPromptCommandConflict
	}
	if affected == 1 {
		return stored, true, nil
	}
	// registered means SQLite durably recorded the command but the server did
	// not durably observe a successful websocket write. Re-sending the same ID
	// is safe because the daemon journals IDs before external execution.
	if stored.Status == PromptRegistered {
		return stored, true, nil
	}
	if stored.Status != PromptFailed || !stored.RetryAllowed || !retryFailed {
		return stored, false, nil
	}

	retry, err := db.conn.Exec(`
		UPDATE prompt_commands
		SET status = 'registered', error = '', outcome = '', retry_allowed = 0, updated_at = ?
		WHERE device_id = ? AND client_msg_id = ?
		  AND status = 'failed' AND retry_allowed = 1`,
		time.Now().Unix(), cmd.DeviceID, cmd.ClientMsgID,
	)
	if err != nil {
		return nil, false, err
	}
	claimed, err := retry.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	stored, err = db.GetPromptCommand(cmd.DeviceID, cmd.ClientMsgID)
	if err != nil {
		return nil, false, err
	}
	return stored, claimed == 1, nil
}

// MarkPromptForwarded records that the websocket write completed. From this
// point a replay must query daemon journal state rather than resend execution.
func (db *DB) MarkPromptForwarded(deviceID, clientMsgID string) (*PromptCommand, bool, error) {
	result, err := db.conn.Exec(`
		UPDATE prompt_commands
		SET status = 'pending', updated_at = ?
		WHERE device_id = ? AND client_msg_id = ? AND status = 'registered'`,
		time.Now().Unix(), deviceID, clientMsgID,
	)
	return db.promptTransitionResult(deviceID, clientMsgID, result, err)
}

func (db *DB) GetPromptCommand(deviceID, clientMsgID string) (*PromptCommand, error) {
	var cmd PromptCommand
	err := db.conn.QueryRow(`
		SELECT device_id, client_msg_id, session_id, prompt, attachments_json, sealed_envelope_json,
		       status, error, outcome, retry_allowed, commit_sent, created_at, updated_at
		FROM prompt_commands
		WHERE device_id = ? AND client_msg_id = ?`,
		deviceID, clientMsgID,
	).Scan(
		&cmd.DeviceID, &cmd.ClientMsgID, &cmd.SessionID, &cmd.Prompt,
		&cmd.AttachmentsJSON, &cmd.SealedEnvelopeJSON, &cmd.Status, &cmd.Error, &cmd.Outcome,
		&cmd.RetryAllowed, &cmd.CommitSent, &cmd.CreatedAt, &cmd.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &cmd, nil
}

// MarkPromptAccepted records the daemon's authoritative acceptance. It may
// override a transport-level failure because websocket writes can fail after a
// complete frame reached the daemon. Accepted itself is terminal.
func (db *DB) MarkPromptAccepted(deviceID, clientMsgID string) (*PromptCommand, bool, error) {
	now := time.Now().Unix()
	result, err := db.conn.Exec(`
		UPDATE prompt_commands
		SET status = 'accepted', error = '', outcome = 'accepted',
		    retry_allowed = 0, commit_sent = 0, updated_at = ?
		WHERE device_id = ? AND client_msg_id = ?
		  AND status IN ('registered', 'pending', 'failed', 'indeterminate')`,
		now, deviceID, clientMsgID,
	)
	return db.promptTransitionResult(deviceID, clientMsgID, result, err)
}

func (db *DB) MarkPromptCommitted(deviceID, clientMsgID string) error {
	_, err := db.conn.Exec(`
		UPDATE prompt_commands
		SET commit_sent = 1, updated_at = ?
		WHERE device_id = ? AND client_msg_id = ? AND status = 'accepted'`,
		time.Now().Unix(), deviceID, clientMsgID,
	)
	return err
}

func (db *DB) ListUncommittedAcceptedPrompts(deviceID string, limit int) ([]*PromptCommand, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := db.conn.Query(`
		SELECT device_id, client_msg_id, session_id, prompt, attachments_json, sealed_envelope_json,
		       status, error, outcome, retry_allowed, commit_sent, created_at, updated_at
		FROM prompt_commands
		WHERE device_id = ? AND status = 'accepted' AND commit_sent = 0
		ORDER BY updated_at ASC, client_msg_id ASC
		LIMIT ?`,
		deviceID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var commands []*PromptCommand
	for rows.Next() {
		cmd := &PromptCommand{}
		if err := rows.Scan(
			&cmd.DeviceID, &cmd.ClientMsgID, &cmd.SessionID, &cmd.Prompt,
			&cmd.AttachmentsJSON, &cmd.SealedEnvelopeJSON, &cmd.Status, &cmd.Error, &cmd.Outcome,
			&cmd.RetryAllowed, &cmd.CommitSent, &cmd.CreatedAt, &cmd.UpdatedAt,
		); err != nil {
			return nil, err
		}
		commands = append(commands, cmd)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return commands, nil
}

// MarkPromptFailed transitions a pending command to a terminal result. An
// indeterminate outcome is deliberately never eligible for ordinary retry.
func (db *DB) MarkPromptFailed(
	deviceID, clientMsgID, message, outcome string,
	retryAllowed bool,
) (*PromptCommand, bool, error) {
	status := PromptFailed
	if outcome == PromptIndeterminate {
		status = PromptIndeterminate
		retryAllowed = false
	}
	if outcome == "" {
		outcome = PromptFailed
	}
	now := time.Now().Unix()
	retryInt := 0
	if retryAllowed {
		retryInt = 1
	}
	var (
		result sql.Result
		err    error
	)
	if status == PromptIndeterminate {
		// A late daemon journal result must be able to tighten an earlier
		// retryable transport failure.
		result, err = db.conn.Exec(`
			UPDATE prompt_commands
			SET status = 'indeterminate', error = ?, outcome = 'indeterminate',
			    retry_allowed = 0, updated_at = ?
			WHERE device_id = ? AND client_msg_id = ?
			  AND status IN ('registered', 'pending', 'failed')`,
			message, now, deviceID, clientMsgID,
		)
	} else {
		result, err = db.conn.Exec(`
			UPDATE prompt_commands
			SET status = ?, error = ?, outcome = ?, retry_allowed = ?, updated_at = ?
			WHERE device_id = ? AND client_msg_id = ?
			  AND status IN ('registered', 'pending')`,
			status, message, outcome, retryInt, now, deviceID, clientMsgID,
		)
	}
	return db.promptTransitionResult(deviceID, clientMsgID, result, err)
}

func (db *DB) promptTransitionResult(
	deviceID, clientMsgID string,
	result sql.Result,
	err error,
) (*PromptCommand, bool, error) {
	if err != nil {
		return nil, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	cmd, err := db.GetPromptCommand(deviceID, clientMsgID)
	if err != nil {
		return nil, false, err
	}
	return cmd, affected == 1, nil
}

// PromptCommandCount is intentionally small and primarily useful for
// diagnostics and focused idempotency tests.
func (db *DB) PromptCommandCount(deviceID, clientMsgID string) (int, error) {
	var count int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM prompt_commands
		WHERE device_id = ? AND client_msg_id = ?`,
		deviceID, clientMsgID,
	).Scan(&count)
	return count, err
}
