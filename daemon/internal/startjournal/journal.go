// Package startjournal durably coordinates native thread creation operations.
//
// A starting record is persisted before an adapter is invoked. If the daemon
// exits while such a record exists, Load converts it to indeterminate so the
// same operation is never dispatched automatically after restart.
package startjournal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	journalVersion          = 1
	maxJournalBytes         = 16 << 20
	maxOperationIDBytes     = 256
	maxSessionIDBytes       = 512
	maxAgentTypeBytes       = 64
	maxProjectDirBytes      = 4096
	maxTerminalMessageBytes = 2048

	RecoveredMessage = "thread start outcome is indeterminate after daemon restart; automatic retry is disabled to prevent duplicate native threads"
)

// Status is the durable lifecycle of one native thread creation operation.
type Status string

const (
	StatusStarting      Status = "thread_starting"
	StatusOwned         Status = "thread_owned"
	StatusFailed        Status = "thread_failed"
	StatusIndeterminate Status = "thread_indeterminate"
)

// Request is the immutable binding for one operation id. PromptDigest must be
// produced by PromptDigest so prompt text is never written to this journal.
type Request struct {
	AgentType    string `json:"agent_type"`
	ProjectDir   string `json:"project_dir"`
	PromptDigest string `json:"prompt_digest"`
}

// Record is the replayable durable result for one operation.
type Record struct {
	Key            string `json:"key"`
	OperationID    string `json:"operation_id"`
	AgentType      string `json:"agent_type"`
	ProjectDir     string `json:"project_dir"`
	PromptDigest   string `json:"prompt_digest"`
	Status         Status `json:"status"`
	SessionID      string `json:"session_id,omitempty"`
	PromptAccepted bool   `json:"prompt_accepted,omitempty"`
	Message        string `json:"message,omitempty"`
	UpdatedAt      int64  `json:"updated_at"`
}

type diskJournal struct {
	Version    int      `json:"version"`
	DeviceHash string   `json:"device_hash"`
	Records    []Record `json:"records"`
}

// ConflictError reports an operation id reused with different immutable input.
type ConflictError struct {
	OperationID string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("operation_id %q is already bound to a different thread start request", e.OperationID)
}

// IsConflict reports whether err is a conflicting duplicate operation.
func IsConflict(err error) bool {
	var target *ConflictError
	return errors.As(err, &target)
}

// Journal is safe for concurrent message handlers in one daemon process.
type Journal struct {
	mu         sync.Mutex
	path       string
	deviceHash string
	records    map[string]Record
}

