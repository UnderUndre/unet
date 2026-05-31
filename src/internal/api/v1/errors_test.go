package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestErrorResponse(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		code       string
		message    string
		context    map[string]any
		wantBody   string
	}{
		{
			name:     "basic error without context",
			status:   http.StatusUnauthorized,
			code:     ErrCodeUnauthorized,
			message:  "Invalid token",
			context:  nil,
			wantBody: `{"error":"unauthorized","message":"Invalid token"}`,
		},
		{
			name:     "error with context",
			status:   http.StatusForbidden,
			code:     ErrCodeForbiddenScope,
			message:  "Insufficient scope",
			context:  map[string]any{"required": "admin", "actual": "read"},
			wantBody: `{"error":"forbidden_scope","message":"Insufficient scope","context":{"actual":"read","required":"admin"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ErrorResponse(rec, tt.status, tt.code, tt.message, tt.context)

			if rec.Code != tt.status {
				t.Errorf("Expected status %d, got %d", tt.status, rec.Code)
			}

			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Expected Content-Type application/json, got %s", ct)
			}

			var got map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("Failed to parse JSON body: %v", err)
			}

			if got["error"] != tt.code {
				t.Errorf("Expected error code %s, got %v", tt.code, got["error"])
			}
			if got["message"] != tt.message {
				t.Errorf("Expected message %s, got %v", tt.message, got["message"])
			}

			if tt.context != nil {
				ctx, ok := got["context"].(map[string]any)
				if !ok {
					t.Fatal("Expected context in response")
				}
				if ctx["required"] != tt.context["required"] {
					t.Errorf("Context required mismatch")
				}
			}
		})
	}
}
