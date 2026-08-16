// Package sealedkeys manages device-catalog and per-session content keys,
// wrapping them for authorized phones.
package sealedkeys

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/nekonest/daemon/internal/sealed"
)

// Manager holds local content keys and sequence counters.
type Manager struct {
	mu           sync.Mutex
	path         string
	catalogKey   []byte
	catalogEpoch uint64
	sessions     map[string]*sessionKey // sessionID -> key
	seqCatalog   uint64
	seqSession   map[string]uint64
}

type sessionKey struct {
	Key   []byte
	Epoch uint64
}

type diskState struct {
	CatalogKeyB64 string            `json:"catalog_key_b64"`
	CatalogEpoch  uint64            `json:"catalog_epoch"`
	Sessions      map[string]diskSK `json:"sessions"`
	SeqCatalog    uint64            `json:"seq_catalog"`
	SeqSession    map[string]uint64 `json:"seq_session"`
}

type diskSK struct {
	KeyB64 string `json:"key_b64"`
	Epoch  uint64 `json:"epoch"`
}

// LoadOrCreate loads keys from path or creates a fresh catalog key.
func LoadOrCreate(path string) (*Manager, error) {
	m := &Manager{
		path:       path,
		sessions:   make(map[string]*sessionKey),
		seqSession: make(map[string]uint64),
	}
	if data, err := os.ReadFile(path); err == nil {
		var st diskState
		if err := json.Unmarshal(data, &st); err != nil {
			return nil, err
		}
		key, err := sealed.B64Decode(st.CatalogKeyB64)
		if err != nil || len(key) != sealed.KeySize {
			return nil, fmt.Errorf("catalog key")
		}
		m.catalogKey = key
		m.catalogEpoch = st.CatalogEpoch
		m.seqCatalog = st.SeqCatalog
		if st.SeqSession != nil {
			m.seqSession = st.SeqSession
		}
		for sid, sk := range st.Sessions {
			k, err := sealed.B64Decode(sk.KeyB64)
			if err != nil || len(k) != sealed.KeySize {
				continue
			}
			m.sessions[sid] = &sessionKey{Key: k, Epoch: sk.Epoch}
		}
		return m, nil
	}
	key, err := sealed.RandomKey()
	if err != nil {
		return nil, err
	}
	m.catalogKey = key
	m.catalogEpoch = 1
	if err := m.persist(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) persist() error {
	if m.path == "" {
		return nil
	}
	st := diskState{
		CatalogKeyB64: sealed.B64(m.catalogKey),
		CatalogEpoch:  m.catalogEpoch,
		Sessions:      make(map[string]diskSK),
		SeqCatalog:    m.seqCatalog,
		SeqSession:    m.seqSession,
	}
	for sid, sk := range m.sessions {
		st.Sessions[sid] = diskSK{KeyB64: sealed.B64(sk.Key), Epoch: sk.Epoch}
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(m.path, data, 0o600)
}

// CatalogEpoch returns the current device-catalog key epoch.
func (m *Manager) CatalogEpoch() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.catalogEpoch
}

// WrapCatalogForPhone wraps the catalog key under a phone-device wrapping key.
func (m *Manager) WrapCatalogForPhone(wrappingKey []byte) (epoch uint64, nonce, ciphertext string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ct, err := sealed.WrapKey(wrappingKey, m.catalogKey)
	if err != nil {
		return 0, "", "", err
	}
	return m.catalogEpoch, n, ct, nil
}

// SessionKey returns (creating if needed) the content key for a session.
func (m *Manager) SessionKey(sessionID string) (key []byte, epoch uint64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sk, ok := m.sessions[sessionID]; ok {
		return append([]byte(nil), sk.Key...), sk.Epoch, nil
	}
	k, err := sealed.RandomKey()
	if err != nil {
		return nil, 0, err
	}
	m.sessions[sessionID] = &sessionKey{Key: k, Epoch: 1}
	if err := m.persist(); err != nil {
		return nil, 0, err
	}
	return append([]byte(nil), k...), 1, nil
}

// WrapSessionForPhone wraps a session content key.
func (m *Manager) WrapSessionForPhone(sessionID string, wrappingKey []byte) (epoch uint64, nonce, ciphertext string, err error) {
	key, epoch, err := m.SessionKey(sessionID)
	if err != nil {
		return 0, "", "", err
	}
	n, ct, err := sealed.WrapKey(wrappingKey, key)
	if err != nil {
		return 0, "", "", err
	}
	return epoch, n, ct, nil
}

// NextCatalogSeq reserves the next sequence for device_catalog scope.
func (m *Manager) NextCatalogSeq() (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seqCatalog++
	seq := m.seqCatalog
	return seq, m.persist()
}

// NextSessionSeq reserves the next sequence for a session scope.
func (m *Manager) NextSessionSeq(sessionID string) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seqSession[sessionID]++
	seq := m.seqSession[sessionID]
	return seq, m.persist()
}

