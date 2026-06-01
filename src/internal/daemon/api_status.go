package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/underundre/unet/internal/config"
)

const gracefulExitFile = ".graceful_exit"

var (
	gracefulMu     sync.Mutex
	gracefulExitCh = make(chan struct{}, 1)
)

// GracefulExitPath returns the path to the graceful exit sentinel file.
func GracefulExitPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, gracefulExitFile), nil
}

// WriteGracefulExit creates the sentinel file on clean shutdown.
func WriteGracefulExit() {
	path, err := GracefulExitPath()
	if err != nil {
		return
	}
	os.WriteFile(path, []byte(time.Now().Format(time.RFC3339)), 0644)
}

// CheckGracefulExit checks and consumes the sentinel file.
// Returns true if daemon exited cleanly (sentinel present).
func CheckGracefulExit() bool {
	path, err := GracefulExitPath()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	// Consume: delete after reading.
	os.Remove(path)
	return len(data) > 0
}

// StatusResponse is the response for GET /api/status.
type StatusResponse struct {
	Tunnel struct {
		Status      string `json:"status"`
		ConnectedAt string `json:"connectedAt,omitempty"`
	} `json:"tunnel"`
	VPS struct {
		Host string `json:"host"`
	} `json:"vps"`
	Ports []PortEntry `json:"ports"`
}

// PortEntry describes an exposed port.
type PortEntry struct {
	Subdomain string `json:"subdomain"`
	FQDN      string `json:"fqdn"`
	LocalPort int    `json:"localPort"`
	Status    string `json:"status"`
}

// StatusHandler serves GET /api/status — aggregated daemon state.
type StatusHandler struct {
	cfgMgr *config.Manager
	server *Server
	// Dependencies injected via setters.
	tunnelStatusFn func() string
}

// NewStatusHandler creates a new StatusHandler.
func NewStatusHandler(cfgMgr *config.Manager, srv *Server) *StatusHandler {
	return &StatusHandler{
		cfgMgr: cfgMgr,
		server: srv,
	}
}

// SetTunnelStatusFn injects the tunnel status provider.
func (h *StatusHandler) SetTunnelStatusFn(fn func() string) {
	h.tunnelStatusFn = fn
}

// RegisterRoutes registers the status endpoint.
func (h *StatusHandler) RegisterRoutes() {
	h.server.HandleFunc("GET /api/status", h.handleStatus)
}

func (h *StatusHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	resp := StatusResponse{}

	// Tunnel status.
	if h.tunnelStatusFn != nil {
		resp.Tunnel.Status = h.tunnelStatusFn()
	}

	// VPS info.
	cfg := h.cfgMgr.Get()
	resp.VPS.Host = cfg.VPS.Host

	// Exposed ports.
	// TODO: read from Caddy/DNS manager when available.
	resp.Ports = []PortEntry{}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// --- Graceful exit sentinel ---

// DeleteGracefulExit removes the sentinel on daemon startup.
func DeleteGracefulExit() {
	path, err := GracefulExitPath()
	if err != nil {
		return
	}
	os.Remove(path)
}
