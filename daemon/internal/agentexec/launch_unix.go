//go:build unix

package agentexec

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func resolveLaunch(command string, args []string) (string, []string, error) {
	return command, args, nil
}

// startManagedProcess starts the agent in its own process group so interrupt
// can signal the whole tree (SIGINT then SIGKILL).
func startManagedProcess(cmd *exec.Cmd) (uintptr, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return 0, nil
}

func releaseJobObject(job uintptr) {}

func interruptProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	pgid := p.Pid
	// Negative pid = process group. Do not Wait here — AgentExecutor owns Wait.
	if err := syscall.Kill(-pgid, syscall.SIGINT); err != nil {
		if err2 := p.Signal(os.Interrupt); err2 != nil {
			return fmt.Errorf("sigint group: %v; process: %w", err, err2)
		}
	}
	// After a short grace, escalate to SIGKILL on the group. The executor's
	// Wait goroutine still reaps the root process.
	go func(pgid int) {
		time.Sleep(3 * time.Second)
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}(pgid)
	return nil
}
