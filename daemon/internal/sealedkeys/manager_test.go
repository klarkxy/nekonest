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

	catalogAAD := sealed.AADFields{
		ProtocolVersion: "1.0",
		TransportMode:   "sealed",
		Type:            "start_thread",
		DeviceID:        "dev",
		ClientMsgID:     "start-1",
		Timestamp:       2,
	}
	catalogWire, err := m2.SealCatalog("phone", "dev", catalogAAD, []byte(`{"prompt":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	catalogAAD.KeyScope = "device_catalog"
	catalogAAD.KeyEpoch = catalogWire.Epoch
	catalogAAD.SenderID = catalogWire.SenderID
	catalogAAD.Sequence = catalogWire.Sequence
	catalogPlain, err := m2.OpenCatalog(catalogWire, catalogAAD)
	if err != nil || string(catalogPlain) != `{"prompt":"secret"}` {
		t.Fatalf("catalog open = %q, %v", catalogPlain, err)
	}
}

func TestOpenSessionDoesNotMintUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	m, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.OpenSession("never-sealed", &sealed.WireSealed{KeyScope: "session", Epoch: 1}, sealed.AADFields{
		ProtocolVersion: "1.0",
		TransportMode:   "sealed",
		Type:            "send_prompt",
		DeviceID:        "dev",
		SessionID:       "never-sealed",
		KeyScope:        "session",
		KeyEpoch:        1,
	})
	if err == nil {
		t.Fatal("unknown session minted a key")
	}
	if _, ok := m.sessions["never-sealed"]; ok {
		t.Fatal("unknown session persisted a key")
	}
}
