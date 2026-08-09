package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nekonest/daemon/internal/adapters"
)

func TestPromptAcceptanceCacheHasNoTimeWindowAndIsBounded(t *testing.T) {
	cache := newPromptAcceptanceCache(2)
	first := acceptedPrompt{prompt: "one"}
	cache.add("session", "id-1", first)

	if got, ok := cache.get("session", "id-1"); !ok || got.prompt != "one" {
		t.Fatalf("accepted ID missing: %#v, %v", got, ok)
	}
	if _, ok := cache.get("other-session", "id-1"); ok {
		t.Fatal("client message IDs must be scoped by session")
	}
	if _, ok := cache.get("session", ""); ok {
		t.Fatal("legacy messages without an ID must not be deduplicated")
	}

	cache.add("session", "id-2", acceptedPrompt{prompt: "two"})
	cache.add("session", "id-3", acceptedPrompt{prompt: "three"})
	if _, ok := cache.get("session", "id-1"); ok {
		t.Fatal("oldest entry was not evicted")
	}
	if _, ok := cache.get("session", "id-2"); !ok {
		t.Fatal("newer entry was unexpectedly evicted")
	}
}

func TestPromptStatusNeverExecutesUnknownAndCacheEchoIsMemoryBounded(t *testing.T) {
	cache := newPromptAcceptanceCache(1)
	if _, err := promptStatus(cache, "session", "unknown"); err == nil ||
		!strings.Contains(err.Error(), "ordinary retry is disabled") {
		t.Fatalf("unknown status did not disable unsafe retry: %v", err)
	}
	if _, err := promptStatus(cache, "session", ""); err == nil {
		t.Fatal("empty client_msg_id was accepted")
	}

	cache.add("session", "accepted", acceptedPrompt{prompt: strings.Repeat("猫", 3000)})
	got, err := promptStatus(cache, "session", "accepted")
	if err != nil {
		t.Fatalf("accepted status missing: %v", err)
	}
	if len(got.prompt) > maxAcceptedPromptEchoBytes || !utf8.ValidString(got.prompt) {
		t.Fatalf("cached prompt echo is not bounded valid UTF-8: bytes=%d", len(got.prompt))
	}
}

func TestSessionLockMapSerializesAndReleasesKeys(t *testing.T) {
	locks := newSessionLockMap()
	unlock := locks.lock("session")

	acquired := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		unlockSecond := locks.lock("session")
		close(acquired)
		unlockSecond()
	}()

	select {
	case <-acquired:
		t.Fatal("second caller acquired the same session lock too early")
	case <-time.After(20 * time.Millisecond):
	}
	unlock()
	wg.Wait()

	locks.mu.Lock()
	defer locks.mu.Unlock()
	if len(locks.locks) != 0 {
		t.Fatalf("released session keys retained: %d", len(locks.locks))
	}
}

type routingAdapter struct {
	name           string
	hasID          string
	ownershipProbe bool
	historyProbe   bool
	nilHistory     bool
	denyOwnership  bool
	sendPrompt     func(adapters.PromptRequest) error
}

func (a *routingAdapter) Name() string      { return a.name }
func (a *routingAdapter) IsAvailable() bool { return true }
func (a *routingAdapter) Discover() ([]*adapters.SessionInfo, error) {
	return nil, nil
}
func (a *routingAdapter) OwnsSession(sessionID string) bool {
	a.ownershipProbe = true
	return !a.denyOwnership && sessionID == a.hasID
}
func (a *routingAdapter) Watch(string) (<-chan *adapters.SessionInfo, error) {
	return nil, fmt.Errorf("unused")
}
func (a *routingAdapter) SendPrompt(_ string, request adapters.PromptRequest) error {
	if a.sendPrompt != nil {
		return a.sendPrompt(request)
	}
	return nil
}
func (a *routingAdapter) Approve(string, string) error { return nil }
func (a *routingAdapter) Deny(string, string) error    { return nil }
func (a *routingAdapter) Interrupt(string) error       { return nil }
func (a *routingAdapter) FetchHistory(sessionID string, _ int) ([]*adapters.HistoryMessage, error) {
	a.historyProbe = true
	if sessionID != a.hasID {
		return nil, fmt.Errorf("not found")
	}
	if a.nilHistory {
		return nil, nil
	}
	return []*adapters.HistoryMessage{}, nil
}

