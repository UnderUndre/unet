package middleware

import (
	"net"
	"net/http"
	"strings"

	"github.com/underundre/unet/internal/api/apicontext"
	"github.com/underundre/unet/internal/api/v1"
	"github.com/underundre/unet/internal/auth"
)

type AuthDispatcherDeps struct {
	TokenCache *auth.TokenCache
	JWTIssuer  *auth.JWTIssuer
}

func AuthDispatcher(deps *AuthDispatcherDeps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}

		if isLoopback(host) {
			ctx := apicontext.WithAuthInfo(r.Context(), &apicontext.AuthInfo{
				Scope:     string(auth.ScopeAdmin),
				Source:    "localhost",
				TokenID:   "localhost",
				TokenName: "localhost",
			})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			v1.ErrorResponse(w, http.StatusUnauthorized, v1.ErrCodeUnauthorized,
				"Authorization header required", nil)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			v1.ErrorResponse(w, http.StatusUnauthorized, v1.ErrCodeUnauthorized,
				"Invalid authorization scheme, use Bearer", nil)
			return
		}

		if strings.HasPrefix(token, "unet_") {
			handlePAT(deps, w, r, token, next)
			return
		}

		if strings.HasPrefix(token, "eyJ") {
			handleJWT(deps, w, r, token, next)
			return
		}

		v1.ErrorResponse(w, http.StatusUnauthorized, v1.ErrCodeUnauthorized,
			"Unrecognized token format", nil)
	})
}

func handlePAT(deps *AuthDispatcherDeps, w http.ResponseWriter, r *http.Request, plainToken string, next http.Handler) {
	result, err := deps.TokenCache.Validate(plainToken)
	if err != nil {
		v1.ErrorResponse(w, http.StatusUnauthorized, v1.ErrCodeUnauthorized,
			"Invalid or expired token", nil)
		return
	}

	ctx := apicontext.WithAuthInfo(r.Context(), &apicontext.AuthInfo{
		TokenID:   result.TokenID,
		TokenName: result.TokenName,
		Scope:     string(result.Scope),
		Source:    "pat",
	})
	next.ServeHTTP(w, r.WithContext(ctx))
}

func handleJWT(deps *AuthDispatcherDeps, w http.ResponseWriter, r *http.Request, tokenStr string, next http.Handler) {
	claims, err := deps.JWTIssuer.Validate(tokenStr)
	if err != nil {
		v1.ErrorResponse(w, http.StatusUnauthorized, v1.ErrCodeUnauthorized,
			"Invalid or expired JWT", nil)
		return
	}

	if deps.TokenCache != nil {
		tokens, listErr := deps.TokenCache.Store().List()
		if listErr != nil {
			v1.ErrorResponse(w, http.StatusUnauthorized, v1.ErrCodeUnauthorized,
				"Token validation failed", nil)
			return
		}
		found := false
		for _, t := range tokens {
			if t.ID == claims.Sub && t.Enabled {
				found = true
				break
			}
		}
		if !found {
			v1.ErrorResponse(w, http.StatusUnauthorized, v1.ErrCodeUnauthorized,
				"Referenced token revoked or deleted", nil)
			return
		}
	}

	ctx := apicontext.WithAuthInfo(r.Context(), &apicontext.AuthInfo{
		TokenID:   claims.Sub,
		TokenName: claims.Name,
		Scope:     string(claims.Scope),
		Source:    "jwt",
		SessionID: claims.Jti,
	})
	next.ServeHTTP(w, r.WithContext(ctx))
}

func isLoopback(host string) bool {
	return host == "127.0.0.1" || host == "::1" || host == "localhost" || strings.HasPrefix(host, "127.")
}
