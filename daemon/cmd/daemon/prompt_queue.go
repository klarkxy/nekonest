package main

import (
	"bytes"
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

func promptQueuePath(configPath, deviceID string) string {
	if configPath == "" {
		configPath = "config.json"
	}
	hash := deviceHash(deviceID)
	return filepath.Join(filepath.Dir(configPath), "prompt-queue-"+hash[:16]+".json")
}

const (
	promptQueueVersion            = 1
	maxPromptQueuePerSession      = 20
	maxCancelledQueuePerSession   = 256
	maxPromptQueueBytes           = 16 << 20
	maxPromptQueueSessionIDBytes  = 512
	maxPromptQueueClientMsgIDSize = 256
	maxPromptQueueAgentTypeBytes  = 64
)

type promptQueueStatus string

const (
	promptQueueQueued    promptQueueStatus = "queued"
	promptQueueRunning   promptQueueStatus = "running"
	promptQueuePaused    promptQueueStatus = "paused"
	promptQueueCancelled promptQueueStatus = "cancelled"
)

// promptQueueItem is a durable queued prompt. SealedEnvelope is deliberately
// a byte slice: it is written as base64 JSON so the opaque envelope round trips
// byte-for-byte, rather than being decoded and re-marshaled as JSON.
type promptQueueItem struct {
	SessionID      string            `json:"session_id"`
	ClientMsgID    string            `json:"client_msg_id"`
	AgentType      string            `json:"agent_type"`
	Prompt         string            `json:"prompt,omitempty"`
	Attachments    json.RawMessage   `json:"attachments,omitempty"`
	SealedEnvelope []byte            `json:"sealed_envelope,omitempty"`
	CreatedAt      int64             `json:"created_at"`
	Order          uint64            `json:"order"`
	Status         promptQueueStatus `json:"status"`
}

type promptQueueDisk struct {
	Version int               `json:"version"`
	Items   []promptQueueItem `json:"items"`
}

// promptQueue persists before exposing every mutation in memory. This keeps a
// failed write from making an item appear queued, running, paused, or cancelled
// when the durable source of truth says otherwise.
type promptQueue struct {
	mu     sync.Mutex
	path   string
	items  map[string]promptQueueItem
	nextID uint64
}

func loadPromptQueue(path string) (*promptQueue, error) {
	queue := &promptQueue{path: path, items: make(map[string]promptQueueItem)}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := queue.persist(queue.items); err != nil {
			return nil, fmt.Errorf("create prompt queue: %w", err)
		}
		return queue, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat prompt queue: %w", err)
	}
	if info.Size() > maxPromptQueueBytes {
		return nil, fmt.Errorf("prompt queue exceeds %d bytes", maxPromptQueueBytes)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("secure prompt queue permissions: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read prompt queue: %w", err)
	}
	var disk promptQueueDisk
	if err := json.Unmarshal(data, &disk); err != nil {
		return nil, fmt.Errorf("parse prompt queue: %w", err)
	}
	if disk.Version != promptQueueVersion {
		return nil, fmt.Errorf("unsupported prompt queue version %d", disk.Version)
	}

	changed := false
	for _, item := range disk.Items {
		if item.Status == promptQueueCancelled && (item.Prompt != "" || len(item.Attachments) > 0 || len(item.SealedEnvelope) > 0) {
			item.Prompt = ""
			item.Attachments = nil
			item.SealedEnvelope = nil
			changed = true
		}
		if err := validatePromptQueueItem(item); err != nil {
			return nil, fmt.Errorf("invalid prompt queue item: %w", err)
		}
		key := promptQueueKey(item.SessionID, item.ClientMsgID)
		if _, duplicate := queue.items[key]; duplicate {
			return nil, fmt.Errorf("prompt queue contains duplicate client_msg_id")
		}
		if item.Status == promptQueueRunning {
			item.Status = promptQueuePaused
			changed = true
		}
		queue.items[key] = clonePromptQueueItem(item)
		if item.Order > queue.nextID {
			queue.nextID = item.Order
		}
	}
	if changed {
		if err := queue.persist(queue.items); err != nil {
			return nil, fmt.Errorf("normalize prompt queue: %w", err)
		}
	}
	return queue, nil
}

func promptQueueKey(sessionID, clientMsgID string) string {
	return sessionID + "\x00" + clientMsgID
}

