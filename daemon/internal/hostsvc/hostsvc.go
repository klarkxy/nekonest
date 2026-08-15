// Package hostsvc registers and queries the current-user host supervisor.
// The OS starts the daemon; this package does not stay resident.
package hostsvc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nekonest/daemon/internal/config"
)

// ErrUnsupported is returned on operating systems that have no host supervisor.
var ErrUnsupported = errors.New("host service management is supported on Windows and Linux only")

// Commands are the supported host-service verbs.
var Commands = []string{"install", "uninstall", "start", "stop", "status"}

// RunFunc executes an OS helper. Tests replace it; production uses exec.
type RunFunc func(stdin, name string, args ...string) (stdout, stderr string, err error)

// Spec identifies one daemon instance to supervise.
type Spec struct {
	Executable string
	ConfigPath string
	UnitDir    string // Linux tests override the systemd user directory
}

// Status is a query of the supervisor and the configured executable.
type Status struct {
	Supported       bool
	Platform        string
	Supervisor      string
	UnitName        string
	Installed       bool
	Enabled         bool
	SupervisorState string
	Executable      string
	ConfigPath      string
	ConfiguredExec  string
	ConfiguredArgs  string
	Detail          string
}

// Manager talks to the current-user supervisor for one config.
type Manager struct {
	spec Spec
	run  RunFunc
}

// New prepares a manager for the resolved executable and config path.
func New(spec Spec) *Manager {
	return NewWithRunner(spec, defaultRun)
}

// NewWithRunner is for tests that must not invoke the real supervisor.
func NewWithRunner(spec Spec, run RunFunc) *Manager {
	spec.Executable = strings.TrimSpace(spec.Executable)
	spec.ConfigPath = strings.TrimSpace(spec.ConfigPath)
	if spec.ConfigPath == "" {
		spec.ConfigPath = config.DefaultConfigPath()
	}
	spec.ConfigPath = CanonicalPath(spec.ConfigPath)
	if spec.Executable != "" {
		spec.Executable = CanonicalPath(spec.Executable)
	}
	if run == nil {
		run = defaultRun
	}
	return &Manager{spec: spec, run: run}
}

// UnitName is the scheduled-task or systemd unit name for this config.
func (m *Manager) UnitName() string {
	return ServiceName(m.spec.ConfigPath)
}

// ConfigPath is the resolved config file used in status output.
func (m *Manager) ConfigPath() string {
	return m.spec.ConfigPath
}

// Executable is the resolved daemon binary the supervisor should launch.
func (m *Manager) Executable() string {
	return m.spec.Executable
}

// FindCommand returns a host-service verb mixed with flags, if present.
func FindCommand(args []string) (string, bool) {
	known := make(map[string]struct{}, len(Commands))
	for _, cmd := range Commands {
		known[cmd] = struct{}{}
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return "", false
		}
		switch {
		case arg == "-config" || arg == "--config":
			// The service command may follow the one global flag that is also
			// accepted by every service subcommand. Skip its value so a config
			// literally named "start" is never mistaken for a command.
			if i+1 >= len(args) {
				return "", false
			}
			i++
			continue
		case strings.HasPrefix(arg, "-config=") || strings.HasPrefix(arg, "--config="):
			continue
		case strings.HasPrefix(arg, "-"):
			// Any legacy daemon flag selects the existing foreground/register/
			// doctor parser. Do not reinterpret its value as a service verb.
			return "", false
		}
		if _, ok := known[arg]; ok {
			return arg, true
		}
		return "", false
	}
	return "", false
}

// ArgsWithoutCommand drops the first exact match of cmd so flags can be parsed.
func ArgsWithoutCommand(args []string, cmd string) []string {
	out := make([]string, 0, len(args))
	removed := false
	for _, arg := range args {
		if !removed && arg == cmd {
			removed = true
			continue
		}
		out = append(out, arg)
	}
	return out
}

// ServiceName is the supervisor object name for a config path.
func ServiceName(configPath string) string {
	base := serviceBaseName()
	if IsDefaultConfigPath(configPath) {
		return base
	}
	return base + "-" + configFingerprint(configPath)
}

// IsDefaultConfigPath reports whether path is the daemon's default config file.
func IsDefaultConfigPath(configPath string) bool {
	if strings.TrimSpace(configPath) == "" {
		return true
	}
	left := CanonicalPath(configPath)
	right := CanonicalPath(config.DefaultConfigPath())
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

// CanonicalPath returns an absolute, symlink-resolved path when possible.
func CanonicalPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return filepath.Clean(abs)
}

// IsEphemeralPath warns when the binary lives in Temp or Downloads.
func IsEphemeralPath(path string) bool {
	abs := CanonicalPath(path)
	if abs == "" {
		return false
	}
	temp := filepath.Clean(os.TempDir())
	if temp != "" {
		rel, err := filepath.Rel(temp, abs)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return true
		}
	}
	slash := strings.ToLower(filepath.ToSlash(abs))
	return strings.Contains(slash, "/downloads/") || strings.HasSuffix(slash, "/downloads")
}

