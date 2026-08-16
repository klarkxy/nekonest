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

func TestACPStartNativeIDsAreNamespacedAtAdapterBoundary(t *testing.T) {
	for _, agent := range []AgentType{AgentKimiCLI, AgentGrokBuild, AgentZCode, AgentCursor} {
		publicID := publicSessionID(agent, "native-acp-id")
		if publicID != string(agent)+":native-acp-id" {
			t.Fatalf("%s public start id = %q", agent, publicID)
		}
		if nativeID, err := nativeSessionID(agent, publicID); err != nil || nativeID != "native-acp-id" {
			t.Fatalf("%s native id = %q, %v", agent, nativeID, err)
		}
	}
}

func TestExternalSessionIDRejectsWrongAgent(t *testing.T) {
	if _, err := nativeSessionID(AgentKimiCLI, "grok_build:abc"); err == nil {
		t.Fatal("expected wrong-agent id to fail")
	}
}

func TestPublicSessionIDEmptyNativeID(t *testing.T) {
	if got := publicSessionID(AgentZCode, ""); got != "" {
		t.Fatalf("empty zcode id = %q", got)
	}
	if got := publicSessionID(AgentCursor, "   "); got != "" {
		t.Fatalf("blank cursor id = %q", got)
	}
}
