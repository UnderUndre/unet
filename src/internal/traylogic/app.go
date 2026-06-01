// Package traylogic is the orchestrator for the system tray application.
// It bridges platform abstractions (tray, notifier, network monitor)
// with the daemon API client.
package traylogic

import (
	"context"
	"fmt"
	"sync"

	"github.com/underundre/unet/internal/platform"
	"github.com/underundre/unet/internal/trayapi"
)

// App is the top-level tray application orchestrator.
type App struct {
	version   string
	tray      platform.Tray
	notifier  platform.Notifier
	monitor   platform.NetworkMonitor
	autostart platform.AutoStart
	client    *trayapi.Client
	cancel    context.CancelFunc

	mu    sync.RWMutex
	state TrayState
}

// TrayState is the current logical state of the tray.
type TrayState struct {
	IconState    platform.IconState
	TunnelStatus string // "connected", "disconnected", "connecting"
	DaemonAlive  bool
	VPSName      string
	ExposedCount int
}

// NewApp creates a new tray application.
func NewApp(version string) (*App, error) {
	client, err := trayapi.NewClient()
	if err != nil {
		return nil, fmt.Errorf("traylogic: create API client: %w", err)
	}

	return &App{
		version:   version,
		tray:      platform.NewTray(),
		notifier:  newThrottledNotifier(platform.NewNotifier()),
		monitor:   platform.NewNetworkMonitor(),
		autostart: platform.NewAutoStart(),
		client:    client,
		state: TrayState{
			IconState:    platform.IconRed,
			TunnelStatus: "disconnected",
			DaemonAlive:  false,
		},
	}, nil
}

// Run starts the tray event loop and background goroutines.
// Blocks until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	ctx, a.cancel = context.WithCancel(ctx)

	// Sync autostart path on launch (F7 from review).
	if a.autostart.IsEnabled() {
		// Re-enable to update path if binary moved.
		_ = a.autostart.Enable()
	}

	// Start background goroutines.
	a.startHealthPolling(ctx)
	a.startStatusPolling(ctx)
	a.startReconnectLoop(ctx)

	// Set initial menu and icon.
	a.tray.SetIcon(platform.IconRed)
	a.tray.SetTooltip("unet: starting...")
	a.tray.SetMenu(a.buildMenu())

	// Run the tray event loop (blocks).
	return a.tray.Run(ctx)
}

// State returns the current tray state (thread-safe).
func (a *App) State() TrayState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}
