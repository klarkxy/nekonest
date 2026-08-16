package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nekonest/daemon/internal/adapters"
	"github.com/nekonest/daemon/internal/attach"
	"github.com/nekonest/daemon/internal/sealedkeys"
	"github.com/nekonest/daemon/internal/startjournal"
)

func TestBuildThreadResultMessageSealsApplicationDetails(t *testing.T) {
	previousTransport := daemonTransport
	previousKeys := daemonSealedKeys
	t.Cleanup(func() {
		daemonTransport = previousTransport
		daemonSealedKeys = previousKeys
	})

	manager, err := sealedkeys.LoadOrCreate(filepath.Join(t.TempDir(), "sealed-keys.json"))
	if err != nil {
		t.Fatal(err)
	}
	daemonTransport = "sealed"
	daemonSealedKeys = manager

	msg := buildThreadResultMessage("1.2",
		"device-a",
		"claude_code",
		"operation-a",
		"thread_failed",
		"native-session-a",
		false,
		"private native error",
	)
	if _, ok := msg["payload"]; ok {
		t.Fatal("sealed result exposed plaintext payload")
	}
	if _, ok := msg["sealed_payload"]; !ok {
		t.Fatal("sealed result missing sealed_payload")
	}
	if got := msg["client_msg_id"]; got != "operation-a" {
		t.Fatalf("client_msg_id = %v", got)
	}
	if got := msg["session_id"]; got != "native-session-a" {
		t.Fatalf("session_id = %v", got)
	}
}

func TestBuildThreadResultMessageDegradesWhenSealingUnavailable(t *testing.T) {
	previousTransport := daemonTransport
	previousKeys := daemonSealedKeys
	t.Cleanup(func() {
		daemonTransport = previousTransport
		daemonSealedKeys = previousKeys
	})
	daemonTransport = "sealed"
	daemonSealedKeys = nil

	msg := buildThreadResultMessage("1.2",
		"device-a",
		"codex",
		"operation-a",
		"thread_owned",
		"untrusted-native-session",
		true,
		"",
	)
	if got := msg["type"]; got != "thread_indeterminate" {
		t.Fatalf("type = %v", got)
	}
	if _, ok := msg["session_id"]; ok {
		t.Fatal("degraded result exposed unauthenticated session_id")
	}
	if _, ok := msg["payload"]; ok {
		t.Fatal("degraded result exposed plaintext payload")
	}
	if _, ok := msg["sealed_payload"]; ok {
		t.Fatal("degraded result unexpectedly contained sealed_payload")
	}
	if got := msg["client_msg_id"]; got != "operation-a" {
		t.Fatalf("client_msg_id = %v", got)
	}
}

type fakeStartAdapter struct {
	mu              sync.Mutex
	name            string
	probe           adapters.ThreadStartCapability
	probeFn         func(context.Context) adapters.ThreadStartCapability
	probeCalls      int
	result          adapters.ThreadStartResult
	startErr        error
	startCalls      int
	ownershipChecks int
	ownsAfter       int
	onStart         func()
	lastRequest     adapters.ThreadStartRequest
}

func (a *fakeStartAdapter) Name() string                               { return a.name }
func (a *fakeStartAdapter) IsAvailable() bool                          { return true }
func (a *fakeStartAdapter) Discover() ([]*adapters.SessionInfo, error) { return nil, nil }
func (a *fakeStartAdapter) OwnsSession(string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ownershipChecks++
	return a.ownsAfter > 0 && a.ownershipChecks >= a.ownsAfter
}
func (a *fakeStartAdapter) Watch(string) (<-chan *adapters.SessionInfo, error) {
	updates := make(chan *adapters.SessionInfo)
	close(updates)
	return updates, nil
}
func (a *fakeStartAdapter) SendPrompt(string, adapters.PromptRequest) error { return nil }
func (a *fakeStartAdapter) Approve(string, string) error                    { return nil }
func (a *fakeStartAdapter) Deny(string, string) error                       { return nil }
func (a *fakeStartAdapter) Interrupt(string) error                          { return nil }
func (a *fakeStartAdapter) FetchHistory(string, int) ([]*adapters.HistoryMessage, error) {
	return nil, nil
}
func (a *fakeStartAdapter) ProbeThreadStart(ctx context.Context) adapters.ThreadStartCapability {
	a.mu.Lock()
	a.probeCalls++
	probeFn := a.probeFn
	probe := a.probe
	a.mu.Unlock()
	if probeFn != nil {
		return probeFn(ctx)
	}
	return probe
}
func (a *fakeStartAdapter) StartNativeThread(_ context.Context, request adapters.ThreadStartRequest) (adapters.ThreadStartResult, error) {
	a.mu.Lock()
	a.startCalls++
	a.lastRequest = request
	onStart := a.onStart
	result, err := a.result, a.startErr
	a.mu.Unlock()
	if onStart != nil {
		onStart()
	}
	return result, err
}

