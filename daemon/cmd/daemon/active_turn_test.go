package main

import (
	"testing"

	"github.com/nekonest/daemon/internal/adapters"
)

func TestActiveTurnRegistryRejectsStaleGeneration(t *testing.T) {
	registry := newActiveTurnRegistry()
	if !registry.bind("session", 1, "first") {
		t.Fatal("bind first turn")
	}
	if registry.matches("session", 2, "second") {
		t.Fatal("stale registry matched another generation")
	}
	if registry.clearMatching("session", 2, "second", "") {
		t.Fatal("stale clear removed current turn")
	}
	if !registry.clearMatching("session", 1, "first", "") {
		t.Fatal("clear first turn")
	}
	if !registry.bind("session", 2, "second") {
		t.Fatal("bind second turn")
	}
	if registry.matches("session", 1, "first") {
		t.Fatal("delayed first-turn interrupt matched second turn")
	}
}

func TestActiveTurnRegistryRequiresNativeRequestMatch(t *testing.T) {
	registry := newActiveTurnRegistry()
	registry.bind("session", 7, "message")
	if !registry.setNativeRequestID("session", 7, "message", "turn-7") {
		t.Fatal("set native request id")
	}
	if _, _, matched := registry.terminalForEvent(adapters.ControlEvent{SessionID: "session", NativeRequestID: "turn-old"}); matched {
		t.Fatal("stale native event matched")
	}
	registry.accept("session", 7, "message")
	if _, ready, matched := registry.terminalForEvent(adapters.ControlEvent{SessionID: "session", NativeRequestID: "turn-7"}); !matched || !ready {
		t.Fatal("current native event did not match")
	}
}

func TestActiveTurnAppearsInReconnectCatalogSnapshot(t *testing.T) {
	registry := newActiveTurnRegistry()
	if !registry.bind("session", 11, "message-11") {
		t.Fatal("bind active turn")
	}
	if !registry.setNativeRequestID("session", 11, "message-11", "turn-11") {
		t.Fatal("bind native request")
	}
	sessions := []*adapters.SessionInfo{{
		ID: "session", AgentType: adapters.AgentCodex, Status: adapters.StatusRunning,
	}}
	overlayActiveTurns(sessions, registry)
	wire := sessionsToWire("device", sessions)
	binding, ok := wire[0]["active_turn"].(*adapters.ActiveTurnBinding)
	if !ok {
		t.Fatalf("active_turn type = %T", wire[0]["active_turn"])
	}
	if binding.Generation != 11 || binding.ClientMsgID != "message-11" || binding.NativeRequestID != "turn-11" {
		t.Fatalf("active_turn = %#v", binding)
	}
}

func TestActiveTurnRegistryDefersTerminalUntilDurableAcceptance(t *testing.T) {
	registry := newActiveTurnRegistry()
	registry.bind("session", 3, "message")
	event := adapters.ControlEvent{SessionID: "session", Generation: 3, ClientMsgID: "message", Lifecycle: adapters.TurnTerminalSuccess}
	if _, ready, matched := registry.terminalForEvent(event); !matched || ready {
		t.Fatalf("pre-acceptance terminal matched=%v ready=%v", matched, ready)
	}
	pending, _ := registry.accept("session", 3, "message")
	if pending == nil || pending.Lifecycle != adapters.TurnTerminalSuccess {
		t.Fatalf("pending terminal = %#v", pending)
	}
}

func TestActiveTurnRegistryCompletionCannotErasePreAcceptanceTerminal(t *testing.T) {
	registry := newActiveTurnRegistry()
	registry.bind("session", 5, "message")
	event := adapters.ControlEvent{SessionID: "session", Generation: 5, ClientMsgID: "message", Lifecycle: adapters.TurnTerminalSuccess}
	registry.terminalForEvent(event)
	if registry.completeMatching("session", 5, "message") {
		t.Fatal("pre-acceptance completion cleared the binding")
	}
	pending, completed := registry.accept("session", 5, "message")
	if completed || pending == nil {
		t.Fatalf("accept pending=%#v completed=%v", pending, completed)
	}
}

func TestParseActiveTurnCommand(t *testing.T) {
	generation, clientMsgID, err := parseActiveTurnCommand(map[string]interface{}{
		"generation": float64(9), "client_msg_id": "message-9",
	})
	if err != nil || generation != 9 || clientMsgID != "message-9" {
		t.Fatalf("parse = (%d, %q, %v)", generation, clientMsgID, err)
	}
	for _, payload := range []map[string]interface{}{
		{},
		{"generation": float64(1.5), "client_msg_id": "message"},
		{"generation": float64(1)},
	} {
		if _, _, err := parseActiveTurnCommand(payload); err == nil {
			t.Fatalf("accepted invalid payload %#v", payload)
		}
	}
}
