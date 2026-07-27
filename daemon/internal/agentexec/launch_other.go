//go:build !windows

package agentexec

import (
	"os"
	"os/exec"
)

func resolveLaunch(command string, args []string) (string, []string, error) {
	return command, args, nil
}

func startManagedProcess(cmd *exec.Cmd) (uintptr, error) {
	return 0, cmd.Start()
}

func releaseJobObject(job uintptr) {}

func interruptProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Signal(os.Interrupt)
}
