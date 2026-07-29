package agentexec

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestStderrDiagnosticsSuppressesNonStdoutAndLogsOnce(t *testing.T) {
	var output bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&output)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})

	var diagnostics stderrDiagnostics
	if diagnostics.suppress("codex", "session-a", "stdout") {
		t.Fatal("stdout must not be suppressed")
	}
	if !diagnostics.suppress("codex", "session-a", "stderr") {
		t.Fatal("stderr must be suppressed")
	}
	if !diagnostics.suppress("codex", "session-a", "diagnostic") {
		t.Fatal("unknown non-stdout sources must fail closed")
	}
	if got := diagnostics.exitFailure("Codex", 0, false); got != "" {
		t.Fatalf("successful exit reported as failure: %q", got)
	}
	if got := diagnostics.exitFailure("Codex", 1, false); !strings.Contains(got, "code 1") {
		t.Fatalf("non-zero exit did not produce a safe failure: %q", got)
	}
	if got := diagnostics.exitFailure("Codex", 1, true); got != "" {
		t.Fatalf("intentional stop reported as failure: %q", got)
	}

	got := output.String()
	if count := strings.Count(got, "content omitted"); count != 1 {
		t.Fatalf("diagnostic notice count = %d, want 1; log=%q", count, got)
	}
}

func TestStderrDiagnosticsDoesNotInventFailureWithoutDiagnostics(t *testing.T) {
	var diagnostics stderrDiagnostics
	if got := diagnostics.exitFailure("Codex", 1, false); got != "" {
		t.Fatalf("failure without diagnostics = %q, want empty", got)
	}
}
