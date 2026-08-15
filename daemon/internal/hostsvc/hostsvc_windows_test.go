//go:build windows

package hostsvc

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func decodePowerShellForTest(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("odd UTF-16LE byte count: %d", len(raw))
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	return string(utf16.Decode(units))
}

func TestWindowsInstallInvokesPowerShellScript(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "nekonest-daemon.exe")
	cfg := filepath.Join(dir, "config.json")
	var stdin, name string
	var args []string
	mgr := NewWithRunner(Spec{Executable: exe, ConfigPath: cfg}, func(gotStdin, gotName string, gotArgs ...string) (string, string, error) {
		stdin, name, args = gotStdin, gotName, gotArgs
		return "", "", nil
	})
	if err := mgr.Install(); err != nil {
		t.Fatal(err)
	}
	if name != "powershell.exe" || len(args) != 6 || strings.Join(args[:5], " ") != "-NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand" {
		t.Fatalf("powershell invocation %s %v", name, args)
	}
	if stdin != "" {
		t.Fatalf("PowerShell unexpectedly used stdin: %q", stdin)
	}
	decoded := decodePowerShellForTest(t, args[5])
	if !strings.Contains(decoded, "ExecutionTimeLimit = 'PT0S'") || !strings.Contains(decoded, mgr.UnitName()) {
		t.Fatalf("script=%s", decoded)
	}
	if !strings.Contains(decoded, "RegisterTaskDefinition($taskName, $definition, 6, $currentUser, $null, 3, $null)") {
		t.Fatalf("script does not preserve the interactive-token task principal: %s", decoded)
	}
	for _, want := range []string{"NEKONEST_HOST_SERVICE_PID_FILE", "Stop-ManagedDaemon", "taskkill.exe", "$pidPath"} {
		if !strings.Contains(decoded, want) {
			t.Fatalf("script missing managed process cleanup %q: %s", want, decoded)
		}
	}
	if !strings.Contains(decoded, "-config") {
		t.Fatal("custom config was not passed to the scheduled task")
	}
}

func TestWindowsStartRequiresInstall(t *testing.T) {
	dir := t.TempDir()
	mgr := NewWithRunner(Spec{
		Executable: filepath.Join(dir, "nekonest-daemon.exe"),
		ConfigPath: filepath.Join(dir, "config.json"),
	}, func(stdin, name string, args ...string) (string, string, error) {
		return `{"found":false}`, "", nil
	})
	if err := mgr.Start(); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("start err=%v", err)
	}
}

func TestWindowsStartWaitsForLeaseWhenTaskAlreadyRunning(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	var startScript string
	mgr := NewWithRunner(Spec{
		Executable: filepath.Join(dir, "nekonest-daemon.exe"),
		ConfigPath: filepath.Join(dir, "config.json"),
	}, func(stdin, name string, args ...string) (string, string, error) {
		calls++
		if calls == 1 {
			return `{"found":true,"state":"Running","enabled":true}`, "", nil
		}
		startScript = decodePowerShellForTest(t, args[len(args)-1])
		return "", "", nil
	})
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("runner calls=%d want query plus start handshake", calls)
	}
	if !strings.Contains(startScript, "Wait-ManagedDaemon -TimeoutSeconds 15") {
		t.Fatalf("start script skipped PID-ready handshake:\n%s", startScript)
	}
}

func TestWindowsStatusDecodesQuery(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "nekonest-daemon.exe")
	mgr := NewWithRunner(Spec{Executable: exe, ConfigPath: filepath.Join(dir, "config.json")}, func(stdin, name string, args ...string) (string, string, error) {
		raw, err := json.Marshal(windowsTaskQuery{
			Found:     true,
			State:     "Running",
			Enabled:   true,
			Execute:   exe,
			Arguments: "",
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw), "", nil
	})
	st, err := mgr.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Installed || !st.Enabled || st.SupervisorState != "Running" || st.Supervisor != "scheduled_task" {
		t.Fatalf("status=%+v", st)
	}
}
