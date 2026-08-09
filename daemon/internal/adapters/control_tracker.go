package adapters

import "sync"

type turnBinding struct {
	generation  uint64
	clientMsgID string
	onLifecycle func(TurnLifecycle, string)
	accepted    bool
	terminal    *turnTerminal
}

type turnTerminal struct {
	exitCode    int
	interrupted bool
}

// turnTracker correlates terminal process callbacks with one exact daemon
// generation so a late callback cannot complete a newer turn.
type turnTracker struct {
	mu       sync.Mutex
	agent    AgentType
	sink     func(ControlEvent)
	bindings map[string]turnBinding
}

func newTurnTracker(agent AgentType) turnTracker {
	return turnTracker{agent: agent, bindings: make(map[string]turnBinding)}
}

func (t *turnTracker) setSink(sink func(ControlEvent)) {
	t.mu.Lock()
	t.sink = sink
	t.mu.Unlock()
}

func (t *turnTracker) begin(sessionID string, request PromptRequest) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.bindings[sessionID]; exists {
		return false
	}
	t.bindings[sessionID] = turnBinding{generation: request.Generation, clientMsgID: request.ClientMsgID, onLifecycle: request.OnLifecycle}
	return true
}

func (t *turnTracker) abort(sessionID string, generation uint64) {
	t.mu.Lock()
	if current, ok := t.bindings[sessionID]; ok && current.generation == generation {
		delete(t.bindings, sessionID)
	}
	t.mu.Unlock()
}

// detachAll is used during daemon shutdown. Durable running queue items are
// intentionally left untouched so loading queue v2 after restart converts
// them to blocked_indeterminate instead of reporting a user interrupt.
func (t *turnTracker) detachAll() {
	t.mu.Lock()
	t.bindings = make(map[string]turnBinding)
	t.mu.Unlock()
}

func (t *turnTracker) accepted(sessionID string, generation uint64) {
	t.mu.Lock()
	binding, ok := t.bindings[sessionID]
	if !ok || binding.generation != generation {
		t.mu.Unlock()
		return
	}
	binding.accepted = true
	pending := binding.terminal
	binding.terminal = nil
	t.bindings[sessionID] = binding
	t.mu.Unlock()
	t.emit(sessionID, generation, TurnAccepted, "accepted", "")
	if pending != nil {
		t.finish(sessionID, pending.exitCode, pending.interrupted)
	}
}

func (t *turnTracker) finish(sessionID string, exitCode int, interrupted bool) {
	t.mu.Lock()
	binding, ok := t.bindings[sessionID]
	if ok && !binding.accepted {
		binding.terminal = &turnTerminal{exitCode: exitCode, interrupted: interrupted}
		t.bindings[sessionID] = binding
		t.mu.Unlock()
		return
	}
	if ok {
		delete(t.bindings, sessionID)
	}
	sink := t.sink
	t.mu.Unlock()
	if !ok {
		return
	}
	lifecycle, class, reason := TurnTerminalSuccess, "completed", ""
	if interrupted {
		lifecycle, class, reason = TurnInterrupted, "interrupted", "native process interrupted"
	} else if exitCode != 0 {
		lifecycle, class, reason = TurnTerminalFailure, "failed", "native process exited unsuccessfully"
	}
	if binding.onLifecycle != nil {
		binding.onLifecycle(lifecycle, reason)
	}
	if sink != nil {
		sink(ControlEvent{SessionID: sessionID, AgentType: t.agent, Generation: binding.generation, ClientMsgID: binding.clientMsgID, Lifecycle: lifecycle, Class: class})
	}
}

func (t *turnTracker) emit(sessionID string, generation uint64, lifecycle TurnLifecycle, class, reason string) {
	t.mu.Lock()
	binding, ok := t.bindings[sessionID]
	sink := t.sink
	t.mu.Unlock()
	if !ok || binding.generation != generation {
		return
	}
	if binding.onLifecycle != nil {
		binding.onLifecycle(lifecycle, reason)
	}
	if sink != nil {
		sink(ControlEvent{SessionID: sessionID, AgentType: t.agent, Generation: generation, ClientMsgID: binding.clientMsgID, Lifecycle: lifecycle, Class: class})
	}
}
