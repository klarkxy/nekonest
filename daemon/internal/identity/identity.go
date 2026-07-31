// Package identity loads and persists the daemon long-term E2E identity.
package identity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nekonest/daemon/internal/sealed"
)

// FileName is stored next to config under ~/.nekonest/
const FileName = "identity.json"

// Stored is the on-disk identity document (private keys included; mode 0600).
type Stored struct {
	Ed25519Private string `json:"ed25519_private"`
	Ed25519Public  string `json:"ed25519_public"`
	X25519Private  string `json:"x25519_private"`
	X25519Public   string `json:"x25519_public"`
	Fingerprint    string `json:"fingerprint"`
}

// Path returns the default identity file path beside the config directory.
func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nekonest", FileName)
}

// PathBesideConfig places identity next to a custom config path.
func PathBesideConfig(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), FileName)
}

// LoadOrCreate loads an existing identity or creates and saves a new one.
func LoadOrCreate(path string) (*sealed.Identity, *Stored, error) {
	if path == "" {
		path = Path()
	}
	if data, err := os.ReadFile(path); err == nil {
		var st Stored
		if err := json.Unmarshal(data, &st); err != nil {
			return nil, nil, fmt.Errorf("parse identity: %w", err)
		}
		id, err := fromStored(&st)
		if err != nil {
			return nil, nil, err
		}
		return id, &st, nil
	}
	id, err := sealed.GenerateIdentity()
	if err != nil {
		return nil, nil, err
	}
	st := toStored(id)
	if err := Save(path, st); err != nil {
		return nil, nil, err
	}
	return id, st, nil
}

// Save writes identity with restrictive permissions.
func Save(path string, st *Stored) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func toStored(id *sealed.Identity) *Stored {
	pub := id.Public()
	return &Stored{
		Ed25519Private: sealed.B64(id.Ed25519Private),
		Ed25519Public:  pub.Ed25519Public,
		X25519Private:  sealed.B64(id.X25519Private[:]),
		X25519Public:   pub.X25519Public,
		Fingerprint:    pub.Fingerprint,
	}
}

func fromStored(st *Stored) (*sealed.Identity, error) {
	edPrivRaw, err := sealed.B64Decode(st.Ed25519Private)
	if err != nil {
		return nil, err
	}
	edPub, err := sealed.ParseEd25519Public(st.Ed25519Public)
	if err != nil {
		return nil, err
	}
	xPrivRaw, err := sealed.B64Decode(st.X25519Private)
	if err != nil {
		return nil, err
	}
	xPub, err := sealed.ParseX25519Public(st.X25519Public)
	if err != nil {
		return nil, err
	}
	var xPriv [32]byte
	if len(xPrivRaw) != 32 {
		return nil, fmt.Errorf("x25519 private length")
	}
	copy(xPriv[:], xPrivRaw)
	return &sealed.Identity{
		Ed25519Public:  edPub,
		Ed25519Private: edPrivRaw,
		X25519Public:   xPub,
		X25519Private:  xPriv,
	}, nil
}

// PairQRPayload is printed/encoded for phone QR scan (no private keys).
type PairQRPayload struct {
	V                   string `json:"v"` // protocol major
	RelayURL            string `json:"relay_url"`
	DeviceID            string `json:"device_id"`
	Name                string `json:"name,omitempty"`
	Code                string `json:"code"`
	ExpiresAt           int64  `json:"expires_at"`
	Ed25519Public       string `json:"ed25519_public"`
	X25519Public        string `json:"x25519_public"`
	IdentityFingerprint string `json:"identity_fingerprint"`
	TransportMode       string `json:"transport_mode,omitempty"`
}

// BuildPairQR builds the phone-facing pair payload.
func BuildPairQR(relayHTTP string, deviceID, name, code string, exp int64, st *Stored, transportMode string) PairQRPayload {
	return PairQRPayload{
		V:                   "1",
		RelayURL:            relayHTTP,
		DeviceID:            deviceID,
		Name:                name,
		Code:                code,
		ExpiresAt:           exp,
		Ed25519Public:       st.Ed25519Public,
		X25519Public:        st.X25519Public,
		IdentityFingerprint: st.Fingerprint,
		TransportMode:       transportMode,
	}
}
