package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeClaudeTranscript(t *testing.T, path string, records ...map[string]interface{}) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	for _, record := range records {
		if _, ok := record["timestamp"]; !ok {
			record["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if err := encoder.Encode(record); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestIsClaudeSubagentRecord(t *testing.T) {
	tests := []struct {
		name   string
		record map[string]interface{}
		want   bool
	}{
		{
			name:   "explicit sidechain agent",
			record: map[string]interface{}{"isSidechain": true, "agentId": "agent-1"},
			want:   true,
		},
		{
			name:   "main transcript",
			record: map[string]interface{}{"isSidechain": false, "agentId": ""},
		},
		{
			name:   "sidechain without agent id fails open",
			record: map[string]interface{}{"isSidechain": true},
		},
		{
			name:   "agent id without sidechain fails open",
			record: map[string]interface{}{"agentId": "agent-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isClaudeSubagentRecord(tt.record); got != tt.want {
				t.Fatalf("isClaudeSubagentRecord() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaudeDiscoverExcludesSubagents(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "D--nekonest")
	mainID := "22ed6069-1ecd-4b37-9faa-25964612dad0"
	sidechainID := "agent-a256850a5fc06544c"
	topLevelSidechainID := "agent-future-layout"
	oldRootID := "old-visible-root"

	writeClaudeTranscript(t, filepath.Join(project, mainID+".jsonl"), map[string]interface{}{
		"type":        "assistant",
		"isSidechain": false,
		"sessionId":   mainID,
		"cwd":         `D:\nekonest`,
		"message": map[string]interface{}{
			"role":    "assistant",
			"content": "ready",
		},
	})
	writeClaudeTranscript(t, filepath.Join(project, mainID, "subagents", sidechainID+".jsonl"), map[string]interface{}{
		"type":        "user",
		"isSidechain": true,
		"agentId":     sidechainID,
		"sessionId":   mainID,
		"message": map[string]interface{}{
			"role":    "user",
			"content": "child task",
		},
	})
	writeClaudeTranscript(t, filepath.Join(project, topLevelSidechainID+".jsonl"), map[string]interface{}{
		"type":        "user",
		"isSidechain": true,
		"agentId":     topLevelSidechainID,
		"sessionId":   mainID,
		"message": map[string]interface{}{
			"role":    "user",
			"content": "future child task",
		},
	})
	oldTime := time.Now().UTC().Add(-30 * 24 * time.Hour)
	oldRootPath := filepath.Join(project, oldRootID+".jsonl")
	writeClaudeTranscript(t, oldRootPath, map[string]interface{}{
		"type":      "assistant",
		"sessionId": oldRootID,
		"timestamp": oldTime.Format(time.RFC3339Nano),
		"message": map[string]interface{}{
			"role":    "assistant",
			"content": "old but visible",
		},
	})
	if err := os.Chtimes(oldRootPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	adapter := NewClaudeCodeAdapter()
	adapter.projectsDir = root

	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("Discover() returned %d sessions, want 1", len(sessions))
	}
	if sessions[0].ID != mainID {
		t.Fatalf("Discover() returned %q, want %q", sessions[0].ID, mainID)
	}
	if !adapter.OwnsSession(mainID) ||
		!adapter.OwnsSession(oldRootID) ||
		adapter.OwnsSession(sidechainID) ||
		adapter.OwnsSession(topLevelSidechainID) {
		t.Fatal("Claude ownership leaked a subagent or rejected the main session")
	}
	if _, err := adapter.FetchHistory(mainID, 10); err != nil {
		t.Fatalf("main history: %v", err)
	}
	if _, err := adapter.FetchHistory(oldRootID, 10); err != nil {
		t.Fatalf("old root history: %v", err)
	}
	for _, hiddenID := range []string{sidechainID, topLevelSidechainID} {
		if _, err := adapter.FetchHistory(hiddenID, 10); err == nil {
			t.Fatalf("FetchHistory exposed Claude subagent %q", hiddenID)
		}
	}

	adapter.watcherMu.Lock()
	defer adapter.watcherMu.Unlock()
	if len(adapter.lastPaths) != 2 {
		t.Fatalf("lastPaths contains %d entries, want 2 visible roots", len(adapter.lastPaths))
	}
	if _, ok := adapter.lastPaths[sidechainID]; ok {
		t.Fatal("nested subagent leaked into lastPaths")
	}
	if _, ok := adapter.lastPaths[topLevelSidechainID]; ok {
		t.Fatal("metadata-marked subagent leaked into lastPaths")
	}
}

func TestClaudeDiscoverColdStartKeepsOldPendingApproval(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "D--old-project")
	sessionID := "old-pending-approval"
	path := filepath.Join(project, sessionID+".jsonl")
	old := time.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Second)
	writeClaudeTranscript(t, path,
		map[string]interface{}{
			"type": "assistant", "cwd": `D:\old-project`, "timestamp": old.Format(time.RFC3339Nano),
			"message": map[string]interface{}{"role": "assistant", "content": "waiting"},
		},
		map[string]interface{}{
			"type": "assistant", "timestamp": old.Format(time.RFC3339Nano),
			"message": map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{map[string]interface{}{
					"type": "tool_use", "name": "Bash",
					"input": map[string]interface{}{"command": "echo waiting"},
				}},
			},
		},
	)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	adapter := NewClaudeCodeAdapter()
	adapter.projectsDir = root
	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != sessionID ||
		sessions[0].Status != StatusWaitingApproval || sessions[0].PendingApproval == nil {
		t.Fatalf("cold-start Claude attention session = %#v", sessions)
	}
	beforeHits, _, entries := adapter.attentionCache.stats()
	if entries != 1 {
		t.Fatalf("attention cache entries = %d, want 1", entries)
	}
	if _, err := adapter.Discover(); err != nil {
		t.Fatal(err)
	}
	afterHits, _, _ := adapter.attentionCache.stats()
	if afterHits-beforeHits != 1 {
		t.Fatalf("second discovery attention cache hits = %d, want 1", afterHits-beforeHits)
	}
}
