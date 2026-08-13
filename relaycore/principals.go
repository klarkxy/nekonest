package relaycore

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"sync"

	db "github.com/klarkxy/nekonest/relaycore/store"
)

var ErrCredentialIdentityConflict = errors.New("relaycore: credential already belongs to another identity")

type phoneCredential struct {
	Digest string
	Name   string
}

// DigestPrincipalRegistry is a small deployment-neutral implementation of
// PrincipalAuthenticator and PrincipalSynchronizer. It stores only SHA-256
// credential digests; presented bearer tokens are hashed before comparison.
// Deployment shells may persist the same model in their own store.
type DigestPrincipalRegistry struct {
	mu      sync.RWMutex
	devices map[string]string
	phones  map[string]phoneCredential
}

func NewDigestPrincipalRegistry() *DigestPrincipalRegistry {
	return &DigestPrincipalRegistry{
		devices: make(map[string]string),
		phones:  make(map[string]phoneCredential),
	}
}

func (r *DigestPrincipalRegistry) SyncDevice(id, _, _ string, credential Credential) error {
	if id == "" {
		return errors.New("relaycore: device id is required")
	}
	digest, err := credentialDigest(credential)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for existingID, existingDigest := range r.devices {
		if existingID != id && constantDigestEqual(existingDigest, digest) {
			return ErrCredentialIdentityConflict
		}
	}
	r.devices[id] = digest
	return nil
}

func (r *DigestPrincipalRegistry) RevokeDevice(id string) error {
	r.mu.Lock()
	delete(r.devices, id)
	r.mu.Unlock()
	return nil
}

func (r *DigestPrincipalRegistry) SyncPhone(id, name string, credential Credential) error {
	if id == "" {
		return errors.New("relaycore: phone id is required")
	}
	digest, err := credentialDigest(credential)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for existingID, existing := range r.phones {
		if existingID != id && constantDigestEqual(existing.Digest, digest) {
			return ErrCredentialIdentityConflict
		}
	}
	r.phones[id] = phoneCredential{Digest: digest, Name: name}
	return nil
}

func (r *DigestPrincipalRegistry) RevokePhone(id string) error {
	r.mu.Lock()
	delete(r.phones, id)
	r.mu.Unlock()
	return nil
}

func (r *DigestPrincipalRegistry) ValidateDeviceToken(id, raw string) bool {
	digest := rawCredentialDigest(raw)
	r.mu.RLock()
	want, ok := r.devices[id]
	r.mu.RUnlock()
	return ok && constantDigestEqual(want, digest)
}

func (r *DigestPrincipalRegistry) ValidatePhoneToken(raw string) (*db.PhoneAuth, error) {
	digest := rawCredentialDigest(raw)
	r.mu.RLock()
	defer r.mu.RUnlock()
	for id, phone := range r.phones {
		if constantDigestEqual(phone.Digest, digest) {
			return &db.PhoneAuth{PhoneID: id, Name: phone.Name}, nil
		}
	}
	return nil, db.ErrPhoneTokenInvalid
}

func credentialDigest(credential Credential) (string, error) {
	if err := validateCredential(credential); err != nil {
		return "", err
	}
	if credential.Kind == CredentialRaw {
		return rawCredentialDigest(credential.Value), nil
	}
	return credential.Value, nil
}

func rawCredentialDigest(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func constantDigestEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

var _ db.PrincipalAuthenticator = (*DigestPrincipalRegistry)(nil)
var _ PrincipalSynchronizer = (*DigestPrincipalRegistry)(nil)