func validatePromptQueueString(name, value string, max int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(value) > max || !utf8.ValidString(value) {
		return fmt.Errorf("%s is too long or invalid UTF-8", name)
	}
	return nil
}

func validatePromptQueueItem(item promptQueueItem) error {
	if err := validatePromptQueueString("session_id", item.SessionID, maxPromptQueueSessionIDBytes); err != nil {
		return err
	}
	if err := validatePromptQueueString("client_msg_id", item.ClientMsgID, maxPromptQueueClientMsgIDSize); err != nil {
		return err
	}
	if err := validatePromptQueueString("agent_type", item.AgentType, maxPromptQueueAgentTypeBytes); err != nil {
		return err
	}
	if item.Order == 0 || item.CreatedAt <= 0 {
		return errors.New("order and created_at are required")
	}
	if item.Status == promptQueueCancelled {
		if item.Prompt != "" || len(item.Attachments) > 0 || len(item.SealedEnvelope) > 0 {
			return errors.New("cancelled prompt queue item must not retain prompt data")
		}
		return nil
	}
	if len(item.Prompt) > 0 && !utf8.ValidString(item.Prompt) {
		return errors.New("prompt is invalid UTF-8")
	}
	if len(item.Attachments) > 0 && !json.Valid(item.Attachments) {
		return errors.New("attachments are not valid JSON")
	}
	if len(item.SealedEnvelope) > 0 && (item.Prompt != "" || len(item.Attachments) != 0) {
		return errors.New("sealed envelope cannot be combined with plaintext prompt data")
	}
	if item.Prompt == "" && len(item.Attachments) == 0 && len(item.SealedEnvelope) == 0 {
		return errors.New("prompt data or sealed envelope is required")
	}
	switch item.Status {
	case promptQueueQueued, promptQueueRunning, promptQueuePaused:
		return nil
	default:
		return fmt.Errorf("invalid status %q", item.Status)
	}
}

func clonePromptQueueItem(item promptQueueItem) promptQueueItem {
	item.Attachments = bytes.Clone(item.Attachments)
	item.SealedEnvelope = bytes.Clone(item.SealedEnvelope)
	return item
}

func clonePromptQueueItems(items map[string]promptQueueItem) map[string]promptQueueItem {
	cloned := make(map[string]promptQueueItem, len(items))
	for key, item := range items {
		cloned[key] = clonePromptQueueItem(item)
	}
	return cloned
}

