package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCodexRollout(t *testing.T, dir, id string, meta map[string]interface{}) string {
	t.Helper()

	meta["id"] = id
	meta["session_id"] = id
	meta["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)

	lines := []map[string]interface{}{
		{
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			"type":      "session_meta",
			"payload":   meta,
		},
		{
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
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

	adapter.watcherMu.Lock()
	defer adapter.watcherMu.Unlock()
	if len(adapter.lastPaths) != 2 {
		t.Fatalf("lastPaths contains %d entries, want 2", len(adapter.lastPaths))
	}
	if _, ok := adapter.lastPaths[threadSourceID]; ok {
		t.Fatal("thread_source subagent leaked into lastPaths")
	}
	if _, ok := adapter.lastPaths[structuredSourceID]; ok {
		t.Fatal("structured-source subagent leaked into lastPaths")
	}
}
