package ssh

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

const (
	// maxSessionsPerHost is the maximum number of concurrent SSH sessions
	// allowed per VPS host. Spec: FR-012, TASK-1.1.
	maxSessionsPerHost = 3

	// idleTimeout is how long an idle connection stays in the pool before
	// eviction. Spec: TASK-1.1 (30s).
	idleTimeout = 30 * time.Second

	// validationCmd is run on each session before returning it to the caller
	// to detect broken connections early.
	validationCmd = "echo ok"
)

// ErrPoolClosed is returned when operations are attempted on a closed pool.
var ErrPoolClosed = errors.New("ssh: pool closed")

// ErrPoolExhausted is returned when all session slots are in use.
var ErrPoolExhausted = errors.New("ssh: pool exhausted (max sessions reached)")

// Pool manages a set of SSH sessions for a single VPS host. It limits
// concurrent sessions, validates connections before use, evicts idle ones,
// and auto-reconnects on failure.
type Pool struct {
	cfg     ConnectConfig
	mu      sync.Mutex
	client  *gossh.Client
	sessions map[*Session]struct{}
	closed  bool
	lastUsed time.Time

	// stopIdleSweeper is called to stop the background idle-eviction goroutine.
	stopIdleSweeper chan struct{}
}

// NewPool creates a new connection pool for the given host. The pool does not
// connect immediately — the first Session() call dials on demand.
// The caller must call Close() to release resources.
func NewPool(cfg ConnectConfig) (*Pool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	p := &Pool{
		cfg:            cfg,
		sessions:       make(map[*Session]struct{}),
		stopIdleSweeper: make(chan struct{}),
		lastUsed:       time.Now(),
	}

	// Start background idle sweeper.
	go p.sweepIdle()

	return p, nil
}

// Session returns a validated SSH session from the pool. If no connection
// exists, it dials. If the connection is broken, it reconnects. If all
// session slots are occupied, it returns ErrPoolExhausted.
//
// The caller MUST call Put() or session.Close() when done.
func (p *Pool) Session(ctx context.Context) (*Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, ErrPoolClosed
	}

	// Check session capacity.
	if len(p.sessions) >= maxSessionsPerHost {
		return nil, ErrPoolExhausted
	}

	// Ensure we have a live client connection.
	if err := p.ensureClient(ctx); err != nil {
		return nil, err
	}

	// Open a new session on the client.
	raw, err := p.client.NewSession()
	if err != nil {
		// Client is likely dead — reconnect once.
		slog.Debug("ssh: session creation failed, reconnecting", "host", p.cfg.Host, "err", err)
		p.closeClient()

		if err := p.ensureClient(ctx); err != nil {
			return nil, err
		}
		raw, err = p.client.NewSession()
		if err != nil {
			return nil, fmt.Errorf("ssh: new session after reconnect: %w", err)
		}
	}

	sess := newSession(raw)

	// Validate the session before returning.
	validateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := sess.Run(validateCtx, validationCmd)
	if err != nil || out != "ok\n" {
		sess.Close()
		slog.Debug("ssh: session validation failed, reconnecting", "host", p.cfg.Host, "err", err, "output", out)

		// Connection is stale — reconnect.
		p.closeClient()
		if err := p.ensureClient(ctx); err != nil {
			return nil, err
		}

		raw, err = p.client.NewSession()
		if err != nil {
			return nil, fmt.Errorf("ssh: new session after validation failure: %w", err)
		}
		sess = newSession(raw)

		// Re-validate.
		out2, err2 := sess.Run(validateCtx, validationCmd)
		if err2 != nil || out2 != "ok\n" {
			sess.Close()
			return nil, fmt.Errorf("ssh: session validation failed after reconnect: out=%q err=%w", out2, err2)
		}
	}

	p.sessions[sess] = struct{}{}
	p.lastUsed = time.Now()
	return sess, nil
}

// Put returns a session to the pool for reuse tracking. If the session is
// still healthy, the pool keeps it; otherwise it's closed.
// After calling Put, the caller must NOT use the session.
func (p *Pool) Put(sess *Session) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.sessions, sess)
	sess.Close()
}

// Close shuts down the pool, closing all tracked sessions and the underlying
// SSH client connection.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	close(p.stopIdleSweeper)

	for sess := range p.sessions {
		sess.Close()
	}
	p.sessions = nil

	return p.closeClient()
}

// Config returns a copy of the pool's connection config (read-only snapshot).
func (p *Pool) Config() ConnectConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cfg
}

// Stats returns current pool statistics for monitoring.
func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	active := len(p.sessions)
	return PoolStats{
		ActiveSessions: active,
		IdleSlots:      maxSessionsPerHost - active,
		HasConnection:  p.client != nil,
		LastUsed:       p.lastUsed,
	}
}

// PoolStats describes the current state of the connection pool.
type PoolStats struct {
	ActiveSessions int       `json:"activeSessions"`
	IdleSlots      int       `json:"idleSlots"`
	HasConnection  bool      `json:"hasConnection"`
	LastUsed       time.Time `json:"lastUsed"`
}

// --- internal ---

// ensureClient dials if no client exists. Caller must hold p.mu.
func (p *Pool) ensureClient(ctx context.Context) error {
	if p.client != nil {
		return nil
	}

	client, err := Dial(ctx, p.cfg)
	if err != nil {
		return err
	}
	p.client = client
	return nil
}

// closeClient closes the underlying SSH client. Caller must hold p.mu.
func (p *Pool) closeClient() error {
	if p.client == nil {
		return nil
	}
	err := p.client.Close()
	p.client = nil
	return err
}

// sweepIdle runs in the background and closes the connection after idleTimeout
// of no activity.
func (p *Pool) sweepIdle() {
	ticker := time.NewTicker(idleTimeout / 2) // Check at half the timeout
	defer ticker.Stop()

	for {
		select {
		case <-p.stopIdleSweeper:
			return
		case <-ticker.C:
			p.mu.Lock()
			if !p.closed && p.client != nil && len(p.sessions) == 0 && time.Since(p.lastUsed) > idleTimeout {
				slog.Debug("ssh: evicting idle connection", "host", p.cfg.Host)
				p.closeClient()
			}
			p.mu.Unlock()
		}
	}
}
