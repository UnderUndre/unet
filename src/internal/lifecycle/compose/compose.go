// Package compose provides embedded Docker Compose template rendering,
// SHA-256 hashing, and drift detection against deployed VPS compose files.
package compose

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// composeTmpl is the parsed compose template, initialized once at package load.
var composeTmpl *template.Template

func init() {
	tmplData, err := templateFS.ReadFile("templates/docker-compose.yml.tmpl")
	if err != nil {
		panic(fmt.Sprintf("compose: read embedded template: %v", err))
	}
	composeTmpl = template.Must(template.New("compose").Parse(string(tmplData)))
}

// RenderConfig holds parameters for docker-compose.yml generation.
type RenderConfig struct {
	// AWGPort is the AmneziaWG listen port (UDP).
	AWGPort int
	// ManualDNS is true when DNS mode is "manual" (HTTP-01 challenge
	// needs port 80/tcp). When false (Cloudflare DNS-01 mode), port 80
	// is omitted from the compose file.
	ManualDNS bool
	// CaddyImage is the container image for the caddy service.
	CaddyImage string
}

// DefaultCaddyImage is used when CaddyImage is empty.
const DefaultCaddyImage = "caddy:2-alpine"

// Render produces a docker-compose.yml from embedded templates with
// variable substitution. Returns the rendered YAML bytes.
func Render(cfg RenderConfig) ([]byte, error) {
	if cfg.AWGPort <= 0 || cfg.AWGPort > 65535 {
		return nil, fmt.Errorf("compose: invalid AWGPort %d (must be 1..65535)", cfg.AWGPort)
	}
	if cfg.CaddyImage == "" {
		cfg.CaddyImage = DefaultCaddyImage
	}

	tmpl := composeTmpl

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return nil, fmt.Errorf("compose: render template: %w", err)
	}

	return buf.Bytes(), nil
}

// Hash computes SHA-256 of data and returns the hex-encoded digest.
func Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// CanonicalHash returns the SHA-256 hash of the canonical compose template
// rendered with default (zero) values. This is the "golden" hash that
// represents the template as shipped in the binary.
func CanonicalHash() (string, error) {
	// Render with a dummy port — the hash is for template comparison,
	// not deployment. The actual rendered hash includes real values.
	data, err := Render(RenderConfig{AWGPort: 51820})
	if err != nil {
		return "", err
	}
	return Hash(data), nil
}

// DriftResult describes the result of comparing a VPS compose file against
// the canonical template.
type DriftResult struct {
	// Match is true when the rendered hash matches the VPS compose hash.
	Match bool `json:"match"`
	// ExpectedHash is the hash of the canonical rendered compose.
	ExpectedHash string `json:"expectedHash"`
	// ActualHash is the hash read from the VPS (empty if missing).
	ActualHash string `json:"actualHash"`
}

// DetectDrift compares the expected hash against the actual hash read from
// the VPS. Returns nil if they match, or a DriftResult with details.
func DetectDrift(expectedHash, actualHash string) (*DriftResult, error) {
	if expectedHash == "" {
		return nil, fmt.Errorf("compose: expected hash is empty")
	}

	result := &DriftResult{
		Match:        expectedHash == actualHash,
		ExpectedHash: expectedHash,
		ActualHash:   actualHash,
	}

	return result, nil
}
