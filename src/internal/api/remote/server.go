package remote

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/underundre/unet/internal/config"
)

// Server represents the remote control plane API server.
type Server struct {
	httpServer *http.Server
	configMgr  *config.Manager
}

// NewServer creates a new remote API server.
func NewServer(cfgMgr *config.Manager, handler http.Handler) *Server {
	return &Server{
		httpServer: &http.Server{
			Handler: handler,
		},
		configMgr: cfgMgr,
	}
}

// Start begins listening on the configured address.
func (s *Server) Start() error {
	cfg := s.configMgr.Get().RemoteAPI

	if !cfg.Enabled {
		return nil
	}

	listenAddr := cfg.ListenAddr
	if listenAddr == "" {
		listenAddr = "0.0.0.0:8443"
	}
	s.httpServer.Addr = listenAddr

	isLoopback := isLoopbackAddress(listenAddr)

	certPath := cfg.TLSCert
	keyPath := cfg.TLSKey

	if certPath == "" || keyPath == "" {
		defCert, defKey, err := DefaultTLSCertPaths()
		if err != nil {
			return fmt.Errorf("failed to get default TLS paths: %w", err)
		}
		if certPath == "" {
			certPath = defCert
		}
		if keyPath == "" {
			keyPath = defKey
		}
	}

	if err := EnsureTLS(certPath, keyPath); err != nil {
		return fmt.Errorf("failed to ensure TLS certificates: %w", err)
	}

	slog.Info("remote API listening", "addr", listenAddr, "tls", true)
	
	// Check if binding to non-loopback. We always require TLS now for remote API, 
	// even on loopback, for simplicity and consistency with the spec (FR-012).
	// FR-012: The listener MUST NOT start in plaintext mode on a network-accessible interface.
	if !isLoopback {
		slog.Info("remote API bound to non-loopback address; TLS is required")
	}

	return s.httpServer.ListenAndServeTLS(certPath, keyPath)
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

func isLoopbackAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	// Simple check, real loopback IPs
	return host == "127.0.0.1" || host == "::1" || host == "localhost" || strings.HasPrefix(host, "127.")
}
