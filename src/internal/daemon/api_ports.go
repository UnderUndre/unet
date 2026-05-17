package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/underundre/unet/internal/config"
	"github.com/underundre/unet/internal/proxy"
	"github.com/underundre/unet/internal/tunnel"
)

// subdomainLabelRe validates a single DNS label: lowercase letters, digits, hyphens;
// must start and end with a letter or digit. Length 1-63.
var subdomainLabelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// subdomainFullRe validates a full subdomain (multi-label OK):
// each label matches subdomainLabelRe, separated by dots, total length 1-253.
var subdomainFullRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-.]{0,251}[a-z0-9])?$`)

// ---------- Request / Response types ----------

// createPortRequest is the request body for POST /api/ports.
type createPortRequest struct {
	LocalPort int    `json:"localPort"`
	Subdomain string `json:"subdomain"`
	Protocol  string `json:"protocol"`
}

// portResponse is the response shape for a single port.
type portResponse struct {
	ID        string `json:"id"`
	LocalPort int    `json:"localPort"`
	Subdomain string `json:"subdomain"`
	Protocol  string `json:"protocol"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// ---------- PortsHandler ----------

// PortsHandler holds state for the port management API endpoints.
type PortsHandler struct {
	cfgMgr  *config.Manager
	caddy   *proxy.CaddyClient
	dns     *proxy.DNSManager
	server  *Server
	proxies map[string]*tunnel.LocalTCPProxy // keyed by port ID
	proxyMu sync.Mutex
}

// NewPortsHandler creates a new PortsHandler with the given dependencies.
func NewPortsHandler(cfgMgr *config.Manager, caddy *proxy.CaddyClient, dns *proxy.DNSManager, srv *Server) *PortsHandler {
	return &PortsHandler{
		cfgMgr:  cfgMgr,
		caddy:   caddy,
		dns:     dns,
		server:  srv,
		proxies: make(map[string]*tunnel.LocalTCPProxy),
	}
}

// RegisterRoutes registers all port management API routes on the server.
func (h *PortsHandler) RegisterRoutes() {
	h.server.HandleFunc("POST /api/ports", h.handleCreate)
	h.server.HandleFunc("DELETE /api/ports/{id}", h.handleRemove)
	h.server.HandleFunc("GET /api/ports", h.handleList)
	h.server.HandleFunc("GET /api/ports/{id}", h.handleGet)
}

// ---------- POST /api/ports ----------

