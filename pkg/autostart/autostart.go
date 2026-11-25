package autostart

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	// AppName is the registry value name under HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
	AppName = "MagicDown"
	// RegistryRunPath is the Windows per-user startup Run key.
	RegistryRunPath = `Software\Microsoft\Windows\CurrentVersion\Run`
)

// Store abstracts the underlying OS startup registry so Manager can be unit-tested
// without mutating the host machine's registry.
type Store interface {
	Get(name string) (string, bool, error)
	Set(name, value string) error
	Delete(name string) error
}

// Manager manages launch-at-startup registration.
type Manager struct {
	appName string
	store   Store
	exePath func() (string, error)
}

// NewManager creates a Manager backed by the given Store.
func NewManager(appName string, store Store, exePath func() (string, error)) *Manager {
	if appName == "" {
		appName = AppName
	}
	if exePath == nil {
		exePath = os.Executable
	}
	return &Manager{
		appName: appName,
		store:   store,
		exePath: exePath,
	}
}

// DefaultManager returns a Manager backed by the current OS's native startup store.
func DefaultManager() *Manager {
	return NewManager(AppName, defaultStore(), os.Executable)
}

// FormatCommand builds the startup command line for the executable.
func FormatCommand(exePath string, minimized bool) string {
	cleaned := strings.TrimSpace(exePath)
	if cleaned == "" {
		return ""
	}
	cmd := fmt.Sprintf(`"%s"`, cleaned)
	if minimized {
		cmd += " --minimized"
	}
	return cmd
}

// ParseCommand extracts the executable path and minimized flag from a Run registry value.
func ParseCommand(raw string) (exePath string, minimized bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	var rest string
	if strings.HasPrefix(s, `"`) {
		end := strings.Index(s[1:], `"`)
		if end >= 0 {
			exePath = s[1 : end+1]
			rest = strings.TrimSpace(s[end+2:])
		} else {
			exePath = strings.Trim(s, `"`)
		}
	} else {
		fields := strings.Fields(s)
		if len(fields) > 0 {
			exePath = fields[0]
			rest = strings.Join(fields[1:], " ")
		}
	}
	for _, arg := range strings.Fields(rest) {
		if arg == "--minimized" || arg == "-minimized" || arg == "--tray" || arg == "-tray" {
			minimized = true
			break
		}
	}
	return exePath, minimized
}

// Enable registers the current executable to start automatically (minimized to tray).
func (m *Manager) Enable() error {
	if m.store == nil {
		return errors.New("autostart store is not available on this platform")
	}
	exe, err := m.exePath()
	if err != nil || strings.TrimSpace(exe) == "" {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}
	return m.store.Set(m.appName, FormatCommand(exe, true))
}

// Disable removes the startup registration if present.
func (m *Manager) Disable() error {
	if m.store == nil {
		return errors.New("autostart store is not available on this platform")
	}
	return m.store.Delete(m.appName)
}

// SetEnabled enables or disables startup registration.
func (m *Manager) SetEnabled(enabled bool) error {
	if enabled {
		return m.Enable()
	}
	return m.Disable()
}

// IsEnabled reports whether startup registration is currently present.
func (m *Manager) IsEnabled() (bool, error) {
	if m.store == nil {
		return false, nil
	}
	val, ok, err := m.store.Get(m.appName)
	if err != nil {
		return false, err
	}
	return ok && strings.TrimSpace(val) != "", nil
}

// Package-level convenience helpers using DefaultManager.
func SetEnabled(enabled bool) error { return DefaultManager().SetEnabled(enabled) }
func IsEnabled() (bool, error)      { return DefaultManager().IsEnabled() }
