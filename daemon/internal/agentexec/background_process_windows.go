//go:build windows

package agentexec

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// configureBackgroundProcess prevents daemon-owned CLI work from allocating or
// inheriting a visible Windows console. CreationFlags is intentionally ORed so
// callers such as startManagedProcess can add their own process flags.
func configureBackgroundProcess(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
