package remote

import (
	"net/http"

	"github.com/underundre/unet/internal/api/middleware"
	"github.com/underundre/unet/internal/api/v1"
	"github.com/underundre/unet/internal/audit"
	"github.com/underundre/unet/internal/auth"
)

type Dependencies struct {
	TokenStore *auth.Store
	TokenCache *auth.TokenCache
	JWTIssuer  *auth.JWTIssuer
	AuditLog   *audit.Logger
	AuditPath  string
	Peers      v1.PeerMutator
	Routes     v1.RouteMutator
	Tunnel     v1.TunnelStatusProvider
}

func RegisterRoutes(deps *Dependencies) http.Handler {
	mux := http.NewServeMux()
	rl := middleware.NewRateLimiter(nil)

	wrap := func(scope auth.Scope, handler http.HandlerFunc) http.Handler {
		h := http.Handler(handler)
		h = middleware.AuditLog(deps.AuditLog, h)
		h = rl.Middleware(h)
		h = middleware.RequireScope(scope, h)
		return h
	}

	mux.Handle("GET /v1/status", wrap(auth.ScopeRead, v1.GetStatus(deps.Tunnel, deps.TokenStore)))
	mux.Handle("GET /v1/peers", wrap(auth.ScopeRead, v1.GetPeers(deps.Peers)))
	mux.Handle("GET /v1/peers/{id}", wrap(auth.ScopeRead, v1.GetPeerByID(deps.Peers)))
	mux.Handle("POST /v1/peers", wrap(auth.ScopeWrite, v1.CreatePeer(deps.Peers)))
	mux.Handle("DELETE /v1/peers/{id}", wrap(auth.ScopeWrite, v1.DeletePeer(deps.Peers)))
	mux.Handle("GET /v1/routes", wrap(auth.ScopeRead, v1.GetRoutes(deps.Routes)))
	mux.Handle("POST /v1/routes", wrap(auth.ScopeWrite, v1.CreateRoute(deps.Routes)))
	mux.Handle("DELETE /v1/routes/{id}", wrap(auth.ScopeWrite, v1.DeleteRoute(deps.Routes)))
	mux.Handle("GET /v1/tunnel/status", wrap(auth.ScopeRead, v1.GetTunnelStatus(deps.Tunnel)))
	mux.Handle("GET /v1/tokens", wrap(auth.ScopeAdmin, v1.GetTokens(deps.TokenStore)))
	mux.Handle("POST /v1/tokens", wrap(auth.ScopeAdmin, v1.CreateToken(deps.TokenStore)))
	mux.Handle("DELETE /v1/tokens/{id}", wrap(auth.ScopeAdmin, v1.RevokeToken(deps.TokenStore, deps.TokenCache)))
	mux.Handle("GET /v1/audit", wrap(auth.ScopeAdmin, v1.GetAudit(deps.AuditPath)))
	mux.Handle("POST /v1/auth/session", wrap(auth.ScopeRead, v1.CreateSession(deps.JWTIssuer, deps.TokenCache)))

	mux.Handle("/", http.HandlerFunc(v1.NotFound))

	return middleware.AuthDispatcher(&middleware.AuthDispatcherDeps{
		TokenCache: deps.TokenCache,
		JWTIssuer:  deps.JWTIssuer,
	}, mux)
}
