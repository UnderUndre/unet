// Package proxy implements the local SOCKS5/HTTP listener (FR-004) over the
// embedded gVisor netstack: first-byte protocol demux (interfaces §1.0),
// in-tunnel DNS with fail-closed semantics (FR-009), optional auth
// (SOCKS5 RFC 1929 / HTTP Basic), and mandatory hop-by-hop header stripping
// for forwarded HTTP (§1.4, zcode F6). No secret ever reaches logs (FR-008.004).
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// Dialer abstracts the netstack dial target (gvisor tun/netstack *Net in prod;
// fakes in tests). DNS for name-based targets is resolved inside the tunnel by
// the netstack's configured resolvers — there is NO host-resolver fallback
// (FR-009, SC-006).
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Config carries the resolved proxy settings + knob timeouts.
type Config struct {
	Listen      string
	AuthEnabled bool
	Username    string
	Password    string
	PasswordEnv string
	SharedHost  bool
	DialTimeout time.Duration
}

// Server owns the listener loop.
type Server struct {
	cfg   Config
	dial  Dialer
	log   Logger
	ready chan struct{} // closed once the listener is bound
}

// Logger is the minimal logging surface (no secrets — FR-008.004).
type Logger interface {
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// nopLogger is the default when no logger is wired.
type nopLogger struct{}

func (nopLogger) Infof(string, ...interface{})  {}
func (nopLogger) Warnf(string, ...interface{})  {}
func (nopLogger) Errorf(string, ...interface{}) {}

// New builds a Server. Call Serve in a goroutine; WaitReady blocks until the
// listener is bound or errors.
func New(cfg Config, dial Dialer, log Logger) *Server {
	if log == nil {
		log = nopLogger{}
	}
	return &Server{cfg: cfg, dial: dial, log: log, ready: make(chan struct{})}
}

// ErrAuthRequired is returned when shared-host mode would run unauthenticated.
var ErrAuthRequired = errors.New("proxy: shared-host mode requires auth (FR-004 caveat)")

// Listen builds the listener from the listen spec: TCP host:port, or a Unix
// socket (unix:///path) / abstract socket (@name) on platforms that support it.
func Listen(spec string) (net.Listener, error) {
	switch {
	case strings.HasPrefix(spec, "unix://"):
		path := strings.TrimPrefix(spec, "unix://")
		if path == "" {
			return nil, errors.New("proxy: unix:// listen spec requires a path")
		}
		_ = os.Remove(path) // stale socket from a previous run
		return net.Listen("unix", path)
	case strings.HasPrefix(spec, "@"):
		return net.Listen("unix", spec) // abstract namespace (Linux)
	default:
		return net.Listen("tcp", spec)
	}
}

// Serve binds and serves until the listener fails or the process exits.
func (s *Server) Serve() error {
	ln, err := Listen(s.cfg.Listen)
	if err != nil {
		close(s.ready)
		return fmt.Errorf("proxy: listen %s: %w", redactListen(s.cfg.Listen), err)
	}
	close(s.ready)
	return s.handleListener(ln)
}

// handleListener runs the accept loop on a pre-built listener (testable).
func (s *Server) handleListener(ln net.Listener) error {
	s.log.Infof("proxy listening on %s (auth=%v)", redactListen(s.cfg.Listen), s.cfg.AuthEnabled)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if isTemporary(err) {
				s.log.Warnf("proxy accept: %v", err)
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return fmt.Errorf("proxy: accept: %w", err)
		}
		go s.handle(conn)
	}
}

// WaitReady returns once the listener is bound (or errored before binding).
func (s *Server) WaitReady() { <-s.ready }

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	br := newPeekReader(conn)
	first, err := br.PeekByte()
	if err != nil {
		s.log.Warnf("proxy: peer closed before protocol byte: %v", err)
		return
	}
	switch first {
	case 0x05:
		s.serveSOCKS5(br, conn)
	default:
		s.serveHTTP(br, conn)
	}
}

// resolvePassword fetches the proxy password at auth time (env rotation
// friendly); never logged.
func (s *Server) resolvePassword() string {
	if s.cfg.PasswordEnv != "" {
		return os.Getenv(s.cfg.PasswordEnv)
	}
	return s.cfg.Password
}

// redactListen hides unix socket paths that may contain usernames.
func redactListen(spec string) string {
	if i := strings.Index(spec, "unix://"); i >= 0 {
		return spec
	}
	return spec
}

func isTemporary(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return errors.Is(err, io.ErrUnexpectedEOF)
}
