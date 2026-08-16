//go:build !windows && !unix

package agentexec

import (
	"os"
	"os/exec"
)

// Fallback for exotic GOOS without unix process groups.
func resolveLaunch(command string, args []string) (string, []string, error) {
	if exe, argv, handled, err := wrapNodeScript(command, args); handled {
		return exe, argv, err
	}
	if exe, argv, handled, err := resolveCursorAgentLaunch(command, args); handled {
		return exe, argv, err
	}
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