// enqueue returns the durable existing item for a duplicate client_msg_id.
// The second result reports whether a new item was added.
func (q *promptQueue) enqueue(item promptQueueItem) (promptQueueItem, bool, error) {
	if err := validatePromptQueueString("session_id", item.SessionID, maxPromptQueueSessionIDBytes); err != nil {
		return promptQueueItem{}, false, err
	}
	if err := validatePromptQueueString("client_msg_id", item.ClientMsgID, maxPromptQueueClientMsgIDSize); err != nil {
		return promptQueueItem{}, false, err
	}
	if err := validatePromptQueueString("agent_type", item.AgentType, maxPromptQueueAgentTypeBytes); err != nil {
		return promptQueueItem{}, false, err
	}
	if len(item.Prompt) > 0 && !utf8.ValidString(item.Prompt) {
		return promptQueueItem{}, false, errors.New("prompt is invalid UTF-8")
	}
	if len(item.Attachments) > 0 && !json.Valid(item.Attachments) {
		return promptQueueItem{}, false, errors.New("attachments are not valid JSON")
	}
	if len(item.SealedEnvelope) > 0 && (item.Prompt != "" || len(item.Attachments) != 0) {
		return promptQueueItem{}, false, errors.New("sealed envelope cannot be combined with plaintext prompt data")
	}
	if item.Prompt == "" && len(item.Attachments) == 0 && len(item.SealedEnvelope) == 0 {
		return promptQueueItem{}, false, errors.New("prompt data or sealed envelope is required")
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	key := promptQueueKey(item.SessionID, item.ClientMsgID)
	if current, exists := q.items[key]; exists {
		return clonePromptQueueItem(current), false, nil
	}
	if q.cancelledCount(item.SessionID) >= maxCancelledQueuePerSession {
		return promptQueueItem{}, false, fmt.Errorf(
			"cancelled prompt tombstone limit reached for session (limit %d)",
			maxCancelledQueuePerSession,
		)
	}
	if q.activeCount(item.SessionID) >= maxPromptQueuePerSession {
		return promptQueueItem{}, false, fmt.Errorf("prompt queue is full for session (limit %d)", maxPromptQueuePerSession)
	}
	item.Order = q.nextID + 1
	item.CreatedAt = time.Now().UnixNano()
	item.Status = promptQueueQueued
	if err := validatePromptQueueItem(item); err != nil {
		return promptQueueItem{}, false, err
	}
	next := clonePromptQueueItems(q.items)
	next[key] = clonePromptQueueItem(item)
	if err := q.persist(next); err != nil {
		return promptQueueItem{}, false, err
	}
	q.items = next
	q.nextID = item.Order
	return clonePromptQueueItem(item), true, nil
}

func (q *promptQueue) next(sessionID string) (promptQueueItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var next promptQueueItem
	found := false
	for _, item := range q.items {
		if item.SessionID != sessionID || item.Status != promptQueueQueued {
			continue
		}
		if !found || item.Order < next.Order {
			next, found = item, true
		}
	}
	return clonePromptQueueItem(next), found
}

// claimNext atomically selects and persists the oldest queued item as running.
// At most one running item may exist per session, even when several completion
// or resume signals race to dispatch the same FIFO.
func (q *promptQueue) claimNext(sessionID string) (promptQueueItem, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var candidate promptQueueItem
	found := false
	for _, item := range q.items {
		if item.SessionID != sessionID {
			continue
		}
		if item.Status == promptQueueRunning {
			return promptQueueItem{}, false, nil
		}
		if item.Status == promptQueueQueued && (!found || item.Order < candidate.Order) {
			candidate, found = item, true
		}
	}
	if !found {
		return promptQueueItem{}, false, nil
	}
	next := clonePromptQueueItems(q.items)
	candidate.Status = promptQueueRunning
	next[promptQueueKey(candidate.SessionID, candidate.ClientMsgID)] = candidate
	if err := q.persist(next); err != nil {
		return promptQueueItem{}, false, err
	}
	q.items = next
	return clonePromptQueueItem(candidate), true, nil
}

func (q *promptQueue) markRunning(sessionID, clientMsgID string) error {
	return q.transition(sessionID, clientMsgID, promptQueueQueued, promptQueueRunning)
}

// complete removes a running item only after its work has completed. The
// caller's durable prompt journal remains the cross-restart delivery record.
func (q *promptQueue) complete(sessionID, clientMsgID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	key := promptQueueKey(sessionID, clientMsgID)
	current, exists := q.items[key]
	if !exists {
		return errors.New("prompt queue item not found")
	}
	if current.Status != promptQueueRunning {
		return fmt.Errorf("cannot complete prompt queue item from %s", current.Status)
	}
	next := clonePromptQueueItems(q.items)
	delete(next, key)
	if err := q.persist(next); err != nil {
		return err
	}
	q.items = next
	return nil
}

func (q *promptQueue) pause(sessionID, clientMsgID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	key := promptQueueKey(sessionID, clientMsgID)
	current, exists := q.items[key]
	if !exists {
		return errors.New("prompt queue item not found")
	}
	if current.Status != promptQueueQueued && current.Status != promptQueueRunning {
		return fmt.Errorf("cannot pause prompt queue item from %s", current.Status)
	}
	next := clonePromptQueueItems(q.items)
	current.Status = promptQueuePaused
	next[key] = current
	if err := q.persist(next); err != nil {
		return err
	}
	q.items = next
	return nil
}

func (q *promptQueue) cancel(sessionID, clientMsgID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	key := promptQueueKey(sessionID, clientMsgID)
	current, exists := q.items[key]
	if !exists {
		return errors.New("prompt queue item not found")
	}
	if current.Status == promptQueueCancelled {
		return nil
	}
	if current.Status != promptQueueQueued && current.Status != promptQueuePaused {
		return fmt.Errorf("cannot cancel prompt queue item from %s", current.Status)
	}
	next := clonePromptQueueItems(q.items)
	current.Status = promptQueueCancelled
	current.Prompt = ""
	current.Attachments = nil
	current.SealedEnvelope = nil
	next[key] = current
	if err := q.persist(next); err != nil {
		return err
	}
	q.items = next
	return nil
}

func (q *promptQueue) resume(sessionID, clientMsgID string) error {
	return q.transition(sessionID, clientMsgID, promptQueuePaused, promptQueueQueued)
}

func (q *promptQueue) transition(sessionID, clientMsgID string, from, to promptQueueStatus) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	key := promptQueueKey(sessionID, clientMsgID)
	current, exists := q.items[key]
	if !exists {
		return errors.New("prompt queue item not found")
	}
	if current.Status == to {
		return nil
	}
	if current.Status != from {
		return fmt.Errorf("cannot transition prompt queue item from %s to %s", current.Status, to)
	}
	next := clonePromptQueueItems(q.items)
	current.Status = to
	next[key] = current
	if err := q.persist(next); err != nil {
		return err
	}
	q.items = next
	return nil
}

