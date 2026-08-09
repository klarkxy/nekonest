package adapters

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseKimiTextPartPreservesFragmentWhitespace(t *testing.T) {
	var item map[string]interface{}
	if err := json.Unmarshal(
		[]byte(`{"message":{"type":"TextPart","payload":{"text":"current "}}}`),
		&item,
	); err != nil {
		t.Fatal(err)
	}
	role, content, _, _, eventType := parseKimiHistoryItem(item)
	if role != "assistant" || content != "current " {
		t.Fatalf("fragment = %q %q", role, content)
	}
	if eventType != "textpart" {
		t.Fatalf("event type = %q", eventType)
	}
}

func TestKimiDiscoversCurrentAndLegacyFixtures(t *testing.T) {
	root := t.TempDir()
	currentHome := filepath.Join(root, ".kimi-code")
	legacyHome := filepath.Join(root, "legacy-share")
	t.Setenv("KIMI_CODE_HOME", currentHome)
	t.Setenv("KIMI_SHARE_DIR", legacyHome)
	now := time.Now().UTC().Truncate(time.Second)

	currentDir := filepath.Join(currentHome, "sessions", "work-key", "current-1")
	mustMkdirAll(t, filepath.Join(currentDir, "agents", "main"))
	mustWriteLines(t, filepath.Join(currentHome, "session_index.jsonl"),
		`{"id":"current-1","workDirKey":"work-key","workDir":"D:\\current","title":"Current Kimi","updatedAt":"`+now.Format(time.RFC3339)+`"}`,
	)
	mustWriteJSON(t, filepath.Join(currentDir, "state.json"), map[string]interface{}{
		"id":           "current-1",
		"custom_title": "Current Kimi",
	})
	mustWriteLines(t, filepath.Join(currentDir, "agents", "main", "wire.jsonl"),
		`{"type":"metadata","protocol_version":"1.0"}`,
		`{"timestamp":`+fmt.Sprintf("%f", float64(now.Unix()))+`,"message":{"type":"TurnBegin","payload":{"user_input":"current question"}}}`,
		`{"timestamp":`+fmt.Sprintf("%f", float64(now.Unix()))+`,"message":{"type":"TextPart","payload":{"text":"current "}}}`,
		`{"timestamp":`+fmt.Sprintf("%f", float64(now.Unix()))+`,"message":{"type":"TextPart","payload":{"text":"answer"}}}`,
		`{"timestamp":`+fmt.Sprintf("%f", float64(now.Unix()))+`,"message":{"type":"TurnEnd","payload":{}}}`,
		`{"timestamp":`+fmt.Sprintf("%f", float64(now.Unix()))+`,"message":{"type":"TurnBegin","payload":{"user_input":"next question"}}}`,
		`{"timestamp":`+fmt.Sprintf("%f", float64(now.Unix()))+`,"message":{"type":"TextPart","payload":{"text":"next "}}}`,
		`{"timestamp":`+fmt.Sprintf("%f", float64(now.Unix()))+`,"message":{"type":"TextPart","payload":{"text":"answer"}}}`,
		`{"timestamp":`+fmt.Sprintf("%f", float64(now.Unix()))+`,"message":{"type":"TurnEnd","payload":{}}}`,
		`{"type":"context.append_message","message":{"role":"user","content":[{"type":"text","text":"v2 question"}],"toolCalls":[],"id":"v2-user"},"time":`+fmt.Sprintf("%d", now.UnixMilli())+`}`,
		`{"type":"context.append_loop_event","event":{"type":"step.begin","uuid":"v2-step"},"time":`+fmt.Sprintf("%d", now.UnixMilli())+`}`,
		`{"type":"context.append_loop_event","event":{"type":"content.part","stepUuid":"v2-step","part":{"type":"think","think":"hidden reasoning"}},"time":`+fmt.Sprintf("%d", now.UnixMilli())+`}`,
		`{"type":"context.append_loop_event","event":{"type":"content.part","stepUuid":"v2-step","part":{"type":"text","text":"v2 "}},"time":`+fmt.Sprintf("%d", now.UnixMilli())+`}`,
		`{"type":"context.append_loop_event","event":{"type":"tool.call","stepUuid":"v2-step","toolCallId":"call-1","name":"Read"},"time":`+fmt.Sprintf("%d", now.UnixMilli())+`}`,
		`{"type":"context.append_loop_event","event":{"type":"tool.result","toolCallId":"call-1","result":{"output":"done"}},"time":`+fmt.Sprintf("%d", now.UnixMilli())+`}`,
		`{"type":"context.append_loop_event","event":{"type":"content.part","stepUuid":"v2-step","part":{"type":"text","text":"answer"}},"time":`+fmt.Sprintf("%d", now.UnixMilli())+`}`,
		`{"type":"context.append_loop_event","event":{"type":"step.end","uuid":"v2-step"},"time":`+fmt.Sprintf("%d", now.UnixMilli())+`}`,
	)

	legacyProject := `D:\legacy`
	sum := md5.Sum([]byte(legacyProject))
	legacyKey := hex.EncodeToString(sum[:])
	legacyDir := filepath.Join(legacyHome, "sessions", legacyKey, "legacy-1")
	mustMkdirAll(t, legacyDir)
	mustWriteJSON(t, filepath.Join(legacyHome, "kimi.json"), map[string]interface{}{
		"work_dirs": []interface{}{
			map[string]interface{}{"path": legacyProject, "kaos": "local"},
		},
	})
	mustWriteJSON(t, filepath.Join(legacyDir, "state.json"), map[string]interface{}{
		"custom_title": "Legacy Kimi",
	})
	mustWriteLines(t, filepath.Join(legacyDir, "context.jsonl"),
		`{"id":"lu","role":"user","content":"legacy question"}`,
		`{"id":"la","role":"assistant","content":"legacy answer"}`,
	)

	archivedDir := filepath.Join(legacyHome, "sessions", legacyKey, "archived")
	mustMkdirAll(t, archivedDir)
	mustWriteJSON(t, filepath.Join(archivedDir, "state.json"), map[string]interface{}{
		"archived": true,
	})

	adapter := NewKimiCLIAdapter()
	if len(adapter.legacyHomes) == 0 || filepath.Clean(adapter.legacyHomes[0]) != filepath.Clean(legacyHome) {
		t.Fatalf("KIMI_SHARE_DIR not prioritized: %#v", adapter.legacyHomes)
	}
	adapter.legacyHomes = []string{legacyHome}
	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %#v, want current and legacy", sessions)
	}

	byID := make(map[string]*SessionInfo)
	for _, session := range sessions {
		byID[session.ID] = session
	}
	if byID["kimi_cli:current-1"] == nil || byID["kimi_cli:legacy-1"] == nil {
		t.Fatalf("prefixed sessions = %#v", byID)
	}
	if byID["kimi_cli:legacy-1"].ProjectDir != legacyProject {
		t.Fatalf("legacy project dir = %q", byID["kimi_cli:legacy-1"].ProjectDir)
	}
	if !adapter.OwnsSession("kimi_cli:current-1") ||
		!adapter.OwnsSession("kimi_cli:legacy-1") ||
		adapter.OwnsSession("kimi_cli:missing") ||
		adapter.OwnsSession("current-1") {
		t.Fatal("Kimi ownership check did not enforce store existence and namespace")
	}

	currentHistory, err := adapter.FetchHistory("kimi_cli:current-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(currentHistory) != 6 ||
		currentHistory[0].Content != "current question" ||
		currentHistory[1].Content != "current answer" ||
		currentHistory[2].Content != "next question" ||
		currentHistory[3].Content != "next answer" ||
		currentHistory[4].Content != "v2 question" ||
		currentHistory[5].Content != "v2 answer" {
		var got []string
		for _, message := range currentHistory {
			got = append(got, message.Role+":"+message.Content)
		}
		t.Fatalf("current history = %#v", got)
	}
	if currentHistory[4].Timestamp != now.Unix() || currentHistory[5].Timestamp != now.Unix() {
		t.Fatalf("v2 epoch-ms timestamps = %d, %d", currentHistory[4].Timestamp, currentHistory[5].Timestamp)
	}

	history, err := adapter.FetchHistory("kimi_cli:legacy-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Content != "legacy question" || history[1].Content != "legacy answer" {
		t.Fatalf("legacy history = %#v", history)
	}

	firstWatch, err := adapter.Watch("kimi_cli:current-1")
	if err != nil {
		t.Fatal(err)
	}
	secondWatch, err := adapter.Watch("kimi_cli:current-1")
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
		t.Fatalf("Kimi Close retained %d watches", watchCount)
	}
}

func TestKimiHidesOldCurrentSessionButKeepsNativeOwnership(t *testing.T) {
	root := t.TempDir()
	currentHome := filepath.Join(root, ".kimi-code")
	sessionDir := filepath.Join(currentHome, "sessions", "work-key", "old-session")
	mustMkdirAll(t, sessionDir)
	old := time.Now().UTC().Add(-90 * 24 * time.Hour).Truncate(time.Second)
	statePath := filepath.Join(sessionDir, "state.json")
	mustWriteJSON(t, statePath, map[string]interface{}{
		"id":           "old-session",
		"custom_title": "Old Kimi",
		"updated_at":   old.Format(time.RFC3339),
	})
	if err := os.Chtimes(statePath, old, old); err != nil {
		t.Fatal(err)
	}

	adapter := NewKimiCLIAdapter()
	adapter.currentHome = currentHome
	adapter.legacyHomes = nil
	sessions, err := adapter.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("old Kimi session remained visible: %#v", sessions)
	}
	if !adapter.OwnsSession("kimi_cli:old-session") {
		t.Fatal("old Kimi session was not positively owned")
	}
}
