// Package state provides persistence for VPS lifecycle entities:
// VPSProfile (~/.unet/vps.json) and MigrationPlan (~/.unet/migration.json).
// Both use atomic write (temp + rename) with mode 0600.
package state

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/underundre/unet/internal/config"
)

// --- Enums ---

type VPSStatus string

const (
	VPSStatusActive        VPSStatus = "active"
	VPSStatusMigrating     VPSStatus = "migrating"
	VPSStatusDecommissioned VPSStatus = "decommissioned"
	VPSStatusUnreachable   VPSStatus = "unreachable"
)

type AuthMode string

const (
	AuthModeKey      AuthMode = "key"
	AuthModePassword AuthMode = "password"
)

// --- VPSProfile ---

// VPSProfile holds SSH coordinates, WireGuard tunnel parameters, and runtime
// status for the managed VPS. Stored in ~/.unet/vps.json.
type VPSProfile struct {
	Host              string    `json:"host"`
	Port              int       `json:"port,omitempty"`
	User              string    `json:"user"`
	AuthMode          AuthMode  `json:"authMode"`
	PrivateKeyPath    string    `json:"privateKeyPath,omitempty"`
	KnownGoodVersion  string    `json:"knownGoodVersion,omitempty"`
	LastSeenAt        string    `json:"lastSeenAt,omitempty"`
	Status            VPSStatus `json:"status"`
	ComposeHash       string    `json:"composeHash,omitempty"`
	WGEndpoint        string    `json:"wgEndpoint,omitempty"`
	WGServerPublicKey string    `json:"wgServerPublicKey,omitempty"`
	TunnelSubnet      string    `json:"tunnelSubnet,omitempty"`
	LockedBy          string    `json:"lockedBy,omitempty"`
}

// Validate checks all fields per data-model.md entity 1 rules.
func (p *VPSProfile) Validate() error {
	if p.Host == "" {
		return fmt.Errorf("state: host is required")
	}
	if !isValidHost(p.Host) {
		return fmt.Errorf("state: invalid host %q (must be IPv4, IPv6, or FQDN)", p.Host)
	}
	if p.Port < 0 || p.Port > 65535 {
		return fmt.Errorf("state: port %d out of range (0-65535)", p.Port)
	}
	if p.User == "" {
		return fmt.Errorf("state: user is required")
	}
	switch p.AuthMode {
	case AuthModeKey:
		if p.PrivateKeyPath == "" {
			return fmt.Errorf("state: privateKeyPath required when authMode=key")
		}
	case AuthModePassword:
		// Password stored in OS keychain, not in this file.
	default:
		return fmt.Errorf("state: invalid authMode %q", p.AuthMode)
	}
	if p.ComposeHash != "" && !isValidSHA256(p.ComposeHash) {
		return fmt.Errorf("state: composeHash must be 64-char lowercase hex")
	}
	if p.LockedBy != "" && !isValidLockFormat(p.LockedBy) {
		return fmt.Errorf("state: lockedBy must be <UUID>:<ISO-8601> or empty")
	}
	return nil
}

// DefaultPort returns the effective SSH port (22 if unset).
func (p *VPSProfile) DefaultPort() int {
	if p.Port == 0 {
		return 22
	}
	return p.Port
}

// --- Persistence ---

// VPSProfilePath returns ~/.unet/vps.json
func VPSProfilePath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "vps.json"), nil
}

// LoadVPSProfile reads and parses ~/.unet/vps.json.
// Returns nil profile if the file does not exist (no VPS configured).
func LoadVPSProfile() (*VPSProfile, error) {
	path, err := VPSProfilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("state: read vps.json: %w", err)
	}

	var profile VPSProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("state: parse vps.json: %w", err)
	}
	return &profile, nil
}

// SaveVPSProfile atomically writes profile to ~/.unet/vps.json.
// Validates before writing. Uses temp + rename.
func SaveVPSProfile(profile *VPSProfile) error {
	if err := profile.Validate(); err != nil {
		return err
	}

	path, err := VPSProfilePath()
	if err != nil {
		return err
	}
	return atomicWriteJSON(path, profile)
}

// --- Validation helpers ---

// fqdnRe matches valid DNS labels (RFC 1035 simplified).
var fqdnRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

// isValidHost checks if host is a valid IPv4, IPv6, or FQDN.
func isValidHost(host string) bool {
	// IPv4
	if net.ParseIP(host) != nil {
		return true
	}
	// IPv6 in brackets
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return net.ParseIP(host[1:len(host)-1]) != nil
	}
	// FQDN
	return fqdnRe.MatchString(host) && len(host) <= 253
}

// hexRe matches 64 lowercase hex characters (SHA256).
var hexRe = regexp.MustCompile(`^[a-f0-9]{64}$`)

func isValidSHA256(s string) bool {
	return hexRe.MatchString(s)
}

// lockRe matches "<UUID>:<ISO-8601>" format.
var lockRe = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}:\d{4}-\d{2}-\d{2}T`)

func isValidLockFormat(s string) bool {
	return lockRe.MatchString(s)
}

// --- Atomic write ---

func atomicWriteJSON(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("state: create dir: %w", err)
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("state: marshal: %w", err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("state: create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("state: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("state: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("state: close temp: %w", err)
	}

	if err := os.Chmod(tmpName, 0o600); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("state: chmod temp: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("state: rename %s -> %s: %w", tmpName, path, err)
	}

	return nil
}
