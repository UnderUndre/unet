package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/google/uuid"

	"github.com/underundre/unet/internal/config"
	"github.com/underundre/unet/internal/provisioner"
)

// VPSSettings is the request body for POST /api/vps/configure.
type VPSSettings struct {
	Host           string `json:"host"`
	SSHPort        int    `json:"sshPort"`
	Username       string `json:"username"`
	AuthMode       string `json:"authMode"`       // "key" | "password"
	PrivateKeyPath string `json:"privateKeyPath"`
	Password       string `json:"password"`
}

// provisionTask tracks an in-flight provisioning operation.
type provisionTask struct {
	ID     string
	Status string // "provisioning" | "completed" | "failed"
	Error  string
}

// VPSHandler holds per-server state for the VPS API handlers.
type VPSHandler struct {
	mgr      *config.Manager
	task     *provisionTask // tracks current provisioning task
	server   *Server        // reference back to the daemon server
}

// NewVPSHandler creates a new VPSHandler with the given config manager.
func NewVPSHandler(mgr *config.Manager, srv *Server) *VPSHandler {
	return &VPSHandler{mgr: mgr, server: srv}
}

// RegisterRoutes registers all VPS and status API routes on the server.
func (h *VPSHandler) RegisterRoutes() {
	h.server.HandleFunc("POST /api/vps/configure", h.handleVPSConfigure)
	h.server.HandleFunc("GET /api/vps/status", h.handleVPSStatus)
	h.server.HandleFunc("GET /api/status", h.handleSystemStatus)
}

// ---------- POST /api/vps/configure ----------

func (h *VPSHandler) handleVPSConfigure(w http.ResponseWriter, r *http.Request) {
	var req VPSSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is not valid JSON")
		return
	}

	// Validate required fields.
	if req.Host == "" {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "host is required")
		return
	}
	if req.SSHPort <= 0 {
		req.SSHPort = 22
	}
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "username is required")
		return
	}
	if req.AuthMode != "key" && req.AuthMode != "password" {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "authMode must be 'key' or 'password'")
		return
	}
	if req.AuthMode == "key" {
		if req.PrivateKeyPath == "" {
			writeError(w, http.StatusBadRequest, "invalid_credentials", "privateKeyPath is required when authMode is 'key'")
			return
		}
		if _, err := os.Stat(req.PrivateKeyPath); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_credentials",
				fmt.Sprintf("privateKeyPath does not exist: %s", req.PrivateKeyPath))
			return
		}
	}
	if req.AuthMode == "password" && req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "password is required when authMode is 'password'")
		return
	}

	// Save VPS credentials to config.
	if err := h.mgr.Update(func(c *config.RootConfig) {
		c.VPS.Host = req.Host
		c.VPS.SSHPort = req.SSHPort
		c.VPS.Username = req.Username
		c.VPS.AuthMode = req.AuthMode
		c.VPS.PrivateKeyPath = req.PrivateKeyPath
		c.VPS.Password = config.SecretString(req.Password)
	}); err != nil {
		slog.Error("api: failed to save VPS config", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to save configuration")
		return
	}

	// Generate task ID and kick off provisioning in background.
	taskID := "provision-" + uuid.New().String()[:8]
	h.task = &provisionTask{
		ID:     taskID,
		Status: "provisioning",
	}

	go h.runProvision(r.Context(), taskID)

	slog.Info("api: VPS configure accepted", "taskId", taskID, "host", req.Host)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"taskId": taskID,
		"status": "provisioning",
	})
}

// runProvision executes the provisioning pipeline in the background.
func (h *VPSHandler) runProvision(ctx context.Context, taskID string) {
	slog.Info("api: provisioning started", "taskId", taskID)

	result, err := provisioner.Provision(ctx, h.mgr)
	if err != nil {
		slog.Error("api: provisioning failed", "taskId", taskID, "error", err)
		h.task.Status = "failed"
		h.task.Error = err.Error()
		return
	}

	slog.Info("api: provisioning completed", "taskId", taskID, "container", result.ContainerID)
	h.task.Status = "completed"
}

