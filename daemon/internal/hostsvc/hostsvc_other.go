//go:build !windows && !linux

package hostsvc

import "runtime"

func serviceBaseName() string { return "nekonest-daemon" }

func (m *Manager) Install() error   { return ErrUnsupported }
func (m *Manager) Uninstall() error { return ErrUnsupported }
func (m *Manager) Start() error     { return ErrUnsupported }
func (m *Manager) Stop() error      { return ErrUnsupported }

func (m *Manager) Status() (Status, error) {
	return Status{
		Supported:  false,
		Platform:   runtime.GOOS,
		UnitName:   m.UnitName(),
		Executable: m.spec.Executable,
		ConfigPath: m.spec.ConfigPath,
	}, ErrUnsupported
}
