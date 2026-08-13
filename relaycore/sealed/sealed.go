// package sealed implements NekoNest v1 E2E crypto primitives:
// Ed25519 identity, X25519 key agreement, HKDF-SHA-256, AES-256-GCM.
//
// This package is intentionally shared-style (stdlib only) so daemon can
// vendor or copy the same algorithms. Cross-language vectors live in
// protocol/testdata/e2e-v1-vectors.json.
package sealed

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

const (
	// AlgAES256GCM is the AEAD algorithm identifier on the wire.
	AlgAES256GCM = "aes-256-gcm"
	// SealedFormatVersion is the sealed_payload.version field.
	SealedFormatVersion = 1
	// NonceSize is AES-GCM standard nonce length.
	NonceSize = 12
	// KeySize is AES-256 key length.
	KeySize = 32
)

// Identity holds long-term Ed25519 + X25519 key material for a phone or daemon.
type Identity struct {
	Ed25519Public  ed25519.PublicKey
	Ed25519Private ed25519.PrivateKey
	X25519Public   [32]byte
	X25519Private  [32]byte
}

// GenerateIdentity creates a fresh identity.
func GenerateIdentity() (*Identity, error) {
	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	var xPriv [32]byte
	if _, err := io.ReadFull(rand.Reader, xPriv[:]); err != nil {
		return nil, err
	}
	// Clamp per X25519.
	xPriv[0] &= 248
	xPriv[31] &= 127
	xPriv[31] |= 64
	var xPub [32]byte
	curve25519.ScalarBaseMult(&xPub, &xPriv)
	return &Identity{
		Ed25519Public:  edPub,
		Ed25519Private: edPriv,
		X25519Public:   xPub,
		X25519Private:  xPriv,
	}, nil
}

// PublicWire is the JSON-serializable public half of an identity.
type PublicWire struct {
	Ed25519Public string `json:"ed25519_public"`
	X25519Public  string `json:"x25519_public"`
	Fingerprint   string `json:"fingerprint"`
}

// Public exports public keys as base64url.
func (id *Identity) Public() PublicWire {
	return PublicWire{
		Ed25519Public: B64(id.Ed25519Public),
		X25519Public:  B64(id.X25519Public[:]),
		Fingerprint:   Fingerprint(id.Ed25519Public, id.X25519Public[:]),
	}
}

