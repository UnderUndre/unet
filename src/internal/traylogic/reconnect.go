package traylogic

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/underundre/unet/internal/platform"
)

const (
	backoffInitial = 1 * time.Second
	backoffCap     = 60 * time.Second
	backoffMult    = 2.0
)

// startReconnectLoop listens for network events and orchestrates reconnect.
func (a *App) startReconnectLoop(ctx context.Context) {
	go func() {
		events := a.monitor.Watch(ctx)
		if events == nil {
			slog.Warn("tray: network monitor not available")
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-events:
				if !ok {
					return
				}
				if evt.Type == platform.NetworkReachabilityLost {
					slog.Info("tray: network lost, starting reconnect backoff")
					a.doReconnectBackoff(ctx, events)
				}
			}
		}
	}()
}

// doReconnectBackoff attempts reconnect with exponential backoff.
// Listens for network restoration events to interrupt backoff sleep.
func (a *App) doReconnectBackoff(ctx context.Context, events <-chan platform.NetworkEvent) {
	delay := backoffInitial
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Set state to connecting.
		a.mu.Lock()
		a.state.TunnelStatus = "connecting"
		a.updateStateIconLocked()
		a.mu.Unlock()

		attempt++
		slog.Info("tray: reconnect attempt", "attempt", attempt, "delay", delay)

		// Disconnect first (full awg-quick down/up cycle per FR-010).
		if err := a.client.Disconnect(ctx); err != nil {
			slog.Debug("tray: disconnect before reconnect (may be expected)", "error", err)
		}

		// Attempt connect.
		if err := a.client.Connect(ctx); err != nil {
			slog.Warn("tray: reconnect failed", "attempt", attempt, "error", err)
		} else {
			slog.Info("tray: reconnected", "attempt", attempt)
			a.notifier.Send("unet", "Tunnel connected", platform.SeverityInfo)
			return
		}

		// Persistent error notification after 3 failures.
		if attempt == 3 {
			a.notifier.Send("unet", "Cannot reach VPS — check network", platform.SeverityError)
		}

		// Wait with backoff, but wake early if network restores.
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			if evt.Type == platform.NetworkReachabilityRestored {
				slog.Info("tray: network restored, attempting immediate reconnect")
				delay = backoffInitial
			}
		case <-time.After(delay):
		}

		// Exponential backoff: min(delay * 2, cap).
		delay = time.Duration(math.Min(float64(delay)*backoffMult, float64(backoffCap)))
	}
}