func (a *fakeStartAdapter) request() adapters.ThreadStartRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastRequest
}

func (a *fakeStartAdapter) counts() (starts, ownership int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startCalls, a.ownershipChecks
}

func (a *fakeStartAdapter) probeCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.probeCalls
}

func newThreadStartTestCoordinator(t *testing.T, agent *fakeStartAdapter, projectDir string) (*threadStartCoordinator, *startjournal.Journal) {
	t.Helper()
	if resolved, err := resolveCurrentProjectDir(projectDir, []string{projectDir}); err != nil {
		canonical, normalized, canonicalErr := canonicalNativeProjectDir(projectDir)
		t.Fatalf("resolve test project %q: %v (canonical=%q normalized=%q err=%v)", projectDir, err, canonical, normalized, canonicalErr)
	} else if resolved == "" {
		t.Fatal("resolved test project is empty")
	}
	journal, err := startjournal.Load(filepath.Join(t.TempDir(), "starts.json"), "device")
	if err != nil {
		t.Fatal(err)
	}
	coordinator := &threadStartCoordinator{
		journal: journal,
		lookupAdapter: func(name string) (adapters.Adapter, bool) {
			if name != agent.name {
				return nil, false
			}
			return agent, true
		},
		snapshotProjectDirs: func() []string { return []string{projectDir} },
		probeTimeout:        time.Second,
		startTimeout:        time.Second,
		ownershipWait:       30 * time.Millisecond,
		ownershipPoll:       time.Millisecond,
	}
	return coordinator, journal
}

func testNativeProjectDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", ".thread-start-project-")
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(abs) })
	return abs
}

func collectStartEvents(coordinator *threadStartCoordinator, command threadStartCommand) []threadStartEvent {
	var events []threadStartEvent
	coordinator.Handle(context.Background(), command, func(event threadStartEvent) {
		events = append(events, event)
	})
	return events
}

func TestThreadStartPersistsBeforeBoundaryAndDuplicateDoesNotStartAgain(t *testing.T) {
	projectDir := testNativeProjectDir(t)
	agent := &fakeStartAdapter{
		name:      string(adapters.AgentClaudeCode),
		probe:     adapters.ThreadStartCapability{Available: true},
		result:    adapters.ThreadStartResult{SessionID: "native-1", Created: true, PromptAccepted: true},
		ownsAfter: 1,
	}
	coordinator, journal := newThreadStartTestCoordinator(t, agent, projectDir)
	agent.onStart = func() {
		record, ok := journal.Lookup("operation-1")
		if !ok || record.Status != startjournal.StatusStarting {
			t.Fatalf("journal at native boundary = %#v, ok=%v", record, ok)
		}
	}
	command := threadStartCommand{
		OperationID: "operation-1",
		AgentType:   adapters.AgentClaudeCode,
		ProjectDir:  projectDir,
		Prompt:      "hello",
	}
	first := collectStartEvents(coordinator, command)
	if len(first) != 2 || first[0].State != startjournal.StatusStarting || first[1].State != startjournal.StatusOwned {
		t.Fatalf("first events = %#v", first)
	}
	second := collectStartEvents(coordinator, command)
	if len(second) != 1 || second[0].State != startjournal.StatusOwned || second[0].SessionID != "native-1" {
		t.Fatalf("duplicate events = %#v", second)
	}
	if starts, _ := agent.counts(); starts != 1 {
		t.Fatalf("native start calls = %d; want 1", starts)
	}
}

