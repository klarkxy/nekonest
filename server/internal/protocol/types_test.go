package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewMessage(t *testing.T) {
	m := NewMessage(MsgSendPrompt, "dev1")
	if m.Type != MsgSendPrompt || m.DeviceID != "dev1" || m.SessionID != "" {
		t.Fatalf("%#v", m)
	}
	if time.Since(time.Unix(m.Timestamp, 0)) > time.Minute {
		t.Fatal("timestamp")
	}
	ms := NewMessageWithSession(MsgSessionMessage, "dev1", "s1")
	if ms.SessionID != "s1" {
		t.Fatal("session")
	}
}

func TestNekoMessageJSONRoundTrip(t *testing.T) {
	m := &NekoMessage{
		Type:      MsgSessionList,
		DeviceID:  "d",
		Timestamp: 123,
		Payload:   map[string]any{"n": float64(1)},
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out NekoMessage
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Type != MsgSessionList || out.Payload["n"].(float64) != 1 {
		t.Fatalf("%#v", out)
	}
}

func TestSupportedAgentTypes(t *testing.T) {
	got := []AgentType{
		AgentClaudeCode,
		AgentCodex,
		AgentKilo,
		AgentKimiCLI,
		AgentGrokBuild,
	}
	want := []AgentType{
		"claude_code",
		"codex",
		"kilo",
		"kimi_cli",
		"grok_build",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("agent[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
