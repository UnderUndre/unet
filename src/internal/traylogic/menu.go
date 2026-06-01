package traylogic

import (
	"context"
	"log/slog"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/underundre/unet/internal/platform"
)

// menuID constants for tray menu items.
const (
	menuConnect      = "connect"
	menuDisconnect   = "disconnect"
	menuCopyURL      = "copy_url"
	menuOpenAdmin    = "open_admin"
	menuSep1         = "sep1"
	menuAutostart    = "autostart"
	menuSep2         = "sep2"
	menuAbout        = "about"
	menuRestart      = "restart_daemon"
	menuQuit         = "quit"
)

// buildMenu creates the tray context menu items based on current state.
func (a *App) buildMenu() []platform.MenuItem {
	a.mu.RLock()
	defer a.mu.RUnlock()

	st := a.state

	items := []platform.MenuItem{}

	// Connect / Disconnect toggle.
	if st.DaemonAlive {
		if st.TunnelStatus == "connected" {
			items = append(items, platform.MenuItem{
				ID:    menuDisconnect,
				Label: "Disconnect",
				OnClick: func() {
					a.actionDisconnect()
				},
			})
		} else {
			items = append(items, platform.MenuItem{
				ID:    menuConnect,
				Label: "Connect",
				OnClick: func() {
					a.actionConnect()
				},
			})
		}
	}

	// Copy public URL.
	disabled := st.ExposedCount == 0
	items = append(items, platform.MenuItem{
		ID:       menuCopyURL,
		Label:    "Copy public URL",
		Disabled: disabled,
		OnClick: func() {
			a.actionCopyURL()
		},
	})

	// Open admin UI.
	items = append(items, platform.MenuItem{
		ID:    menuOpenAdmin,
		Label: "Open admin UI",
		OnClick: func() {
			a.actionOpenAdmin()
		},
	})

	// Separator.
	items = append(items, platform.MenuItem{ID: menuSep1})

	// Start at login.
	autostartChecked := a.autostart.IsEnabled()
	items = append(items, platform.MenuItem{
		ID:      menuAutostart,
		Label:   "Start at login",
		Checked: autostartChecked,
		OnClick: func() {
			a.actionToggleAutostart()
		},
	})

	// Separator.
	items = append(items, platform.MenuItem{ID: menuSep2})

	// Restart daemon (only shown when daemon is dead).
	if !st.DaemonAlive {
		items = append(items, platform.MenuItem{
			ID:    menuRestart,
			Label: "Restart daemon",
			OnClick: func() {
				a.actionRestartDaemon()
			},
		})
	}

	// About.
	items = append(items, platform.MenuItem{
		ID:    menuAbout,
		Label: "About",
		OnClick: func() {
			a.actionAbout()
		},
	})

	// Quit.
	items = append(items, platform.MenuItem{
		ID:    menuQuit,
		Label: "Quit",
		OnClick: func() {
			a.actionQuit()
		},
	})

	return items
}

// --- Click handlers ---

func (a *App) actionConnect() {
	slog.Info("tray: connect clicked")
	ctx := context.Background()
	if err := a.client.Connect(ctx); err != nil {
		slog.Error("tray: connect failed", "error", err)
		a.notifier.Send("unet", "Failed to connect tunnel", platform.SeverityError)
	}
}

func (a *App) actionDisconnect() {
	slog.Info("tray: disconnect clicked")
	ctx := context.Background()
	if err := a.client.Disconnect(ctx); err != nil {
		slog.Error("tray: disconnect failed", "error", err)
	}
}

func (a *App) actionCopyURL() {
	slog.Info("tray: copy URL clicked")
	ctx := context.Background()
	urls, err := a.client.GetPortURLs(ctx)
	if err != nil {
		slog.Error("tray: get URLs failed", "error", err)
		return
	}
	if len(urls) == 0 {
		return
	}
	// Copy first URL to clipboard.
	if err := clipboardWrite(urls[0]); err != nil {
		slog.Error("tray: clipboard write failed", "error", err)
	}
}

func (a *App) actionOpenAdmin() {
	slog.Info("tray: open admin UI clicked")
	port := 8080 // TODO: discover from client
	url := "http://localhost:" + strconv.Itoa(port)
	if err := openBrowser(url); err != nil {
		slog.Error("tray: open browser failed", "error", err)
	}
}

func (a *App) actionToggleAutostart() {
	if a.autostart.IsEnabled() {
		slog.Info("tray: disabling autostart")
		if err := a.autostart.Disable(); err != nil {
			slog.Error("tray: disable autostart failed", "error", err)
		}
	} else {
		slog.Info("tray: enabling autostart")
		if err := a.autostart.Enable(); err != nil {
			slog.Error("tray: enable autostart failed", "error", err)
		}
	}
	// Rebuild menu to reflect new state.
	a.tray.SetMenu(a.buildMenu())
}

func (a *App) actionRestartDaemon() {
	slog.Info("tray: restart daemon clicked")
	// TODO: discover daemon binary path and relaunch.
	// For now, just log.
	slog.Warn("tray: daemon restart not yet implemented")
}

func (a *App) actionAbout() {
	slog.Info("tray: about clicked")
	a.notifier.Send("unet", "unet v"+a.version, platform.SeverityInfo)
}

func (a *App) actionQuit() {
	slog.Info("tray: quit clicked")
	a.cancel()
}

// --- Helpers ---

func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	c := exec.Command(cmd, args...)
	if err := c.Start(); err != nil {
		return err
	}
	go c.Wait() // Prevent zombie processes.
	return nil
}

func clipboardWrite(text string) error {
	cmd := exec.Command("clip")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
