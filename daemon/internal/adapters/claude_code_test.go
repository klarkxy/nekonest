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

	adapter.watcherMu.Lock()
	defer adapter.watcherMu.Unlock()
	if len(adapter.lastPaths) != 1 {
		t.Fatalf("lastPaths contains %d entries, want 1", len(adapter.lastPaths))
	}
	if _, ok := adapter.lastPaths[sidechainID]; ok {
		t.Fatal("nested subagent leaked into lastPaths")
	}
	if _, ok := adapter.lastPaths[topLevelSidechainID]; ok {
		t.Fatal("metadata-marked subagent leaked into lastPaths")
	}
}