// Fingerprint is SHA-256(ed25519||x25519) hex for QR display.
func Fingerprint(edPub, xPub []byte) string {
	h := sha256.New()
	h.Write(edPub)
	h.Write(xPub)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Sign signs a transcript with the Ed25519 identity key.
func (id *Identity) Sign(transcript []byte) []byte {
	return ed25519.Sign(id.Ed25519Private, transcript)
}

// VerifySignature checks an Ed25519 signature over transcript.
func VerifySignature(edPub ed25519.PublicKey, transcript, sig []byte) bool {
	return ed25519.Verify(edPub, transcript, sig)
}

// SharedSecret computes X25519(private, peerPublic).
func SharedSecret(priv, peerPub [32]byte) ([32]byte, error) {
	var out [32]byte
	curve25519.ScalarMult(&out, &priv, &peerPub)
	// Reject low-order points (all-zero).
	var zero [32]byte
	if out == zero {
		return out, errors.New("invalid X25519 shared secret")
	}
	return out, nil
}

// DeriveKey HKDF-SHA-256 expands secret+info into a 32-byte key.
func DeriveKey(secret []byte, salt []byte, info string) ([]byte, error) {
	r := hkdf.New(sha256.New, secret, salt, []byte(info))
	out := make([]byte, KeySize)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Seal encrypts plaintext with AES-256-GCM. aad is authenticated but not encrypted.
// Returns nonce||ciphertext+tag (nonce prepended for convenience in tests);
// wire format keeps nonce separate.
func Seal(key, nonce, plaintext, aad []byte) (ciphertext []byte, err error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("key must be %d bytes", KeySize)
	}
	if len(nonce) != NonceSize {
		return nil, fmt.Errorf("nonce must be %d bytes", NonceSize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Seal(nil, nonce, plaintext, aad), nil
}

// Open decrypts AES-256-GCM ciphertext.
func Open(key, nonce, ciphertext, aad []byte) (plaintext []byte, err error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("key must be %d bytes", KeySize)
	}
	if len(nonce) != NonceSize {
		return nil, fmt.Errorf("nonce must be %d bytes", NonceSize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, aad)
}

// RandomNonce returns a 12-byte random nonce.
func RandomNonce() ([]byte, error) {
	n := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, n); err != nil {
		return nil, err
	}
	return n, nil
}

// RandomKey returns a 32-byte random key.
func RandomKey() ([]byte, error) {
	k := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return nil, err
	}
	return k, nil
}

// B64 encodes raw bytes as unpadded base64url.
func B64(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// B64Decode decodes unpadded base64url.
func B64Decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// AADFields are the outer routing fields authenticated with ciphertext.
// Serialized as a stable JSON object (sorted keys via struct field order + json).
type AADFields struct {
	ProtocolVersion string `json:"protocol_version"`
	TransportMode   string `json:"transport_mode"`
	Type            string `json:"type"`
	DeviceID        string `json:"device_id"`
	SessionID       string `json:"session_id,omitempty"`
	ClientMsgID     string `json:"client_msg_id,omitempty"`
	KeyScope        string `json:"key_scope"`
	KeyEpoch        uint64 `json:"key_epoch"`
	SenderID        string `json:"sender_id"`
	Sequence        uint64 `json:"sequence"`
	Timestamp       int64  `json:"timestamp"`
}

// EncodeAAD produces deterministic JSON bytes for AAD.
func EncodeAAD(f AADFields) ([]byte, error) {
	// encoding/json struct order is stable for identical field sets.
	return json.Marshal(f)
}

// EncodeSequenceBE encodes sequence as 8-byte big-endian (for tests/info).
func EncodeSequenceBE(seq uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, seq)
	return b
}

// ParseX25519Public decodes a base64url X25519 public key.
func ParseX25519Public(s string) ([32]byte, error) {
	var out [32]byte
	raw, err := B64Decode(s)
	if err != nil {
		return out, err
	}
	if len(raw) != 32 {
		return out, fmt.Errorf("x25519 public key length %d", len(raw))
	}
	copy(out[:], raw)
	return out, nil
}

// ParseEd25519Public decodes a base64url Ed25519 public key.
func ParseEd25519Public(s string) (ed25519.PublicKey, error) {
	raw, err := B64Decode(s)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("ed25519 public key length %d", len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// DerivePairWrappingKey derives the phone-device wrapping key from X25519 shared
// secret and a pairing transcript.
func DerivePairWrappingKey(shared [32]byte, transcript []byte) ([]byte, error) {
	// salt = SHA-256(transcript); info = "nekonest-v1-pair-wrap"
	sum := sha256.Sum256(transcript)
	return DeriveKey(shared[:], sum[:], "nekonest-v1-pair-wrap")
}

// WrapKey encrypts a content key under a wrapping key (AES-GCM).
func WrapKey(wrappingKey, contentKey []byte) (nonce, ciphertext string, err error) {
	n, err := RandomNonce()
	if err != nil {
		return "", "", err
	}
	ct, err := Seal(wrappingKey, n, contentKey, []byte("nekonest-v1-key-package"))
	if err != nil {
		return "", "", err
	}
	return B64(n), B64(ct), nil
}

// UnwrapKey decrypts a content key.
func UnwrapKey(wrappingKey []byte, nonceB64, ctB64 string) ([]byte, error) {
	n, err := B64Decode(nonceB64)
	if err != nil {
		return nil, err
	}
	ct, err := B64Decode(ctB64)
	if err != nil {
		return nil, err
	}
	return Open(wrappingKey, n, ct, []byte("nekonest-v1-key-package"))
}

// WireSealed is the JSON shape of sealed_payload on the wire.
type WireSealed struct {
	Alg         string `json:"alg"`
	Version     int    `json:"version,omitempty"`
	KeyScope    string `json:"key_scope"`
	Epoch       uint64 `json:"epoch"`
	SenderID    string `json:"sender_id"`
	RecipientID string `json:"recipient_id"`
	Sequence    uint64 `json:"sequence"`
	Nonce       string `json:"nonce"`
	Ciphertext  string `json:"ciphertext"`
}

// SealWire encrypts plaintext and returns a wire sealed_payload.
func SealWire(key []byte, epoch uint64, keyScope, senderID, recipientID string, seq uint64, aad AADFields, plaintext []byte) (*WireSealed, error) {
	nonce, err := RandomNonce()
	if err != nil {
		return nil, err
	}
	aadBytes, err := EncodeAAD(aad)
	if err != nil {
		return nil, err
	}
	ct, err := Seal(key, nonce, plaintext, aadBytes)
	if err != nil {
		return nil, err
	}
	return &WireSealed{
		Alg:         AlgAES256GCM,
		Version:     SealedFormatVersion,
		KeyScope:    keyScope,
		Epoch:       epoch,
		SenderID:    senderID,
		RecipientID: recipientID,
		Sequence:    seq,
		Nonce:       B64(nonce),
		Ciphertext:  B64(ct),
	}, nil
}

// OpenWire decrypts a wire sealed_payload.
func OpenWire(key []byte, wire *WireSealed, aad AADFields) ([]byte, error) {
	if wire == nil {
		return nil, errors.New("nil sealed payload")
	}
	nonce, err := B64Decode(wire.Nonce)
	if err != nil {
		return nil, err
	}
	ct, err := B64Decode(wire.Ciphertext)
	if err != nil {
		return nil, err
	}
	aadBytes, err := EncodeAAD(aad)
	if err != nil {
		return nil, err
	}
	return Open(key, nonce, ct, aadBytes)
}
