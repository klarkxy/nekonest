package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	promptJournalVersion       = 2
	maxPromptJournalBytes      = 16 << 20
	maxPromptJournalEchoBytes  = 512
	maxJournalSessionIDBytes   = 512
	maxJournalClientMsgIDBytes = 256
	promptOutcomeIndeterminate = "prompt outcome is indeterminate; ordinary retry is disabled to prevent duplicate execution; inspect the agent session before any manual retry"
)

type promptJournalStatus string

const (
	promptJournalDispatching   promptJournalStatus = "dispatching"
	promptJournalAccepted      promptJournalStatus = "accepted"
	promptJournalCommitted     promptJournalStatus = "committed"
	promptJournalIndeterminate promptJournalStatus = "indeterminate"
)

type promptJournalRecord struct {
	Key          string              `json:"key"`
	SessionID    string              `json:"session_id"`
	ClientMsgID  string              `json:"client_msg_id"`
	LegacyOpaque bool                `json:"legacy_opaque,omitempty"`
	Status       promptJournalStatus `json:"status"`
	PromptEcho   string              `json:"prompt_echo,omitempty"`
	UpdatedAt    int64               `json:"updated_at"`
}

type promptJournalDisk struct {
	Version    int                   `json:"version"`
	DeviceHash string                `json:"device_hash"`
	Records    []promptJournalRecord `json:"records"`
}

// promptJournal is the durable source of truth for whether a command may be
// dispatched. A crash after writing "dispatching" but before writing
// "accepted" is deliberately treated as indeterminate on the next start.
type promptJournal struct {
	mu         sync.Mutex
	path       string
	deviceHash string
	max        int
	records    map[string]promptJournalRecord
}

func deviceHash(deviceID string) string {
	sum := sha256.Sum256([]byte(deviceID))
	return hex.EncodeToString(sum[:])
}

func promptJournalPath(configPath, deviceID string) string {
	if configPath == "" {
		configPath = "config.json"
	}
	hash := deviceHash(deviceID)
	return filepath.Join(filepath.Dir(configPath), "prompt-journal-"+hash[:16]+".json")
}

func loadPromptJournal(path, deviceID string, max int) (*promptJournal, error) {
	if max <= 0 {
		max = maxAcceptedPromptIDs
	}
	journal := &promptJournal{
		path:       path,
		deviceHash: deviceHash(deviceID),
		max:        max,
		records:    make(map[string]promptJournalRecord, max),
	}

	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := journal.persistRecords(journal.records); err != nil {
			return nil, fmt.Errorf("create prompt journal: %w", err)
		}
		return journal, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat prompt journal: %w", err)
	}
	if info.Size() > maxPromptJournalBytes {
		return nil, fmt.Errorf("prompt journal exceeds %d bytes", maxPromptJournalBytes)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("secure prompt journal permissions: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read prompt journal: %w", err)
	}
	var disk promptJournalDisk
	if err := json.Unmarshal(data, &disk); err != nil {
		return nil, fmt.Errorf("parse prompt journal: %w", err)
	}
	if disk.Version != 1 && disk.Version != promptJournalVersion {
		return nil, fmt.Errorf("unsupported prompt journal version %d", disk.Version)
	}
	if disk.DeviceHash != journal.deviceHash {
		return nil, fmt.Errorf("prompt journal belongs to a different device")
	}

	legacyV1 := disk.Version == 1
	changed := legacyV1
	for _, record := range disk.Records {
		if !validJournalKey(record.Key) {
			return nil, fmt.Errorf("prompt journal contains invalid key")
		}
		if legacyV1 {
			// Version 1 stored only the hashed key. Keep it as an opaque,
			// non-evictable record; an incoming duplicate/status query can
			// still find it by recomputing the same key. A later commit frame
			// backfills the bounded routing identifiers.
			record.LegacyOpaque = true
		} else if record.LegacyOpaque {
			if record.SessionID != "" || record.ClientMsgID != "" {
				return nil, fmt.Errorf("opaque prompt journal record contains partial identifiers")
			}
		} else {
			if err := validateJournalIdentifiers(record.SessionID, record.ClientMsgID); err != nil {
				return nil, fmt.Errorf("prompt journal contains invalid identifiers: %w", err)
			}
			if expected := promptJournalKeyFromHash(journal.deviceHash, record.SessionID, record.ClientMsgID); record.Key != expected {
				return nil, fmt.Errorf("prompt journal key does not match its identifiers")
			}
		}
		switch record.Status {
		case promptJournalAccepted, promptJournalCommitted, promptJournalIndeterminate:
		case promptJournalDispatching:
			// The prior process may have reached the agent before crashing.
			record.Status = promptJournalIndeterminate
			changed = true
		default:
			return nil, fmt.Errorf("prompt journal contains invalid status %q", record.Status)
		}
		boundedEcho := boundedUTF8(record.PromptEcho, maxPromptJournalEchoBytes)
		if boundedEcho != record.PromptEcho {
			record.PromptEcho = boundedEcho
			changed = true
		}
		if _, duplicate := journal.records[record.Key]; duplicate {
			return nil, fmt.Errorf("prompt journal contains duplicate key")
		}
		journal.records[record.Key] = record
	}
	if err := journal.trimCommitted(journal.records); err != nil {
		return nil, err
	}
	if len(journal.records) != len(disk.Records) {
		changed = true
	}
	if changed {
		if err := journal.persistRecords(journal.records); err != nil {
			return nil, fmt.Errorf("normalize prompt journal: %w", err)
		}
	}
	return journal, nil
}