// PromptDigest returns the stable binding digest without retaining prompt text.
func PromptDigest(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

// Path returns the device-isolated journal location adjacent to daemon config.
func Path(configPath, deviceID string) string {
	if configPath == "" {
		configPath = "config.json"
	}
	hash := hashString(deviceID)
	return filepath.Join(filepath.Dir(configPath), "thread-start-journal-"+hash[:16]+".json")
}

// Load opens a journal and fail-closes any operation interrupted while starting.
func Load(path, deviceID string) (*Journal, error) {
	if strings.TrimSpace(deviceID) == "" {
		return nil, fmt.Errorf("device id required")
	}
	j := &Journal{
		path:       path,
		deviceHash: hashString(deviceID),
		records:    make(map[string]Record),
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open thread start journal: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("secure thread start journal permissions: %w", err)
	}

	limited := io.LimitReader(f, maxJournalBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("read thread start journal: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close thread start journal: %w", err)
	}
	if len(data) > maxJournalBytes {
		return nil, fmt.Errorf("thread start journal exceeds %d bytes", maxJournalBytes)
	}
	var disk diskJournal
	if err := json.Unmarshal(data, &disk); err != nil {
		return nil, fmt.Errorf("decode thread start journal: %w", err)
	}
	if disk.Version != journalVersion {
		return nil, fmt.Errorf("unsupported thread start journal version %d", disk.Version)
	}
	if disk.DeviceHash != j.deviceHash {
		return nil, fmt.Errorf("thread start journal belongs to another device")
	}

	changed := false
	for _, record := range disk.Records {
		if err := j.validateRecord(record); err != nil {
			return nil, err
		}
		if _, duplicate := j.records[record.Key]; duplicate {
			return nil, fmt.Errorf("thread start journal contains duplicate operation key")
		}
		if record.Status == StatusStarting {
			record.Status = StatusIndeterminate
			record.Message = RecoveredMessage
			record.UpdatedAt = time.Now().UnixNano()
			changed = true
		}
		j.records[record.Key] = record
	}
	if changed {
		if err := j.persist(j.records); err != nil {
			return nil, fmt.Errorf("recover thread start journal: %w", err)
		}
	}
	return j, nil
}

// Begin atomically binds operationID to request and records starting. created
// is false for a duplicate with identical input; its existing state is replayed.
func (j *Journal) Begin(operationID string, request Request) (record Record, created bool, err error) {
	if err := validateOperationID(operationID); err != nil {
		return Record{}, false, err
	}
	if err := validateRequest(request); err != nil {
		return Record{}, false, err
	}
	key := operationKey(j.deviceHash, operationID)
	j.mu.Lock()
	defer j.mu.Unlock()
	if existing, ok := j.records[key]; ok {
		if existing.OperationID != operationID || !sameRequest(existing, request) {
			return Record{}, false, &ConflictError{OperationID: operationID}
		}
		return existing, false, nil
	}
	next := cloneRecords(j.records)
	record = Record{
		Key:          key,
		OperationID:  operationID,
		AgentType:    request.AgentType,
		ProjectDir:   request.ProjectDir,
		PromptDigest: request.PromptDigest,
		Status:       StatusStarting,
		UpdatedAt:    time.Now().UnixNano(),
	}
	next[key] = record
	if err := j.persist(next); err != nil {
		return Record{}, false, err
	}
	j.records = next
	return record, true, nil
}

// Finish persists one terminal result. It is idempotent only when the complete
// result matches an already persisted terminal record.
func (j *Journal) Finish(operationID string, status Status, sessionID, message string) (Record, error) {
	return j.FinishWithPromptOutcome(operationID, status, sessionID, message, false)
}

// FinishWithPromptOutcome persists a terminal result and its first-prompt
// acceptance evidence. Owned is valid only when both the native session id and
// positive prompt acceptance are present.
func (j *Journal) FinishWithPromptOutcome(operationID string, status Status, sessionID, message string, promptAccepted bool) (Record, error) {
	if !terminal(status) {
		return Record{}, fmt.Errorf("invalid terminal thread start status %q", status)
	}
	if err := validateOperationID(operationID); err != nil {
		return Record{}, err
	}
	if len(sessionID) > maxSessionIDBytes || !utf8.ValidString(sessionID) {
		return Record{}, fmt.Errorf("session id is too long or invalid UTF-8")
	}
	message = boundedUTF8(message, maxTerminalMessageBytes)
	key := operationKey(j.deviceHash, operationID)
	j.mu.Lock()
	defer j.mu.Unlock()
	current, ok := j.records[key]
	if !ok {
		return Record{}, fmt.Errorf("thread start operation not found")
	}
	if terminal(current.Status) {
		if current.Status == status && current.SessionID == sessionID && current.Message == message && current.PromptAccepted == promptAccepted {
			return current, nil
		}
		return Record{}, fmt.Errorf("thread start operation already finished as %s", current.Status)
	}
	if current.Status != StatusStarting {
		return Record{}, fmt.Errorf("invalid thread start transition from %s", current.Status)
	}
	if status == StatusOwned && strings.TrimSpace(sessionID) == "" {
		return Record{}, fmt.Errorf("owned thread start requires session id")
	}
	if status == StatusOwned && !promptAccepted {
		return Record{}, fmt.Errorf("owned thread start requires positive initial-prompt acknowledgement")
	}
	next := cloneRecords(j.records)
	current.Status = status
	current.SessionID = sessionID
	current.PromptAccepted = promptAccepted
	current.Message = message
	current.UpdatedAt = time.Now().UnixNano()
	next[key] = current
	if err := j.persist(next); err != nil {
		return Record{}, err
	}
	j.records = next
	return current, nil
}

// Lookup returns a copy of the current record for an operation.
func (j *Journal) Lookup(operationID string) (Record, bool) {
	if validateOperationID(operationID) != nil {
		return Record{}, false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	record, ok := j.records[operationKey(j.deviceHash, operationID)]
	return record, ok
}

// FailClosed marks a still-starting operation indeterminate in memory after a
// persistence failure. Its on-disk starting record will be recovered to the
// same state on restart, while duplicates in this process are refused now.
func (j *Journal) FailClosed(operationID, message string) (Record, bool) {
	if validateOperationID(operationID) != nil {
		return Record{}, false
	}
	key := operationKey(j.deviceHash, operationID)
	j.mu.Lock()
	defer j.mu.Unlock()
	record, ok := j.records[key]
	if !ok {
		return Record{}, false
	}
	if record.Status == StatusStarting {
		record.Status = StatusIndeterminate
		record.Message = boundedUTF8(message, maxTerminalMessageBytes)
		record.UpdatedAt = time.Now().UnixNano()
		j.records[key] = record
	}
	return record, true
}

func (j *Journal) validateRecord(record Record) error {
	if err := validateOperationID(record.OperationID); err != nil {
		return fmt.Errorf("thread start journal has invalid operation id: %w", err)
	}
	request := Request{AgentType: record.AgentType, ProjectDir: record.ProjectDir, PromptDigest: record.PromptDigest}
	if err := validateRequest(request); err != nil {
		return fmt.Errorf("thread start journal has invalid request binding: %w", err)
	}
	if expected := operationKey(j.deviceHash, record.OperationID); record.Key != expected {
		return fmt.Errorf("thread start journal operation key mismatch")
	}
	switch record.Status {
	case StatusStarting, StatusOwned, StatusFailed, StatusIndeterminate:
	default:
		return fmt.Errorf("thread start journal has invalid status %q", record.Status)
	}
	if record.Status == StatusOwned && (strings.TrimSpace(record.SessionID) == "" || !record.PromptAccepted) {
		return fmt.Errorf("thread start journal owned record lacks session id or prompt acknowledgement")
	}
	if len(record.SessionID) > maxSessionIDBytes || !utf8.ValidString(record.SessionID) {
		return fmt.Errorf("thread start journal has invalid session id")
	}
	if len(record.Message) > maxTerminalMessageBytes || !utf8.ValidString(record.Message) {
		return fmt.Errorf("thread start journal has invalid terminal message")
	}
	return nil
}

func (j *Journal) persist(records map[string]Record) error {
	ordered := make([]Record, 0, len(records))
	for _, record := range records {
		ordered = append(ordered, record)
	}
	sort.Slice(ordered, func(i, k int) bool {
		if ordered[i].UpdatedAt == ordered[k].UpdatedAt {
			return ordered[i].Key < ordered[k].Key
		}
		return ordered[i].UpdatedAt < ordered[k].UpdatedAt
	})
	data, err := json.MarshalIndent(diskJournal{
		Version:    journalVersion,
		DeviceHash: j.deviceHash,
		Records:    ordered,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal thread start journal: %w", err)
	}
	if len(data) > maxJournalBytes {
		return fmt.Errorf("thread start journal would exceed %d bytes", maxJournalBytes)
	}
	if err := writeFileAtomic(j.path, data, 0o600); err != nil {
		return fmt.Errorf("persist thread start journal: %w", err)
	}
	return nil
}

func validateOperationID(operationID string) error {
	if strings.TrimSpace(operationID) == "" {
		return fmt.Errorf("operation_id required for safe thread start")
	}
	if len(operationID) > maxOperationIDBytes || !utf8.ValidString(operationID) {
		return fmt.Errorf("operation_id is too long or invalid UTF-8")
	}
	return nil
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.AgentType) == "" || len(request.AgentType) > maxAgentTypeBytes || !utf8.ValidString(request.AgentType) {
		return fmt.Errorf("agent_type is required, too long, or invalid UTF-8")
	}
	if strings.TrimSpace(request.ProjectDir) == "" || len(request.ProjectDir) > maxProjectDirBytes || !utf8.ValidString(request.ProjectDir) {
		return fmt.Errorf("project_dir is required, too long, or invalid UTF-8")
	}
	if len(request.PromptDigest) != sha256.Size*2 {
		return fmt.Errorf("invalid prompt digest")
	}
	if _, err := hex.DecodeString(request.PromptDigest); err != nil {
		return fmt.Errorf("invalid prompt digest")
	}
	return nil
}

func sameRequest(record Record, request Request) bool {
	return record.AgentType == request.AgentType &&
		record.ProjectDir == request.ProjectDir &&
		record.PromptDigest == request.PromptDigest
}

func terminal(status Status) bool {
	return status == StatusOwned || status == StatusFailed || status == StatusIndeterminate
}

func operationKey(deviceHash, operationID string) string {
	return hashString(deviceHash + "\x00" + operationID)
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func cloneRecords(records map[string]Record) map[string]Record {
	cloned := make(map[string]Record, len(records))
	for key, record := range records {
		cloned[key] = record
	}
	return cloned
}

func boundedUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	suffix := "..."
	cut := maxBytes - len(suffix)
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut] + suffix
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".thread-start-journal-*.tmp")
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
	if err := replaceFile(tmpPath, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
