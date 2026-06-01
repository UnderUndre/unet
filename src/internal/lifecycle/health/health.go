package health

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/underundre/unet/internal/ssh"
	"github.com/underundre/unet/internal/state"
)

// Manager coordinates the health prober and reconnect logic for a single VPS.
// It manages lifecycle: start/stop prober, handle reconnect triggers, expose
// cached snapshots.
type Manager struct {
	pool    *ssh.Pool
	profile *state.VPSProfile

	prober     *Prober
	reconnect  *Reconnector
	cancelFunc context.CancelFunc

	mu       sync.RWMutex
	running  bool
	snapshot *HealthSnapshot
}

// NewManager creates a health manager for the given VPS pool.
func NewManager(pool *ssh.Pool, profile *state.VPSProfile) *Manager {
	m := &Manager{
		pool:    pool,
		profile: profile,
	}

	// Build prober.
	icmpTarget := profile.WGEndpoint
	httpTarget := profile.TunnelSubnet
	if httpTarget != "" {
		httpTarget = httpTarget + ":2019/config/"
	}

	m.prober = NewProber(ProberConfig{
		ICMPTarget: icmpTarget,
		HTTPTarget: httpTarget,
	}, nil, nil)

	// Build reconnector.
	m.reconnect = NewReconnector(pool)

	// Wire prober → reconnect.
	m.prober.OnReconnect = func(ctx context.Context) {
		slog.Info("health: reconnect triggered by prober")
		m.handleReconnect(ctx)
	}

	// Wire prober → cached snapshot.
	m.prober.OnSnapshot = func(snap HealthSnapshot) {
		m.mu.Lock()
		m.snapshot = &snap
		m.mu.Unlock()
	}

	return m
}

// Start begins the health probe loop in a background goroutine.
// No-op if already running. Call Stop to shut down.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	m.cancelFunc = cancel
	m.running = true

	go func() {
		slog.Info("health: starting prober", "host", m.profile.Host)
		m.prober.Run(ctx)
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
		slog.Info("health: prober stopped")
	}()
}

// Stop shuts down the health prober gracefully.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancelFunc != nil {
		m.cancelFunc()
		m.cancelFunc = nil
	}
	m.running = false
}

// Snapshot returns the most recent cached health snapshot.
// No probe-on-demand — returns nil if no probe has run yet.
func (m *Manager) Snapshot() *HealthSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}

// ReconnectState returns the current reconnect state.
func (m *Manager) ReconnectState() ReconnectState {
	return m.reconnect.State()
}

// IsRunning reports whether the prober loop is active.
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// handleReconnect executes a reconnect attempt. If it fails, schedules retry.
func (m *Manager) handleReconnect(ctx context.Context) {
	err := m.reconnect.Execute(ctx)
	if err != nil {
		slog.Warn("health: reconnect failed, will retry", "err", err)
		// Schedule retry after backoff delay.
		go func() {
			rs := m.reconnect.State()
			select {
			case <-ctx.Done():
				return
			case <-timeAfter(rs.CurrentDelay):
				if ctx.Err() == nil {
					m.handleReconnect(ctx)
				}
			}
		}()
	}
}

// timeAfter wraps time.After for testability.
var timeAfter = time.After