func (h *PortsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createPortRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is not valid JSON")
		return
	}

	// Validate localPort: 1-65535.
	if req.LocalPort < 1 || req.LocalPort > 65535 {
		writeError(w, http.StatusBadRequest, "invalid_port",
			fmt.Sprintf("localPort must be between 1 and 65535, got %d", req.LocalPort))
		return
	}

	// Validate subdomain format (multi-label allowed for manual DNS mode).
	if req.Subdomain == "" {
		writeError(w, http.StatusBadRequest, "invalid_subdomain", "subdomain is required")
		return
	}
	if !subdomainFullRe.MatchString(req.Subdomain) {
		writeError(w, http.StatusBadRequest, "invalid_subdomain",
			"subdomain must match: *.domain.com and contain only a-z, 0-9, hyphens")
		return
	}
	// Verify each label individually (dot-separated).
	for _, label := range strings.Split(req.Subdomain, ".") {
		if !subdomainLabelRe.MatchString(label) {
			writeError(w, http.StatusBadRequest, "invalid_subdomain",
				fmt.Sprintf("subdomain label %q is invalid: must be 1-63 chars, lowercase letters, digits, hyphens", label))
			return
		}
	}

	// Default protocol to "http" if empty.
	if req.Protocol == "" {
		req.Protocol = "http"
	}
	req.Protocol = strings.ToLower(req.Protocol)
	if req.Protocol != "http" && req.Protocol != "https" && req.Protocol != "tcp" {
		writeError(w, http.StatusBadRequest, "invalid_protocol",
			fmt.Sprintf("protocol must be one of http, https, tcp; got %q", req.Protocol))
		return
	}

	// Check tunnel is connected.
	cfg := h.cfgMgr.Get()
	if cfg.Tunnel.Status != "connected" {
		writeError(w, http.StatusConflict, "tunnel_not_connected",
			"Tunnel must be connected before exposing ports")
		return
	}

	// Phase 2: Cloudflare mode restricts to single-label subdomains.
	if cfg.DNS.Provider == "cloudflare" && strings.Contains(req.Subdomain, ".") {
		labelsUnderBase := strings.Count(req.Subdomain, ".") + 1
		fullSubdomain := req.Subdomain + "." + cfg.DNS.Zone
		writeErrorWithCtx(w, http.StatusBadRequest, "invalid_subdomain_depth",
			fmt.Sprintf("Cloudflare mode uses a wildcard certificate (*.%s) which covers only single-label subdomains. '%s' has %d labels under baseDomain — switch to manual DNS mode for multi-level subdomains, or use hyphens instead.",
				cfg.DNS.Zone, fullSubdomain, labelsUnderBase),
			map[string]interface{}{
				"subdomain":                fullSubdomain,
				"baseDomain":               cfg.DNS.Zone,
				"labelsUnderBase":          labelsUnderBase,
				"maxAllowedInCloudflareMode": 1,
				"remediation":              "rename | switch_dns_mode",
			})
		return
	}

	// Generate ID and create the port entry.
	portID := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)
	status := "active"

	// Start local TCP proxy to avoid exposing the user's service on 0.0.0.0.
	// The proxy listens on tunnelIP:randomPort and forwards to 127.0.0.1:localPort.
	localIP := cfg.Tunnel.LocalIP
	targetAddr := fmt.Sprintf("127.0.0.1:%d", req.LocalPort)
	px := tunnel.NewLocalTCPProxy(localIP, targetAddr)
	proxyPort, err := px.Start(r.Context())
	if err != nil {
		slog.Error("api: failed to start local proxy", "error", err)
		writeError(w, http.StatusInternalServerError, "proxy_error", "failed to start local TCP proxy")
		return
	}
	h.proxies[portID] = px

	// Use proxy port as upstream (Caddy dials through tunnel to our proxy).
	upstreamDial := fmt.Sprintf("%s:%d", localIP, proxyPort)

	// Build the full host for Caddy: subdomain.zone.
	host := req.Subdomain
	if cfg.DNS.Zone != "" && !strings.HasSuffix(req.Subdomain, cfg.DNS.Zone) {
		host = req.Subdomain + "." + cfg.DNS.Zone
	}

	// External calls (Caddy + DNS) BEFORE acquiring write lock.
	if err := h.caddy.AddRoute(r.Context(), host, upstreamDial); err != nil {
		slog.Error("api: failed to add caddy route", "error", err)
		status = "error"
	}
	var dnsRecordCreated bool
	if status == "active" {
		if err := h.dns.CreateRecord(r.Context(), req.Subdomain); err != nil {
			slog.Error("api: failed to create DNS record", "error", err)
			status = "error"
		} else {
			dnsRecordCreated = true
		}
	}

	// Build the new port entry.
	newPort := config.ExposedPort{
		ID:         portID,
		Protocol:   req.Protocol,
		Internal:   req.LocalPort,
		HostHeader: req.Subdomain,
		Status:     status,
		CreatedAt:  now,
	}

	// Atomic config update with dup check inside the write lock.
	dup := false
	if err := h.cfgMgr.Update(func(c *config.RootConfig) {
		// Dup check INSIDE the write lock.
		for _, ep := range c.ExposedPorts {
			if ep.HostHeader == req.Subdomain {
				dup = true
				return // don't append
			}
		}
		c.ExposedPorts = append(c.ExposedPorts, newPort)
	}); err != nil {
		// Config save failed — rollback external resources.
		h.caddy.RemoveRoute(r.Context(), host)
		if dnsRecordCreated {
			h.dns.DeleteRecord(r.Context(), req.Subdomain)
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to save port configuration")
		return
	}

	if dup {
		// Another request won the race — rollback our external resources.
		h.caddy.RemoveRoute(r.Context(), host)
		if dnsRecordCreated {
			h.dns.DeleteRecord(r.Context(), req.Subdomain)
		}
		writeError(w, http.StatusConflict, "duplicate_subdomain",
			fmt.Sprintf("subdomain %q is already in use", req.Subdomain))
		return
	}

	// Q3: Return proper HTTP status on partial failure.
	if status == "error" {
		slog.Warn("api: port created with errors", "id", portID, "subdomain", req.Subdomain)
		resp := portResponse{
			ID:        portID,
			LocalPort: req.LocalPort,
			Subdomain: req.Subdomain,
			Protocol:  req.Protocol,
			Status:    status,
			CreatedAt: now,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway) // 502
		json.NewEncoder(w).Encode(resp)
		return
	}

	slog.Info("api: port exposed", "id", portID, "localPort", req.LocalPort, "subdomain", req.Subdomain, "status", status)
	resp := portResponse{
		ID:        portID,
		LocalPort: req.LocalPort,
		Subdomain: req.Subdomain,
		Protocol:  req.Protocol,
		Status:    status,
		CreatedAt: now,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// ---------- DELETE /api/ports/{id} ----------

func (h *PortsHandler) handleRemove(w http.ResponseWriter, r *http.Request) {
	portID := r.PathValue("id")
	if portID == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "port id is required")
		return
	}

	// Find the port by ID in config.
	cfg := h.cfgMgr.Get()
	var target *config.ExposedPort
	var targetIdx int = -1
	for i, ep := range cfg.ExposedPorts {
		if ep.ID == portID {
			target = &cfg.ExposedPorts[i]
			targetIdx = i
			break
		}
	}
	// Also support legacy port-N format for backwards compatibility.
	if target == nil && strings.HasPrefix(portID, "port-") {
		numStr := strings.TrimPrefix(portID, "port-")
		if n, err := strconv.Atoi(numStr); err == nil && n >= 0 && n < len(cfg.ExposedPorts) {
			target = &cfg.ExposedPorts[n]
			targetIdx = n
		}
	}

	if target == nil {
		writeError(w, http.StatusNotFound, "not_found",
			fmt.Sprintf("No exposed port with id: %s", portID))
		return
	}

	// Build the full host for Caddy cleanup.
	host := target.HostHeader
	if cfg.DNS.Zone != "" && !strings.HasSuffix(target.HostHeader, cfg.DNS.Zone) {
		host = target.HostHeader + "." + cfg.DNS.Zone
	}

	// Remove Caddy route.
	if err := h.caddy.RemoveRoute(r.Context(), host); err != nil {
		slog.Error("api: failed to remove caddy route", "error", err)
	}

	// Delete DNS record.
	if err := h.dns.DeleteRecord(r.Context(), target.HostHeader); err != nil {
		slog.Error("api: failed to delete DNS record", "error", err)
	}

	// Stop the local TCP proxy.
	h.proxyMu.Lock()
	if px, ok := h.proxies[portID]; ok {
		px.Stop()
		delete(h.proxies, portID)
	}
	h.proxyMu.Unlock()

	// Remove from config.
	if err := h.cfgMgr.Update(func(c *config.RootConfig) {
		// Re-find the index inside the lock (may have shifted).
		idx := -1
		for i, ep := range c.ExposedPorts {
			if ep.ID == portID {
				idx = i
				break
			}
		}
		if idx == -1 {
			// Fallback to position-based.
			if targetIdx >= 0 && targetIdx < len(c.ExposedPorts) {
				idx = targetIdx
			}
		}
		if idx >= 0 && idx < len(c.ExposedPorts) {
			c.ExposedPorts = append(c.ExposedPorts[:idx], c.ExposedPorts[idx+1:]...)
		}
	}); err != nil {
		slog.Error("api: failed to persist port removal", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update configuration")
		return
	}

	slog.Info("api: port removed", "id", portID, "subdomain", target.HostHeader)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"id":     portID,
		"status": "removed",
	})
}

