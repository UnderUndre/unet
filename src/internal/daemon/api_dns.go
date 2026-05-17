package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/underundre/unet/internal/config"
)

// DNSHandler handles DNS configuration API endpoints.
type DNSHandler struct {
	cfg    *config.Manager
	server *Server
}

// NewDNSHandler creates a new DNS handler.
func NewDNSHandler(cfg *config.Manager, srv *Server) *DNSHandler {
	return &DNSHandler{cfg: cfg, server: srv}
}

// RegisterRoutes registers DNS API routes on the server.
func (h *DNSHandler) RegisterRoutes() {
	h.server.HandleFunc("POST /api/dns/configure", h.handleConfigure)
}

type dnsConfigureRequest struct {
	Mode            string `json:"mode"`
	CloudflareToken string `json:"cloudflareToken"`
	Zone            string `json:"zone"`
}

func (h *DNSHandler) handleConfigure(w http.ResponseWriter, r *http.Request) {
	var req dnsConfigureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	if req.Mode != "cloudflare" && req.Mode != "manual" {
		writeError(w, http.StatusBadRequest, "bad_request", "mode must be 'cloudflare' or 'manual'")
		return
	}

	if req.Mode == "cloudflare" && req.CloudflareToken == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "cloudflare token is required for cloudflare mode")
		return
	}

	err := h.cfg.Update(func(c *config.RootConfig) {
		c.DNS.Provider = req.Mode
		if req.Mode == "cloudflare" {
			c.DNS.Token = config.SecretString(req.CloudflareToken)
		}
		c.DNS.Zone = req.Zone
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to save DNS configuration")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "configured",
		"mode":   req.Mode,
	})
}
