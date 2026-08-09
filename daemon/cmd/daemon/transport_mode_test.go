package main

import "testing"

func TestRegistrationTransportModeRequiresServerResponseAndMatchesRequest(t *testing.T) {
	if _, err := registrationTransportMode("", ""); err == nil {
		t.Fatal("missing server mode accepted")
	}
	if _, err := registrationTransportMode("sealed", "open"); err == nil {
		t.Fatal("mismatched requested mode accepted")
	}
	mode, err := registrationTransportMode("sealed", "sealed")
	if err != nil || mode != "sealed" {
		t.Fatalf("mode=%q err=%v", mode, err)
	}
}
