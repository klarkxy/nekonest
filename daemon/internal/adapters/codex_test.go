package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nekonest/daemon/internal/agentexec"
)

func TestCodexAppServerOutputSinkAccumulatesStableMessage(t *testing.T) {
	adapter := NewCodexAdapter()
	adapter.appServer.RegisterThreadIDs("thread-1", "session-1", "wire-1")
	var events []OutputEvent
	adapter.SetOutputSink(func(event OutputEvent) {
		events = append(events, event)
	})

	adapter.handleAppServerNotification(
		"item/agentMessage/delta",
		json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","delta":"hel"}`),
	)
	adapter.handleAppServerNotification(
		"item/agentMessage/delta",
		json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","delta":"lo"}`),
	)
	adapter.handleAppServerNotification(
		"item/completed",
		json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","item":{"id":"item-1","type":"agentMessage","text":"hello"}}`),
	)

	if len(events) != 3 {
		t.Fatalf("events=%#v", events)
	}
	for _, event := range events {
		if event.SessionID != "wire-1" || event.AgentType != AgentCodex || event.Type != "assistant" || event.MessageID != "item-1" {
			t.Fatalf("event=%#v", event)
		}
	}
	if events[0].Content != "hel" || events[1].Content != "hello" || events[2].Content != "hello" {
		t.Fatalf("events=%#v", events)
	}
}

func TestCodexAppServerRequestEmitsStructuredUserInputImmediately(t *testing.T) {
	adapter := NewCodexAdapter()
	adapter.appServer.RegisterThreadIDs("thread-1", "session-1", "wire-1")
	var events []ControlEvent
	adapter.SetControlSink(func(event ControlEvent) {
		events = append(events, event)
	})
	req := agentexec.ServerRequest{
		ID:     "request-1",
		Method: "item/tool/requestUserInput",
		Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","isBlocking":true,"questions":[{"id":"choice","header":"Choice","question":"Pick one","options":[{"label":"Alpha","description":"First"}]}]}`),
	}
	if pending := adapter.appServer.TrackUserInput(req); pending == nil {
		t.Fatal("current requestUserInput schema was not tracked")
	}
	adapter.handleAppServerRequest(req)
	if len(events) != 1 || events[0].SessionID != "wire-1" || events[0].Status != StatusWaitingUser || events[0].PendingUserInput == nil {
		t.Fatalf("structured user input events = %#v", events)
	}
	if got := events[0].PendingUserInput.Questions; len(got) != 1 || got[0].ID != "choice" {
		t.Fatalf("structured questions = %#v", got)
	}
}

func TestApplyAppServerTerminalStatus(t *testing.T) {
	for _, test := range []struct {
		status string
		want   AgentStatus
	}{
		{status: "completed", want: StatusIdle},
		{status: "interrupted", want: StatusIdle},
		{status: "failed", want: StatusError},
		{status: "inProgress", want: StatusRunning},
	} {
		session := &SessionInfo{Status: StatusRunning}
		applyAppServerTerminalStatus(session, test.status)
		if session.Status != test.want {
			t.Fatalf("status %q produced %q, want %q", test.status, session.Status, test.want)
		}
	}
}