func TestThreadStartAttachmentFailureDoesNotCrossNativeBoundary(t *testing.T) {
	projectDir := testNativeProjectDir(t)
	agent := &fakeStartAdapter{
		name: string(adapters.AgentCodex), probe: adapters.ThreadStartCapability{Available: true},
		result: adapters.ThreadStartResult{SessionID: "native-1", Created: true, PromptAccepted: true}, ownsAfter: 1,
	}
	coordinator, _ := newThreadStartTestCoordinator(t, agent, projectDir)
	coordinator.materializeAttachments = func(string, []attach.Ref) (string, []attach.LocalFile, string, error) {
		return "", nil, "", errors.New("HTTP 503")
	}
	events := collectStartEvents(coordinator, threadStartCommand{
		OperationID: "attachment-download-fails", AgentType: adapters.AgentCodex, ProjectDir: projectDir, Prompt: "inspect",
		Attachments: []attach.Ref{{URL: "https://nest.invalid/api/attachments/a", Name: "a.txt", MIME: "text/plain"}},
	})
	if got := events[len(events)-1]; got.State != startjournal.StatusFailed || !strings.Contains(got.Message, "attachment download failed") {
		t.Fatalf("events = %#v", events)
	}
	if starts, _ := agent.counts(); starts != 0 {
		t.Fatalf("native starter called %d times after attachment failure", starts)
	}
}

func TestThreadStartPassesMaterializedAttachmentsToNativeStarter(t *testing.T) {
	projectDir := testNativeProjectDir(t)
	agent := &fakeStartAdapter{
		name: string(adapters.AgentCodex), probe: adapters.ThreadStartCapability{Available: true},
		result: adapters.ThreadStartResult{SessionID: "native-1", Created: true, PromptAccepted: true}, ownsAfter: 1,
	}
	coordinator, _ := newThreadStartTestCoordinator(t, agent, projectDir)
	attachmentDir := t.TempDir()
	coordinator.materializeAttachments = func(string, []attach.Ref) (string, []attach.LocalFile, string, error) {
		return attachmentDir, []attach.LocalFile{{Path: filepath.Join(attachmentDir, "photo.png"), Name: "photo.png", MIME: "image/png"}}, "\n[attachment]", nil
	}
	events := collectStartEvents(coordinator, threadStartCommand{
		OperationID: "attachment-pass", AgentType: adapters.AgentCodex, ProjectDir: projectDir, Prompt: "inspect",
		Attachments: []attach.Ref{{URL: "https://nest.invalid/api/attachments/a", Name: "photo.png", MIME: "image/png"}},
	})
	if got := events[len(events)-1]; got.State != startjournal.StatusOwned {
		t.Fatalf("events = %#v", events)
	}
	request := agent.request()
	if len(request.Attachments) != 1 || request.Attachments[0].Name != "photo.png" || !strings.Contains(request.Prompt, "[attachment]") {
		t.Fatalf("request = %#v", request)
	}
	if request.OnComplete == nil {
		t.Fatal("native starter missing attachment cleanup callback")
	}
}

func TestThreadStartRejectsConflictingOperationWithoutSecondStart(t *testing.T) {
	projectDir := testNativeProjectDir(t)
	agent := &fakeStartAdapter{
		name:      string(adapters.AgentKimiCLI),
		probe:     adapters.ThreadStartCapability{Available: true},
		result:    adapters.ThreadStartResult{SessionID: "kimi_cli:native", Created: true, PromptAccepted: true},
		ownsAfter: 1,
	}
	coordinator, _ := newThreadStartTestCoordinator(t, agent, projectDir)
	command := threadStartCommand{OperationID: "same", AgentType: adapters.AgentKimiCLI, ProjectDir: projectDir, Prompt: "first"}
	_ = collectStartEvents(coordinator, command)
	command.Prompt = "different"
	events := collectStartEvents(coordinator, command)
	if len(events) != 1 || events[0].State != startjournal.StatusFailed || !strings.Contains(events[0].Message, "different thread start request") {
		t.Fatalf("conflict events = %#v", events)
	}
	if starts, _ := agent.counts(); starts != 1 {
		t.Fatalf("native start calls = %d; want 1", starts)
	}
}