// SealCatalog encrypts a JSON application payload under the catalog key.
func (m *Manager) SealCatalog(senderID, recipientID string, aad sealed.AADFields, plaintext []byte) (*sealed.WireSealed, error) {
	m.mu.Lock()
	key := append([]byte(nil), m.catalogKey...)
	epoch := m.catalogEpoch
	m.seqCatalog++
	seq := m.seqCatalog
	_ = m.persist()
	m.mu.Unlock()

	aad.KeyScope = "device_catalog"
	aad.KeyEpoch = epoch
	aad.SenderID = senderID
	aad.Sequence = seq
	return sealed.SealWire(key, epoch, "device_catalog", senderID, recipientID, seq, aad, plaintext)
}

// SealSession encrypts under a session content key.
func (m *Manager) SealSession(sessionID, senderID, recipientID string, aad sealed.AADFields, plaintext []byte) (*sealed.WireSealed, error) {
	key, epoch, err := m.SessionKey(sessionID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.seqSession[sessionID]++
	seq := m.seqSession[sessionID]
	_ = m.persist()
	m.mu.Unlock()

	aad.KeyScope = "session"
	aad.KeyEpoch = epoch
	aad.SenderID = senderID
	aad.Sequence = seq
	aad.SessionID = sessionID
	return sealed.SealWire(key, epoch, "session", senderID, recipientID, seq, aad, plaintext)
}

// OpenSession decrypts a session-scoped sealed payload. Unknown session IDs
// do not mint a new key; only SealSession / WrapSessionForPhone may create one.
func (m *Manager) OpenSession(sessionID string, wire *sealed.WireSealed, aad sealed.AADFields) ([]byte, error) {
	if wire != nil && wire.KeyScope != "session" {
		return nil, fmt.Errorf("session key scope mismatch")
	}
	m.mu.Lock()
	sk, ok := m.sessions[sessionID]
	var key []byte
	var epoch uint64
	if ok {
		key = append([]byte(nil), sk.Key...)
		epoch = sk.Epoch
	}
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown session key")
	}
	if wire != nil && wire.Epoch != epoch {
		return nil, fmt.Errorf("session key epoch mismatch")
	}
	return sealed.OpenWire(key, wire, aad)
}

// OpenCatalog decrypts a device-scoped command that does not yet have a
// native session key, such as start_thread.
func (m *Manager) OpenCatalog(wire *sealed.WireSealed, aad sealed.AADFields) ([]byte, error) {
	m.mu.Lock()
	key := append([]byte(nil), m.catalogKey...)
	epoch := m.catalogEpoch
	m.mu.Unlock()
	if wire == nil || wire.KeyScope != "device_catalog" || wire.Epoch != epoch {
		return nil, fmt.Errorf("catalog key scope or epoch mismatch")
	}
	return sealed.OpenWire(key, wire, aad)
}

// DefaultPath returns ~/.nekonest/sealed-keys.json
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nekonest", "sealed-keys.json")
}
