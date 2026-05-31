package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/underundre/unet/internal/api/apicontext"
	"github.com/underundre/unet/internal/audit"
	"github.com/underundre/unet/internal/auth"
)

type PeerView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	PublicKey     string `json:"publicKey"`
	AllowedIP     string `json:"allowedIp"`
	CreatedVia    string `json:"createdVia"`
	CreatedAt     string `json:"createdAt"`
	Connected     bool   `json:"connected"`
	LastHandshake string `json:"lastHandshake,omitempty"`
	TransferRx    int64  `json:"transferRx"`
	TransferTx    int64  `json:"transferTx"`
}

type PeerDetailView struct {
	PeerView
	ClientConfig string `json:"clientConfig,omitempty"`
}

type RouteView struct {
	ID        string `json:"id"`
	Subdomain string `json:"subdomain"`
	LocalPort int    `json:"localPort"`
	Status    string `json:"status"`
	FQDN      string `json:"fqdn,omitempty"`
	CreatedAt string `json:"createdAt"`
}

type PeerLister interface {
	List(ctx context.Context) ([]PeerView, error)
	GetByID(ctx context.Context, id string) (*PeerDetailView, error)
}

type RouteLister interface {
	List(ctx context.Context) ([]RouteView, error)
}

type TunnelStatusProvider interface {
	Status() string
	IsConnected() bool
	GetConfig() *TunnelConfigView
}

type TunnelConfigView struct {
	LocalIP        string `json:"localIp"`
	ServerIP       string `json:"serverIp"`
	ServerEndpoint string `json:"serverEndpoint"`
	Status         string `json:"status"`
	ConnectedAt    string `json:"connectedAt"`
}

func GetPeers(peers PeerLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := peers.List(r.Context())
		if err != nil {
			ErrorResponse(w, http.StatusInternalServerError, ErrCodeInternalError, "Failed to list peers", nil)
			return
		}
		if list == nil {
			list = []PeerView{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	}
}

func GetPeerByID(peers PeerLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "peer id is required", nil)
			return
		}

		peer, err := peers.GetByID(r.Context(), id)
		if err != nil {
			ErrorResponse(w, http.StatusNotFound, ErrCodeNotFound, "Peer not found", map[string]any{"peerId": id})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(peer)
	}
}

func GetRoutes(routes RouteLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := routes.List(r.Context())
		if err != nil {
			ErrorResponse(w, http.StatusInternalServerError, ErrCodeInternalError, "Failed to list routes", nil)
			return
		}
		if list == nil {
			list = []RouteView{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	}
}

func GetTunnelStatus(tunnel TunnelStatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := tunnel.GetConfig()
		status := tunnel.Status()
		if status == "" {
			status = "disconnected"
		}

		resp := map[string]any{
			"status":         status,
			"localIp":        cfg.LocalIP,
			"serverIp":       cfg.ServerIP,
			"serverEndpoint": cfg.ServerEndpoint,
			"connectedAt":    cfg.ConnectedAt,
		}

		if !tunnel.IsConnected() && cfg.LocalIP != "" {
			resp["stale"] = true
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func GetStatus(tunnel TunnelStatusProvider, tokenStore *auth.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := tunnel.GetConfig()
		tunnelStatus := tunnel.Status()

		tokens, _ := tokenStore.List()

		resp := map[string]any{
			"apiVersion": "2026-05-27",
			"tunnel":     map[string]any{"status": tunnelStatus},
			"tokens":     map[string]any{"count": len(tokens)},
			"tls":        map[string]any{"certExpiryWarning": false},
			"localIp":    cfg.LocalIP,
			"serverIp":   cfg.ServerIP,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func GetTokens(store *auth.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokens, err := store.List()
		if err != nil {
			ErrorResponse(w, http.StatusInternalServerError, ErrCodeInternalError, "Failed to list tokens", nil)
			return
		}

		type tokenInfo struct {
			ID           string     `json:"id"`
			Name         string     `json:"name"`
			TokenPrefix  string     `json:"tokenPrefix"`
			Scope        string     `json:"scope"`
			CreatedBy    string     `json:"createdBy"`
			CreatedAt    time.Time  `json:"createdAt"`
			ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
			LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
			RequestCount int64      `json:"requestCount"`
			Enabled      bool       `json:"enabled"`
		}

		result := make([]tokenInfo, 0, len(tokens))
		for _, t := range tokens {
			result = append(result, tokenInfo{
				ID:           t.ID,
				Name:         t.Name,
				TokenPrefix:  t.TokenPrefix,
				Scope:        string(t.Scope),
				CreatedBy:    t.CreatedBy,
				CreatedAt:    t.CreatedAt,
				ExpiresAt:    t.ExpiresAt,
				LastUsedAt:   t.LastUsedAt,
				RequestCount: t.RequestCount,
				Enabled:      t.Enabled,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func GetAudit(auditPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		params := audit.QueryParams{
			Offset: 0,
			Limit:  50,
		}

		if v := q.Get("offset"); v != "" {
			params.Offset, _ = strconv.Atoi(v)
		}
		if v := q.Get("limit"); v != "" {
			params.Limit, _ = strconv.Atoi(v)
		}
		if v := q.Get("actor"); v != "" {
			params.Actor = v
		}
		if v := q.Get("action"); v != "" {
			params.Action = audit.Action(v)
		}
		if v := q.Get("from"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				params.From = &t
			}
		}
		if v := q.Get("to"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				params.To = &t
			}
		}

		if params.Offset < 0 {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "offset must be >= 0", nil)
			return
		}
		if params.Limit <= 0 || params.Limit > 200 {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "limit must be between 1 and 200", nil)
			return
		}

		result, err := audit.Query(auditPath, params)
		if err != nil {
			ErrorResponse(w, http.StatusInternalServerError, ErrCodeInternalError, "Failed to query audit log", nil)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func updateTokenUsage(store *auth.Store, tokenID string) {
	token, err := store.GetByID(tokenID)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	token.LastUsedAt = &now
	token.RequestCount++
	store.Update(token)
}

func Placeholder(w http.ResponseWriter, r *http.Request) {
	ErrorResponse(w, http.StatusNotImplemented, "not_implemented",
		"Endpoint not yet implemented", nil)
}

func NotFound(w http.ResponseWriter, r *http.Request) {
	ErrorResponse(w, http.StatusNotFound, "not_found", "Endpoint not found", nil)
}

func getAuthInfo(r *http.Request) *apicontext.AuthInfo {
	info, _ := apicontext.AuthInfoFromContext(r.Context())
	return info
}
