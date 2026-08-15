// Package hostsvc registers and queries the current-user host supervisor.
// The OS starts the daemon; this package does not stay resident.
package hostsvc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nekonest/daemon/internal/config"
)

// ErrUnsupported is returned on operating systems that have no host supervisor.
var ErrUnsupported = errors.New("host service management is supported on Windows and Linux only")

const managedPIDEnvironment = "NEKONEST_HOST_SERVICE_PID_FILE"

type managedPIDLease struct {
	Version   int       `json:"version"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

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

// ClaimManagedProcess publishes the daemon PID for the Windows scheduled-task
// wrapper. The wrapper itself remains the Task Scheduler action; the PID file
// lets stop and uninstall terminate the daemon tree after Task Scheduler stops
// wscript.exe. Foreground and Linux launches do not set the environment value.
func ClaimManagedProcess() (func(), error) {
	path := strings.TrimSpace(os.Getenv(managedPIDEnvironment))
	_ = os.Unsetenv(managedPIDEnvironment)
	if path == "" {
		return func() {}, nil
	}
	if err := validateSupervisorPath("managed process", path); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("managed process path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	lease := managedPIDLease{Version: 1, PID: os.Getpid(), StartedAt: time.Now().UTC()}
	data, err := json.Marshal(lease)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nekonest-hostsvc-pid-*.tmp")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	if err := os.Rename(tmpPath, path); err != nil {
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			current, err := os.ReadFile(path)
			var currentLease managedPIDLease
			if err == nil && json.Unmarshal(current, &currentLease) == nil &&
				currentLease.PID == lease.PID && currentLease.StartedAt.Equal(lease.StartedAt) {
				_ = os.Remove(path)
			}
		})
	}, nil
}

// RenderWindowsTaskScript is the PowerShell used by the Windows supervisor.
func RenderWindowsTaskScript(action, taskName, exe, workDir, arguments string) string {
	var b strings.Builder
	b.WriteString("$ErrorActionPreference = 'Stop'\n")
	b.WriteString("$taskName = " + psSingleQuote(taskName) + "\n")
	b.WriteString("$exe = " + psSingleQuote(exe) + "\n")
	b.WriteString("$workDir = " + psSingleQuote(workDir) + "\n")
	b.WriteString("$arguments = " + psSingleQuote(arguments) + "\n")
	b.WriteString("$launcherBody = " + psSingleQuote(RenderWindowsLauncher(taskName, exe, workDir, arguments)) + "\n")
	b.WriteString(windowsLauncherPreamble)
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
	if action == "install" || action == "uninstall" || action == "start" || action == "stop" || action == "query" {
		b.WriteString("exit 0\n")
	}
	return b.String()
}

// RenderWindowsLauncher builds the hidden WScript wrapper owned by a scheduled
// task. The wrapper waits for the daemon and publishes a task-specific PID file
// so stop and uninstall can clean up the child tree after Task Scheduler stops
// wscript.exe.
func RenderWindowsLauncher(taskName, exe, workDir, arguments string) string {
	commandLine := `"` + exe + `"`
	if arguments != "" {
		commandLine += " " + arguments
	}
	return strings.Join([]string{
		"Option Explicit",
		"Dim shell",
		"Dim environment",
		`Set shell = CreateObject("WScript.Shell")`,
		`Set environment = shell.Environment("PROCESS")`,
		"environment(" + vbsStringLiteral(managedPIDEnvironment) + ") = shell.ExpandEnvironmentStrings(" + vbsStringLiteral(`%LOCALAPPDATA%\NekoNest\hostsvc\`+taskName+`.pid`) + ")",
		"shell.CurrentDirectory = " + vbsStringLiteral(workDir),
		"WScript.Quit shell.Run(" + vbsStringLiteral(commandLine) + ", 0, True)",
	}, "\r\n")
}

func vbsStringLiteral(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
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
	windowsLauncherPreamble = `
$launcherRoot = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'NekoNest\hostsvc'
$launcherPath = Join-Path $launcherRoot ($taskName + '.vbs')
$manifestPath = Join-Path $launcherRoot ($taskName + '.json')
$pidPath = Join-Path $launcherRoot ($taskName + '.pid')

function Stop-ManagedDaemon {
  $existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
  $wasRunning = $null -ne $existing -and [string]$existing.State -eq 'Running'
  if ($null -ne $existing) {
    try { Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue } catch {}
  }
  $managed = Get-ManagedDaemon
  if ($null -eq $managed -and $wasRunning) {
    $managed = Wait-ManagedDaemon -TimeoutSeconds 5
  }
  if ($null -ne $managed) {
    $managedPid = [int]$managed.ProcessId
    $taskkill = Join-Path $env:SystemRoot 'System32\taskkill.exe'
    & $taskkill /PID $managedPid /T /F 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) {
      Stop-Process -Id $managedPid -Force -ErrorAction SilentlyContinue
    }
    if (-not (Wait-ManagedDaemonExit -TimeoutSeconds 5)) {
      throw 'failed to stop the managed NekoNest daemon process tree'
    }
  }
  Remove-Item -LiteralPath $pidPath -Force -ErrorAction SilentlyContinue
}

