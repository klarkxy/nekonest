package adapters

import "testing"

func TestTurnTrackerOrdersEarlyTerminalAfterAcceptance(t *testing.T) {
	tracker := newTurnTracker(AgentClaudeCode)
	var got []TurnLifecycle
	tracker.setSink(func(event ControlEvent) { got = append(got, event.Lifecycle) })
	if !tracker.begin("session", PromptRequest{Generation: 7, ClientMsgID: "message"}) {
		t.Fatal("initial turn was not reserved")
	}
	if tracker.begin("session", PromptRequest{Generation: 8}) {
		t.Fatal("concurrent turn replaced active generation")
	}
	tracker.finish("session", 0, false)
	if len(got) != 0 {
		t.Fatalf("terminal emitted before durable acceptance: %#v", got)
	}
	tracker.accepted("session", 6) // stale generation must be ignored
	if len(got) != 0 {
		t.Fatalf("stale generation emitted lifecycle: %#v", got)
	}
	tracker.accepted("session", 7)
	if len(got) != 2 || got[0] != TurnAccepted || got[1] != TurnTerminalSuccess {
		t.Fatalf("lifecycle order = %#v", got)
	}
}

func TestTurnTrackerDetachAllSuppressesShutdownInterrupt(t *testing.T) {
	tracker := newTurnTracker(AgentKimiCLI)
	emitted := 0
	tracker.setSink(func(ControlEvent) { emitted++ })
	if !tracker.begin("session", PromptRequest{Generation: 1}) {
		t.Fatal("turn was not reserved")
	}
	tracker.accepted("session", 1)
	emitted = 0
	tracker.detachAll()
	tracker.finish("session", 1, true)
	if emitted != 0 {
		t.Fatalf("shutdown emitted %d lifecycle events", emitted)
	}
}