// RenderSystemdUserUnit is the Linux user unit written by install.
func RenderSystemdUserUnit(exe, configPath string) string {
	execStart := quoteSystemdArg(exe)
	if !IsDefaultConfigPath(configPath) {
		execStart += " -config " + quoteSystemdArg(configPath)
	}
	return strings.Join([]string{
		"[Unit]",
		"Description=NekoNest host daemon (outbound nest bridge)",
		"After=network-online.target",
		"Wants=network-online.target",
		"",
		"[Service]",
		"Type=simple",
		"ExecStart=" + execStart,
		"Restart=on-failure",
		"RestartSec=5",
		"",
		"[Install]",
		"WantedBy=default.target",
		"",
	}, "\n")
}

func validateSupervisorPath(label, value string) error {
	if strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("%s path contains an unsupported control character", label)
	}
	return nil
}

// RenderWindowsTaskScript is the PowerShell used by the Windows supervisor.
func RenderWindowsTaskScript(action, taskName, exe, workDir, arguments string) string {
	var b strings.Builder
	b.WriteString("$ErrorActionPreference = 'Stop'\n")
	b.WriteString("$taskName = " + psSingleQuote(taskName) + "\n")
	b.WriteString("$exe = " + psSingleQuote(exe) + "\n")
	b.WriteString("$workDir = " + psSingleQuote(workDir) + "\n")
	b.WriteString("$arguments = " + psSingleQuote(arguments) + "\n")
	switch action {
	case "install":
		b.WriteString(windowsInstallBody)
	case "uninstall":
		b.WriteString(windowsUninstallBody)
	case "start":
		b.WriteString(windowsStartBody)
	case "stop":
		b.WriteString(windowsStopBody)
	case "query":
		b.WriteString(windowsQueryBody)
	default:
		b.WriteString("throw 'unsupported host service action'\n")
	}
	return b.String()
}

func daemonConfigArgs(configPath string) string {
	if IsDefaultConfigPath(configPath) {
		return ""
	}
	if strings.ContainsAny(configPath, " \t\"") {
		return `-config "` + strings.ReplaceAll(configPath, `"`, `\"`) + `"`
	}
	return "-config " + configPath
}

func configFingerprint(configPath string) string {
	sum := sha256.Sum256([]byte(CanonicalPath(configPath)))
	return hex.EncodeToString(sum[:])[:8]
}

func quoteSystemdArg(value string) string {
	if value == "" {
		return value
	}
	// systemd expands percent specifiers even inside quoted arguments.
	value = strings.ReplaceAll(value, "%", "%%")
	if !strings.ContainsAny(value, " \t\"'\\") {
		return value
	}
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func psSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func defaultRun(stdin, name string, args ...string) (string, string, error) {
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return stdout.String(), stderr.String(), fmt.Errorf("%w: %s", err, detail)
		}
	}
	return stdout.String(), stderr.String(), err
}

const (
	windowsInstallBody = `
if ($arguments -ne '') {
  $action = New-ScheduledTaskAction -Execute $exe -Argument $arguments -WorkingDirectory $workDir
} else {
  $action = New-ScheduledTaskAction -Execute $exe -WorkingDirectory $workDir
}
$currentUser = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $currentUser
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -MultipleInstances IgnoreNew -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
$principal = New-ScheduledTaskPrincipal -UserId $currentUser -LogonType Interactive -RunLevel Limited
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Settings $settings -Principal $principal -Description 'NekoNest host daemon' -Force | Out-Null
$service = New-Object -ComObject 'Schedule.Service'
$service.Connect()
$root = $service.GetFolder('\')
$registered = $root.GetTask($taskName)
$definition = $registered.Definition
$definition.Settings.ExecutionTimeLimit = 'PT0S'
$root.RegisterTaskDefinition($taskName, $definition, 6, $currentUser, $null, 3, $null) | Out-Null
`
	windowsUninstallBody = `
$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($null -eq $existing) { exit 0 }
try { Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue } catch {}
Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
`
	windowsStartBody = `
$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($null -eq $existing) { throw 'host service is not installed; run nekonest-daemon install' }
if ([string]$existing.State -eq 'Running') { exit 0 }
Start-ScheduledTask -TaskName $taskName
`
	windowsStopBody = `
$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($null -eq $existing) { exit 0 }
try { Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue } catch {}
`
	windowsQueryBody = `
$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($null -eq $existing) {
  [pscustomobject]@{ found = $false } | ConvertTo-Json -Compress
  exit 0
}
$action = $existing.Actions | Select-Object -First 1
[pscustomobject]@{
  found = $true
  state = [string]$existing.State
  enabled = [bool]$existing.Settings.Enabled
  execute = [string]$action.Execute
  arguments = [string]$action.Arguments
} | ConvertTo-Json -Compress
`
)
