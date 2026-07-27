package adapters

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDecodeClaudeProjectDir(t *testing.T) {
	if decodeClaudeProjectDir("") != "" || decodeClaudeProjectDir(".") != "" {
		t.Fatal("empty")
	}
	got := decodeClaudeProjectDir("C--Users-admin-foo")
	if runtime.GOOS == "windows" {
		want := `C:\Users\admin\foo`
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	} else {
		if got != "C:"+string(filepath.Separator)+"Users"+string(filepath.Separator)+"admin"+string(filepath.Separator)+"foo" {
			t.Fatalf("got %q", got)
		}
	}
}

func TestContentToTextAndExtract(t *testing.T) {
	if contentToText("hi") != "hi" {
		t.Fatal("str")
	}
	blocks := []interface{}{
		map[string]interface{}{"type": "text", "text": "a"},
		map[string]interface{}{"type": "text", "text": "b"},
		map[string]interface{}{"type": "tool_use", "name": "Bash"},
	}
	if contentToText(blocks) != "a b" {
		t.Fatalf("%q", contentToText(blocks))
	}
	msg := map[string]interface{}{"content": blocks}
	if !hasToolUse(msg) {
		t.Fatal("tool")
	}
	if extractToolName(msg) != "Bash" {
		t.Fatal("name")
	}
	msg2 := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{
				"type": "tool_use",
				"name": "Read",
				"input": map[string]interface{}{
					"file_path": "/tmp/x.go",
				},
			},
		},
	}
	if extractToolDescription(msg2) != "Read: x.go" {
		t.Fatalf("%q", extractToolDescription(msg2))
	}
	bash := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{
				"type": "tool_use",
				"name": "Bash",
				"input": map[string]interface{}{
					"command": "echo hello world this is a very long command that should be truncated at eighty characters exactly!!",
				},
			},
		},
	}
	desc := extractToolDescription(bash)
	if len(desc) < 10 || desc[:4] != "Run:" {
		t.Fatalf("%q", desc)
	}
}

func TestIsReadOnlyTool(t *testing.T) {
	if !isReadOnlyTool("Read") || !isReadOnlyTool("grep") {
		t.Fatal("ro")
	}
	if isReadOnlyTool("Bash") {
		t.Fatal("bash")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("ab\ncd", 10) != "ab cd" {
		t.Fatal("nl")
	}
	if truncate("hello", 3) != "hel..." {
		t.Fatalf("%q", truncate("hello", 3))
	}
}

func TestParseMessageTime(t *testing.T) {
	tm, ok := parseMessageTime(float64(1700000000))
	if !ok || tm.Unix() != 1700000000 {
		t.Fatal("sec")
	}
	tm, ok = parseMessageTime(float64(1700000000000))
	if !ok || tm.UnixMilli() != 1700000000000 {
		t.Fatal("ms")
	}
	tm, ok = parseMessageTime("2024-01-02T03:04:05Z")
	if !ok || tm.Year() != 2024 {
		t.Fatal("rfc")
	}
	if _, ok := parseMessageTime(""); ok {
		t.Fatal("empty")
	}
	if _, ok := parseMessageTime(nil); ok {
		t.Fatal("nil")
	}
}

func TestCodexSessionIDFromFilename(t *testing.T) {
	name := "rollout-2026-07-25T18-02-04-019f98b9-787f-7573-9d8e-9d308b04aaf6.jsonl"
	id := codexSessionIDFromFilename(name)
	if id != "019f98b9-787f-7573-9d8e-9d308b04aaf6" {
		t.Fatalf("%q", id)
	}
	if codexSessionIDFromFilename("nope.jsonl") != "" {
		t.Fatal("bad")
	}
	if !looksLikeUUID("019f98b9-787f-7573-9d8e-9d308b04aaf6") {
		t.Fatal("uuid")
	}
	if looksLikeUUID("not-a-uuid") {
		t.Fatal("not uuid")
	}
}

func TestParseCodexTimestamp(t *testing.T) {
	if parseCodexTimestamp(float64(100)).Unix() != 100 {
		t.Fatal("sec")
	}
	if parseCodexTimestamp(float64(1e13)).IsZero() {
		t.Fatal("ms")
	}
	if parseCodexTimestamp("2024-06-01T12:00:00Z").Year() != 2024 {
		t.Fatal("str")
	}
	if !parseCodexTimestamp("").Equal(time.Time{}) {
		t.Fatal("empty")
	}
}

func TestExtractCodexContent(t *testing.T) {
	if extractCodexContent(map[string]interface{}{"content": "x"}) != "x" {
		t.Fatal("str")
	}
	if extractCodexContent(map[string]interface{}{"message": "m"}) != "m" {
		t.Fatal("msg")
	}
	m := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"text": "a"},
			map[string]interface{}{"text": "b"},
		},
	}
	if extractCodexContent(m) != "a b" {
		t.Fatalf("%q", extractCodexContent(m))
	}
}
