package v1

import (
	"encoding/json"
	"net/http"
)

// Common error codes
const (
	ErrCodeUnauthorized       = "unauthorized"
	ErrCodeForbiddenScope     = "forbidden_scope"
	ErrCodeNotFound           = "not_found"
	ErrCodePeerNameConflict   = "peer_name_conflict"
	ErrCodeRouteConflict      = "route_conflict"
	ErrCodeTunnelNotConnected = "tunnel_not_connected"
	ErrCodeIPPoolExhausted    = "ip_pool_exhausted"
	ErrCodeVPSUnreachable     = "vps_unreachable"
	ErrCodeRateLimited        = "rate_limited"
	ErrCodeInternalError      = "internal_error"
	ErrCodeBadRequest         = "bad_request"
)

// APIError represents the standard error response body.
type APIError struct {
	Error   string         `json:"error"`
	Message string         `json:"message"`
	Context map[string]any `json:"context,omitempty"`
}

// ErrorResponse writes a structured JSON error response.
func ErrorResponse(w http.ResponseWriter, status int, code, message string, context map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := APIError{
		Error:   code,
		Message: message,
		Context: context,
	}

	_ = json.NewEncoder(w).Encode(resp)
}
