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
	name   string
	hasID  string
	probed bool
}

func (a *routingAdapter) Name() string      { return a.name }
func (a *routingAdapter) IsAvailable() bool { return true }
func (a *routingAdapter) Discover() ([]*adapters.SessionInfo, error) {
	return nil, nil
}
func (a *routingAdapter) Watch(string) (<-chan *adapters.SessionInfo, error) {
	return nil, fmt.Errorf("unused")
}
func (a *routingAdapter) SendPrompt(string, string) error { return nil }
func (a *routingAdapter) Approve(string, string) error    { return nil }
func (a *routingAdapter) Deny(string, string) error       { return nil }
func (a *routingAdapter) Interrupt(string) error          { return nil }
func (a *routingAdapter) FetchHistory(sessionID string, _ int) ([]*adapters.HistoryMessage, error) {
	a.probed = true
	if sessionID != a.hasID {
		return nil, fmt.Errorf("not found")
	}
	return []*adapters.HistoryMessage{}, nil
}

func TestPickAdapterProbesUUIDInsteadOfAssumingCodex(t *testing.T) {
	const id = "019f9c8f-21bb-7203-899f-9d855fe9505d"
	codex := &routingAdapter{name: "codex", hasID: id}
	claude := &routingAdapter{name: "claude_code", hasID: id}

	got := pickAdapterForSession(id, []adapters.Adapter{codex, claude})
	if got != claude {
		t.Fatalf("UUID routed to %v, want Claude local-store match", got)
	}
	if !claude.probed {
		t.Fatal("Claude store was not probed")
	}
}
