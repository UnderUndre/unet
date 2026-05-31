package middleware

import (
	"net/http"
	"strings"

	"github.com/underundre/unet/internal/api/apicontext"
	"github.com/underundre/unet/internal/audit"
)

type recordingWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *recordingWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func AuditLog(logger *audit.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &recordingWriter{ResponseWriter: w, statusCode: 200}
		next.ServeHTTP(rw, r)

		if logger == nil {
			return
		}

		info, ok := apicontext.AuthInfoFromContext(r.Context())
		if !ok || info.Source == "localhost" {
			return
		}

		method := r.Method
		if method != "POST" && method != "DELETE" {
			return
		}

		if rw.statusCode < 200 || rw.statusCode >= 300 {
			return
		}

		action := methodToAction(method, r.URL.Path)

		go func() {
			_ = logger.Write(audit.Entry{
				ActorTokenID:   info.TokenID,
				ActorTokenName: info.TokenName,
				Action:         action,
				SourceIP:       r.RemoteAddr,
				UserAgent:      r.UserAgent(),
			})
		}()
	})
}

func methodToAction(method, path string) audit.Action {
	if strings.Contains(path, "/peers") {
		if method == "POST" {
			return audit.ActionCreatePeer
		}
		return audit.ActionDeletePeer
	}
	if strings.Contains(path, "/routes") {
		if method == "POST" {
			return audit.ActionCreateRoute
		}
		return audit.ActionDeleteRoute
	}
	if strings.Contains(path, "/tokens") {
		if method == "POST" {
			return audit.ActionCreateToken
		}
		return audit.ActionRevokeToken
	}
	return audit.Action("")
}
