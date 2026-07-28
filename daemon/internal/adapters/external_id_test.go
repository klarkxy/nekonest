package adapters

import "testing"

func TestExternalSessionIDRoundTrip(t *testing.T) {
	publicID := publicSessionID(AgentGrokBuild, "abc-123")
	if publicID != "grok_build:abc-123" {
		t.Fatalf("public id = %q", publicID)
	}
	nativeID, err := nativeSessionID(AgentGrokBuild, publicID)
	if err != nil {
		t.Fatal(err)
	}
	if nativeID != "abc-123" {
		t.Fatalf("native id = %q", nativeID)
	}
}

func TestExternalSessionIDRejectsWrongAgent(t *testing.T) {
	if _, err := nativeSessionID(AgentKimiCLI, "grok_build:abc"); err == nil {
		t.Fatal("expected wrong-agent id to fail")
	}
}
