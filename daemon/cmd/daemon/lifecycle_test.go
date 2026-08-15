package main

import (
	"flag"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nekonest/daemon/internal/hostsvc"
)

func TestParseHostServiceFlags(t *testing.T) {
	cfg, err := parseHostServiceFlags("install", []string{"-config", "foo.json"})
	if err != nil || cfg != "foo.json" {
		t.Fatalf("parse=%q err=%v", cfg, err)
	}
	if _, err := parseHostServiceFlags("start", []string{"extra"}); err == nil {
		t.Fatal("extra args were accepted")
	}
	_, err = parseHostServiceFlags("status", []string{"-h"})
	if err != flag.ErrHelp {
		t.Fatalf("help err=%v", err)
	}
}

func TestFormatHostServiceStatus(t *testing.T) {
	out := formatHostServiceStatus(hostsvc.Status{
		Supported:       true,
		Platform:        "windows",
		Supervisor:      "scheduled_task",
		UnitName:        "NekoNestDaemon",
		Installed:       true,
		Enabled:         true,
		SupervisorState: "Running",
		Executable:      `D:\NekoNest\nekonest-daemon.exe`,
		ConfigPath:      `C:\Users\x\.nekonest\config.json`,
	}, true, nil)
	for _, want := range []string{
		"platform: windows",
		"installed: yes",
		"process_lock: held",
		"unit: NekoNestDaemon",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status missing %q:\n%s", want, out)
		}
	}
}

func TestInstanceLockHeldReportsFreeAndHeld(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	held, err := instanceLockHeld(cfg)
	if err != nil || held {
		t.Fatalf("missing lock held=%v err=%v", held, err)
	}
	lock, err := acquireDaemonInstanceLock(cfg + ".daemon.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	held, err = instanceLockHeld(cfg)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v", held, err)
	}
}
