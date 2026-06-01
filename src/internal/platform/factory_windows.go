//go:build windows

package platform

import "context"

// stubTray is unused — tray_windows.go provides the real NewTray().
// This file exists to keep the build tag consistent and provide
// non-tray factory functions if needed.

// Ensure stubTray satisfies Tray at compile time.
var _ Tray = (*stubTray)(nil)

type stubTray struct{}

func (s *stubTray) Run(_ context.Context) error                  { return nil }
func (s *stubTray) SetIcon(_ IconState)                          {}
func (s *stubTray) SetTooltip(_ string)                          {}
func (s *stubTray) SetMenu(_ []MenuItem)                         {}
func (s *stubTray) SetMenuItemLabel(_, _ string)                 {}
func (s *stubTray) SetMenuItemDisabled(_ string, _ bool)         {}
func (s *stubTray) SetMenuItemChecked(_ string, _ bool)          {}
