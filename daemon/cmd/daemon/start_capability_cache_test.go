package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nekonest/daemon/internal/adapters"
)

func startCapabilityTestRegistry(t *testing.T, agents ...*fakeStartAdapter) *adapters.Registry {
	t.Helper()
	items := make([]adapters.Adapter, 0, len(agents))
	for _, agent := range agents {
		items = append(items, agent)
	}
	registry, err := adapters.NewRegistry(items...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func waitForStartCapabilityProbes(t *testing.T, agents ...*fakeStartAdapter) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		complete := true
		for _, agent := range agents {
			if agent.probeCount() == 0 {
				complete = false
				break
			}
		}
		if complete {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for start capability probes")
}

func startCapabilityFor(entries []map[string]interface{}, agentType adapters.AgentType) map[string]interface{} {
	for _, entry := range entries {
		if entry["agent_type"] == string(agentType) {
			return entry
		}
	}
	return nil
}

func TestStartCapabilityCacheUsesActivityTiers(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	claude := &fakeStartAdapter{name: string(adapters.AgentClaudeCode), probe: adapters.ThreadStartCapability{Available: true}}
	codex := &fakeStartAdapter{name: string(adapters.AgentCodex), probe: adapters.ThreadStartCapability{Available: true}}
	kimi := &fakeStartAdapter{name: string(adapters.AgentKimiCLI), probe: adapters.ThreadStartCapability{Available: true}}
	grok := &fakeStartAdapter{name: string(adapters.AgentGrokBuild), probe: adapters.ThreadStartCapability{Available: true}}
	cache := newAgentStartCapabilityCache()
	cache.now = func() time.Time { return now }
	registry := startCapabilityTestRegistry(t, claude, codex, kimi, grok)
	sessions := []*adapters.SessionInfo{
		{AgentType: adapters.AgentClaudeCode, Status: adapters.StatusRunning, LastActivity: now.Add(-48 * time.Hour)},
		{AgentType: adapters.AgentCodex, Status: adapters.StatusIdle, LastActivity: now.Add(-14 * time.Minute)},
		{AgentType: adapters.AgentKimiCLI, Status: adapters.StatusIdle, LastActivity: now.Add(-2 * time.Hour)},
	}
	_ = cache.Get(context.Background(), registry, sessions)
	waitForStartCapabilityProbes(t, claude, codex, kimi, grok)
	entries := cache.Get(context.Background(), registry, sessions)
	for _, agentType := range supportedStartAgents {
		if entry := startCapabilityFor(entries, agentType); entry == nil || entry["spawn"] != true {
			t.Fatalf("%s entry = %#v", agentType, entry)
		}
	}
	wantNext := map[adapters.AgentType]time.Time{
		adapters.AgentClaudeCode: now.Add(startCapabilityActiveInterval),
		adapters.AgentCodex:      now.Add(startCapabilityActiveInterval),
		adapters.AgentKimiCLI:    now.Add(startCapabilityRecentInterval),
		adapters.AgentGrokBuild:  now.Add(startCapabilityDormantInterval),
	}
	for agentType, want := range wantNext {
		cache.mu.Lock()
		got := cache.entries[agentType].nextProbe
		cache.mu.Unlock()
		if !got.Equal(want) {
			t.Fatalf("%s next probe = %s, want %s", agentType, got, want)
		}
	}
}

func TestStartCapabilityCacheDoesNotBypassDormantDueTime(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	agent := &fakeStartAdapter{name: string(adapters.AgentKimiCLI), probe: adapters.ThreadStartCapability{Available: true}}
	cache := newAgentStartCapabilityCache()
	cache.now = func() time.Time { return now }
	registry := startCapabilityTestRegistry(t, agent)
	_ = cache.Get(context.Background(), registry, nil)
	waitForStartCapabilityProbes(t, agent)
	now = now.Add(23*time.Hour + 59*time.Minute)
	_ = cache.Get(context.Background(), registry, nil)
	if got := agent.probeCount(); got != 1 {
		t.Fatalf("probe count before dormant due time = %d, want 1", got)
	}
	now = now.Add(time.Minute)
	_ = cache.Get(context.Background(), registry, nil)
	deadline := time.Now().Add(time.Second)
	for agent.probeCount() != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := agent.probeCount(); got != 2 {
		t.Fatalf("probe count at dormant due time = %d, want 2", got)
	}
}

func TestStartCapabilityCacheRecomputesDueTimeWhenActivityChanges(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	agent := &fakeStartAdapter{name: string(adapters.AgentKimiCLI), probe: adapters.ThreadStartCapability{Available: true}}
	cache := newAgentStartCapabilityCache()
	cache.now = func() time.Time { return now }
	registry := startCapabilityTestRegistry(t, agent)

	_ = cache.Get(context.Background(), registry, nil)
	waitForStartCapabilityProbes(t, agent)
	now = now.Add(startCapabilityActiveInterval)
	active := []*adapters.SessionInfo{{
		AgentType:    adapters.AgentKimiCLI,
		Status:       adapters.StatusRunning,
		LastActivity: now,
	}}
	_ = cache.Get(context.Background(), registry, active)
	deadline := time.Now().Add(time.Second)
	for agent.probeCount() != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := agent.probeCount(); got != 2 {
		t.Fatalf("probe count after dormant-to-active transition = %d, want 2", got)
	}
	cache.mu.Lock()
	entry := cache.entries[adapters.AgentKimiCLI]
	activity, next := entry.activity, entry.nextProbe
	cache.mu.Unlock()
	if activity != startCapabilityActiveTier {
		t.Fatalf("activity = %q, want %q", activity, startCapabilityActiveTier)
	}
	if want := now.Add(startCapabilityActiveInterval); !next.Equal(want) {
		t.Fatalf("next probe = %s, want %s", next, want)
	}
}

func TestStartCapabilityCacheSingleFlightFailsClosedBeforeFirstResult(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	agent := &fakeStartAdapter{name: string(adapters.AgentKimiCLI)}
	agent.probeFn = func(context.Context) adapters.ThreadStartCapability {
		once.Do(func() { close(started) })
		<-release
		return adapters.ThreadStartCapability{Available: true}
	}
	cache := newAgentStartCapabilityCache()
	registry := startCapabilityTestRegistry(t, agent)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entry := startCapabilityFor(cache.Get(context.Background(), registry, nil), adapters.AgentKimiCLI)
			if entry["spawn"] != false {
				t.Errorf("in-flight entry unexpectedly startable: %#v", entry)
			}
		}()
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	wg.Wait()
	if got := agent.probeCount(); got != 1 {
		t.Fatalf("concurrent probes = %d, want 1", got)
	}
	close(release)
	waitForStartCapabilityProbes(t, agent)
	deadline := time.Now().Add(time.Second)
	for startCapabilityFor(cache.Get(context.Background(), registry, nil), adapters.AgentKimiCLI)["spawn"] != true && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

func TestStartCapabilityCacheFailsClosedOnTimeoutAndPanic(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		agent := &fakeStartAdapter{name: string(adapters.AgentKimiCLI)}
		agent.probeFn = func(ctx context.Context) adapters.ThreadStartCapability {
			<-ctx.Done()
			return adapters.ThreadStartCapability{Available: true}
		}
		cache := newAgentStartCapabilityCache()
		cache.probeTimeout = time.Millisecond
		capability, err := cache.ProbeForStart(context.Background(), startCapabilityTestRegistry(t, agent), adapters.AgentKimiCLI, nil)
		if err == nil || capability.Available {
			t.Fatalf("timeout capability = %#v, err = %v", capability, err)
		}
	})
	t.Run("panic", func(t *testing.T) {
		agent := &fakeStartAdapter{name: string(adapters.AgentKimiCLI)}
		agent.probeFn = func(context.Context) adapters.ThreadStartCapability { panic(errors.New("probe panic")) }
		cache := newAgentStartCapabilityCache()
		capability, err := cache.ProbeForStart(context.Background(), startCapabilityTestRegistry(t, agent), adapters.AgentKimiCLI, nil)
		if err == nil || capability.Available {
			t.Fatalf("panic capability = %#v, err = %v", capability, err)
		}
	})
}

