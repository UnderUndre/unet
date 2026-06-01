//go:build !windows

package platform

import "context"

// DarwinTray is a macOS stub — not yet implemented.
type DarwinTray struct{}

func NewTray() Tray { return &DarwinTray{} }

func (d *DarwinTray) Run(_ context.Context) error                  { return nil }
func (d *DarwinTray) SetIcon(_ IconState)                          {}
func (d *DarwinTray) SetTooltip(_ string)                          {}
func (d *DarwinTray) SetMenu(_ []MenuItem)                         {}
func (d *DarwinTray) SetMenuItemLabel(_, _ string)                 {}
func (d *DarwinTray) SetMenuItemDisabled(_ string, _ bool)         {}
func (d *DarwinTray) SetMenuItemChecked(_ string, _ bool)          {}

// DarwinNotifier is a macOS notification stub.
type DarwinNotifier struct{}

func NewNotifier() Notifier { return &DarwinNotifier{} }

func (d *DarwinNotifier) Send(_, _ string, _ Severity) error { return nil }

// DarwinAutoStart is a macOS LaunchAgent stub.
type DarwinAutoStart struct{}

func NewAutoStart() AutoStart { return &DarwinAutoStart{} }

func (d *DarwinAutoStart) Enable() error  { return nil }
func (d *DarwinAutoStart) Disable() error { return nil }
func (d *DarwinAutoStart) IsEnabled() bool { return false }

// DarwinNetworkMonitor is a macOS network monitor stub.
type DarwinNetworkMonitor struct{}

func NewNetworkMonitor() NetworkMonitor { return &DarwinNetworkMonitor{} }

func (d *DarwinNetworkMonitor) Watch(_ context.Context) <-chan NetworkEvent {
	ch := make(chan NetworkEvent)
	close(ch)
	return ch
}