// ---------- GET /api/ports ----------

func (h *PortsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfgMgr.Get()
	ports := buildPortInfos(cfg)

	// Ensure we always return a JSON array, never null.
	if ports == nil {
		ports = []portInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ports)
}

// ---------- GET /api/ports/{id} ----------

func (h *PortsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	portID := r.PathValue("id")
	if portID == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "port id is required")
		return
	}

	cfg := h.cfgMgr.Get()
	for _, ep := range cfg.ExposedPorts {
		if ep.ID == portID {
			resp := portResponse{
				ID:        ep.ID,
				LocalPort: ep.Internal,
				Subdomain: ep.HostHeader,
				Protocol:  ep.Protocol,
				Status:    ep.Status,
				CreatedAt: ep.CreatedAt,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
	}

	// Legacy port-N support.
	if strings.HasPrefix(portID, "port-") {
		numStr := strings.TrimPrefix(portID, "port-")
		if n, err := strconv.Atoi(numStr); err == nil && n >= 0 && n < len(cfg.ExposedPorts) {
			ep := cfg.ExposedPorts[n]
			resp := portResponse{
				ID:        ep.ID,
				LocalPort: ep.Internal,
				Subdomain: ep.HostHeader,
				Protocol:  ep.Protocol,
				Status:    ep.Status,
				CreatedAt: ep.CreatedAt,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
	}

	writeError(w, http.StatusNotFound, "not_found",
		fmt.Sprintf("No exposed port with id: %s", portID))
}
