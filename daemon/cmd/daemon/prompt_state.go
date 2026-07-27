package main

import (
	"crypto/sha256"
	"errors"
	"sync"
	"unicode/utf8"
)

const maxAcceptedPromptIDs = 4096
const maxAcceptedPromptEchoBytes = 4096

// acceptedPrompt is the immutable acknowledgement data retained for a
// successfully-started prompt. Keeping it lets the daemon re-acknowledge a
// retransmission without executing the same command twice.
type acceptedPrompt struct {
	prompt string
}

// promptAcceptanceCache is a bounded, lifetime-scoped idempotency cache.
// Entries have no time expiry; once full, the oldest accepted ID is evicted.
type promptAcceptanceCache struct {
	mu      sync.Mutex
	max     int
	entries map[string]acceptedPrompt
	order   []string
	next    int
}

func newPromptAcceptanceCache(max int) *promptAcceptanceCache {
	if max <= 0 {
		max = maxAcceptedPromptIDs
	}
	return &promptAcceptanceCache{
		max:     max,
		entries: make(map[string]acceptedPrompt, max),
		order:   make([]string, 0, max),
	}
}

func promptCacheKey(sessionID, clientMsgID string) string {
	sum := sha256.Sum256([]byte(sessionID + "\x00" + clientMsgID))
	return string(sum[:])
}

func boundedPromptEcho(prompt string) string {
	if len(prompt) <= maxAcceptedPromptEchoBytes {
		return prompt
	}
	cut := maxAcceptedPromptEchoBytes - 3
	for cut > 0 && !utf8.ValidString(prompt[:cut]) {
		cut--
	}
	return prompt[:cut] + "..."
}

func (c *promptAcceptanceCache) get(sessionID, clientMsgID string) (acceptedPrompt, bool) {
	if clientMsgID == "" {
		return acceptedPrompt{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	record, ok := c.entries[promptCacheKey(sessionID, clientMsgID)]
	return record, ok
}

func (c *promptAcceptanceCache) add(sessionID, clientMsgID string, record acceptedPrompt) {
	if clientMsgID == "" {
		return
	}
	key := promptCacheKey(sessionID, clientMsgID)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		return
	}
	record.prompt = boundedPromptEcho(record.prompt)
	if len(c.order) < c.max {
		c.order = append(c.order, key)
	} else {
		oldest := c.order[c.next]
		delete(c.entries, oldest)
		c.order[c.next] = key
		c.next = (c.next + 1) % c.max
	}
	c.entries[key] = record
}

func promptStatus(cache *promptAcceptanceCache, sessionID, clientMsgID string) (acceptedPrompt, error) {
	if clientMsgID == "" {
		return acceptedPrompt{}, errors.New("client_msg_id required")
	}
	if accepted, ok := cache.get(sessionID, clientMsgID); ok {
		return accepted, nil
	}
	return acceptedPrompt{}, errors.New(promptOutcomeIndeterminate)
}

type sessionLock struct {
	mu   sync.Mutex
	refs int
}

// sessionLockMap serializes sends within one session without retaining an
// unbounded set of attacker-controlled session IDs.
type sessionLockMap struct {
	mu    sync.Mutex
	locks map[string]*sessionLock
}

func newSessionLockMap() *sessionLockMap {
	return &sessionLockMap{locks: make(map[string]*sessionLock)}
}

func (m *sessionLockMap) lock(sessionID string) func() {
	m.mu.Lock()
	entry := m.locks[sessionID]
	if entry == nil {
		entry = &sessionLock{}
		m.locks[sessionID] = entry
	}
	entry.refs++
	m.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		m.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(m.locks, sessionID)
		}
		m.mu.Unlock()
	}
}