// ---------- GET /api/vps/status ----------

func (h *VPSHandler) handleVPSStatus(w http.ResponseWriter, r *http.Request) {
	cfg := h.mgr.Get()

	resp := map[string]any{
		"configured":      cfg.VPS.Host != "",
		"provisioned":     cfg.VPS.IsProvisioned,
		"host":            cfg.VPS.Host,
		"lastProvisionAt": nil,
	}

	// If there's an active task, include its status.
	if h.task != nil {
		resp["taskStatus"] = h.task.Status
		if h.task.Error != "" {
			resp["taskError"] = h.task.Error
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ---------- GET /api/status ----------

// systemStatus is the response shape for GET /api/status.
type systemStatus struct {
	Privileged bool          `json:"privileged"`
	VPS        vpsStatusInfo `json:"vps"`
	Tunnel     tunnelInfo    `json:"tunnel"`
	Ports      []portInfo    `json:"ports"`
	DaemonPort int           `json:"daemonPort"`
}

type vpsStatusInfo struct {
	Configured  bool   `json:"configured"`
	Provisioned bool   `json:"provisioned"`
	Host        string `json:"host"`
}

type tunnelInfo struct {
	Status   string `json:"status"`
	LocalIP  string `json:"localIp,omitempty"`
	ServerIP string `json:"serverIp,omitempty"`
}

type portInfo struct {
	ID        string `json:"id"`
	LocalPort int    `json:"localPort"`
	Subdomain string `json:"subdomain"`
	Protocol  string `json:"protocol"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt,omitempty"`
}

func (h *VPSHandler) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	cfg := h.mgr.Get()

	status := systemStatus{
		Privileged: isPrivileged(),
		VPS: vpsStatusInfo{
			Configured:  cfg.VPS.Host != "",
			Provisioned: cfg.VPS.IsProvisioned,
			Host:        cfg.VPS.Host,
		},
		Tunnel: tunnelInfo{
			Status:   cfg.Tunnel.Status,
			LocalIP:  cfg.Tunnel.LocalIP,
			ServerIP: cfg.Tunnel.ServerIP,
		},
		Ports:      buildPortInfos(cfg),
		DaemonPort: h.server.Port(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// ---------- helpers ----------

// buildPortInfos converts ExposedPort entries into API response port infos.
func buildPortInfos(cfg *config.RootConfig) []portInfo {
	if len(cfg.ExposedPorts) == 0 {
		return []portInfo{}
	}
	out := make([]portInfo, 0, len(cfg.ExposedPorts))
	for _, ep := range cfg.ExposedPorts {
		id := ep.ID
		if id == "" {
			// Legacy entries without ID — skip or assign fallback.
			continue
		}
		out = append(out, portInfo{
			ID:        id,
			LocalPort: ep.Internal,
			Subdomain: ep.HostHeader,
			Protocol:  ep.Protocol,
			Status:    ep.Status,
			CreatedAt: ep.CreatedAt,
		})
	}
	return out
}

// isPrivileged returns true if the current process is running with
// elevated privileges (root on POSIX, admin on Windows).
func isPrivileged() bool {
	// Simple check: can we create a raw socket or equivalent.
	// On POSIX, uid 0 means privileged.
	uid := os.Getuid()
	return uid == 0
}

// writeError writes a standardised JSON error response.
func writeError(w http.ResponseWriter, code int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   errType,
		"message": message,
	})
}

// writeErrorWithCtx writes a JSON error response with additional structured context.
func writeErrorWithCtx(w http.ResponseWriter, code int, errType, message string, ctx map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	type errResp struct {
		Error   string                 `json:"error"`
		Message string                 `json:"message"`
		Context map[string]interface{} `json:"context,omitempty"`
	}
	json.NewEncoder(w).Encode(errResp{Error: errType, Message: message, Context: ctx})
}