func TestThreadStartRejectsConflictingAttachmentSetWithoutSecondStart(t *testing.T) {
	projectDir := testNativeProjectDir(t)
	agent := &fakeStartAdapter{
		name: string(adapters.AgentCodex), probe: adapters.ThreadStartCapability{Available: true},
		result: adapters.ThreadStartResult{SessionID: "codex:native", Created: true, PromptAccepted: true}, ownsAfter: 1,
	}
	coordinator, _ := newThreadStartTestCoordinator(t, agent, projectDir)
	attachmentDir := t.TempDir()
	coordinator.materializeAttachments = func(string, []attach.Ref) (string, []attach.LocalFile, string, error) {
		return attachmentDir, []attach.LocalFile{{Path: filepath.Join(attachmentDir, "file.txt"), Name: "file.txt", MIME: "text/plain"}}, "\n[attachment]", nil
	}
	command := threadStartCommand{
		OperationID: "same-attachments", AgentType: adapters.AgentCodex,
		ProjectDir: projectDir, Prompt: "inspect",
		Attachments: []attach.Ref{{ID: "a", URL: "https://nest.invalid/api/attachments/a", Name: "file.txt", MIME: "text/plain"}},
	}
	first := collectStartEvents(coordinator, command)
	if got := first[len(first)-1]; got.State != startjournal.StatusOwned {
		t.Fatalf("first events = %#v", first)
	}
	command.Attachments[0].URL = "https://nest.invalid/api/attachments/b"
	second := collectStartEvents(coordinator, command)
	if len(second) != 1 || second[0].State != startjournal.StatusFailed || !strings.Contains(second[0].Message, "different thread start request") {
		t.Fatalf("attachment conflict events = %#v", second)
	}
	if starts, _ := agent.counts(); starts != 1 {
		t.Fatalf("native start calls = %d; want 1", starts)
	}
}

func TestThreadStartWaitsForOwnershipLag(t *testing.T) {
	projectDir := testNativeProjectDir(t)
	agent := &fakeStartAdapter{
		name:      string(adapters.AgentGrokBuild),
		probe:     adapters.ThreadStartCapability{Available: true},
		result:    adapters.ThreadStartResult{SessionID: "grok_build:native", Created: true, PromptAccepted: true},
		ownsAfter: 3,
	}
	coordinator, _ := newThreadStartTestCoordinator(t, agent, projectDir)
	events := collectStartEvents(coordinator, threadStartCommand{
		OperationID: "lag",
		AgentType:   adapters.AgentGrokBuild,
		ProjectDir:  projectDir,
		Prompt:      "hello",
	})
	if len(events) != 2 || events[1].State != startjournal.StatusOwned {
		t.Fatalf("ownership-lag events = %#v", events)
	}
	if _, checks := agent.counts(); checks < 3 {
		t.Fatalf("ownership checks = %d; want at least 3", checks)
	}
}

func TestThreadStartCreatedButUnownedIsIndeterminateAndNotRetried(t *testing.T) {
	projectDir := testNativeProjectDir(t)
	agent := &fakeStartAdapter{
		name:   string(adapters.AgentKimiCLI),
		probe:  adapters.ThreadStartCapability{Available: true},
		result: adapters.ThreadStartResult{SessionID: "native-kimi", Created: true},
	}
	coordinator, _ := newThreadStartTestCoordinator(t, agent, projectDir)
	coordinator.ownershipWait = 3 * time.Millisecond
	command := threadStartCommand{OperationID: "unknown", AgentType: adapters.AgentKimiCLI, ProjectDir: projectDir, Prompt: "hello"}
	first := collectStartEvents(coordinator, command)
	if len(first) != 2 || first[1].State != startjournal.StatusIndeterminate || first[1].SessionID != "native-kimi" {
		t.Fatalf("indeterminate events = %#v", first)
	}
	second := collectStartEvents(coordinator, command)
	if len(second) != 1 || second[0].State != startjournal.StatusIndeterminate {
		t.Fatalf("indeterminate replay = %#v", second)
	}
	if starts, _ := agent.counts(); starts != 1 {
		t.Fatalf("native start calls = %d; want 1", starts)
	}
}

func TestThreadStartConfirmedOwnershipWithoutPromptAckIsIndeterminate(t *testing.T) {
	projectDir := testNativeProjectDir(t)
	agent := &fakeStartAdapter{
		name:      string(adapters.AgentKimiCLI),
		probe:     adapters.ThreadStartCapability{Available: true},
		result:    adapters.ThreadStartResult{SessionID: "kimi_cli:native", Created: true},
		ownsAfter: 1,
	}
	coordinator, _ := newThreadStartTestCoordinator(t, agent, projectDir)
	command := threadStartCommand{OperationID: "owned-unconfirmed", AgentType: adapters.AgentKimiCLI, ProjectDir: projectDir, Prompt: "hello"}
	events := collectStartEvents(coordinator, command)
	if len(events) != 2 || events[1].State != startjournal.StatusIndeterminate || events[1].PromptAccepted {
		t.Fatalf("indeterminate unconfirmed events = %#v", events)
	}
	replayed := collectStartEvents(coordinator, command)
	if len(replayed) != 1 || replayed[0].State != startjournal.StatusIndeterminate || replayed[0].PromptAccepted {
		t.Fatalf("indeterminate unconfirmed replay = %#v", replayed)
	}
}