function Get-ManagedDaemon {
  if (-not (Test-Path -LiteralPath $pidPath)) { return $null }
  try {
    $lease = Get-Content -LiteralPath $pidPath -Raw | ConvertFrom-Json
    $managedPid = [int]$lease.pid
    if ([int]$lease.version -ne 1 -or $managedPid -le 0) { return $null }
    $managed = Get-CimInstance Win32_Process -Filter ("ProcessId = " + $managedPid) -ErrorAction SilentlyContinue
    if ($null -eq $managed -or -not [string]::Equals([string]$managed.ExecutablePath, $exe, [StringComparison]::OrdinalIgnoreCase)) {
      return $null
    }
    $leaseStart = [DateTimeOffset]::Parse([string]$lease.started_at).UtcDateTime
    $processStart = ([datetime]$managed.CreationDate).ToUniversalTime()
    if ([Math]::Abs(($processStart - $leaseStart).TotalSeconds) -gt 30) { return $null }
    return $managed
  } catch {
    return $null
  }
}

function Wait-ManagedDaemon {
  param([int]$TimeoutSeconds)
  $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
  do {
    $managed = Get-ManagedDaemon
    if ($null -ne $managed) { return $managed }
    Start-Sleep -Milliseconds 100
  } while ([DateTime]::UtcNow -lt $deadline)
  return $null
}

function Wait-ManagedDaemonExit {
  param([int]$TimeoutSeconds)
  $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
  do {
    if ($null -eq (Get-ManagedDaemon)) { return $true }
    Start-Sleep -Milliseconds 100
  } while ([DateTime]::UtcNow -lt $deadline)
  return $null -eq (Get-ManagedDaemon)
}
`
	windowsInstallBody = `
$null = New-Item -ItemType Directory -Path $launcherRoot -Force
$launcherTemp = $launcherPath + '.tmp-' + [Guid]::NewGuid().ToString('N')
$manifestTemp = $manifestPath + '.tmp-' + [Guid]::NewGuid().ToString('N')
try {
  Set-Content -LiteralPath $launcherTemp -Value $launcherBody -Encoding Unicode -NoNewline
  Move-Item -LiteralPath $launcherTemp -Destination $launcherPath -Force
$manifest = [pscustomobject]@{
    version = 1
    executable = $exe
    arguments = $arguments
    work_dir = $workDir
    launcher = $launcherPath
    pid_file = $pidPath
  }
  Set-Content -LiteralPath $manifestTemp -Value ($manifest | ConvertTo-Json -Compress) -Encoding UTF8 -NoNewline
  Move-Item -LiteralPath $manifestTemp -Destination $manifestPath -Force
} finally {
  Remove-Item -LiteralPath $launcherTemp -Force -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath $manifestTemp -Force -ErrorAction SilentlyContinue
}
$wscript = Join-Path $env:SystemRoot 'System32\wscript.exe'
$taskArguments = '//B //NoLogo "' + $launcherPath + '"'
$action = New-ScheduledTaskAction -Execute $wscript -Argument $taskArguments -WorkingDirectory $workDir
$currentUser = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $currentUser
$settings = New-ScheduledTaskSettingsSet -Hidden -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -MultipleInstances IgnoreNew -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
$principal = New-ScheduledTaskPrincipal -UserId $currentUser -LogonType Interactive -RunLevel Limited
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Settings $settings -Principal $principal -Description 'NekoNest host daemon' -Force | Out-Null
$service = New-Object -ComObject 'Schedule.Service'
$service.Connect()
$root = $service.GetFolder('\')
$registered = $root.GetTask($taskName)
$definition = $registered.Definition
$definition.Settings.ExecutionTimeLimit = 'PT0S'
$definition.Settings.Hidden = $true
$root.RegisterTaskDefinition($taskName, $definition, 6, $currentUser, $null, 3, $null) | Out-Null
`
	windowsUninstallBody = `
$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
Stop-ManagedDaemon
if ($null -ne $existing) {
  Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
}
Remove-Item -LiteralPath $launcherPath -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $manifestPath -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $pidPath -Force -ErrorAction SilentlyContinue
`
	windowsStartBody = `
$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($null -eq $existing) { throw 'host service is not installed; run nekonest-daemon install' }
if ([string]$existing.State -ne 'Running') {
  Start-ScheduledTask -TaskName $taskName
}
$managed = Wait-ManagedDaemon -TimeoutSeconds 15
if ($null -eq $managed) { throw 'NekoNest daemon did not publish its managed process lease' }
`
	windowsStopBody = `
Stop-ManagedDaemon
`
	windowsQueryBody = `
$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($null -eq $existing) {
  [pscustomobject]@{ found = $false } | ConvertTo-Json -Compress
  exit 0
}
$action = $existing.Actions | Select-Object -First 1
$reportedExecute = [string]$action.Execute
$reportedArguments = [string]$action.Arguments
$reportedState = [string]$existing.State
if ((Test-Path -LiteralPath $manifestPath) -and ([IO.Path]::GetFileName($reportedExecute) -ieq 'wscript.exe')) {
  try {
    $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    if ([string]$manifest.launcher -eq $launcherPath -and $reportedArguments.Contains($launcherPath)) {
      $reportedExecute = [string]$manifest.executable
      $reportedArguments = [string]$manifest.arguments
    }
  } catch {}
}
if ($null -ne (Get-ManagedDaemon)) {
  $reportedState = 'Running'
}
[pscustomobject]@{
  found = $true
  state = $reportedState
  enabled = [bool]$existing.Settings.Enabled
  execute = $reportedExecute
  arguments = $reportedArguments
} | ConvertTo-Json -Compress
`
)