func validJournalKey(key string) bool {
	if len(key) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

func boundedUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	suffix := "..."
	cut := maxBytes - len(suffix)
	if cut <= 0 {
		return suffix[:maxBytes]
	}
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut] + suffix
}

func cloneJournalRecords(records map[string]promptJournalRecord) map[string]promptJournalRecord {
	cloned := make(map[string]promptJournalRecord, len(records))
	for key, record := range records {
		cloned[key] = record
	}
	return cloned
}

func (j *promptJournal) state(sessionID, clientMsgID string) (promptJournalRecord, bool) {
	if clientMsgID == "" {
		return promptJournalRecord{}, false
	}
	key := promptJournalKeyFromHash(j.deviceHash, sessionID, clientMsgID)
	j.mu.Lock()
	defer j.mu.Unlock()
	record, ok := j.records[key]
	return record, ok
}

func promptJournalKeyFromHash(deviceIDHash, sessionID, clientMsgID string) string {
	sum := sha256.Sum256([]byte(deviceIDHash + "\x00" + sessionID + "\x00" + clientMsgID))
	return hex.EncodeToString(sum[:])
}

func validateJournalIdentifiers(sessionID, clientMsgID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session_id required for durable dispatch")
	}
	if strings.TrimSpace(clientMsgID) == "" {
		return fmt.Errorf("client_msg_id required for durable dispatch")
	}
	if len(sessionID) > maxJournalSessionIDBytes || !utf8.ValidString(sessionID) {
		return fmt.Errorf("session_id is too long or invalid UTF-8")
	}
	if len(clientMsgID) > maxJournalClientMsgIDBytes || !utf8.ValidString(clientMsgID) {
		return fmt.Errorf("client_msg_id is too long or invalid UTF-8")
	}
	return nil
}

func (j *promptJournal) markDispatching(sessionID, clientMsgID, prompt string) error {
	if err := validateJournalIdentifiers(sessionID, clientMsgID); err != nil {
		return err
	}
	key := promptJournalKeyFromHash(j.deviceHash, sessionID, clientMsgID)
	j.mu.Lock()
	defer j.mu.Unlock()
	if existing, ok := j.records[key]; ok {
		return fmt.Errorf("prompt already recorded as %s", existing.Status)
	}

	next := cloneJournalRecords(j.records)
	if len(next) >= j.max {
		if err := j.evictOldestCommitted(next); err != nil {
			return err
		}
	}
	next[key] = promptJournalRecord{
		Key:         key,
		SessionID:   sessionID,
		ClientMsgID: clientMsgID,
		Status:      promptJournalDispatching,
		PromptEcho:  boundedUTF8(prompt, maxPromptJournalEchoBytes),
		UpdatedAt:   time.Now().UnixNano(),
	}
	if err := j.persistRecords(next); err != nil {
		return err
	}
	j.records = next
	return nil
}

func (j *promptJournal) markAccepted(sessionID, clientMsgID string) error {
	return j.transition(sessionID, clientMsgID, promptJournalAccepted)
}

// rollbackDispatching removes a command only while it is still known to have
// been rejected before crossing the agent boundary. Persist first so a failed
// disk write never enables an unsafe retry in memory.
func (j *promptJournal) rollbackDispatching(sessionID, clientMsgID string) error {
	if err := validateJournalIdentifiers(sessionID, clientMsgID); err != nil {
		return err
	}
	key := promptJournalKeyFromHash(j.deviceHash, sessionID, clientMsgID)
	j.mu.Lock()
	defer j.mu.Unlock()
	current, ok := j.records[key]
	if !ok {
		return fmt.Errorf("prompt journal record not found")
	}
	if current.Status != promptJournalDispatching {
		return fmt.Errorf("cannot roll back prompt from %s", current.Status)
	}
	next := cloneJournalRecords(j.records)
	delete(next, key)
	if err := j.persistRecords(next); err != nil {
		return err
	}
	j.records = next
	return nil
}

