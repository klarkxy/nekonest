package agentexec

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/nekonest/daemon/internal/opslog"
)

func TestStderrDiagnosticsSuppressesNonStdoutAndLogsOnce(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(opslog.New(&output, opslog.Config{Format: "text", Level: slog.LevelDebug}))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	var diagnostics stderrDiagnostics
	if diagnostics.suppress("codex", "session-a", "stdout", "stdout-sentinel") {
		t.Fatal("stdout must not be suppressed")
	}
	if !diagnostics.suppress("codex", "session-a", "stderr", `prompt-sentinel C:\Users\user-sentinel\secret-attachment.txt`) {
		t.Fatal("stderr must be suppressed")
	}
	if !diagnostics.suppress("codex", "session-a", "diagnostic", "authorization-sentinel") {
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
	if count := strings.Count(got, "stderr_suppressed"); count != 1 {
		t.Fatalf("diagnostic notice count = %d, want 1; log=%q", count, got)
	}
	for _, secret := range []string{"prompt-sentinel", "user-sentinel", "secret-attachment.txt", "authorization-sentinel"} {
		if strings.Contains(got, secret) {
			t.Fatalf("CLI diagnostic leaked %q: %q", secret, got)
		}
	}
}

func TestStderrDiagnosticsDoesNotInventFailureWithoutDiagnostics(t *testing.T) {
	var diagnostics stderrDiagnostics
	if got := diagnostics.exitFailure("Codex", 1, false); got != "" {
		t.Fatalf("failure without diagnostics = %q, want empty", got)
	}
}
