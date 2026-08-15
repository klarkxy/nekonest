//go:build windows

package hostsvc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

func serviceBaseName() string { return "NekoNestDaemon" }

type windowsTaskQuery struct {
	Found     bool   `json:"found"`
	State     string `json:"state"`
	Enabled   bool   `json:"enabled"`
	Execute   string `json:"execute"`
	Arguments string `json:"arguments"`
}

func (m *Manager) Install() error {
	if m.spec.Executable == "" {
		return fmt.Errorf("daemon executable path is required")
	}
	if err := validateSupervisorPath("daemon executable", m.spec.Executable); err != nil {
		return err
	}
	if err := validateSupervisorPath("config", m.spec.ConfigPath); err != nil {
		return err
	}
	return m.runWindows("install")
}

func (m *Manager) Uninstall() error {
	return m.runWindows("uninstall")
}

func (m *Manager) Start() error {
	query, err := m.queryWindows()
	if err != nil {
		return err
	}
	if !query.Found {
		return fmt.Errorf("host service is not installed; run nekonest-daemon install")
	}
	// The scheduled-task wrapper can report Running before the child daemon has
	// published its managed-process lease. Always enter the start script: it is
	// idempotent for an already-running task and waits for the PID-ready handshake.
	return m.runWindows("start")
}

func (m *Manager) Stop() error {
	return m.runWindows("stop")
}

func (m *Manager) Status() (Status, error) {
	st := Status{
		Supported:  true,
		Platform:   "windows",
		Supervisor: "scheduled_task",
		UnitName:   m.UnitName(),
		Executable: m.spec.Executable,
		ConfigPath: m.spec.ConfigPath,
	}
	query, err := m.queryWindows()
	if err != nil {
		return st, err
	}
	st.Installed = query.Found
	st.Enabled = query.Found && query.Enabled
	st.SupervisorState = query.State
	st.ConfiguredExec = query.Execute
	st.ConfiguredArgs = query.Arguments
	return st, nil
}

func (m *Manager) runWindows(action string) error {
	script := RenderWindowsTaskScript(
		action,
		m.UnitName(),
		m.spec.Executable,
		filepath.Dir(m.spec.Executable),
		daemonConfigArgs(m.spec.ConfigPath),
	)
	_, _, err := m.run("", "powershell.exe", powershellArgs(script)...)
	if err != nil {
		return fmt.Errorf("scheduled task %s: %w", action, err)
	}
	return nil
}

func (m *Manager) queryWindows() (windowsTaskQuery, error) {
	script := RenderWindowsTaskScript(
		"query",
		m.UnitName(),
		m.spec.Executable,
		filepath.Dir(m.spec.Executable),
		daemonConfigArgs(m.spec.ConfigPath),
	)
	stdout, _, err := m.run("", "powershell.exe", powershellArgs(script)...)
	if err != nil {
		return windowsTaskQuery{}, fmt.Errorf("scheduled task query: %w", err)
	}
	var query windowsTaskQuery
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &query); err != nil {
		return windowsTaskQuery{}, fmt.Errorf("decode scheduled task status: %w", err)
	}
	return query, nil
}

func powershellArgs(script string) []string {
	units := utf16.Encode([]rune(script))
	raw := make([]byte, len(units)*2)
	for i, unit := range units {
		raw[i*2] = byte(unit)
		raw[i*2+1] = byte(unit >> 8)
	}
	return []string{
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(raw),
	}
}
