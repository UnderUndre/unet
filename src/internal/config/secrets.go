package config

// SecretString is a string type whose value should never appear in logs
// or API responses unmasked.  Use RedactedString() in slog calls and
// GetMasked() for API handlers.
type SecretString string

// RedactedString always returns "<redacted>" so that accidental use in
// slog attributes never leaks the real value.
func (SecretString) RedactedString() string { return "<redacted>" }

// Plain returns the underlying cleartext.  Use sparingly — only in code
// that genuinely needs the secret (SSH dial, WireGuard key derivation,
// etc.).  Never pass the result to a logger or HTTP response.
func (s SecretString) Plain() string { return string(s) }

// mask returns "****<last-4>" if the secret is at least 4 characters,
// otherwise "****".  The result is safe for API responses and UI display.
func (s SecretString) mask() string {
	v := string(s)
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}
