package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/underundre/unet/internal/auth"
)

var peerNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type PeerMutator interface {
	PeerLister
	Create(ctx context.Context, name string) (*PeerDetailView, error)
	Delete(ctx context.Context, id string) error
}

type RouteMutator interface {
	RouteLister
	Create(ctx context.Context, subdomain string, port int) (*RouteView, error)
	Delete(ctx context.Context, id string) error
}

func CreatePeer(peers PeerMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "Invalid request body", nil)
			return
		}

		if !peerNameRe.MatchString(req.Name) {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest,
				"Name must be 1-64 chars, [a-zA-Z0-9_-]", nil)
			return
		}

		peer, err := peers.Create(r.Context(), req.Name)
		if err != nil {
			handlePeerError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(peer)
	}
}

func DeletePeer(peers PeerMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "peer id is required", nil)
			return
		}

		if err := peers.Delete(r.Context(), id); err != nil {
			ErrorResponse(w, http.StatusNotFound, ErrCodeNotFound, "Peer not found", map[string]any{"peerId": id})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "removed"})
	}
}

func CreateRoute(routes RouteMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Subdomain string `json:"subdomain"`
			LocalPort int    `json:"localPort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "Invalid request body", nil)
			return
		}

		if req.Subdomain == "" {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "subdomain is required", nil)
			return
		}
		if req.LocalPort < 1 || req.LocalPort > 65535 {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest,
				fmt.Sprintf("localPort must be 1-65535, got %d", req.LocalPort), nil)
			return
		}

		route, err := routes.Create(r.Context(), req.Subdomain, req.LocalPort)
		if err != nil {
			handleRouteError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(route)
	}
}

func DeleteRoute(routes RouteMutator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "route id is required", nil)
			return
		}

		if err := routes.Delete(r.Context(), id); err != nil {
			ErrorResponse(w, http.StatusNotFound, ErrCodeNotFound, "Route not found", map[string]any{"routeId": id})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "removed"})
	}
}

func CreateToken(store *auth.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name      string     `json:"name"`
			Scope     string     `json:"scope"`
			ExpiresAt *time.Time `json:"expiresAt,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "Invalid request body", nil)
			return
		}

		if req.Name == "" {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "name is required", nil)
			return
		}
		scope := auth.Scope(req.Scope)
		if scope != auth.ScopeRead && scope != auth.ScopeWrite && scope != auth.ScopeAdmin {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest,
				"scope must be one of: read, write, admin", nil)
			return
		}

		info := getAuthInfo(r)
		createdBy := "system"
		if info != nil && info.TokenID != "localhost" {
			createdBy = info.TokenID
		}

		token, plain, err := auth.NewAPIToken(req.Name, scope, createdBy, req.ExpiresAt)
		if err != nil {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, err.Error(), nil)
			return
		}

		if err := store.Create(token); err != nil {
			if err == auth.ErrDuplicateName {
				ErrorResponse(w, http.StatusConflict, "peer_name_conflict",
					"Token name already exists", nil)
				return
			}
			ErrorResponse(w, http.StatusInternalServerError, ErrCodeInternalError,
				"Failed to create token", nil)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":          token.ID,
			"name":        token.Name,
			"tokenPrefix": token.TokenPrefix,
			"scope":       string(token.Scope),
			"plainToken":  plain,
			"createdAt":   token.CreatedAt,
		})
	}
}

func RevokeToken(store *auth.Store, cache *auth.TokenCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			ErrorResponse(w, http.StatusBadRequest, ErrCodeBadRequest, "token id is required", nil)
			return
		}

		token, err := store.GetByID(id)
		if err != nil {
			ErrorResponse(w, http.StatusNotFound, ErrCodeNotFound, "Token not found", nil)
			return
		}

		if err := store.SoftDelete(id); err != nil {
			ErrorResponse(w, http.StatusInternalServerError, ErrCodeInternalError,
				"Failed to revoke token", nil)
			return
		}

		cache.Invalidate(token.TokenPrefix)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "revoked"})
	}
}

func CreateSession(issuer *auth.JWTIssuer, cache *auth.TokenCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := getAuthInfo(r)
		if info == nil || info.Source == "jwt" {
			ErrorResponse(w, http.StatusUnauthorized, ErrCodeUnauthorized,
				"Cannot exchange JWT for JWT", nil)
			return
		}

		token, err := issuer.Issue(info.TokenID, info.TokenName, auth.Scope(info.Scope))
		if err != nil {
			ErrorResponse(w, http.StatusInternalServerError, ErrCodeInternalError,
				"Failed to issue JWT", nil)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"token":     token,
			"expiresIn": int(issuer.TTL().Seconds()),
			"scope":     info.Scope,
		})
	}
}

func handlePeerError(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(strings.ToLower(msg), "conflict"),
		strings.Contains(strings.ToLower(msg), "duplicate"):
		ErrorResponse(w, http.StatusConflict, ErrCodePeerNameConflict, msg, nil)
	case strings.Contains(strings.ToLower(msg), "exhausted"),
		strings.Contains(strings.ToLower(msg), "ip"):
		ErrorResponse(w, http.StatusInsufficientStorage, ErrCodeIPPoolExhausted, msg, nil)
	case strings.Contains(strings.ToLower(msg), "unreachable"),
		strings.Contains(strings.ToLower(msg), "vps"):
		ErrorResponse(w, http.StatusServiceUnavailable, ErrCodeVPSUnreachable, msg, nil)
	default:
		ErrorResponse(w, http.StatusInternalServerError, ErrCodeInternalError, msg, nil)
	}
}

func handleRouteError(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(strings.ToLower(msg), "conflict"),
		strings.Contains(strings.ToLower(msg), "duplicate"):
		ErrorResponse(w, http.StatusConflict, ErrCodeRouteConflict, msg, nil)
	case strings.Contains(strings.ToLower(msg), "tunnel"),
		strings.Contains(strings.ToLower(msg), "connected"):
		ErrorResponse(w, http.StatusPreconditionFailed, ErrCodeTunnelNotConnected, msg, nil)
	default:
		ErrorResponse(w, http.StatusInternalServerError, ErrCodeInternalError, msg, nil)
	}
}
