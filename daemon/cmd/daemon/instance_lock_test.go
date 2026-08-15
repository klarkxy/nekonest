package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstanceLockHelperProcess(t *testing.T) {
	if os.Getenv("NEKONEST_INSTANCE_LOCK_HELPER") != "1" {
		return
	}
	path := os.Getenv("NEKONEST_INSTANCE_LOCK_PATH")
	lock, err := acquireDaemonInstanceLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	fmt.Println("NEKONEST_LOCKED")
	_ = os.Stdout.Sync()
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func TestDaemonInstanceLockIsExclusiveAcrossProcessesAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	child := exec.Command(os.Args[0], "-test.run=^TestInstanceLockHelperProcess$")
	child.Env = append(
		os.Environ(),
		"NEKONEST_INSTANCE_LOCK_HELPER=1",
		"NEKONEST_INSTANCE_LOCK_PATH="+path,
	)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var childErr strings.Builder
	child.Stderr = &childErr
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = stdin // Keep the pipe open so the helper remains alive until killed.
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = child.Process.Kill()
			_, _ = child.Process.Wait()
		}
	}()

	locked := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == "NEKONEST_LOCKED" {
				locked <- nil
				return
			}
		}
		locked <- fmt.Errorf("helper exited before locking: %s", childErr.String())
	}()
	select {
	case err := <-locked:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not acquire instance lock")
	}

	if second, err := acquireDaemonInstanceLock(path); err == nil {
		_ = second.Close()
		t.Fatal("second process acquired the same daemon lock")
	} else if !errors.Is(err, errDaemonInstanceLockHeld) {
		t.Fatalf("second process returned non-contention error: %v", err)
	}
	// Simulate a crash: the process does not run its deferred unlock. The OS
	// must release the advisory lock when the handle disappears.
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait() // an exit error is expected after Kill
	finished = true

	restarted, err := acquireDaemonInstanceLock(path)
	if err != nil {
		t.Fatalf("lock was not released after process exit: %v", err)
	}
	if err := restarted.Close(); err != nil {
		t.Fatalf("release restarted lock: %v", err)
	}
}
