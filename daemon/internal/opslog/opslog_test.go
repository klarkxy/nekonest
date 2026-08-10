package opslog

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestFormatsLevelsAndInvalidConfig(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		cfg, err := ParseConfig(format, "debug")
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		previous := slog.Default()
		slog.SetDefault(New(&out, cfg))
		Info("daemon.test", "format_checked", "format checked")
		slog.SetDefault(previous)
		if !strings.Contains(out.String(), "component") || !strings.Contains(out.String(), "event") {
			t.Fatalf("%s output missing stable fields: %q", format, out.String())
		}
		if format == "json" {
			var record map[string]any
			if err := json.Unmarshal(out.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"time", "level", "msg", "component", "event"} {
				if _, ok := record[field]; !ok {
					t.Fatalf("JSON record missing %s: %#v", field, record)
				}
			}
		}
	}
	if _, err := ParseConfig("format-secret-sentinel", "info"); err == nil {
		t.Fatal("invalid format accepted")
	} else if strings.Contains(err.Error(), "secret-sentinel") {
		t.Fatalf("invalid format value leaked: %q", err)
	}
	if _, err := ParseConfig("json", "level-secret-sentinel"); err == nil {
		t.Fatal("invalid level accepted")
	} else if strings.Contains(err.Error(), "secret-sentinel") {
		t.Fatalf("invalid level value leaked: %q", err)
	}
	var out bytes.Buffer
	logger := New(&out, Config{Format: "json", Level: slog.LevelWarn})
	logger.Info("suppressed")
	if out.Len() != 0 {
		t.Fatalf("info logged at warn level: %q", out.String())
	}
}

func TestJSONEmitsAllFourLevels(t *testing.T) {
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(New(&out, Config{Format: "json", Level: slog.LevelDebug}))
	t.Cleanup(func() { slog.SetDefault(previous) })
	Debug("daemon.test", "debug_event", "debug message")
	Info("daemon.test", "info_event", "info message")
	Warn("daemon.test", "warn_event", "warn message")
	Error("daemon.test", "error_event", "error message", errors.New("secret error body"))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("records=%d want=4: %q", len(lines), out.String())
	}
	want := []string{"DEBUG", "INFO", "WARN", "ERROR"}
	for i, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["level"] != want[i] {
			t.Fatalf("level[%d]=%v want=%s", i, record["level"], want[i])
		}
	}
	if strings.Contains(out.String(), "secret error body") {
		t.Fatalf("raw error leaked: %q", out.String())
	}
}

func TestConcurrentEventsHaveStableFieldsAndRedactErrors(t *testing.T) {
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(New(&out, Config{Format: "json", Level: slog.LevelDebug}))
	t.Cleanup(func() { slog.SetDefault(previous) })
	const workers = 16
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Error("daemon.test", "write_failed", "write failed", errors.New("token-sentinel prompt-sentinel C:\\secret\\path"))
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != workers {
		t.Fatalf("records=%d want=%d", len(lines), workers)
	}
	if strings.Contains(out.String(), "token-sentinel") || strings.Contains(out.String(), "secret\\path") {
		t.Fatalf("raw error leaked: %q", out.String())
	}
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["component"] != "daemon.test" || record["event"] != "write_failed" || record["error"] != "operation_failed" {
			t.Fatalf("unstable record: %#v", record)
		}
	}
}

func TestWireIdentifiersAreValidatedBeforeLogging(t *testing.T) {
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(New(&out, Config{Format: "json", Level: slog.LevelDebug}))
	t.Cleanup(func() { slog.SetDefault(previous) })

	Info("daemon.test", "identifier_checked", "identifier checked",
		"session_id", "codex:native-1",
		"client_msg_id", "message\nsecret-sentinel",
		"operation_id", strings.Repeat("a", 257),
	)
	var record map[string]any
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["session_id"] != pseudonymousIdentifier("codex:native-1") ||
		record["client_msg_id"] != pseudonymousIdentifier("message\nsecret-sentinel") ||
		record["operation_id"] != pseudonymousIdentifier(strings.Repeat("a", 257)) {
		t.Fatalf("unexpected identifier sanitization: %#v", record)
	}
	if strings.Contains(out.String(), "secret-sentinel") {
		t.Fatalf("hostile identifier leaked: %q", out.String())
	}
}
