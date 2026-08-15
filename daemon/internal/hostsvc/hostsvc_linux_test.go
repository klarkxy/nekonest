//go:build linux

package hostsvc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxInstallWritesUnitAndEnables(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "nekonest-daemon")
	cfg := filepath.Join(dir, "config.json")
	unitDir := filepath.Join(dir, "systemd")
	var cmds []string
	mgr := NewWithRunner(Spec{Executable: exe, ConfigPath: cfg, UnitDir: unitDir}, func(stdin, name string, args ...string) (string, string, error) {
		cmds = append(cmds, name+" "+strings.Join(args, " "))
		return "", "", nil
	})
	if err := mgr.Install(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(unitDir, mgr.UnitName()+".service"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "ExecStart="+exe+" -config "+cfg) {
		t.Fatalf("unit body=%s", body)
	}
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "systemctl --user daemon-reload") || !strings.Contains(joined, "systemctl --user enable "+mgr.UnitName()+".service") {
		t.Fatalf("systemctl calls=%v", cmds)
	}
}

func TestLinuxStartRequiresInstall(t *testing.T) {
	dir := t.TempDir()
	mgr := NewWithRunner(Spec{
		Executable: filepath.Join(dir, "nekonest-daemon"),
		ConfigPath: filepath.Join(dir, "config.json"),
		UnitDir:    filepath.Join(dir, "systemd"),
	}, func(stdin, name string, args ...string) (string, string, error) {
		t.Fatalf("unexpected command %s %v", name, args)
		return "", "", nil
	})
	if err := mgr.Start(); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("start err=%v", err)
	}
}

func TestLinuxStatusParsesSystemctlShow(t *testing.T) {
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "systemd")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := NewWithRunner(Spec{
		Executable: filepath.Join(dir, "nekonest-daemon"),
		ConfigPath: filepath.Join(dir, "config.json"),
		UnitDir:    unitDir,
	}, func(stdin, name string, args ...string) (string, string, error) {
		return "ActiveState=active\nUnitFileState=enabled\nFragmentPath=" + filepath.Join(unitDir, "unit.service") + "\n", "", nil
	})
	if err := os.WriteFile(filepath.Join(unitDir, mgr.UnitName()+".service"), []byte("[Service]\nExecStart=/opt/nekonest-daemon -config /tmp/config.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := mgr.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Installed || !st.Enabled || st.SupervisorState != "active" || st.Supervisor != "systemd_user" {
		t.Fatalf("status=%+v", st)
	}
	if st.ConfiguredExec != "/opt/nekonest-daemon -config /tmp/config.json" {
		t.Fatalf("configured exec=%q", st.ConfiguredExec)
	}
}

func TestParseSystemdShow(t *testing.T) {
	got := parseSystemdShow("ActiveState=inactive\nUnitFileState=disabled\n")
	if got["ActiveState"] != "inactive" || got["UnitFileState"] != "disabled" {
		t.Fatalf("parsed=%v", got)
	}
}

func TestParseSystemdExecStart(t *testing.T) {
	if got := parseSystemdExecStart("[Service]\nExecStart=/bin/daemon -config /tmp/cfg\n"); got != "/bin/daemon -config /tmp/cfg" {
		t.Fatalf("exec start=%q", got)
	}
}
