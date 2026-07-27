package main

import "testing"

func TestListenAddressDefaultsToLoopbackWithoutSecret(t *testing.T) {
	if got := listenAddress("8080", ""); got != "127.0.0.1:8080" {
		t.Fatalf("listenAddress without secret = %q", got)
	}
	if got := listenAddress("8080", "secret"); got != ":8080" {
		t.Fatalf("listenAddress with secret = %q", got)
	}
}

func TestDefaultLocalOriginsRejectsDNSRebindingHosts(t *testing.T) {
	got := defaultLocalOrigins("8080")
	if got != "http://127.0.0.1:8080,http://localhost:8080,http://[::1]:8080" {
		t.Fatalf("defaultLocalOrigins=%q", got)
	}
}
