package middleware

import (
	"net/http"

	"github.com/underundre/unet/internal/api/apicontext"
	"github.com/underundre/unet/internal/api/v1"
	"github.com/underundre/unet/internal/auth"
)

func RequireScope(required auth.Scope, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, ok := apicontext.AuthInfoFromContext(r.Context())
		if !ok {
			v1.ErrorResponse(w, http.StatusUnauthorized, v1.ErrCodeUnauthorized,
				"Authentication required", nil)
			return
		}

		if !auth.Scope(info.Scope).Allows(required) {
			v1.ErrorResponse(w, http.StatusForbidden, v1.ErrCodeForbiddenScope,
				"Insufficient scope", map[string]any{
					"required_scope": string(required),
					"actual_scope":   info.Scope,
				})
			return
		}

		next.ServeHTTP(w, r)
	})
}
