package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/nekonest/daemon/internal/agentexec"
)

func TestAppServerExitDegradesSessionsCleansStateAndRecovers(t *testing.T) {
	a := NewCodexAdapter()
	defer a.Close()

	cleaned := make(chan struct{}, 1)
	a.outputMu.Lock()
	a.appOutput["partial"] = "secret partial output"
	a.initialTurnCleanup["turn-1"] = func() { cleaned <- struct{}{} }
	a.outputMu.Unlock()

	controls := make(chan ControlEvent, 1)
	a.SetControlSink(func(event ControlEvent) { controls <- event })
	recovered := make(chan struct{}, 1)
	a.SetRecoverySink(func() { recovered <- struct{}{} })
	a.recoveryMu.Lock()
	a.recoverySleep = func(context.Context, time.Duration) bool { return true }
	a.recoveryEnsure = func() error { return nil }
	a.recoveryMu.Unlock()

	a.handleAppServerExit(agentexec.AppServerExit{
		Generation: 7,
		Sessions:   []string{"wire-1"},
		Err:        agentexec.ErrAppServerExited,
	})

	select {
	case event := <-controls:
		if event.SessionID != "wire-1" || event.Status != StatusError || event.Class != "failed" {
			t.Fatalf("control event = %#v", event)
		}
		if event.Capabilities == nil || event.Capabilities.ControlMode != ControlExecResume || event.Capabilities.Queue {
			t.Fatalf("degraded capabilities = %#v", event.Capabilities)
		}
	case <-time.After(time.Second):
		t.Fatal("missing degraded control event")
	}
	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("retained first-turn files were not released")
	}
	a.outputMu.Lock()
	if len(a.appOutput) != 0 || len(a.initialTurnCleanup) != 0 {
		t.Fatalf("app-server state was not cleared: output=%v cleanup=%v", a.appOutput, a.initialTurnCleanup)
	}
	a.outputMu.Unlock()
	select {
	case <-recovered:
	case <-time.After(time.Second):
		t.Fatal("successful bounded recovery did not request rediscovery")
	}
}
