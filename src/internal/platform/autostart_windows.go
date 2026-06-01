//go:build windows

package platform

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath    = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
	runKeyName    = "unet"
)

// WindowsAutoStart implements AutoStart via Windows Registry HKCU Run key.
type WindowsAutoStart struct{}

func NewAutoStart() AutoStart {
	return &WindowsAutoStart{}
}

func (a *WindowsAutoStart) Enable() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("autostart: get executable path: %w", err)
	}
	// Resolve symlinks.
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("autostart: resolve symlinks: %w", err)
	}

	// MUST quote the path to handle spaces (F10 from review).
	value := fmt.Sprintf(`"%s"`, exe)

	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("autostart: open registry key: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue(runKeyName, value); err != nil {
		return fmt.Errorf("autostart: set registry value: %w", err)
	}

	slog.Info("autostart: enabled", "path", value)
	return nil
}

func (a *WindowsAutoStart) Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		// Key doesn't exist = already disabled.
		return nil
	}
	defer k.Close()

	if err := k.DeleteValue(runKeyName); err != nil {
		// Value doesn't exist = already disabled.
		return nil
	}

	slog.Info("autostart: disabled")
	return nil
}

func (a *WindowsAutoStart) IsEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	val, _, err := k.GetStringValue(runKeyName)
	if err != nil {
		return false
	}

	// Verify the path matches current binary (F7: sync on start).
	exe, err := os.Executable()
	if err != nil {
		return val != ""
	}
	exe, _ = filepath.EvalSymlinks(exe)

	// Unquote stored value for comparison.
	stored := val
	if len(stored) >= 2 && stored[0] == '"' && stored[len(stored)-1] == '"' {
		stored = stored[1 : len(stored)-1]
	}

	return stored == exe
}
