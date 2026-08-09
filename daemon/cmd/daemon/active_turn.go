package main

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/nekonest/daemon/internal/adapters"
)

type activeTurnRegistry struct {
	mu       sync.Mutex
	bindings map[string]activeTurnState
}

type activeTurnState struct {
	binding         adapters.ActiveTurnBinding
	accepted        bool
	indeterminate   bool
	nativeComplete  bool
	pendingTerminal *adapters.ControlEvent
}

func newActiveTurnRegistry() *activeTurnRegistry {
	return &activeTurnRegistry{bindings: make(map[string]activeTurnState)}
}

func (r *activeTurnRegistry) bind(sessionID string, generation uint64, clientMsgID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	clientMsgID = strings.TrimSpace(clientMsgID)
	if sessionID == "" || generation == 0 || clientMsgID == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.bindings[sessionID]; exists {
		return false
	}
	r.bindings[sessionID] = activeTurnState{binding: adapters.ActiveTurnBinding{Generation: generation, ClientMsgID: clientMsgID}}
	return true
}

func (r *activeTurnRegistry) setNativeRequestID(sessionID string, generation uint64, clientMsgID, nativeRequestID string) bool {
	nativeRequestID = strings.TrimSpace(nativeRequestID)
	if nativeRequestID == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.bindings[sessionID]
	if !ok || state.binding.Generation != generation || state.binding.ClientMsgID != clientMsgID {
		return false
	}
	state.binding.NativeRequestID = nativeRequestID
	r.bindings[sessionID] = state
	return true
}

func (r *activeTurnRegistry) current(sessionID string) (*adapters.ActiveTurnBinding, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.bindings[sessionID]
	if !ok {
		return nil, false
	}
	copy := state.binding
	return &copy, true
}

// overlayActiveTurns materializes daemon-owned turn bindings into the
// authoritative discovery snapshot used for phone reconnects.
func overlayActiveTurns(sessions []*adapters.SessionInfo, registry *activeTurnRegistry) {
	for _, session := range sessions {
		if session == nil {
			continue
		}
		session.ActiveTurn = nil
		if binding, ok := registry.current(session.ID); ok {
			session.ActiveTurn = binding
		}
	}
}

func (r *activeTurnRegistry) matches(sessionID string, generation uint64, clientMsgID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.bindings[sessionID]
	return ok && state.binding.Generation == generation && state.binding.ClientMsgID == clientMsgID
}

func (r *activeTurnRegistry) clearMatching(sessionID string, generation uint64, clientMsgID, nativeRequestID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.bindings[sessionID]
	if !ok || state.binding.Generation != generation || state.binding.ClientMsgID != clientMsgID {
		return false
	}
	if nativeRequestID != "" && state.binding.NativeRequestID != nativeRequestID {
		return false
	}
	delete(r.bindings, sessionID)
	return true
}

// accept opens the terminal-event gate only after the durable journal has
// recorded native acceptance. A terminal notification that raced the journal
// write is returned for ordered replay through the normal control sink.
func (r *activeTurnRegistry) accept(sessionID string, generation uint64, clientMsgID string) (*adapters.ControlEvent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.bindings[sessionID]
	if !ok || state.binding.Generation != generation || state.binding.ClientMsgID != clientMsgID {
		return nil, false
	}
	state.accepted = true
	pending := state.pendingTerminal
	state.pendingTerminal = nil
	if state.nativeComplete && pending == nil {
		delete(r.bindings, sessionID)
		return nil, true
	}
	r.bindings[sessionID] = state
	return pending, false
}

// completeMatching transfers cleanup from a native completion callback. A
// callback that races durable acceptance is remembered instead of clearing the
// binding and losing a pending terminal event.
func (r *activeTurnRegistry) completeMatching(sessionID string, generation uint64, clientMsgID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.bindings[sessionID]
	if !ok || state.binding.Generation != generation || state.binding.ClientMsgID != clientMsgID {
		return false
	}
	if !state.accepted && !state.indeterminate {
		state.nativeComplete = true
		r.bindings[sessionID] = state
		return false
	}
	delete(r.bindings, sessionID)
	return true
}

// abandonAcceptance marks a post-boundary journal failure. The binding stays
// controllable while the native process is still alive, then clears on its
// completion callback.
func (r *activeTurnRegistry) abandonAcceptance(sessionID string, generation uint64, clientMsgID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.bindings[sessionID]
	if !ok || state.binding.Generation != generation || state.binding.ClientMsgID != clientMsgID {
		return false
	}
	if state.nativeComplete {
		delete(r.bindings, sessionID)
		return true
	}
	state.indeterminate = true
	state.pendingTerminal = nil
	r.bindings[sessionID] = state
	return false
}

// terminalForEvent accepts only an event attributable to the current turn and
// holds it until durable acceptance. A native request id, when present, must
// match exactly; generation/client ids are never inferred across a mismatch.
func (r *activeTurnRegistry) terminalForEvent(event adapters.ControlEvent) (*adapters.ActiveTurnBinding, bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.bindings[event.SessionID]
	if !ok {
		return nil, false, false
	}
	binding := state.binding
	if event.Generation != 0 && event.Generation != binding.Generation {
		return nil, false, false
	}
	if event.ClientMsgID != "" && event.ClientMsgID != binding.ClientMsgID {
		return nil, false, false
	}
	if event.NativeRequestID != "" && event.NativeRequestID != binding.NativeRequestID {
		return nil, false, false
	}
	copy := binding
	if !state.accepted {
		pending := event
		state.pendingTerminal = &pending
		r.bindings[event.SessionID] = state
		return &copy, false, true
	}
	return &copy, true, true
}

func parseActiveTurnCommand(payload map[string]interface{}) (uint64, string, error) {
	if payload == nil {
		return 0, "", errors.New("active turn binding required")
	}
	rawGeneration, ok := payload["generation"]
	if !ok {
		return 0, "", errors.New("active turn generation required")
	}
	var generation uint64
	switch value := rawGeneration.(type) {
	case float64:
		if value <= 0 || value > float64(^uint64(0)) || math.Trunc(value) != value {
			return 0, "", errors.New("active turn generation invalid")
		}
		generation = uint64(value)
	case uint64:
		generation = value
	case int:
		if value > 0 {
			generation = uint64(value)
		}
	}
	clientMsgID, _ := payload["client_msg_id"].(string)
	clientMsgID = strings.TrimSpace(clientMsgID)
	if generation == 0 || clientMsgID == "" {
		return 0, "", fmt.Errorf("active turn generation and client_msg_id required")
	}
	return generation, clientMsgID, nil
}
