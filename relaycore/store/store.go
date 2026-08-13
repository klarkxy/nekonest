// Package store defines the persistence boundary consumed by a single relay
// engine. Implementations are scoped to one nest; placement and admission
// decisions deliberately live outside this package.
package store

import (
	"errors"
	"time"

	"github.com/klarkxy/nekonest/relaycore/protocol"
)

const (
	PromptRegistered    = "registered"
	PromptPending       = "pending"
	PromptAccepted      = "accepted"
	PromptFailed        = "failed"
	PromptIndeterminate = "indeterminate"
)

var (
	ErrPhoneNotFound         = errors.New("phone not found")
	ErrPhoneRevoked          = errors.New("phone revoked")
	ErrPhoneTokenInvalid     = errors.New("invalid phone token")
	ErrPromptCommandConflict = errors.New("client_msg_id already belongs to a different prompt")
)

type PhoneIdentity struct {
	ID, Name, Ed25519Public, X25519Public string
	CreatedAt, LastSeen, RevokedAt        int64
}

type PhoneAuth struct {
	PhoneID     string
	Name        string
	AdminBypass bool
}

type PhoneGrant struct {
	PhoneID, DeviceID, Ed25519Public, X25519Public string
	PairedAt                                       int64
}

type DevicePublicKeys struct {
	Ed25519Public string
	X25519Public  string
	Fingerprint   string
}

type PromptCommand struct {
	DeviceID, ClientMsgID, SessionID, Prompt string
	AttachmentsJSON, SealedEnvelopeJSON      string
	Status, Error, Outcome                   string
	RetryAllowed, CommitSent                 bool
	CreatedAt, UpdatedAt                     int64
}

type PushSubscription struct {
	ID       int64  `json:"id"`
	DeviceID string `json:"device_id"`
	PhoneID  string `json:"phone_id,omitempty"`
	Endpoint string `json:"endpoint"`
	P256DH   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

type PrincipalAuthenticator interface {
	ValidateDeviceToken(deviceID, token string) bool
	ValidatePhoneToken(token string) (*PhoneAuth, error)
}

type DeviceStore interface {
	RegisterDevice(id, name string, osName ...string) (string, error)
	GetDevice(id string) (*protocol.Device, error)
	ListDevices() ([]*protocol.Device, error)
	DeviceExists(id string) bool
	UpdateDeviceLastSeen(id string) error
	UpdateDeviceSessions(id string, count int) error
	SetDevicePublicKeys(deviceID, ed25519Pub, x25519Pub, fingerprint string) error
	GetDevicePublicKeys(deviceID string) (*DevicePublicKeys, error)

	CreatePairCode(code, deviceID string, expiresAt time.Time) error
	ConsumePairCode(code string) (string, error)
	AcceptAttentionEvent(deviceID, eventID string, createdAt time.Time) (bool, error)
}

type PhoneGrantStore interface {
	CreatePhoneIdentity(name string) (phoneID, token string, err error)
	ListPhones() ([]*PhoneIdentity, error)
	SetPhonePublicKeys(phoneID, ed25519Pub, x25519Pub string) error
	RevokePhone(phoneID string) error
	GrantPhoneDevice(phoneID, deviceID string) error
	PhoneHasDeviceGrant(phoneID, deviceID string) bool
	ListPhoneDeviceIDs(phoneID string) ([]string, error)
	ListPhoneGrantsForDevice(deviceID string) ([]*PhoneGrant, error)
	SavePushSubscription(sub *PushSubscription) error
	GetPushSubscriptions(deviceID string) ([]*PushSubscription, error)
	DeletePushSubscription(endpoint string) error
}

type KeyPackageStore interface {
	UpsertKeyPackage(phoneID, deviceID, scope, sessionID string, epoch uint64, wrappedKey, nonce string) error
	ListKeyPackages(phoneID, deviceID string) ([]map[string]any, error)
}

type MessageStore interface {
	SaveMessage(deviceID, sessionID string, msg *protocol.SessionMessage) error
	SaveSealedMessage(deviceID, sessionID string, msg *protocol.NekoMessage) error
	GetMessages(deviceID, sessionID string, limit int) ([]*protocol.SessionMessage, error)
}

type PromptJournal interface {
	RegisterPromptCommand(cmd *PromptCommand, retryFailed bool) (*PromptCommand, bool, error)
	GetPromptCommand(deviceID, clientMsgID string) (*PromptCommand, error)
	MarkPromptForwarded(deviceID, clientMsgID string) (*PromptCommand, bool, error)
	MarkPromptAccepted(deviceID, clientMsgID string) (*PromptCommand, bool, error)
	MarkPromptFailed(deviceID, clientMsgID, message, outcome string, retryAllowed bool) (*PromptCommand, bool, error)
	MarkPromptCommitted(deviceID, clientMsgID string) error
	ListUncommittedAcceptedPrompts(deviceID string, limit int) ([]*PromptCommand, error)
}

// Store is the standalone SQLite adapter's compatibility aggregate.
type Store interface {
	PrincipalAuthenticator
	DeviceStore
	PhoneGrantStore
	PromptJournal
	MessageStore
	KeyPackageStore
}
