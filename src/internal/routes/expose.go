package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/underundre/unet/internal/api/v1"
	"github.com/underundre/unet/internal/config"
	"github.com/underundre/unet/internal/wizard/dnscheck"
)

var subdomainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

type ExposeRequest struct {
	LocalPort int    `json:"local_port"`
	Subdomain string `json:"subdomain,omitempty"`
}

type ExposeRoute struct {
	ID        string `json:"id"`
	Subdomain string `json:"subdomain"`
	LocalPort int    `json:"local_port"`
	FQDN      string `json:"fqdn"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type ExposeResponse struct {
	Route ExposeRoute `json:"route"`
	URL   string      `json:"url"`
}

type DNSCreator interface {
	UpsertRecord(ctx context.Context, subdomain, ip string) error
	DeleteRecord(ctx context.Context, subdomain string) error
}

type TunnelStatusChecker interface {
	IsConnected() bool
}

type Handler struct {
	cfgMgr    *config.Manager
	dns       DNSCreator
	tunnel    TunnelStatusChecker
	vpsPublicIP string
}

func NewHandler(cfgMgr *config.Manager, dns DNSCreator, tunnel TunnelStatusChecker) *Handler {
	return &Handler{
		cfgMgr: cfgMgr,
		dns:    dns,
		tunnel: tunnel,
	}
}

func (h *Handler) SetVPSPublicIP(ip string) {
	h.vpsPublicIP = ip
}

func RegisterRoutes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /v1/routes/expose", h.HandleExpose)
}

func (h *Handler) HandleExpose(w http.ResponseWriter, r *http.Request) {
	var req ExposeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		v1.ErrorResponse(w, http.StatusBadRequest, v1.ErrCodeBadRequest, "invalid request body", nil)
		return
	}

	if req.LocalPort < 1 || req.LocalPort > 65535 {
		v1.ErrorResponse(w, http.StatusBadRequest, v1.ErrCodeBadRequest,
			fmt.Sprintf("local_port must be 1-65535, got %d", req.LocalPort), nil)
		return
	}

	if !h.tunnel.IsConnected() {
		v1.ErrorResponse(w, http.StatusPreconditionFailed, v1.ErrCodeTunnelNotConnected,
			"tunnel is not connected", nil)
		return
	}

	subdomain := req.Subdomain
	if subdomain == "" {
		subdomain = GenerateSubdomain("")
	}

	if !subdomainRe.MatchString(subdomain) {
		v1.ErrorResponse(w, http.StatusBadRequest, v1.ErrCodeBadRequest,
			fmt.Sprintf("subdomain %q must match ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$", subdomain), nil)
		return
	}

	cfg := h.cfgMgr.Get()
	for _, ep := range cfg.ExposedPorts {
		if ep.HostHeader == subdomain {
			suggestions := generateSuggestions(subdomain)
			v1.ErrorResponse(w, http.StatusConflict, v1.ErrCodeRouteConflict,
				fmt.Sprintf("subdomain %q is already in use", subdomain),
				map[string]any{"suggestions": suggestions})
			return
		}
	}

	fqdn, err := h.buildFQDN(cfg, subdomain)
	if err != nil {
		v1.ErrorResponse(w, http.StatusBadRequest, v1.ErrCodeBadRequest, err.Error(), nil)
		return
	}

	routeID := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)

	newPort := config.ExposedPort{
		ID:         routeID,
		Internal:   req.LocalPort,
		HostHeader: subdomain,
		Protocol:   "http",
		Status:     "active",
		CreatedAt:  now,
	}

	if err := h.cfgMgr.Update(func(c *config.RootConfig) {
		for _, ep := range c.ExposedPorts {
			if ep.HostHeader == subdomain {
				return
			}
		}
		c.ExposedPorts = append(c.ExposedPorts, newPort)
	}); err != nil {
		v1.ErrorResponse(w, http.StatusInternalServerError, v1.ErrCodeInternalError, "failed to save route", nil)
		return
	}

	targetIP := h.resolveTargetIP(cfg)
	if err := h.dns.UpsertRecord(r.Context(), subdomain, targetIP); err != nil {
		slog.Error("routes/expose: DNS upsert failed, rolling back route", "error", err, "subdomain", subdomain)
		_ = h.cfgMgr.Update(func(c *config.RootConfig) {
			for i, ep := range c.ExposedPorts {
				if ep.ID == routeID {
					c.ExposedPorts = append(c.ExposedPorts[:i], c.ExposedPorts[i+1:]...)
					return
				}
			}
		})
		v1.ErrorResponse(w, http.StatusInternalServerError, v1.ErrCodeInternalError,
			fmt.Sprintf("DNS record creation failed: %v", err), nil)
		return
	}

	url := fmt.Sprintf("https://%s", fqdn)
	resp := ExposeResponse{
		Route: ExposeRoute{
			ID:        routeID,
			Subdomain: subdomain,
			LocalPort: req.LocalPort,
			FQDN:      fqdn,
			Status:    "active",
			CreatedAt: now,
		},
		URL: url,
	}

	slog.Info("routes/expose: route created", "id", routeID, "subdomain", subdomain, "fqdn", fqdn)
	v1JSONResponse(w, http.StatusCreated, resp)
}

func (h *Handler) buildFQDN(cfg *config.RootConfig, subdomain string) (string, error) {
	if h.vpsPublicIP != "" {
		return dnscheck.BuildNipioSubdomain(subdomain, h.vpsPublicIP), nil
	}
	if cfg.DNS.Zone != "" {
		return subdomain + "." + cfg.DNS.Zone, nil
	}
	return "", fmt.Errorf("no domain zone or VPS public IP configured")
}

func (h *Handler) resolveTargetIP(cfg *config.RootConfig) string {
	if h.vpsPublicIP != "" {
		return h.vpsPublicIP
	}
	return cfg.Tunnel.ServerIP
}

func GenerateSubdomain(hint string) string {
	if hint != "" {
		return hint
	}
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 4)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return "svc-" + string(b)
}

func BuildNipioFQDN(subdomain, vpsPublicIP string) string {
	return dnscheck.BuildNipioSubdomain(subdomain, vpsPublicIP)
}

func generateSuggestions(subdomain string) []string {
	suggestions := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		suggestions = append(suggestions, GenerateSubdomain(""))
	}
	return suggestions
}

func v1JSONResponse(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
