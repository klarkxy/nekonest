package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"
)

const registrationProofDomain = "nekonest-cloud/device-registration-proof/v1"
const registrationRetryDomain = "nekonest-cloud/device-registration-retry/v1"

// registrationProofTranscript binds one Cloud pairing credential to the
// daemon's long-term identity. Length prefixes avoid delimiter ambiguity and
// keep the transcript byte-for-byte reproducible in the Cloud worker.
func registrationProofTranscript(
	bootstrapToken string,
	osName string,
	ed25519Public string,
	x25519Public string,
	identityFingerprint string,
	transportMode string,
) []byte {
	fields := []string{
		strings.TrimSpace(bootstrapToken),
		strings.ToLower(strings.TrimSpace(osName)),
		strings.TrimSpace(ed25519Public),
		strings.TrimSpace(x25519Public),
		strings.ToLower(strings.TrimSpace(identityFingerprint)),
		strings.TrimSpace(transportMode),
	}
	var transcript bytes.Buffer
	transcript.WriteString(registrationProofDomain)
	for _, field := range fields {
		_ = binary.Write(&transcript, binary.BigEndian, uint32(len([]byte(field))))
		transcript.WriteString(field)
	}
	return transcript.Bytes()
}

// registrationRetryKey is reproducible after an ambiguous HTTP result without
// persisting another secret. It is scoped to the one-time bootstrap credential,
// stable service endpoint, and daemon identity, so changed registration
// material cannot recover a previously issued device token.
func registrationRetryKey(bootstrapToken, serviceURL, identityFingerprint string) string {
	fields := []string{
		strings.TrimSpace(bootstrapToken),
		strings.TrimSpace(serviceURL),
		strings.ToLower(strings.TrimSpace(identityFingerprint)),
	}
	hash := sha256.New()
	hash.Write([]byte(registrationRetryDomain))
	for _, field := range fields {
		_ = binary.Write(hash, binary.BigEndian, uint32(len([]byte(field))))
		hash.Write([]byte(field))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
