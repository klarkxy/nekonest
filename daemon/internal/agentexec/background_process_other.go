//go:build !windows

package agentexec

import "os/exec"

// configureBackgroundProcess is intentionally a no-op outside Windows.
func configureBackgroundProcess(cmd *exec.Cmd) {}
