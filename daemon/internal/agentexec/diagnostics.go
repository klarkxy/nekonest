package agentexec

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
)

// stderrDiagnostics suppresses untrusted CLI stderr content while preserving a
// single operational breadcrumb per process. Agent stderr can contain prompts,
// local paths, headers, or tokens, so its body must not enter durable logs.
type stderrDiagnostics struct {
	once sync.Once
	seen atomic.Bool
}

func (d *stderrDiagnostics) suppress(agentType, sessionID, source string) bool {
	if source == "stdout" {
		return false
	}
	d.seen.Store(true)
	d.once.Do(func() {
		log.Printf(
			"[%s] session %s emitted non-stdout diagnostics; content omitted",
			agentType,
			sessionID,
		)
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
