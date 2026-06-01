// Package platform defines cross-platform abstractions for desktop integration.
// Implementations are build-tagged per OS.
package platform

import "context"

// --- IconState ---

// IconState represents the tray icon visual state.
type IconState int

const (
	IconGreen  IconState = iota // Connected: tunnel up, VPS reachable
	IconYellow                  // Connecting/transient: reconnect in progress
	IconRed                     // Disconnected/error: tunnel down or daemon dead
)

// --- MenuItem ---

// MenuItem represents a single entry in the tray context menu.
type MenuItem struct {
	ID       string
	Label    string
	Disabled bool
	Checked  bool
	SubItems []MenuItem // Sub-menu items
	OnClick  func()
}

// --- Tray interface ---

// Tray is the cross-platform system tray interface.
type Tray interface {
	// Run starts the tray event loop. Blocks until ctx is cancelled or
	// the tray is closed by the user.
	Run(ctx context.Context) error

	// SetIcon changes the tray icon to reflect the given state.
	SetIcon(state IconState)

	// SetTooltip sets the hover tooltip text.
	SetTooltip(text string)

	// SetMenu updates the entire context menu.
	SetMenu(items []MenuItem)

	// SetMenuItemLabel updates a single menu item's label by ID.
	SetMenuItemLabel(id, label string)

	// SetMenuItemDisabled enables/disables a menu item by ID.
	SetMenuItemDisabled(id string, disabled bool)

	// SetMenuItemChecked checks/unchecks a menu item by ID.
	SetMenuItemChecked(id string, checked bool)
}

// --- Notifier interface ---

// Severity levels for notifications.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityError
)

// Notifier sends OS-native notifications.
type Notifier interface {
	Send(title, body string, severity Severity) error
}

// --- NetworkMonitor interface ---

// NetworkEvent represents a detected network change.
type NetworkEvent struct {
	Type      NetworkEventType
	Timestamp string
	Details   string
}

// NetworkEventType describes what changed.
type NetworkEventType int

const (
	NetworkReachabilityLost    NetworkEventType = iota
	NetworkReachabilityRestored
	NetworkDefaultRouteChanged
)

// NetworkMonitor watches for network connectivity changes.
type NetworkMonitor interface {
	// Watch returns a channel that emits network events. The monitor runs
	// until ctx is cancelled.
	Watch(ctx context.Context) <-chan NetworkEvent
}

// --- AutoStart interface ---

// AutoStart manages login autostart configuration.
type AutoStart interface {
	// Enable registers the binary for autostart at login.
	Enable() error
	// Disable removes the autostart registration.
	Disable() error
	// IsEnabled reports whether autostart is currently enabled.
	IsEnabled() bool
}
