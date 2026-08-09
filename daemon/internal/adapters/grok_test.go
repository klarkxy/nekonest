package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGrokDiscoverAndHistoryFixture(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "d%3A%5Crepo", "grok-1")
	mustMkdirAll(t, sessionDir)
	now := time.Now().UTC().Truncate(time.Second)
	mustWriteJSON(t, filepath.Join(sessionDir, "summary.json"), map[string]interface{}{
		"info": map[string]interface{}{
			"id":  "grok-1",
			"cwd": `D:\repo`,
		},
		"session_summary": "Grok fixture",
		"updated_at":      now.Format(time.RFC3339),
	})
	mustWriteLines(t, filepath.Join(sessionDir, "chat_history.jsonl"),
		`{"type":"user","content":"hidden primer"}`,
		`{"type":"user","synthetic_reason":"project_instructions","content":"secret instructions"}`,
		`{"id":"u1","type":"user","prompt_index":0,"content":"fallback hello","timestamp":"`+now.Format(time.RFC3339)+`"}`,
		`{"id":"a1","type":"assistant","content":[{"type":"text","text":"hi"}],"timestamp":"`+now.Format(time.RFC3339)+`"}`,
	)
	mustWriteLines(t, filepath.Join(sessionDir, "updates.jsonl"),
		`{"timestamp":`+fmt.Sprintf("%d", now.Unix())+`,"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","role":"system","content":{"type":"text","text":"system primer"}}}}`,
		`{"timestamp":`+fmt.Sprintf("%d", now.Unix())+`,"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","synthetic_reason":"project_instructions","content":{"type":"text","text":"synthetic primer"}}}}`,
		`{"timestamp":`+fmt.Sprintf("%d", now.Unix())+`,"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hello"}}}}`,
		`{"timestamp":`+fmt.Sprintf("%d", now.Unix())+`,"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"h"}}}}`,
		`{"timestamp":`+fmt.Sprintf("%d", now.Unix())+`,"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"i"}}}}`,
		`{"timestamp":`+fmt.Sprintf("%d", now.Unix())+`,"method":"session/update","params":{"update":{"sessionUpdate":"turn_completed"}}}`,
	)

	// Nested Grok workers are implementation details, not phone-visible sessions.
	subagentDir := filepath.Join(sessionDir, "subagents", "worker", "child")
	mustMkdirAll(t, subagentDir)
	mustWriteJSON(t, filepath.Join(subagentDir, "summary.json"), map[string]interface{}{
		"info":       map[string]interface{}{"id": "child"},
		"updated_at": now.Format(time.RFC3339),
	})

	adapter := NewGrokBuildAdapter()
	adapter.sessionsDir = root
	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	if sessions[0].ID != "grok_build:grok-1" {
		t.Fatalf("public id = %q", sessions[0].ID)
	}
	if sessions[0].ProjectDir != `D:\repo` {
		t.Fatalf("project dir = %q", sessions[0].ProjectDir)
	}
	if !adapter.OwnsSession("grok_build:grok-1") ||
		adapter.OwnsSession("grok_build:missing") ||
		adapter.OwnsSession("grok-1") {
		t.Fatal("Grok ownership check did not enforce store existence and namespace")
	}

	history, err := adapter.FetchHistory(sessions[0].ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Content != "hello" || history[1].Content != "hi" {
		t.Fatalf("history = %#v", history)
	}

	firstWatch, err := adapter.Watch(sessions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	secondWatch, err := adapter.Watch(sessions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	assertWatchClosed(t, firstWatch)
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	assertWatchClosed(t, secondWatch)
	adapter.watches.mu.Lock()
	watchCount := len(adapter.watches.entries)
	adapter.watches.mu.Unlock()
	if watchCount != 0 {
		t.Fatalf("Grok Close retained %d watches", watchCount)
	}
}

func TestGrokChatHistoryFallbackFiltersSyntheticPrimer(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "d%3A%5Crepo", "grok-fallback")
	mustMkdirAll(t, sessionDir)
	now := time.Now().UTC().Truncate(time.Second)
	mustWriteJSON(t, filepath.Join(sessionDir, "summary.json"), map[string]interface{}{
		"info":       map[string]interface{}{"id": "grok-fallback", "cwd": `D:\repo`},
		"updated_at": now.Format(time.RFC3339),
	})
	mustWriteLines(t, filepath.Join(sessionDir, "chat_history.jsonl"),
		`{"type":"user","content":"hidden primer"}`,
		`{"type":"user","synthetic_reason":"system_reminder","content":"hidden reminder"}`,
		`{"role":"system","content":"hidden system"}`,
		`{"message":{"role":"user","metadata":{"synthetic":true},"content":"nested synthetic"}}`,
		`{"type":"user","prompt_index":0,"content":"[grok-build-vscode primer v4]\n\n## HIDDEN PRIMER\n\nThis is a system message, not a user request."}`,
		`{"type":"assistant","content":"ok"}`,
		`{"type":"tool_result","content":"hidden tool result"}`,
		`{"type":"reasoning","content":"hidden reasoning"}`,
		`{"type":"assistant","content":"still hidden"}`,
		`{"type":"user","prompt_index":0,"content":"visible question"}`,
		`{"type":"assistant","content":"visible answer"}`,
	)

	adapter := NewGrokBuildAdapter()
	adapter.sessionsDir = root
	if _, err := adapter.Discover(); err != nil {
		t.Fatal(err)
	}
	history, err := adapter.FetchHistory("grok_build:grok-fallback", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 ||
		history[0].Content != "visible question" ||
		history[1].Content != "visible answer" {
		t.Fatalf("filtered fallback history = %#v", history)
	}
}

func TestGrokFiltersTopLevelSubagentAndGeneratedPrimer(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "d%3A%5Crepo")
	parentDir := filepath.Join(projectDir, "grok-parent")
	childDir := filepath.Join(projectDir, "grok-child")
	mustMkdirAll(t, parentDir)
	mustMkdirAll(t, childDir)
	now := time.Now().UTC().Truncate(time.Second)
	mustWriteJSON(t, filepath.Join(parentDir, "summary.json"), map[string]interface{}{
		"info":            map[string]interface{}{"id": "grok-parent", "cwd": `D:\repo`},
		"session_summary": "Plan Mode Exit Verdict Handling Primer",
		"updated_at":      now.Format(time.RFC3339),
	})
	mustWriteJSON(t, filepath.Join(childDir, "summary.json"), map[string]interface{}{
		"info":            map[string]interface{}{"id": "grok-child", "cwd": `D:\repo`},
		"session_summary": "hidden reviewer",
		"updated_at":      now.Format(time.RFC3339),
	})
	subagentMetaDir := filepath.Join(parentDir, "subagents", "grok-child")
	mustMkdirAll(t, subagentMetaDir)
	mustWriteJSON(
		t,
		filepath.Join(subagentMetaDir, "meta.json"),
		map[string]interface{}{
			"subagent_id":       "grok-child",
			"parent_session_id": "grok-parent",
			"child_session_id":  "grok-child",
		},
	)
	mustWriteLines(t, filepath.Join(parentDir, "updates.jsonl"),
		`{"timestamp":`+fmt.Sprintf("%d", now.Unix())+`,"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"[grok-build-vscode primer v4]\n\n## HIDDEN PRIMER\n\nThis is a system message, not a user request."},"_meta":{"promptIndex":0}}}}`,
		`{"timestamp":`+fmt.Sprintf("%d", now.Unix())+`,"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ok"}}}}`,
		`{"timestamp":`+fmt.Sprintf("%d", now.Unix())+`,"method":"session/update","params":{"update":{"sessionUpdate":"turn_completed"}}}`,
		`{"timestamp":`+fmt.Sprintf("%d", now.Unix())+`,"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"visible question"}}}}`,
		`{"timestamp":`+fmt.Sprintf("%d", now.Unix())+`,"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"visible answer"}}}}`,
		`{"timestamp":`+fmt.Sprintf("%d", now.Unix())+`,"method":"session/update","params":{"update":{"sessionUpdate":"turn_completed"}}}`,
	)

	adapter := NewGrokBuildAdapter()
	adapter.sessionsDir = root
	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "grok_build:grok-parent" {
		t.Fatalf("visible sessions = %#v", sessions)
	}
	if sessions[0].Summary != "grok-parent" {
		t.Fatalf("primer summary was exposed: %q", sessions[0].Summary)
	}
	if adapter.OwnsSession("grok_build:grok-child") {
		t.Fatal("top-level Grok subagent was treated as a user session")
	}
	history, err := adapter.FetchHistory("grok_build:grok-parent", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 ||
		history[0].Content != "visible question" ||
		history[1].Content != "visible answer" {
		t.Fatalf("filtered updates history = %#v", history)
	}
}

func TestGrokMarkerCachePrunesOutsideRecentCandidateSet(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "d%3A%5Crepo")
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-30 * 24 * time.Hour)
	for _, fixture := range []struct {
		id      string
		updated time.Time
	}{
		{id: "parent", updated: now},
		{id: "recent-child", updated: now},
		{id: "old-child", updated: old},
	} {
		dir := filepath.Join(projectDir, fixture.id)
		mustMkdirAll(t, dir)
		path := filepath.Join(dir, "summary.json")
		mustWriteJSON(t, path, map[string]interface{}{
			"info":       map[string]interface{}{"id": fixture.id, "cwd": `D:\repo`},
			"updated_at": fixture.updated.Format(time.RFC3339),
		})
		if err := os.Chtimes(path, fixture.updated, fixture.updated); err != nil {
			t.Fatal(err)
		}
	}
	for _, childID := range []string{"recent-child", "old-child"} {
		markerDir := filepath.Join(projectDir, "parent", "subagents", childID)
		mustMkdirAll(t, markerDir)
		mustWriteJSON(t, filepath.Join(markerDir, "meta.json"), map[string]interface{}{
			"parent_session_id": "parent", "child_session_id": childID,
		})
	}

	adapter := NewGrokBuildAdapter()
	adapter.sessionsDir = root
	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "grok_build:parent" {
		t.Fatalf("visible sessions = %#v", sessions)
	}
	_, _, entries := adapter.markerCache.stats()
	if entries != 1 {
		t.Fatalf("marker cache entries = %d, want only the recent child marker", entries)
	}

	recentSummary := filepath.Join(projectDir, "recent-child", "summary.json")
	if err := os.Chtimes(recentSummary, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Discover(); err != nil {
		t.Fatal(err)
	}
	_, _, entries = adapter.markerCache.stats()
	if entries != 0 {
		t.Fatalf("marker cache retained %d entries outside the recent candidate set", entries)
	}
}

func TestGrokHidesOldSessionButKeepsNativeOwnership(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "d%3A%5Cold-repo", "grok-old")
	mustMkdirAll(t, sessionDir)
	old := time.Now().UTC().Add(-90 * 24 * time.Hour).Truncate(time.Second)
	mustWriteJSON(t, filepath.Join(sessionDir, "summary.json"), map[string]interface{}{
		"info":       map[string]interface{}{"id": "grok-old", "cwd": `D:\old-repo`},
		"updated_at": old.Format(time.RFC3339),
	})

	adapter := NewGrokBuildAdapter()
	adapter.sessionsDir = root
	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("old session remained visible: %#v", sessions)
	}
	if !adapter.OwnsSession("grok_build:grok-old") {
		t.Fatal("old Grok session was not positively owned")
	}
	history, err := adapter.FetchHistory("grok_build:grok-old", 10)
	if err != nil {
		t.Fatalf("empty history returned error: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("empty history = %#v", history)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteJSON(t *testing.T, path string, value interface{}) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustWriteLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
