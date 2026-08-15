package agentexec

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/nekonest/daemon/internal/opslog"
)

func TestDrainStderrDiagnosticsSuppressesSensitiveContentAndExits(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(opslog.New(&output, opslog.Config{Format: "text", Level: slog.LevelDebug}))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	reader, writer := io.Pipe()
	var diagnostics stderrDiagnostics
	done := make(chan struct{})
	go func() {
		drainStderrDiagnostics(reader, &diagnostics, "codex", "")
		close(done)
	}()

	const sentinel = "app-server-stderr-sensitive-sentinel"
	if _, err := io.WriteString(writer, sentinel); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stderr drain did not terminate at EOF")
	}

	got := output.String()
	if count := strings.Count(got, "stderr_suppressed"); count != 1 {
		t.Fatalf("stderr breadcrumb count = %d, log=%q", count, got)
	}
	if strings.Contains(got, sentinel) {
		t.Fatalf("stderr content leaked to log: %q", got)
	}
}
