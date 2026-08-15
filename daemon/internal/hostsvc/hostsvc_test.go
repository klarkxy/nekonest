package hostsvc

import (
	"encoding/json"
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
		"ExecutionTimeLimit = 'PT0S'",
		"RegisterTaskDefinition($taskName, $definition, 6, $currentUser, $null, 3, $null)",
		"AllowStartIfOnBatteries",
		"New-ScheduledTaskSettingsSet -Hidden",
		"$definition.Settings.Hidden = $true",
		"LogonType Interactive",
		"Register-ScheduledTask",
		"System32\\wscript.exe",
		"//B //NoLogo",
		"Set-Content -LiteralPath $launcherTemp",
		"NekoNest\\hostsvc",
		"$taskName = 'NekoNestDaemon'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "New-ScheduledTaskAction -Execute $exe") {
		t.Fatal("scheduled task still executes the console daemon directly")
	}
	if !strings.HasSuffix(script, "exit 0\n") {
		t.Fatal("successful install script does not normalize its process exit code")
	}
	start := RenderWindowsTaskScript("start", "NekoNestDaemon", "exe", "wd", "")
	if !strings.Contains(start, "not installed") || !strings.Contains(start, "Start-ScheduledTask") ||
		!strings.Contains(start, "Wait-ManagedDaemon -TimeoutSeconds 15") {
		t.Fatalf("start script unexpected:\n%s", start)
	}
	stop := RenderWindowsTaskScript("stop", "NekoNestDaemon", "exe", "wd", "")
	if !strings.Contains(stop, "Wait-ManagedDaemon -TimeoutSeconds 5") {
		t.Fatalf("stop script does not close the PID-ready race:\n%s", stop)
	}
	if !strings.Contains(stop, "Wait-ManagedDaemonExit -TimeoutSeconds 5") ||
		!strings.Contains(stop, "throw 'failed to stop the managed NekoNest daemon process tree'") {
		t.Fatalf("stop script does not fail closed when process-tree termination fails:\n%s", stop)
	}
	throwAt := strings.Index(stop, "throw 'failed to stop the managed NekoNest daemon process tree'")
	cleanupAt := strings.Index(stop, "Remove-Item -LiteralPath $pidPath")
	if throwAt < 0 || cleanupAt < 0 || throwAt > cleanupAt {
		t.Fatalf("stop script can remove the lease before reporting a surviving daemon:\n%s", stop)
	}
}

func TestRenderWindowsLauncherQuotesPathsAndWaits(t *testing.T) {
	launcher := RenderWindowsLauncher(
		"NekoNestDaemon-猫",
		`D:\猫娘 Nest\nekonest-daemon.exe`,
		`D:\猫娘 Nest`,
		`-config "D:\猫娘 Nest\profile\.nekonest\config.json"`,
	)
	for _, want := range []string{
		`Set shell = CreateObject("WScript.Shell")`,
		`environment("NEKONEST_HOST_SERVICE_PID_FILE") = shell.ExpandEnvironmentStrings("%LOCALAPPDATA%\NekoNest\hostsvc\NekoNestDaemon-猫.pid")`,
		`shell.CurrentDirectory = "D:\猫娘 Nest"`,
		`"""D:\猫娘 Nest\nekonest-daemon.exe"" -config ""D:\猫娘 Nest\profile\.nekonest\config.json"""`,
		`, 0, True)`,
	} {
		if !strings.Contains(launcher, want) {
			t.Fatalf("launcher missing %q:\n%s", want, launcher)
		}
	}
}

func TestRenderWindowsUninstallCleansManagedLauncher(t *testing.T) {
	script := RenderWindowsTaskScript("uninstall", "NekoNestDaemon", "exe", "wd", "")
	for _, want := range []string{"$launcherPath", "$manifestPath", "$pidPath", "Stop-ManagedDaemon", "Unregister-ScheduledTask"} {
		if !strings.Contains(script, want) {
			t.Fatalf("uninstall script missing %q:\n%s", want, script)
		}
	}
	if !strings.Contains(script, "$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue\nStop-ManagedDaemon\nif ($null -ne $existing)") {
		t.Fatal("uninstall only stops the managed daemon when the task still exists")
	}
}

func TestClaimManagedProcessPublishesAndCleansPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	t.Setenv(managedPIDEnvironment, path)
	cleanup, err := ClaimManagedProcess()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lease managedPIDLease
	if err := json.Unmarshal(raw, &lease); err != nil {
		t.Fatal(err)
	}
	if lease.Version != 1 || lease.PID != os.Getpid() || lease.StartedAt.IsZero() {
		t.Fatalf("pid lease = %#v", lease)
	}
	if _, inherited := os.LookupEnv(managedPIDEnvironment); inherited {
		t.Fatal("managed PID environment was left available to child processes")
	}
	cleanup()
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("pid file still exists: %v", err)
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
