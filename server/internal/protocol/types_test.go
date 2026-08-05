package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewMessage(t *testing.T) {
	m := NewMessage(MsgSendPrompt, "dev1")
	if m.Type != MsgSendPrompt || m.DeviceID != "dev1" || m.SessionID != "" {
		t.Fatalf("%#v", m)
	}
	if m.ProtocolVersion != CurrentProtocolVersion {
		t.Fatalf("protocol_version=%q", m.ProtocolVersion)
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
		ProtocolVersion: CurrentProtocolVersion,
		TransportMode:   TransportOpen,
		Type:            MsgSessionList,
		DeviceID:        "d",
		ClientMsgID:     "c1",
		Timestamp:       123,
		Payload:         map[string]any{"n": float64(1)},
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
	if out.ProtocolVersion != CurrentProtocolVersion || out.TransportMode != TransportOpen {
		t.Fatalf("envelope meta: %#v", out)
	}
	if out.ClientMsgID != "c1" {
		t.Fatalf("client_msg_id=%q", out.ClientMsgID)
	}
}

func TestSealedPayloadJSONRoundTrip(t *testing.T) {
	m := &NekoMessage{
		ProtocolVersion: "1.0",
		TransportMode:   TransportSealed,
		Type:            MsgSendPrompt,
		DeviceID:        "d",
		Timestamp:       1,
		SealedPayload: &SealedPayload{
			Alg:         "aes-256-gcm",
			Version:     1,
			KeyScope:    KeyScopeSession,
			Epoch:       2,
			SenderID:    "phone",
			RecipientID: "dev",
			Sequence:    9,
			Nonce:       "nonce",
			Ciphertext:  "ct",
		},
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out NekoMessage
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Payload != nil || out.SealedPayload == nil || out.SealedPayload.Sequence != 9 {
		t.Fatalf("%#v", out)
	}
}

func TestValidateEnvelopeForm(t *testing.T) {
	if err := ValidateEnvelopeForm(&NekoMessage{Type: MsgHeartbeat}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEnvelopeForm(&NekoMessage{
		Payload: map[string]any{"a": 1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEnvelopeForm(&NekoMessage{
		SealedPayload: &SealedPayload{Alg: "aes-256-gcm"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEnvelopeForm(&NekoMessage{
		Payload:       map[string]any{"a": 1},
		SealedPayload: &SealedPayload{Alg: "aes-256-gcm"},
	}); err == nil {
		t.Fatal("expected mixed payload rejection")
	}
}

func TestParseAndNegotiateProtocolVersion(t *testing.T) {
	if _, err := ParseProtocolVersion(""); err == nil {
		t.Fatal("empty")
	}
	if _, err := ParseProtocolVersion("1"); err == nil {
		t.Fatal("missing minor")
	}
	if _, err := ParseProtocolVersion("01.0"); err == nil {
		t.Fatal("padded major")
	}
	got, err := ParseProtocolVersion("1.2")
	if err != nil || got.Major != 1 || got.Minor != 2 {
		t.Fatalf("%v %v", got, err)
	}

	neg, err := NegotiateProtocolVersion("1.0", 1, 0)
	if err != nil || neg != "1.0" {
		t.Fatalf("neg=%q err=%v", neg, err)
	}
	neg, err = NegotiateProtocolVersion("1.5", 1, 2)
	if err != nil || neg != "1.2" {
		t.Fatalf("min minor: neg=%q err=%v", neg, err)
	}
	if _, err := NegotiateProtocolVersion("0.9", 1, 0); err == nil {
		t.Fatal("major mismatch")
	}
	if _, err := NegotiateProtocolVersion("2.0", 1, 0); err == nil {
		t.Fatal("major mismatch high")
	}
}

func TestParseTransportMode(t *testing.T) {
	if m, err := ParseTransportMode("sealed"); err != nil || m != TransportSealed {
		t.Fatalf("%v %v", m, err)
	}
	if m, err := ParseTransportMode("open"); err != nil || m != TransportOpen {
		t.Fatalf("%v %v", m, err)
	}
	if _, err := ParseTransportMode(""); err == nil {
		t.Fatal("empty")
	}
	if _, err := ParseTransportMode("mixed"); err == nil {
		t.Fatal("invalid")
	}
}

func TestNegotiateHandshake(t *testing.T) {
	ok := NegotiateHandshake("1.0", "open", TransportOpen, 0)
	if ok.ErrorCode != "" || ok.NegotiatedVersion != "1.0" || ok.TransportMode != TransportOpen {
		t.Fatalf("%#v", ok)
	}
	modeFail := NegotiateHandshake("1.0", "open", TransportSealed, 0)
	if modeFail.ErrorCode != ErrCodeTransportModeMismatch {
		t.Fatalf("mode: %#v", modeFail)
	}
	verFail := NegotiateHandshake("0.9", "sealed", TransportSealed, 0)
	if verFail.ErrorCode != ErrCodeVersionMismatch {
		t.Fatalf("version: %#v", verFail)
	}
	missing := NegotiateHandshake("", "sealed", TransportSealed, 0)
	if missing.ErrorCode != ErrCodeVersionMismatch {
		t.Fatalf("missing version: %#v", missing)
	}
}

func TestSessionCapabilitiesDefaults(t *testing.T) {
	var nilCaps *SessionCapabilities
	if CapabilityBool(nilCaps, func(c *SessionCapabilities) bool { return c.Approve }) {
		t.Fatal("nil caps must default unsupported")
	}
	caps := &SessionCapabilities{}
	caps.Normalize()
	if caps.AttachmentMode != AttachmentUnsupported {
		t.Fatalf("attachment default=%q", caps.AttachmentMode)
	}
	if caps.ControlMode != ControlCompatibility {
		t.Fatalf("control default=%q", caps.ControlMode)
	}
	if caps.Steer || caps.Spawn || caps.Queue {
		t.Fatal("bool caps default false")
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

func TestAgentStartCapabilityAndThreadStartPayloadJSON(t *testing.T) {
	catalog := SessionListPayload{StartCapabilities: []AgentStartCapability{{
		AgentType:      AgentKimiCLI,
		Available:      true,
		Spawn:          true,
		ControlPath:    "acp",
		ControlVersion: "1",
	}}}
	b, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"start_capabilities"`) || !strings.Contains(string(b), `"spawn":true`) {
		t.Fatalf("catalog JSON=%s", b)
	}
	var out SessionListPayload
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.StartCapabilities) != 1 || out.StartCapabilities[0].AgentType != AgentKimiCLI || !out.StartCapabilities[0].Spawn {
		t.Fatalf("catalog=%#v", out)
	}

	start := StartThreadPayload{AgentType: AgentKimiCLI, OperationID: "op-1", ProjectDir: "D:/project", Prompt: "hello"}
	result := ThreadStartResult{AgentType: AgentKimiCLI, OperationID: "op-1", State: ThreadStartIndeterminate, SessionID: "native-1", ThreadID: "thread-1", Error: "ownership pending", Message: "try discovery later"}
	for _, value := range []any{start, result} {
		if _, err := json.Marshal(value); err != nil {
			t.Fatalf("marshal %T: %v", value, err)
		}
	}
}

func TestAgentStatusValues(t *testing.T) {
	want := []AgentStatus{AgentIdle, AgentRunning, AgentWaitingUser, AgentWaitingApproval, AgentError}
	for _, s := range want {
		if s == "" {
			t.Fatal("empty status")
		}
	}
}
