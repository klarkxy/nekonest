package relaycore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"time"

	db "github.com/klarkxy/nekonest/relaycore/store"
)

type Clock interface{ Now() time.Time }
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type Attachment struct {
	ID, Key, Name, MIME, DeviceID, SessionID string
	Size, Created                            int64
}
type AttachmentStore interface {
	Put(context.Context, Attachment, []byte) error
	Get(context.Context, string) (Attachment, io.ReadCloser, error)
	Delete(context.Context, string) error
}
type AttachmentURLBuilder interface {
	BuildAttachmentURL(id, capabilityKey string) string
}

type PushMessage struct{ DeviceID, SessionID, Title, Body, URL string }
type PushSink interface {
	Enabled() bool
	PublicKey() string
	Validate(endpoint, p256dh, auth string) error
	Send(context.Context, []db.PushSubscription, PushMessage, func(string)) bool
}

type AuditEvent struct {
	Component, Name, Message string
	Attributes               []any
	Err                      error
}
type AuditSink interface {
	Emit(context.Context, AuditEvent)
}
type nopAudit struct{}

func (nopAudit) Emit(context.Context, AuditEvent) {}

// PrincipalSynchronizer is an optional trusted-shell port for importing and
// revoking control-plane-approved bearer credentials without rotating them.
type CredentialKind string

const (
	CredentialRaw    CredentialKind = "raw"
	CredentialSHA256 CredentialKind = "sha256"
)

type Credential struct {
	Kind  CredentialKind
	Value string
}

func SHA256Credential(raw string) Credential {
	digest := sha256.Sum256([]byte(raw))
	return Credential{Kind: CredentialSHA256, Value: hex.EncodeToString(digest[:])}
}

type PrincipalSynchronizer interface {
	SyncDevice(id, name, osName string, credential Credential) error
	RevokeDevice(id string) error
	SyncPhone(id, name string, credential Credential) error
	RevokePhone(id string) error
}

type Ports struct {
	PrincipalAuthenticator db.PrincipalAuthenticator
	PrincipalSynchronizer  PrincipalSynchronizer
	DeviceStore            db.DeviceStore
	PhoneGrantStore        db.PhoneGrantStore
	PromptJournal          db.PromptJournal
	MessageStore           db.MessageStore
	KeyPackageStore        db.KeyPackageStore
	AttachmentStore        AttachmentStore
	AttachmentURLBuilder   AttachmentURLBuilder
	PushSink               PushSink
	AuditSink              AuditSink
	Clock                  Clock
}

type composedStore struct {
	db.PrincipalAuthenticator
	db.DeviceStore
	db.PhoneGrantStore
	db.PromptJournal
	db.MessageStore
	db.KeyPackageStore
}
