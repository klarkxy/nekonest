package main

import (
	"encoding/hex"
	"regexp"
	"testing"

	"github.com/nekonest/daemon/internal/sealed"
)

func TestRegistrationProofTranscriptIsCanonicalAndSigned(t *testing.T) {
	transcript := registrationProofTranscript(
		" pair_0123456789abcdef0123456789abcdef.ABCDEF0123456789ABCD ",
		" Windows ",
		"ed-public",
		"x-public",
		"ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789",
		" sealed ",
	)
	const expectedHex = "6e656b6f6e6573742d636c6f75642f6465766963652d726567697374726174696f6e2d70726f6f662f76310000003a706169725f30313233343536373839616263646566303132333435363738396162636465662e41424344454630313233343536373839414243440000000777696e646f77730000000965642d7075626c696300000008782d7075626c69630000004061626364656630313233343536373839616263646566303132333435363738396162636465663031323334353637383961626364656630313233343536373839000000067365616c6564"
	if got := hex.EncodeToString(transcript); got != expectedHex {
		t.Fatalf("transcript = %s", got)
	}

	identity, err := sealed.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	signature := identity.Sign(transcript)
	if !sealed.VerifySignature(identity.Ed25519Public, transcript, signature) {
		t.Fatal("registration proof signature did not verify")
	}
	tampered := append([]byte(nil), transcript...)
	tampered[len(tampered)-1] ^= 1
	if sealed.VerifySignature(identity.Ed25519Public, tampered, signature) {
		t.Fatal("registration proof accepted a modified transcript")
	}
}

func TestRegistrationRetryKeyIsStableAndScoped(t *testing.T) {
	first := registrationRetryKey("pair_abc.secret", "wss://connect.example/ws", "AA:BB")
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(first) {
		t.Fatalf("retry key = %q", first)
	}
	if again := registrationRetryKey("pair_abc.secret", "wss://connect.example/ws", "aa:bb"); again != first {
		t.Fatalf("retry key is not stable: %q != %q", again, first)
	}
	if other := registrationRetryKey("pair_abc.secret", "wss://other.example/ws", "aa:bb"); other == first {
		t.Fatal("retry key must be scoped to the service endpoint")
	}
}