func TestThreadStartUsesUnionOfDiscoveredProjects(t *testing.T) {
	projectDir := testNativeProjectDir(t)
	agent := &fakeStartAdapter{
		name:      string(adapters.AgentClaudeCode),
		probe:     adapters.ThreadStartCapability{Available: true},
		result:    adapters.ThreadStartResult{SessionID: "claude-native", Created: true, PromptAccepted: true},
		ownsAfter: 1,
	}
	coordinator, _ := newThreadStartTestCoordinator(t, agent, projectDir)
	// The project has no agent association here: it is the union produced from
	// all discovered sessions, and the selected agent may start within it.
	events := collectStartEvents(coordinator, threadStartCommand{
		OperationID: "union",
		AgentType:   adapters.AgentClaudeCode,
		ProjectDir:  projectDir,
		Prompt:      "hello",
	})
	if len(events) != 2 || events[1].State != startjournal.StatusOwned {
		t.Fatalf("union events = %#v", events)
	}
}

func TestProjectDirNormalizationWindowsAndPOSIX(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		want      string
		wantError bool
	}{
		{name: "windows separators and case", value: `d:\Repo\Project\`, want: "windows:d:/repo/project"},
		{name: "windows traversal", value: `D:\Repo\..\Secret`, wantError: true},
		{name: "windows drive relative", value: `D:Repo`, wantError: true},
		{name: "unc", value: `\\Server\Share\Repo`, want: "windows:unc/server/share/repo"},
		{name: "device namespace", value: `\\.\C:\Repo`, wantError: true},
		{name: "posix", value: `/srv/Repo/`, want: "posix:/srv/Repo"},
		{name: "posix traversal", value: `/srv/../secret`, wantError: true},
		{name: "relative", value: `repo`, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeProjectDir(tt.value)
			if tt.wantError {
				if err == nil {
					t.Fatalf("normalizeProjectDir(%q) = %q, want error", tt.value, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("normalizeProjectDir(%q) = %q, %v; want %q", tt.value, got, err, tt.want)
			}
		})
	}
}

func TestParseStartAgentTypeRejectsMissingAndUnknown(t *testing.T) {
	if got, ok := parseStartAgentType(""); ok || got != "" {
		t.Fatalf("missing agent = %q, %v", got, ok)
	}
	if got, ok := parseStartAgentType("unknown"); ok || got != "" {
		t.Fatalf("unknown agent = %q, %v", got, ok)
	}
	if got, ok := parseStartAgentType("zcode"); !ok || got != adapters.AgentZCode {
		t.Fatalf("zcode agent = %q, %v", got, ok)
	}
	if got, ok := parseStartAgentType("cursor"); !ok || got != adapters.AgentCursor {
		t.Fatalf("cursor agent = %q, %v", got, ok)
	}
}

func TestStartCapabilityCatalogIncludesSpawnAndUnavailableReasons(t *testing.T) {
	agent := &fakeStartAdapter{
		name:  string(adapters.AgentClaudeCode),
		probe: adapters.ThreadStartCapability{Available: true},
	}
	registry, err := adapters.NewRegistry(agent)
	if err != nil {
		t.Fatal(err)
	}
	entries := newAgentStartCapabilityCache(time.Minute).Get(context.Background(), registry)
	alwaysAdvertised := 0
	for _, agentType := range supportedStartAgents {
		if advertiseStartCapability(agentType, nil) {
			alwaysAdvertised++
		}
	}
	if len(entries) != alwaysAdvertised {
		t.Fatalf("capability entries = %d, want %d", len(entries), alwaysAdvertised)
	}
	if startCapabilityFor(entries, adapters.AgentZCode) != nil || startCapabilityFor(entries, adapters.AgentCursor) != nil {
		t.Fatal("uninstalled zcode/cursor must stay out of start_capabilities")
	}
	for _, entry := range entries {
		available, _ := entry["available"].(bool)
		spawn, _ := entry["spawn"].(bool)
		if available != spawn {
			t.Fatalf("available/spawn mismatch: %#v", entry)
		}
		if !available {
			if reason, _ := entry["reason"].(string); reason == "" {
				t.Fatalf("unavailable capability has no reason: %#v", entry)
			}
		}
	}
}

func TestStartCapabilityCatalogIncludesInstalledZCodeAndCursor(t *testing.T) {
	zcode := &fakeStartAdapter{
		name:  string(adapters.AgentZCode),
		probe: adapters.ThreadStartCapability{Available: true, ControlPath: "cli"},
	}
	cursor := &fakeStartAdapter{
		name:  string(adapters.AgentCursor),
		probe: adapters.ThreadStartCapability{Available: true, ControlPath: "cli"},
	}
	registry, err := adapters.NewRegistry(zcode, cursor)
	if err != nil {
		t.Fatal(err)
	}
	cache := newAgentStartCapabilityCache(time.Minute)
	_ = cache.Get(context.Background(), registry)
	waitForStartCapabilityProbes(t, zcode, cursor)
	entries := cache.Get(context.Background(), registry)
	for _, agentType := range []adapters.AgentType{adapters.AgentZCode, adapters.AgentCursor} {
		entry := startCapabilityFor(entries, agentType)
		if entry == nil || entry["available"] != true || entry["spawn"] != true {
			t.Fatalf("%s entry = %#v", agentType, entry)
		}
	}
}

func TestThreadStartFailureBeforeCreationIsTerminalFailed(t *testing.T) {
	projectDir := testNativeProjectDir(t)
	agent := &fakeStartAdapter{
		name:     string(adapters.AgentClaudeCode),
		probe:    adapters.ThreadStartCapability{Available: true},
		startErr: errors.New("CLI missing"),
	}
	coordinator, _ := newThreadStartTestCoordinator(t, agent, projectDir)
	events := collectStartEvents(coordinator, threadStartCommand{
		OperationID: "failed",
		AgentType:   adapters.AgentClaudeCode,
		ProjectDir:  projectDir,
		Prompt:      "hello",
	})
	if len(events) != 2 || events[1].State != startjournal.StatusFailed || events[1].Message != "CLI missing" {
		t.Fatalf("failed events = %#v", events)
	}
}

func TestThreadStartOwnedSessionWithPromptErrorIsIndeterminate(t *testing.T) {
	projectDir := testNativeProjectDir(t)
	agent := &fakeStartAdapter{
		name:      string(adapters.AgentCodex),
		probe:     adapters.ThreadStartCapability{Available: true},
		result:    adapters.ThreadStartResult{SessionID: "codex-owned", Created: true},
		startErr:  errors.New("turn/start failed"),
		ownsAfter: 1,
	}
	coordinator, _ := newThreadStartTestCoordinator(t, agent, projectDir)
	events := collectStartEvents(coordinator, threadStartCommand{
		OperationID: "owned-prompt-unknown",
		AgentType:   adapters.AgentCodex,
		ProjectDir:  projectDir,
		Prompt:      "hello",
	})
	if len(events) != 2 || events[1].State != startjournal.StatusIndeterminate || events[1].PromptAccepted || events[1].SessionID != "codex-owned" || !strings.Contains(events[1].Message, "initial prompt was not") {
		t.Fatalf("indeterminate with unconfirmed prompt events = %#v", events)
	}
}

func TestCoalesceStartProjectDirRejectsConflictingLegacyCWD(t *testing.T) {
	if _, err := coalesceStartProjectDir(`D:\repo\one`, `D:\repo\two`); err == nil {
		t.Fatal("conflicting project_dir and cwd were accepted")
	}
	got, err := coalesceStartProjectDir(`D:\Repo\same`, `d:/repo/same/`)
	if err != nil || got != `D:\Repo\same` {
		t.Fatalf("equivalent project fields = %q, %v", got, err)
	}
}

func TestThreadStartPreallocatedIDBeforeCreationFailureIsTerminalFailed(t *testing.T) {
	projectDir := testNativeProjectDir(t)
	agent := &fakeStartAdapter{
		name:     string(adapters.AgentGrokBuild),
		probe:    adapters.ThreadStartCapability{Available: true},
		result:   adapters.ThreadStartResult{SessionID: "grok_build:preallocated"},
		startErr: errors.New("process did not start"),
	}
	coordinator, _ := newThreadStartTestCoordinator(t, agent, projectDir)
	events := collectStartEvents(coordinator, threadStartCommand{
		OperationID: "preallocated-failed",
		AgentType:   adapters.AgentGrokBuild,
		ProjectDir:  projectDir,
		Prompt:      "hello",
	})
	if len(events) != 2 || events[1].State != startjournal.StatusFailed || events[1].SessionID != "" || events[1].Message != "process did not start" {
		t.Fatalf("preallocated-id failure events = %#v", events)
	}
	if _, checks := agent.counts(); checks != 0 {
		t.Fatalf("ownership checks = %d; want 0 before the native boundary", checks)
	}
}
