package traylogic

import (
	"context"
	"log/slog"
	"time"

	"github.com/underundre/unet/internal/platform"
)

const (
	healthCheckInterval = 5 * time.Second
	healthCheckTimeout  = 5 * time.Second
	healthFailThreshold = 3 // 3 consecutive failures = daemon dead
	statusPollConnected = 3 * time.Second
	statusPollDisconnected = 10 * time.Second
)

// startHealthPolling starts the daemon health check loop.
func (a *App) startHealthPolling(ctx context.Context) {
	go func() {
		consecutiveFails := 0
		ticker := time.NewTicker(healthCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				alive := a.client.HealthCheck(ctx)
				a.mu.Lock()
				wasAlive := a.state.DaemonAlive
				a.state.DaemonAlive = alive
				a.mu.Unlock()

				if alive {
					consecutiveFails = 0
					if !wasAlive {
						slog.Info("tray: daemon recovered")
						a.notifier.Send("unet", "Daemon recovered", platform.SeverityInfo)
					}
				} else {
					consecutiveFails++
					if wasAlive && consecutiveFails >= healthFailThreshold {
						slog.Warn("tray: daemon appears dead", "failures", consecutiveFails)
						a.notifier.Send("unet", "Daemon stopped", platform.SeverityError)
						a.updateStateIcon()
					}
				}
			}
		}
	}()
}

// startStatusPolling polls daemon status for tunnel/routes info.
func (a *App) startStatusPolling(ctx context.Context) {
	go func() {
		for {
			a.mu.RLock()
			interval := statusPollConnected
			if a.state.TunnelStatus != "connected" {
				interval = statusPollDisconnected
			}
			a.mu.RUnlock()

			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
				a.pollDaemonStatus(ctx)
			}
		}
	}()
}

// pollDaemonStatus fetches and applies the daemon's status.
func (a *App) pollDaemonStatus(ctx context.Context) {
	status, err := a.client.GetStatus(ctx)
	if err != nil {
		// Health check handles daemon liveness.
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	prev := a.state.TunnelStatus
	a.state.TunnelStatus = status.Tunnel.Status
	a.state.VPSName = status.VPS.Host
	a.state.ExposedCount = 0
	for _, p := range status.Ports {
		if p.Status == "active" {
			a.state.ExposedCount++
		}
	}

	// State transition notifications.
	if prev != a.state.TunnelStatus {
		slog.Info("tray: tunnel status changed", "from", prev, "to", a.state.TunnelStatus)
	}

	a.updateStateIconLocked()
}

// updateStateIcon refreshes the tray icon and tooltip from current state.
func (a *App) updateStateIcon() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.updateStateIconLocked()
}

// updateStateIconLocked refreshes icon+tooltip (caller must hold mu).
func (a *App) updateStateIconLocked() {
	switch {
	case !a.state.DaemonAlive:
		a.state.IconState = platform.IconRed
	case a.state.TunnelStatus == "connected":
		a.state.IconState = platform.IconGreen
	case a.state.TunnelStatus == "connecting":
		a.state.IconState = platform.IconYellow
	default:
		a.state.IconState = platform.IconRed
	}

	a.tray.SetIcon(a.state.IconState)
	a.tray.SetTooltip(a.buildTooltipLocked())
}

// buildTooltipLocked builds tooltip text (caller must hold mu).
func (a *App) buildTooltipLocked() string {
	switch {
	case !a.state.DaemonAlive:
		return "unet: Daemon not running"
	case a.state.TunnelStatus == "connected":
		vps := a.state.VPSName
		if vps == "" {
			vps = "unknown"
		}
		return "unet: Connected (" + vps + "), " + itoa2(a.state.ExposedCount) + " routes"
	case a.state.TunnelStatus == "connecting":
		return "unet: Connecting..."
	default:
		return "unet: Disconnected"
	}
}

func itoa2(n int) string {
	if n == 0 {
		return "0"
	}
	// Simple int to string without strconv import.
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
