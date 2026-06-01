// Package logger provides a custom slog.Handler for structured JSONL logging
// with secret redaction, per-component level filtering, and dual-write (file + ring buffer).
package logger

import (
	"strings"
)

// secretKeys are case-insensitive keys whose values must be redacted.
var secretKeys = map[string]struct{}{
	"password":    {},
	"secret":      {},
	"private_key": {},
	"privatekey":  {},
	"token":       {},
	"apikey":      {},
	"api_key":     {},
	"bearertoken": {},
	"uitoken":     {},
	"mtls_key":    {},
	"cf_token":    {},
}

// pemMarkers are PEM block delimiters that indicate a secret value.
var pemMarkers = []string{
	"-----BEGIN PRIVATE KEY-----",
	"-----BEGIN RSA PRIVATE KEY-----",
	"-----BEGIN EC PRIVATE KEY-----",
	"-----BEGIN OPENSSH PRIVATE KEY-----",
}

// RedactFields returns a copy of fields with secret values replaced by "<redacted>".
// Recurses into nested maps. Returns nil if input is nil.
func RedactFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	result := make(map[string]any, len(fields))
	for k, v := range fields {
		result[k] = redactValue(k, v)
	}
	return result
}

// redactValue checks a single key-value pair for secrets.
func redactValue(key string, val any) any {
	// Check if key is a secret key (case-insensitive)
	lowerKey := strings.ToLower(key)
	if _, ok := secretKeys[lowerKey]; ok {
		return "<redacted>"
	}

	switch v := val.(type) {
	case string:
		// Check for PEM blocks
		for _, marker := range pemMarkers {
			if strings.Contains(v, marker) {
				return "<redacted>"
			}
		}
		return v
	case map[string]any:
		// Recurse into nested maps
		return RedactFields(v)
	default:
		return val
	}
}
