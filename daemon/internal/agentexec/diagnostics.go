package agentexec

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/nekonest/daemon/internal/opslog"
)

// stderrDiagnostics suppresses untrusted CLI stderr content while preserving a
// single operational breadcrumb per process. Agent stderr can contain prompts,
// local paths, headers, or tokens, so its body must not enter durable logs.
type stderrDiagnostics struct {
	once sync.Once
	seen atomic.Bool
}

func (d *stderrDiagnostics) suppress(agentType, sessionID, source, line string) bool {
	if source == "stdout" {
		return false
	}
	// line is intentionally never inspected or logged: CLI diagnostics may
	// contain prompts, credentials, response bodies, and local paths.
	d.seen.Store(true)
	d.once.Do(func() {
		opslog.Warn("daemon.agentexec", "stderr_suppressed", "non-stdout CLI diagnostics suppressed", "agent_type", agentType, "session_id", sessionID)
	})
	return true
}

func (d *stderrDiagnostics) exitFailure(
	agentLabel string,
	exitCode int,
	intentionallyStopped bool,
) string {
	if exitCode == 0 || intentionallyStopped || !d.seen.Load() {
		return ""
	}
	return fmt.Sprintf(
		"%s CLI exited with code %d after emitting local diagnostics; check the CLI on the PC",
		agentLabel,
		exitCode,
	)
}
