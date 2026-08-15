package hostsvc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nekonest/daemon/internal/config"
)

func TestFindCommand(t *testing.T) {
	tests := []struct {
		args []string
		cmd  string
		ok   bool
	}{
		{[]string{"install"}, "install", true},
		{[]string{"-config", "x", "start"}, "start", true},
		{[]string{"--config=x", "status"}, "status", true},
		{[]string{"status", "-config", "x"}, "status", true},
		{[]string{"-config", "start"}, "", false},
		{[]string{"-name", "start", "-register"}, "", false},
		{[]string{"-register"}, "", false},
		{[]string{"doctor"}, "", false},
		{[]string{"foo"}, "", false},
		{[]string{"--", "install"}, "", false},
	}
	for _, test := range tests {
		cmd, ok := FindCommand(test.args)
		if cmd != test.cmd || ok != test.ok {
			t.Fatalf("FindCommand(%q)=%q,%v want %q,%v", test.args, cmd, ok, test.cmd, test.ok)
		}
	}
}

func TestArgsWithoutCommand(t *testing.T) {
	got := ArgsWithoutCommand([]string{"-config", "x", "install", "-config", "y"}, "install")
	if strings.Join(got, " ") != "-config x -config y" {
		t.Fatalf("ArgsWithoutCommand=%q", got)
	}
}

func TestServiceNameDefaultAndCustom(t *testing.T) {
	def := ServiceName("")
	if def == "" {
		t.Fatal("empty default service name")
	}
	if ServiceName(config.DefaultConfigPath()) != def {
		t.Fatal("default config path should use the same service name")
	}
	path := filepath.Join(t.TempDir(), "custom.json")
	name := ServiceName(path)
	if name == def || !strings.HasPrefix(name, def+"-") {
		t.Fatalf("custom service name %q default %q", name, def)
	}
	if ServiceName(path) != name {
		t.Fatal("custom service name is unstable")
	}
}

func TestRenderSystemdUserUnit(t *testing.T) {
	unit := RenderSystemdUserUnit("/home/u/.local/bin/nekonest-daemon", filepath.Join(t.TempDir(), "config.json"))
	for _, want := range []string{
		"After=network-online.target",
		"Type=simple",
		"Restart=on-failure",
		"RestartSec=5",
		"WantedBy=default.target",
		"ExecStart=/home/u/.local/bin/nekonest-daemon -config ",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "NEKONEST_TRANSPORT_MODE") {
		t.Fatal("unit must not force a transport mode")
	}
}

func TestRenderSystemdUserUnitQuotesSpaces(t *testing.T) {
	unit := RenderSystemdUserUnit(`/opt/neko nest/100%/nekonest-daemon`, "")
	if !strings.Contains(unit, `ExecStart="/opt/neko nest/100%%/nekonest-daemon"`) {
		t.Fatalf("quoted ExecStart missing:\n%s", unit)
	}
}

func TestValidateSupervisorPathRejectsUnitInjection(t *testing.T) {
	if err := validateSupervisorPath("config", "/tmp/config.json\nEnvironment=BAD=1"); err == nil {
		t.Fatal("newline-bearing supervisor path was accepted")
	}
}

func TestRenderWindowsTaskScript(t *testing.T) {
	script := RenderWindowsTaskScript("install", "NekoNestDaemon", `D:\NekoNest\nekonest-daemon.exe`, `D:\NekoNest`, "")
	for _, want := range []string{
		"New-ScheduledTaskTrigger -AtLogOn",
		"WindowsIdentity]::GetCurrent().Name",
		"-AtLogOn -User $currentUser",
		"ExecutionTimeLimit = [TimeSpan]::Zero",
		"AllowStartIfOnBatteries",
		"LogonType Interactive",
		"Register-ScheduledTask",
		"$taskName = 'NekoNestDaemon'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install script missing %q:\n%s", want, script)
		}
	}
	start := RenderWindowsTaskScript("start", "NekoNestDaemon", "exe", "wd", "")
	if !strings.Contains(start, "not installed") || !strings.Contains(start, "Start-ScheduledTask") {
		t.Fatalf("start script unexpected:\n%s", start)
	}
}

func TestIsEphemeralPath(t *testing.T) {
	if !IsEphemeralPath(filepath.Join(os.TempDir(), "nekonest-daemon.exe")) {
		t.Fatal("temp path should be ephemeral")
	}
	if IsEphemeralPath(filepath.Join(t.TempDir(), "stable", "nekonest-daemon")) && !strings.HasPrefix(strings.ToLower(t.TempDir()), strings.ToLower(os.TempDir())) {
		t.Fatal("non-temp path should not be ephemeral")
	}
}

func TestDaemonConfigArgsOmitsDefault(t *testing.T) {
	if got := daemonConfigArgs(""); got != "" {
		t.Fatalf("default args=%q", got)
	}
	custom := filepath.Join(t.TempDir(), "cfg.json")
	got := daemonConfigArgs(custom)
	if got != "-config "+custom && got != "-config "+CanonicalPath(custom) {
		t.Fatalf("custom args=%q", got)
	}
}