func (j *promptJournal) markIndeterminate(sessionID, clientMsgID string) error {
	return j.transition(sessionID, clientMsgID, promptJournalIndeterminate)
}

func (j *promptJournal) markCommitted(sessionID, clientMsgID string) error {
	return j.transition(sessionID, clientMsgID, promptJournalCommitted)
}

func (j *promptJournal) transition(sessionID, clientMsgID string, status promptJournalStatus) error {
	if err := validateJournalIdentifiers(sessionID, clientMsgID); err != nil {
		return err
	}
	key := promptJournalKeyFromHash(j.deviceHash, sessionID, clientMsgID)
	j.mu.Lock()
	defer j.mu.Unlock()
	current, ok := j.records[key]
	if !ok {
		return fmt.Errorf("prompt journal record not found")
	}
	if current.Status == status {
		return nil
	}
	switch status {
	case promptJournalAccepted:
		if current.Status != promptJournalDispatching {
			return fmt.Errorf("cannot transition prompt from %s to accepted", current.Status)
		}
	case promptJournalIndeterminate:
		if current.Status != promptJournalDispatching {
			return fmt.Errorf("cannot transition prompt from %s to indeterminate", current.Status)
		}
	case promptJournalCommitted:
		if current.Status != promptJournalAccepted {
			return fmt.Errorf("cannot transition prompt from %s to committed", current.Status)
		}
	default:
		return fmt.Errorf("invalid prompt journal transition %s", status)
	}
	next := cloneJournalRecords(j.records)
	if current.LegacyOpaque {
		current.SessionID = sessionID
		current.ClientMsgID = clientMsgID
		current.LegacyOpaque = false
	}
	current.Status = status
	current.UpdatedAt = time.Now().UnixNano()
	next[key] = current
	if err := j.persistRecords(next); err != nil {
		return err
	}
	j.records = next
	return nil
}

func (j *promptJournal) uncommittedAccepted() []promptJournalRecord {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]promptJournalRecord, 0)
	for _, record := range j.records {
		if record.Status == promptJournalAccepted && !record.LegacyOpaque {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, k int) bool {
		if out[i].UpdatedAt == out[k].UpdatedAt {
			return out[i].Key < out[k].Key
		}
		return out[i].UpdatedAt < out[k].UpdatedAt
	})
	return out
}

func (j *promptJournal) trimCommitted(records map[string]promptJournalRecord) error {
	for len(records) > j.max {
		if err := j.evictOldestCommitted(records); err != nil {
			return fmt.Errorf("prompt journal has %d unresolved records but limit is %d", len(records), j.max)
		}
	}
	return nil
}

func (j *promptJournal) evictOldestCommitted(records map[string]promptJournalRecord) error {
	var oldest promptJournalRecord
	found := false
	for _, record := range records {
		if record.Status != promptJournalCommitted {
			continue
		}
		if !found || record.UpdatedAt < oldest.UpdatedAt {
			oldest = record
			found = true
		}
	}
	if !found {
		return fmt.Errorf("prompt journal is full of uncommitted commands; reconnect to the server or reconcile them before sending more")
	}
	delete(records, oldest.Key)
	return nil
}

func (j *promptJournal) persistRecords(records map[string]promptJournalRecord) error {
	ordered := make([]promptJournalRecord, 0, len(records))
	for _, record := range records {
		ordered = append(ordered, record)
	}
	sort.Slice(ordered, func(i, k int) bool {
		if ordered[i].UpdatedAt == ordered[k].UpdatedAt {
			return ordered[i].Key < ordered[k].Key
		}
		return ordered[i].UpdatedAt < ordered[k].UpdatedAt
	})
	disk := promptJournalDisk{
		Version:    promptJournalVersion,
		DeviceHash: j.deviceHash,
		Records:    ordered,
	}
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal prompt journal: %w", err)
	}
	if len(data) > maxPromptJournalBytes {
		return fmt.Errorf("prompt journal would exceed %d bytes", maxPromptJournalBytes)
	}
	if err := writeFileAtomic(j.path, data, 0o600); err != nil {
		return fmt.Errorf("persist prompt journal: %w", err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".prompt-journal-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
