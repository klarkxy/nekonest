package agentexec

import "io"

// drainStderrDiagnostics continuously consumes diagnostics without retaining,
// parsing, forwarding, or logging their content. A process gets at most one
// privacy-safe operational breadcrumb if it emits any stderr bytes.
func drainStderrDiagnostics(reader io.Reader, diagnostics *stderrDiagnostics, agentType, sessionID string) {
	if reader == nil || diagnostics == nil {
		return
	}
	var buffer [32 * 1024]byte
	for {
		n, err := reader.Read(buffer[:])
		if n > 0 {
			diagnostics.suppress(agentType, sessionID, "stderr", "")
		}
		if err != nil {
			return
		}
	}
}
