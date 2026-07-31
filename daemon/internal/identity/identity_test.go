package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	id1, st1, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if st1.Fingerprint == "" || st1.Ed25519Public == "" {
		t.Fatalf("%#v", st1)
	}
	id2, st2, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if st1.Fingerprint != st2.Fingerprint {
		t.Fatal("fingerprint changed on reload")
	}
	if string(id1.Ed25519Public) != string(id2.Ed25519Public) {
		t.Fatal("public key mismatch")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// On Windows mode bits are less strict; just ensure file exists and is non-empty.
	if info.Size() < 32 {
		t.Fatal("identity file too small")
	}
}

func TestBuildPairQR(t *testing.T) {
	st := &Stored{
		Ed25519Public: "ed",
		X25519Public:  "x",
		Fingerprint:   "fp",
	}
	q := BuildPairQR("https://nest.example", "dev1", "PC", "abc123", 99, st, "sealed")
	if q.Code != "abc123" || q.IdentityFingerprint != "fp" || q.V != "1" {
		t.Fatalf("%#v", q)
	}
}