func TestThreadStartPreflightJoinsDiscoveryProbeAndUpdatesCache(t *testing.T) {
	projectDir := testNativeProjectDir(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	agent := &fakeStartAdapter{
		name:      string(adapters.AgentKimiCLI),
		result:    adapters.ThreadStartResult{SessionID: "kimi-native", Created: true, PromptAccepted: true},
		ownsAfter: 1,
	}
	agent.probeFn = func(context.Context) adapters.ThreadStartCapability {
		once.Do(func() { close(started) })
		<-release
		return adapters.ThreadStartCapability{Available: true}
	}
	registry := startCapabilityTestRegistry(t, agent)
	cache := newAgentStartCapabilityCache()
	_ = cache.Get(context.Background(), registry, nil)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("discovery probe did not start")
	}
	coordinator, _ := newThreadStartTestCoordinator(t, agent, projectDir)
	coordinator.probeStartCapability = func(ctx context.Context, agentType adapters.AgentType, _ adapters.NativeThreadStarter) (adapters.ThreadStartCapability, error) {
		return cache.ProbeForStart(ctx, registry, agentType, nil)
	}
	eventsDone := make(chan []threadStartEvent, 1)
	go func() {
		eventsDone <- collectStartEvents(coordinator, threadStartCommand{OperationID: "join-probe", AgentType: adapters.AgentKimiCLI, ProjectDir: projectDir, Prompt: "hello"})
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		cache.mu.Lock()
		waiters := cache.entries[adapters.AgentKimiCLI].waiters
		cache.mu.Unlock()
		if waiters == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cache.mu.Lock()
	waiters := cache.entries[adapters.AgentKimiCLI].waiters
	cache.mu.Unlock()
	if waiters != 1 {
		t.Fatal("thread-start preflight did not join the discovery probe")
	}
	close(release)
	var events []threadStartEvent
	select {
	case events = <-eventsDone:
	case <-time.After(time.Second):
		t.Fatal("thread-start preflight did not finish")
	}
	if got := agent.probeCount(); got != 1 {
		t.Fatalf("preflight probe count = %d, want 1", got)
	}
	if final := events[len(events)-1]; final.State != "thread_owned" {
		t.Fatalf("thread-start events = %#v", events)
	}
	entry := startCapabilityFor(cache.Get(context.Background(), registry, nil), adapters.AgentKimiCLI)
	if entry["spawn"] != true {
		t.Fatalf("preflight did not update cache: %#v", entry)
	}
}