func (q *promptQueue) list(sessionID string) []promptQueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]promptQueueItem, 0)
	for _, item := range q.items {
		if sessionID == "" || item.SessionID == sessionID {
			items = append(items, clonePromptQueueItem(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Order < items[j].Order })
	return items
}

func (q *promptQueue) item(sessionID, clientMsgID string) (promptQueueItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	item, ok := q.items[promptQueueKey(sessionID, clientMsgID)]
	return clonePromptQueueItem(item), ok
}

func (q *promptQueue) hasActive(sessionID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, item := range q.items {
		if item.SessionID == sessionID && item.Status != promptQueueCancelled {
			return true
		}
	}
	return false
}

func (q *promptQueue) running(sessionID string) (promptQueueItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, item := range q.items {
		if item.SessionID == sessionID && item.Status == promptQueueRunning {
			return clonePromptQueueItem(item), true
		}
	}
	return promptQueueItem{}, false
}

// pauseSession fails closed after an interrupted or failed active turn: no
// following item may overtake it until an explicit resume command arrives.
func (q *promptQueue) pauseSession(sessionID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	next := clonePromptQueueItems(q.items)
	changed := false
	for key, item := range next {
		if item.SessionID == sessionID && (item.Status == promptQueueQueued || item.Status == promptQueueRunning) {
			item.Status = promptQueuePaused
			next[key] = item
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := q.persist(next); err != nil {
		return err
	}
	q.items = next
	return nil
}

// resumeSession returns all paused entries to their original FIFO ordering.
func (q *promptQueue) resumeSession(sessionID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	next := clonePromptQueueItems(q.items)
	changed := false
	for key, item := range next {
		if item.SessionID == sessionID && item.Status == promptQueuePaused {
			item.Status = promptQueueQueued
			next[key] = item
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := q.persist(next); err != nil {
		return err
	}
	q.items = next
	return nil
}

// position is one-based among runnable queued items. It returns zero when an
// item is not currently queued (including running, paused, and cancelled).
func (q *promptQueue) position(sessionID, clientMsgID string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	item, exists := q.items[promptQueueKey(sessionID, clientMsgID)]
	if !exists || item.Status != promptQueueQueued {
		return 0
	}
	position := 1
	for _, other := range q.items {
		if other.SessionID == sessionID && other.Status == promptQueueQueued && other.Order < item.Order {
			position++
		}
	}
	return position
}

func (q *promptQueue) activeCount(sessionID string) int {
	count := 0
	for _, item := range q.items {
		if item.SessionID == sessionID && item.Status != promptQueueCancelled {
			count++
		}
	}
	return count
}

func (q *promptQueue) cancelledCount(sessionID string) int {
	count := 0
	for _, item := range q.items {
		if item.SessionID == sessionID && item.Status == promptQueueCancelled {
			count++
		}
	}
	return count
}

func (q *promptQueue) persist(items map[string]promptQueueItem) error {
	ordered := make([]promptQueueItem, 0, len(items))
	for _, item := range items {
		ordered = append(ordered, clonePromptQueueItem(item))
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Order < ordered[j].Order })
	data, err := json.MarshalIndent(promptQueueDisk{Version: promptQueueVersion, Items: ordered}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal prompt queue: %w", err)
	}
	if len(data) > maxPromptQueueBytes {
		return fmt.Errorf("prompt queue would exceed %d bytes", maxPromptQueueBytes)
	}
	if err := writeFileAtomic(q.path, data, 0o600); err != nil {
		return fmt.Errorf("persist prompt queue: %w", err)
	}
	return nil
}
