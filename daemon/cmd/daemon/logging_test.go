package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/nekonest/daemon/internal/opslog"
)

func TestRejectedFrameLogRedactsUnknownWireValues(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(opslog.New(&output, opslog.Config{Format: "json", Level: slog.LevelDebug}))
	t.Cleanup(func() { slog.SetDefault(previous) })
	hostile := "prompt-sentinel C:\\Users\\path-sentinel?token=secret-sentinel"
	opslog.Warn("daemon.main", "frame_rejected", "frame rejected", "message_type", safeInboundMessageTypeForLog(hostile))
	if strings.Contains(output.String(), "prompt-sentinel") || strings.Contains(output.String(), "path-sentinel") || strings.Contains(output.String(), "secret-sentinel") {
		t.Fatalf("hostile wire value leaked: %q", output.String())
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["message_type"] != "unknown" {
		t.Fatalf("message_type=%v want unknown", record["message_type"])
	}
}

func TestEmptyPromptRejectionLogRedactsHostileWireIdentifiers(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(opslog.New(&output, opslog.Config{Format: "json", Level: slog.LevelDebug}))
	t.Cleanup(func() { slog.SetDefault(previous) })

	sessionSentinel := "session\npath-sentinel?token=secret-sentinel"
	messageSentinel := "message\tprompt-sentinel"
	logEmptyPromptRejected(sessionSentinel, messageSentinel)

	if strings.Contains(output.String(), "path-sentinel") || strings.Contains(output.String(), "secret-sentinel") || strings.Contains(output.String(), "prompt-sentinel") {
		t.Fatalf("hostile wire identifier leaked: %q", output.String())
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["event"] != "prompt_rejected_empty" || !strings.HasPrefix(record["session_id"].(string), "id:") || !strings.HasPrefix(record["client_msg_id"].(string), "id:") {
		t.Fatalf("unexpected sanitized record: %#v", record)
	}
}
