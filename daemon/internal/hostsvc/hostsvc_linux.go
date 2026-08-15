//go:build linux

package hostsvc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func serviceBaseName() string { return "nekonest-daemon" }

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
	path := m.unitPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create systemd user directory: %w", err)
	}
	body := RenderSystemdUserUnit(m.spec.Executable, m.spec.ConfigPath)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write systemd user unit: %w", err)
	}
	unit := m.UnitName() + ".service"
	if _, _, err := m.run("", "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if _, _, err := m.run("", "systemctl", "--user", "enable", unit); err != nil {
		return fmt.Errorf("systemctl enable: %w", err)
	}
	return nil
}

func (m *Manager) Uninstall() error {
	unit := m.UnitName() + ".service"
	_, _, _ = m.run("", "systemctl", "--user", "stop", unit)
	_, _, _ = m.run("", "systemctl", "--user", "disable", unit)
	path := m.unitPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove systemd user unit: %w", err)
	}
	_, _, _ = m.run("", "systemctl", "--user", "daemon-reload")
	return nil
}

func (m *Manager) Start() error {
	if _, err := os.Stat(m.unitPath()); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("host service is not installed; run nekonest-daemon install")
		}
		return fmt.Errorf("stat systemd user unit: %w", err)
	}
	if _, _, err := m.run("", "systemctl", "--user", "start", m.UnitName()+".service"); err != nil {
		return fmt.Errorf("systemctl start: %w", err)
	}
	return nil
}

func (m *Manager) Stop() error {
	if _, err := os.Stat(m.unitPath()); os.IsNotExist(err) {
		return nil
	}
	if _, _, err := m.run("", "systemctl", "--user", "stop", m.UnitName()+".service"); err != nil {
		return fmt.Errorf("systemctl stop: %w", err)
	}
	return nil
}

func (m *Manager) Status() (Status, error) {
	st := Status{
		Supported:  true,
		Platform:   "linux",
		Supervisor: "systemd_user",
		UnitName:   m.UnitName(),
		Executable: m.spec.Executable,
		ConfigPath: m.spec.ConfigPath,
	}
	if _, err := os.Stat(m.unitPath()); err == nil {
		st.Installed = true
		if body, readErr := os.ReadFile(m.unitPath()); readErr == nil {
			st.ConfiguredExec = parseSystemdExecStart(string(body))
		} else {
			st.Detail = "read installed unit: " + readErr.Error()
		}
	} else if !os.IsNotExist(err) {
		return st, fmt.Errorf("stat systemd user unit: %w", err)
	}
	stdout, _, err := m.run("", "systemctl", "--user", "show", m.UnitName()+".service",
		"--property=ActiveState", "--property=UnitFileState", "--property=FragmentPath")
	if err != nil {
		if !st.Installed {
			return st, nil
		}
		st.Detail = err.Error()
		return st, nil
	}
	props := parseSystemdShow(stdout)
	if props["ActiveState"] != "" {
		st.SupervisorState = props["ActiveState"]
	}
	switch props["UnitFileState"] {
	case "enabled", "enabled-runtime", "static", "linked", "linked-runtime":
		st.Enabled = true
		st.Installed = true
	case "disabled", "generated":
		st.Installed = true
	}
	if frag := strings.TrimSpace(props["FragmentPath"]); frag != "" {
		st.Installed = true
		if st.Detail == "" {
			st.Detail = "unit_file=" + frag
		}
	}
	return st, nil
}

func parseSystemdExecStart(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && key == "ExecStart" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (m *Manager) unitPath() string {
	dir := m.spec.UnitDir
	if dir == "" {
		cfgDir, err := os.UserConfigDir()
		if err != nil || cfgDir == "" {
			home, _ := os.UserHomeDir()
			cfgDir = filepath.Join(home, ".config")
		}
		dir = filepath.Join(cfgDir, "systemd", "user")
	}
	return filepath.Join(dir, m.UnitName()+".service")
}

func parseSystemdShow(raw string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		out[key] = value
	}
	return out
}
