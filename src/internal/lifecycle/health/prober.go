// Package health provides periodic VPS health probing over WireGuard tunnel,
// partition detection via 3-consecutive-failure threshold, and exponential-
// backoff reconnect.
package health

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// --- HealthSnapshot (matches data-model.md entity 4) ---

// ContainerStatus describes the status of a single container.
type ContainerStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "running", "restarting", "exited", "unknown"
	Healthy bool  `json:"healthy"`
}

// HealthSnapshot is the result of a single health probe cycle.
type HealthSnapshot struct {
	Timestamp         time.Time         `json:"timestamp"`
	VPSReachable      bool              `json:"vpsReachable"`
	WGTunnelUp        bool              `json:"wgTunnelUp"`
	ContainerStatuses []ContainerStatus `json:"containerStatuses,omitempty"`
	WGHandshakeRecency string           `json:"wgHandshakeRecency,omitempty"`
	Errors            []string          `json:"errors,omitempty"`
	ProbeLatencyMS    int64             `json:"probeLatencyMs"`
}

// --- Injectable transports for testing ---

// Transport is the interface for health probe methods. Injectable for testing.
type Transport interface {
	// Probe checks VPS health. Returns nil error on success.
	Probe(ctx context.Context, target string) error
}

// ICMPPing implements Transport via ICMP ping.
type ICMPPing struct {
	Timeout time.Duration
}

func (p *ICMPPing) Probe(ctx context.Context, target string) error {
	if p.Timeout == 0 {
		p.Timeout = 3 * time.Second
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
		deadline, _ = ctx.Deadline()
	}
	_ = deadline

	// ICMP requires raw sockets — use net.DialTimeout as a connectivity check.
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		host = target
	}
	conn, err := net.DialTimeout("ip4:icmp", host, p.Timeout)
	if err != nil {
		return fmt.Errorf("ICMP ping failed: %w", err)
	}
	conn.Close()
	return nil
}

// HTTPCheck implements Transport via HTTP GET.
type HTTPCheck struct {
	Client *http.Client
}

func (h *HTTPCheck) Probe(ctx context.Context, target string) error {
	if h.Client == nil {
		h.Client = &http.Client{Timeout: 5 * time.Second}
	}
	url := target
	if len(url) > 0 && url[0] != 'h' {
		url = "http://" + target
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("HTTP probe: %w", err)
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP probe failed: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("HTTP probe: server error %d", resp.StatusCode)
	}
	return nil
}

// --- Prober ---

// ProberConfig holds prober configuration.
type ProberConfig struct {
	// Interval between probes. Default 15s.
	Interval time.Duration
	// FailureThreshold consecutive failures before triggering reconnect. Default 3.
	FailureThreshold int
	// ICMP target (host:port or just IP). Optional — uses WG IP.
	ICMPTarget string
	// HTTP target (URL). Optional — uses Caddy admin endpoint.
	HTTPTarget string
}

// Prober performs periodic health checks and emits signals on state changes.
type Prober struct {
	cfg     ProberConfig
	icmp    Transport
	http    Transport

	mu              sync.RWMutex
	lastSnapshot    *HealthSnapshot
	consecutiveFail int
	running         bool

	// OnReconnect is called when failureThreshold is reached.
	// The prober stops itself while reconnect is in progress.
	OnReconnect func(ctx context.Context)

	// OnSnapshot is called after each probe with the result.
	OnSnapshot func(snap HealthSnapshot)

	// Now returns the current time. Injectable for testing.
	Now func() time.Time
}

// NewProber creates a health prober with the given config and transports.
func NewProber(cfg ProberConfig, icmpTransport, httpTransport Transport) *Prober {
	if cfg.Interval == 0 {
		cfg.Interval = 15 * time.Second
	}
	if cfg.FailureThreshold == 0 {
		cfg.FailureThreshold = 3
	}
	p := &Prober{
		cfg:  cfg,
		icmp: icmpTransport,
		http: httpTransport,
		Now:  time.Now,
	}
	if p.icmp == nil {
		p.icmp = &ICMPPing{}
	}
	if p.http == nil {
		p.http = &HTTPCheck{}
	}
	return p
}

// Run starts the probe loop. Blocks until ctx is cancelled.
func (p *Prober) Run(ctx context.Context) {
	p.mu.Lock()
	p.running = true
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
	}()

	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := p.probe(ctx)
			p.processSnapshot(ctx, snap)
		}
	}
}

// probe executes a single health check cycle.
func (p *Prober) probe(ctx context.Context) HealthSnapshot {
	start := p.Now()
	snap := HealthSnapshot{
		Timestamp: start,
	}

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Try ICMP first.
	icmpErr := p.icmp.Probe(probeCtx, p.cfg.ICMPTarget)
	if icmpErr == nil {
		snap.VPSReachable = true
		snap.WGTunnelUp = true
	} else {
		// Fallback to HTTP.
		httpErr := p.http.Probe(probeCtx, p.cfg.HTTPTarget)
		if httpErr == nil {
			snap.VPSReachable = true
			snap.WGTunnelUp = true
		} else {
			snap.Errors = append(snap.Errors, icmpErr.Error(), httpErr.Error())
		}
	}

	snap.ProbeLatencyMS = time.Since(start).Milliseconds()
	return snap
}

// processSnapshot updates internal state and triggers callbacks.
func (p *Prober) processSnapshot(ctx context.Context, snap HealthSnapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.lastSnapshot = &snap

	if snap.VPSReachable {
		p.consecutiveFail = 0
	} else {
		p.consecutiveFail++
		slog.Warn("health: probe failed",
			"consecutive", p.consecutiveFail,
			"threshold", p.cfg.FailureThreshold,
			"errors", snap.Errors)
	}

	// Emit snapshot callback.
	if p.OnSnapshot != nil {
		p.OnSnapshot(snap)
	}

	// Check threshold.
	if p.consecutiveFail >= p.cfg.FailureThreshold && p.OnReconnect != nil {
		slog.Warn("health: failure threshold reached, triggering reconnect",
			"failures", p.consecutiveFail)
		// Reset counter to avoid re-triggering.
		p.consecutiveFail = 0
		// Call reconnect handler.
		p.OnReconnect(ctx)
	}
}

// Snapshot returns the most recent health snapshot (cached, no probe-on-demand).
func (p *Prober) Snapshot() *HealthSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastSnapshot
}

// IsRunning returns whether the prober loop is active.
func (p *Prober) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}
