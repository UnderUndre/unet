package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/underundre/unet/internal/config"
	"github.com/underundre/unet/internal/tunnel"
)

// TunnelHandler holds state for the tunnel management API endpoints.
type TunnelHandler struct {
	tunnelMgr *tunnel.Manager
	cfgMgr    *config.Manager
	server    *Server
}

// NewTunnelHandler creates a new TunnelHandler with the given tunnel manager,
// config manager, and server reference.
func NewTunnelHandler(tunnelMgr *tunnel.Manager, cfgMgr *config.Manager, srv *Server) *TunnelHandler {
	return &TunnelHandler{
		tunnelMgr: tunnelMgr,
		cfgMgr:    cfgMgr,
		server:    srv,
	}
}

// RegisterRoutes registers all tunnel API routes on the server.
func (h *TunnelHandler) RegisterRoutes() {
	h.server.HandleFunc("POST /api/tunnel/connect", h.handleConnect)
	h.server.HandleFunc("POST /api/tunnel/disconnect", h.handleDisconnect)
	h.server.HandleFunc("GET /api/tunnel/status", h.handleStatus)
}

// ---------- POST /api/tunnel/connect ----------

func (h *TunnelHandler) handleConnect(w http.ResponseWriter, r *http.Request) {
	// Check privileges first.
	if !isPrivileged() {
		writeError(w, http.StatusServiceUnavailable, "not_privileged",
			"Administrator/root privileges required to manage network interfaces")
		return
	}

	// Check if already connected or connecting.
	status := h.tunnelMgr.Status()
	if status == "connected" || status == "connecting" {
		writeError(w, http.StatusConflict, "already_connected",
			"Tunnel is already in connected state")
		return
	}

	// Kick off connect in background; return 202 immediately.
	go func() {
		if err := h.tunnelMgr.Connect(context.Background()); err != nil {
			slog.Error("api: tunnel connect failed", "error", err)
		}
	}()

	slog.Info("api: tunnel connect initiated")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "connecting",
	})
}

// ---------- POST /api/tunnel/disconnect ----------

func (h *TunnelHandler) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.tunnelMgr.Disconnect(); err != nil {
		slog.Error("api: tunnel disconnect failed", "error", err)
		writeError(w, http.StatusInternalServerError, "disconnect_error",
			"Failed to disconnect tunnel")
		return
	}

	slog.Info("api: tunnel disconnected")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "disconnected",
	})
}

// ---------- GET /api/tunnel/status ----------

// tunnelStatusResponse is the response shape for GET /api/tunnel/status.
type tunnelStatusResponse struct {
	Status         string `json:"status"`
	LocalIP        string `json:"localIp,omitempty"`
	ServerIP       string `json:"serverIp,omitempty"`
	ServerEndpoint string `json:"serverEndpoint,omitempty"`
	ConnectedAt    string `json:"connectedAt,omitempty"`
}

func (h *TunnelHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfgMgr.Get()
	status := h.tunnelMgr.Status()

	resp := tunnelStatusResponse{
		Status:         status,
		LocalIP:        cfg.Tunnel.LocalIP,
		ServerIP:       cfg.Tunnel.ServerIP,
		ServerEndpoint: cfg.Tunnel.ServerEndpoint,
		ConnectedAt:    cfg.Tunnel.ConnectedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