func TestCodexTurnAbortedClearsRunningStatus(t *testing.T) {
	dir := t.TempDir()
	sessionID := "019fbd16-6879-7c31-9051-888f4751859a"
	path := writeCodexRollout(t, dir, sessionID, map[string]interface{}{
		"thread_source": "user",
		"source":        "appServer",
		"cwd":           `D:\nekonest`,
	})
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	for _, payload := range []map[string]interface{}{
		{"type": "task_started"},
		{"type": "approval_request", "tool_name": "command", "description": "sleep"},
		{"type": "turn_aborted", "turn_id": "turn-1", "reason": "interrupted"},
	} {
		if err := encoder.Encode(map[string]interface{}{
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload":   payload,
		}); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewCodexAdapter()
	session, err := adapter.parseRolloutFile(path, info)
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != StatusIdle || session.PendingApproval != nil {
		t.Fatalf("aborted session status=%q pending=%#v", session.Status, session.PendingApproval)
	}
}

func TestCodexStaleTaskStartedReturnsIdle(t *testing.T) {
	dir := t.TempDir()
	sessionID := "019fbd3e-41a9-7771-86f7-be462ff76824"
	path := writeCodexRollout(t, dir, sessionID, map[string]interface{}{
		"thread_source": "user",
		"source":        "appServer",
		"cwd":           `D:\nekonest`,
	})
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	stale := time.Now().UTC().Add(-codexOrphanTaskInactivityTimeout - time.Minute)
	if err := json.NewEncoder(file).Encode(map[string]interface{}{
		"timestamp": stale.Format(time.RFC3339Nano),
		"type":      "event_msg",
		"payload":   map[string]interface{}{"type": "task_started", "turn_id": "turn-stale"},
	}); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewCodexAdapter()
	session, err := adapter.parseRolloutFile(path, info)
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != StatusIdle {
		t.Fatalf("stale task status=%q", session.Status)
	}
}

func TestCodexLongTaskWithRecentActivityStaysRunning(t *testing.T) {
	dir := t.TempDir()
	sessionID := "019fbd3e-41a9-7771-86f7-be462ff76825"
	started := time.Now().UTC().Add(-codexOrphanTaskInactivityTimeout - time.Minute)
	path := writeCodexRolloutAt(t, dir, sessionID, map[string]interface{}{
		"thread_source": "user",
		"source":        "appServer",
		"cwd":           `D:\nekonest`,
	}, started)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	for _, event := range []map[string]interface{}{
		{
			"timestamp": started.Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload":   map[string]interface{}{"type": "task_started", "turn_id": "turn-long"},
		},
		{
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			"type":      "response_item",
			"payload":   map[string]interface{}{"type": "reasoning", "summary": []string{"still working"}},
		},
	} {
		if err := encoder.Encode(event); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewCodexAdapter()
	session, err := adapter.parseRolloutFile(path, info)
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != StatusRunning {
		t.Fatalf("long task with recent activity status=%q", session.Status)
	}
}

func writeCodexRollout(t *testing.T, dir, id string, meta map[string]interface{}) string {
	t.Helper()
	return writeCodexRolloutAt(t, dir, id, meta, time.Now().UTC())
}

func writeCodexRolloutAt(
	t *testing.T,
	dir, id string,
	meta map[string]interface{},
	at time.Time,
) string {
	t.Helper()

	meta["id"] = id
	meta["session_id"] = id
	meta["timestamp"] = at.Format(time.RFC3339Nano)

	lines := []map[string]interface{}{
		{
			"timestamp": at.Format(time.RFC3339Nano),
			"type":      "session_meta",
			"payload":   meta,
		},
		{
			"timestamp": at.Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload": map[string]interface{}{
				"type":    "agent_message",
				"message": "ready",
			},
		},
	}

	path := filepath.Join(dir, "rollout-2026-07-27T23-00-00-"+id+".jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	for _, line := range lines {
		if err := encoder.Encode(line); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIsCodexSubagentSessionMeta(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]interface{}
		want    bool
	}{
		{
			name:    "thread source",
			payload: map[string]interface{}{"thread_source": "subagent"},
			want:    true,
		},
		{
			name: "structured source",
			payload: map[string]interface{}{
				"source": map[string]interface{}{"subagent": map[string]interface{}{"thread_spawn": map[string]interface{}{}}},
			},
			want: true,
		},
		{
			name:    "string source compatibility",
			payload: map[string]interface{}{"source": "subagent"},
			want:    true,
		},
		{
			name:    "main vscode session",
			payload: map[string]interface{}{"thread_source": "user", "source": "vscode"},
		},
		{
			name: "lineage alone is not a subagent",
			payload: map[string]interface{}{
				"parent_thread_id": "parent",
				"forked_from_id":   "fork",
				"agent_path":       "/root/worker",
			},
		},
		{
			name:    "unknown metadata fails open",
			payload: map[string]interface{}{"thread_source": "future", "source": 42},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCodexSubagentSessionMeta(tt.payload); got != tt.want {
				t.Fatalf("isCodexSubagentSessionMeta() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCodexDiscoverExcludesSubagents(t *testing.T) {
	dir := t.TempDir()
	mainID := "019fa2b6-a8da-7431-a47a-5134638ac09c"
	lineageID := "019fa2b7-231d-7983-a157-c80c8f12b181"
	threadSourceID := "019fa2b7-5a7f-7a93-b6d9-f37fc8ca2672"
	structuredSourceID := "019fa2b7-9c42-7ae3-aabd-b147b2b2dc65"
	oldRootID := "019fa2b7-c09d-74bf-b5cb-f08f1dd2a51e"

	writeCodexRollout(t, dir, mainID, map[string]interface{}{
		"thread_source": "user",
		"source":        "vscode",
		"cwd":           `D:\nekonest`,
	})
	writeCodexRollout(t, dir, lineageID, map[string]interface{}{
		"source":           "vscode",
		"parent_thread_id": mainID,
		"forked_from_id":   "fork",
	})
	writeCodexRollout(t, dir, threadSourceID, map[string]interface{}{
		"thread_source":    "subagent",
		"parent_thread_id": mainID,
	})
	writeCodexRollout(t, dir, structuredSourceID, map[string]interface{}{
		"source": map[string]interface{}{
			"subagent": map[string]interface{}{"other": "guardian"},
		},
	})
	writeCodexRolloutAt(t, dir, oldRootID, map[string]interface{}{
		"thread_source": "user",
		"source":        "vscode",
	}, time.Now().UTC().Add(-30*24*time.Hour))

	adapter := NewCodexAdapter()
	adapter.sessionsDir = dir

	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("Discover() returned %d sessions, want 2", len(sessions))
	}

	got := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		got[session.ID] = true
	}
	if !got[mainID] || !got[lineageID] {
		t.Fatalf("Discover() kept sessions %#v, want main and lineage-only sessions", got)
	}
	if got[threadSourceID] || got[structuredSourceID] {
		t.Fatalf("Discover() leaked subagents %#v", got)
	}
	if !adapter.OwnsSession(mainID) ||
		!adapter.OwnsSession(lineageID) ||
		!adapter.OwnsSession(oldRootID) ||
		adapter.OwnsSession(threadSourceID) ||
		adapter.OwnsSession(structuredSourceID) {
		t.Fatal("Codex ownership leaked a subagent or rejected a visible session")
	}
	if _, err := adapter.FetchHistory(mainID, 10); err != nil {
		t.Fatalf("main history: %v", err)
	}
	if _, err := adapter.FetchHistory(oldRootID, 10); err != nil {
		t.Fatalf("old root history: %v", err)
	}
	for _, hiddenID := range []string{threadSourceID, structuredSourceID} {
		if _, err := adapter.FetchHistory(hiddenID, 10); err == nil {
			t.Fatalf("FetchHistory exposed Codex subagent %q", hiddenID)
		}
	}

	adapter.watcherMu.Lock()
	defer adapter.watcherMu.Unlock()
	if len(adapter.lastPaths) != 3 {
		t.Fatalf("lastPaths contains %d entries, want 3 visible roots", len(adapter.lastPaths))
	}
	if _, ok := adapter.lastPaths[threadSourceID]; ok {
		t.Fatal("thread_source subagent leaked into lastPaths")
	}
	if _, ok := adapter.lastPaths[structuredSourceID]; ok {
		t.Fatal("structured-source subagent leaked into lastPaths")
	}
}

func TestCodexDiscoverColdStartKeepsOldAttentionSessions(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Second)
	approvalID := "019fa2b8-1111-7111-8111-111111111111"
	runningID := "019fa2b8-2222-7222-8222-222222222222"
	userInputID := "019fa2b8-3333-7333-8333-333333333333"

	approvalPath := writeCodexRolloutAt(t, dir, approvalID, map[string]interface{}{
		"thread_source": "user", "cwd": `D:\old-approval`,
	}, old)
	file, err := os.OpenFile(approvalPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(file).Encode(map[string]interface{}{
		"timestamp": old.Format(time.RFC3339Nano),
		"type":      "event_msg",
		"payload": map[string]interface{}{
			"type": "approval_request", "tool_name": "shell", "description": "old approval",
		},
	}); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(approvalPath, old, old); err != nil {
		t.Fatal(err)
	}
	writeCodexRolloutAt(t, dir, runningID, map[string]interface{}{
		"thread_source": "user", "cwd": `D:\old-running`,
	}, old)
	writeCodexRolloutAt(t, dir, userInputID, map[string]interface{}{
		"thread_source": "user", "cwd": `D:\old-input`,
	}, old)

	adapter := NewCodexAdapter()
	adapter.sessionsDir = dir
	adapter.appServer.RegisterThreadIDs(runningID, runningID, runningID)
	adapter.appServer.SetActiveTurn(runningID, "turn-running")
	adapter.appServer.RegisterThreadIDs(userInputID, userInputID, userInputID)
	if pending := adapter.appServer.TrackUserInput(agentexec.ServerRequest{
		ID: "input-1", Method: "item/tool/requestUserInput",
		Params: json.RawMessage(`{"threadId":"` + userInputID + `","turnId":"turn-input","itemId":"item-input","questions":[{"id":"choice","header":"Mode","question":"Pick one","options":[{"label":"Safe","description":"recommended"}]}]}`),
	}); pending == nil {
		t.Fatal("failed to register pending user input")
	}

	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	statuses := make(map[string]AgentStatus, len(sessions))
	for _, session := range sessions {
		statuses[session.ID] = session.Status
	}
	if statuses[approvalID] != StatusWaitingApproval ||
		statuses[runningID] != StatusRunning ||
		statuses[userInputID] != StatusWaitingUser {
		t.Fatalf("cold-start attention statuses = %#v", statuses)
	}
	_, _, entries := adapter.attentionCache.stats()
	if entries != 3 {
		t.Fatalf("attention cache entries = %d, want 3", entries)
	}
	beforeHits, _, _ := adapter.attentionCache.stats()
	if _, err := adapter.Discover(); err != nil {
		t.Fatal(err)
	}
	afterHits, _, _ := adapter.attentionCache.stats()
	if afterHits-beforeHits != 3 {
		t.Fatalf("second discovery attention cache hits = %d, want 3", afterHits-beforeHits)
	}
}