func TestPickAdapterUsesPositiveOwnershipInsteadOfHistory(t *testing.T) {
	const id = "019f9c8f-21bb-7203-899f-9d855fe9505d"
	codex := &routingAdapter{name: "codex"}
	claude := &routingAdapter{name: "claude_code", hasID: id}

	got := pickAdapterForSession(id, []adapters.Adapter{codex, claude})
	if got != claude {
		t.Fatalf("UUID routed to %v, want Claude local-store match", got)
	}
	if !claude.ownershipProbe {
		t.Fatal("Claude ownership was not checked")
	}
	if codex.historyProbe || claude.historyProbe {
		t.Fatal("routing probed history instead of OwnsSession")
	}
}

func TestPickAdapterRejectsAmbiguousLegacyID(t *testing.T) {
	const id = "shared-id"
	first := &routingAdapter{name: "codex", hasID: id}
	second := &routingAdapter{name: "claude_code", hasID: id}
	if got := pickAdapterForSession(id, []adapters.Adapter{first, second}); got != nil {
		t.Fatalf("ambiguous ID routed to %s", got.Name())
	}
}

func TestPickAdapterRequiresOwnershipForNamespacedID(t *testing.T) {
	const id = "grok_build:native-id"
	grok := &routingAdapter{name: "grok_build", hasID: id}
	if got := pickAdapterForSession("grok_build:native-id", []adapters.Adapter{grok}); got != grok {
		t.Fatalf("namespaced ID routed to %v", got)
	}
	if !grok.ownershipProbe || grok.historyProbe {
		t.Fatal("namespaced ID was not positively owned")
	}

	unknown := &routingAdapter{name: "grok_build"}
	if got := pickAdapterForSession(id, []adapters.Adapter{unknown}); got != nil {
		t.Fatalf("unknown namespaced ID routed to %v", got)
	}
}

func TestFetchHistoryKeepsKnownEmptySource(t *testing.T) {
	const id = "kimi_cli:empty"
	preferred := &routingAdapter{
		name:       "kimi_cli",
		hasID:      id,
		nilHistory: true,
	}
	unrelated := &routingAdapter{name: "codex", hasID: id}

	history, source, err := fetchHistoryForSession(
		id,
		40,
		preferred,
		[]adapters.Adapter{preferred, unrelated},
	)
	if err != nil {
		t.Fatal(err)
	}
	if history == nil || len(history) != 0 {
		t.Fatalf("empty history = %#v", history)
	}
	if source != "kimi_cli" {
		t.Fatalf("source = %q, want kimi_cli", source)
	}
	if unrelated.historyProbe {
		t.Fatal("empty owned history fell through to an unrelated adapter")
	}
}

func TestFetchHistoryDoesNotProbeUnownedStores(t *testing.T) {
	const id = "hidden-child"
	leaky := &routingAdapter{
		name:          "codex",
		hasID:         id,
		denyOwnership: true,
	}

	history, source, err := fetchHistoryForSession(
		id,
		40,
		nil,
		[]adapters.Adapter{leaky},
	)
	if err != nil {
		t.Fatal(err)
	}
	if history != nil || source != "" {
		t.Fatalf("unowned history was returned: source=%q history=%#v", source, history)
	}
	if !leaky.ownershipProbe {
		t.Fatal("history routing skipped positive ownership")
	}
	if leaky.historyProbe {
		t.Fatal("history was read from an unowned store")
	}
}
