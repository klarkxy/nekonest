//go:build windows

package agentexec

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestConfigureBackgroundProcessPreservesCreationFlags(t *testing.T) {
	const existingFlag = 0x00000004 // CREATE_SUSPENDED, used by startManagedProcess.
	cmd := exec.Command("fixture")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: existingFlag}

	configureBackgroundProcess(cmd)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatal("background command did not set HideWindow")
	}
	if got := cmd.SysProcAttr.CreationFlags; got&existingFlag == 0 || got&createNoWindow == 0 {
		t.Fatalf("CreationFlags = 0x%x, want CREATE_SUSPENDED and CREATE_NO_WINDOW", got)
	}
}

func TestConfigureBackgroundProcessInitializesSysProcAttr(t *testing.T) {
	cmd := exec.Command("fixture")
	configureBackgroundProcess(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("SysProcAttr = %#v, want hidden no-window process", cmd.SysProcAttr)
	}
}
