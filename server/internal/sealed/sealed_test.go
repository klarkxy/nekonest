package sealed

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIdentityRoundTrip(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	pub := id.Public()
	if pub.Fingerprint == "" || pub.Ed25519Public == "" || pub.X25519Public == "" {
		t.Fatalf("%#v", pub)
	}
	msg := []byte("nekonest-pair-transcript-v1")
	sig := id.Sign(msg)
	edPub, err := ParseEd25519Public(pub.Ed25519Public)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifySignature(edPub, msg, sig) {
		t.Fatal("verify failed")
	}
	if VerifySignature(edPub, []byte("tampered"), sig) {
		t.Fatal("tamper should fail")
	}
}

func TestX25519AndSeal(t *testing.T) {
	a, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	ssAB, err := SharedSecret(a.X25519Private, b.X25519Public)
	if err != nil {
		t.Fatal(err)
	}
	ssBA, err := SharedSecret(b.X25519Private, a.X25519Public)
	if err != nil {
		t.Fatal(err)
	}
	if ssAB != ssBA {
		t.Fatal("shared secret mismatch")
	}
	wrap, err := DerivePairWrappingKey(ssAB, []byte("transcript"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	nB64, ctB64, err := WrapKey(wrap, content)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnwrapKey(wrap, nB64, ctB64)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("unwrap mismatch")
	}
	// Wrong AAD / tamper
	if _, err := UnwrapKey(wrap, nB64, B64(append([]byte{1}, mustDecode(t, ctB64)...))); err == nil {
		t.Fatal("expected tamper fail")
	}
}

func TestSealOpenWithAAD(t *testing.T) {
	key, err := RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := RandomNonce()
	if err != nil {
		t.Fatal(err)
	}
	aad, err := EncodeAAD(AADFields{
		ProtocolVersion: "1.0",
		TransportMode:   "sealed",
		Type:            "send_prompt",
		DeviceID:        "dev1",
		SessionID:       "s1",
		ClientMsgID:     "c1",
		KeyScope:        "session",
		KeyEpoch:        1,
		SenderID:        "phone1",
		Sequence:        7,
		Timestamp:       100,
	})
	if err != nil {
		t.Fatal(err)
	}
	ct, err := Seal(key, nonce, []byte(`{"prompt":"hi"}`), aad)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := Open(key, nonce, ct, aad)
	if err != nil || string(pt) != `{"prompt":"hi"}` {
		t.Fatalf("%q %v", pt, err)
	}
	// Modified type in AAD must fail.
	badAAD, _ := EncodeAAD(AADFields{
		ProtocolVersion: "1.0",
		TransportMode:   "sealed",
		Type:            "session_message", // tampered
		DeviceID:        "dev1",
		SessionID:       "s1",
		ClientMsgID:     "c1",
		KeyScope:        "session",
		KeyEpoch:        1,
		SenderID:        "phone1",
		Sequence:        7,
		Timestamp:       100,
	})
	if _, err := Open(key, nonce, ct, badAAD); err == nil {
		t.Fatal("aad tamper should fail")
	}
}

func TestAADStableJSON(t *testing.T) {
	a, _ := EncodeAAD(AADFields{ProtocolVersion: "1.0", Type: "t", DeviceID: "d", KeyScope: "session", SenderID: "s", Sequence: 1, Timestamp: 2})
	b, _ := EncodeAAD(AADFields{ProtocolVersion: "1.0", Type: "t", DeviceID: "d", KeyScope: "session", SenderID: "s", Sequence: 1, Timestamp: 2})
	if !bytes.Equal(a, b) {
		t.Fatalf("%s vs %s", a, b)
	}
	// Ensure it is valid JSON object
	var m map[string]any
	if err := json.Unmarshal(a, &m); err != nil {
		t.Fatal(err)
	}
}

func TestGoldenVectorsFile(t *testing.T) {
	// Generate a small vectors file if missing, and always verify local vectors.
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	nonce := make([]byte, 12)
	for i := range nonce {
		nonce[i] = byte(i + 10)
	}
	aad := []byte(`{"protocol_version":"1.0","transport_mode":"sealed","type":"send_prompt","device_id":"dev","key_scope":"session","key_epoch":1,"sender_id":"phone","sequence":1,"timestamp":1}`)
	pt := []byte(`{"prompt":"hello"}`)
	ct, err := Seal(key, nonce, pt, aad)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(key, nonce, ct, aad)
	if err != nil || !bytes.Equal(got, pt) {
		t.Fatal(err)
	}

	// Write vectors next to protocol for PWA to consume (best-effort).
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	outDir := filepath.Join(root, "protocol", "testdata")
	_ = os.MkdirAll(outDir, 0o755)
	vectors := map[string]any{
		"alg":         AlgAES256GCM,
		"version":     SealedFormatVersion,
		"key_b64":     B64(key),
		"nonce_b64":   B64(nonce),
		"aad":         string(aad),
		"plaintext":   string(pt),
		"ciphertext_b64": B64(ct),
	}
	raw, _ := json.MarshalIndent(vectors, "", "  ")
	path := filepath.Join(outDir, "e2e-v1-vectors.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Logf("write vectors: %v", err)
	}

	// Identity sign vector
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	msg := []byte("nekonest-v1-vector")
	sig := ed25519.Sign(priv, msg)
	if !ed25519.Verify(pub, msg, sig) {
		t.Fatal("ed25519 vector")
	}
}

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := B64Decode(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
