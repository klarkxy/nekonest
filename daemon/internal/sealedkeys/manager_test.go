package sealedkeys

import (
	"path/filepath"
	"testing"

	"github.com/nekonest/daemon/internal/sealed"
)

func TestManagerCatalogAndSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	m, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	wrap, err := sealed.RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	ep, n, ct, err := m.WrapCatalogForPhone(wrap)
	if err != nil || ep != 1 || n == "" || ct == "" {
		t.Fatalf("wrap catalog: %v %d", err, ep)
	}
	// Reload
	m2, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if m2.CatalogEpoch() != 1 {
		t.Fatal("epoch")
	}
	sk, ep2, err := m2.SessionKey("sess-1")
	if err != nil || ep2 != 1 || len(sk) != 32 {
		t.Fatalf("%v %d %d", err, ep2, len(sk))
	}
	aad := sealed.AADFields{
		ProtocolVersion: "1.0",
		TransportMode:   "sealed",
		Type:            "session_message",
		DeviceID:        "dev",
		SessionID:       "sess-1",
		KeyScope:        "session",
		KeyEpoch:        1,
		SenderID:        "dev",
		Sequence:        1,
		Timestamp:       1,
	}
	wire, err := m2.SealSession("sess-1", "dev", "phone", aad, []byte(`{"hi":1}`))
	if err != nil {
		t.Fatal(err)
	}
	// Rebuild AAD with actual sequence from wire
	aad.Sequence = wire.Sequence
	aad.KeyEpoch = wire.Epoch
	pt, err := m2.OpenSession("sess-1", wire, aad)
	if err != nil || string(pt) != `{"hi":1}` {
		t.Fatalf("%q %v", pt, err)
	}
}
