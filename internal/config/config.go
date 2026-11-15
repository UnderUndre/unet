// Package config implements unet's TunnelConfig: JSON loading, strict
// validation (contracts/config.schema.json is the authoritative shape),
// owner-only permission handling, and defaults (FR-008, FR-009, FR-010,
// FR-007; zcode F1/F2/F4/F6/F7/F11/F15).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// SchemaVersion is the only supported config schema version (zcode F11).
const SchemaVersion = 1

// Resolution mode: v1 ships tunnel-side DNS ONLY (zcode F2) — there is no
// exit-side hostname-forwarding protocol over the raw L3 tunnel; it is
// deferred to 003-ecosystem-integration.
const ResolutionMode = "tunnel_dns"

// KeyLength is the WireGuard key size in bytes.
const KeyLength = 32

// Config mirrors contracts/config.schema.json (additionalProperties:false is
// enforced by strict JSON decoding).
type Config struct {
	Path string `json:"-"` // where this config was loaded from (reload, FR-007.003)

	SchemaVersion    int               `json:"schema_version"`
	LocalAddresses   []netip.Prefix    `json:"local_addresses"`
	PrivateKey       string            `json:"private_key,omitempty"`
	PublicKey        string            `json:"public_key"`
	Peer             PeerConfig        `json:"peer"`
	Obfuscation      ObfuscationParams `json:"obfuscation"`
	ResolutionMode   string            `json:"resolution_mode"`
	DNS              []netip.Addr      `json:"dns"`
	MTU              int               `json:"mtu,omitempty"`
	Proxy            ProxyConfig       `json:"proxy,omitempty"`
	Gateway          GatewayConfig     `json:"gateway,omitempty"`
	Reconnect        ReconnectConfig   `json:"reconnect,omitempty"`
	reconnectPresent bool              // whether the file carried a reconnect block
}

// PeerConfig is the remote AmneziaWG peer / exit.
type PeerConfig struct {
	PublicKey           string         `json:"public_key"`
	Endpoint            string         `json:"endpoint"`
	AllowedIPs          []netip.Prefix `json:"allowed_ips"`
	PresharedKey        string         `json:"preshared_key,omitempty"`
	PersistentKeepalive *int           `json:"persistent_keepalive,omitempty"`
}

// ObfuscationParams mirrors AWG 1.5 UAPI (device/uapi.go): jc/jmin/jmax must
// be >=1 when present (UAPI rejects <=0 — omit entirely to disable);
// h1..h4 are uint32 or "min-max" range strings, pairwise disjoint; s3/s4 and
// i1..i5 optional (zcode F4).
type ObfuscationParams struct {
	JC   *int        `json:"jc,omitempty"`
	JMin *int        `json:"jmin,omitempty"`
	JMax *int        `json:"jmax,omitempty"`
	S1   int         `json:"s1"`
	S2   int         `json:"s2"`
	S3   int         `json:"s3,omitempty"`
	S4   int         `json:"s4,omitempty"`
	H1   HeaderValue `json:"h1"`
	H2   HeaderValue `json:"h2"`
	H3   HeaderValue `json:"h3"`
	H4   HeaderValue `json:"h4"`
	I1   string      `json:"i1,omitempty"`
	I2   string      `json:"i2,omitempty"`
	I3   string      `json:"i3,omitempty"`
	I4   string      `json:"i4,omitempty"`
	I5   string      `json:"i5,omitempty"`
}

// HeaderValue accepts the schema's awgHeaderValue oneOf: a uint32 JSON number
// or a "min-max" range string (device/magic-header.go). Stored as the raw spec
// string the UAPI layer expects (zcode F4).
type HeaderValue string

// UnmarshalJSON accepts both number and string forms.
func (h *HeaderValue) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "null" {
		*h = ""
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(b, &num); err == nil {
		*h = HeaderValue(num.String())
		return nil
	}
	var str string
	if err := json.Unmarshal(b, &str); err == nil {
		*h = HeaderValue(str)
		return nil
	}
	return fmt.Errorf("h value must be a uint32 number or a \"min-max\" string: %s", trimmed)
}

// ProxyConfig is the local SOCKS5/HTTP listener (FR-004).
type ProxyConfig struct {
	Listen      string `json:"listen,omitempty"`
	AuthEnabled bool   `json:"auth_enabled,omitempty"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"` // secret-bearing (FR-008.003)
	PasswordEnv string `json:"password_env,omitempty"`
	SharedHost  bool   `json:"shared_host,omitempty"`
}

// GatewayConfig is the reverse-forward gateway + client-side allow-list (FR-006).
type GatewayConfig struct {
	Address            string        `json:"address,omitempty"`
	CapabilityToken    string        `json:"capability_token,omitempty"` // secret-bearing (FR-008.003)
	CapabilityTokenEnv string        `json:"capability_token_env,omitempty"`
	AllowList          []PortMapping `json:"allow_list,omitempty"`
}

// PortMapping is one default-deny reverse-forward authorization entry (FR-006.001).
type PortMapping struct {
	PublicPort int    `json:"public_port"`
	PublicHost string `json:"public_host,omitempty"` // informational; authority is the gateway table
	Proto      string `json:"proto,omitempty"`       // "tcp" (default) | "udp"
	LocalHost  string `json:"local_host"`
	LocalPort  int    `json:"local_port"`
}

// ReconnectConfig: backoff, liveness, terminal-failure knobs (FR-007).
// Exactly ONE terminal policy branch must hold (zcode F7).
type ReconnectConfig struct {
	InitialBackoffMS    int   `json:"initial_backoff_ms,omitempty"`
	MaxBackoffMS        int   `json:"max_backoff_ms,omitempty"`
	Jitter              *bool `json:"jitter,omitempty"`
	MaxTotalDurationMS  int   `json:"max_total_duration_ms,omitempty"`
	MaxAttempts         int   `json:"max_attempts,omitempty"`
	RetryForever        bool  `json:"retry_forever,omitempty"`
	KeepaliveIntervalMS int   `json:"keepalive_interval_ms,omitempty"`
	KeepaliveTimeoutMS  int   `json:"keepalive_timeout_ms,omitempty"`
	DialTimeoutMS       int   `json:"dial_timeout_ms,omitempty"`
}

// Defaulted reconnect knobs (published in schema descriptions, zcode F7).
const (
	DefaultInitialBackoffMS    = 1000
	DefaultMaxBackoffMS        = 60000
	DefaultMaxTotalDurationMS  = 600000
	DefaultMaxAttempts         = 8
	DefaultKeepaliveIntervalMS = 25000
	DefaultKeepaliveTimeoutMS  = 60000
	DefaultDialTimeoutMS       = 10000
	DefaultPersistentKeepalive = 25
	DefaultMTUCeiling          = 1420 // FR-010 / zcode F9: schema maximum
	DefaultPathMTU             = 1500
	wgDataHeaderBytes          = 32 // WireGuard data-plane header
	wgOuterOverhead            = 60 // 20 outer IP + 8 UDP (FR-010)
)

// DefaultMTU computes the FR-010 default: path_mtu − outer(60) − WG header(32) − S4.
func (o ObfuscationParams) DefaultMTU() int {
	mtu := DefaultPathMTU - wgOuterOverhead - wgDataHeaderBytes - o.S4
	if mtu < 576 {
		return 576
	}
	if mtu > DefaultMTUCeiling {
		return DefaultMTUCeiling
	}
	return mtu
}

// HasSecrets reports whether the config carries any secret-bearing value in
// the FILE itself (env references do not count — FR-008.003).
func (c *Config) HasSecrets() bool {
	return c.PrivateKey != "" ||
		c.Peer.PresharedKey != "" ||
		c.Proxy.Password != "" ||
		c.Gateway.CapabilityToken != ""
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Options tunes Load.
type Options struct {
	// InsecurePerms downgrades owner-only permission enforcement to a loud
	// warning instead of refuse-to-start (FR-008.003).
	InsecurePerms bool
}

// Load reads, strictly decodes, permission-checks and validates the config
// file at path. It returns a fully-defaulted Config or an actionable error.
func Load(path string, opts Options) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	// Permission enforcement BEFORE parsing secrets (FR-008.003): a scan of
	// the raw JSON avoids full decoding for the secret-presence check.
	if rawHasSecrets(raw) {
		owner, perr := checkOwnerOnly(path)
		switch {
		case perr == nil && !owner:
			if opts.InsecurePerms {
				fmt.Fprintf(os.Stderr, "WARNING[unet]: %s contains secrets and is not owner-only; continuing due to --insecure-permissions (FR-008.003)\n", path)
			} else {
				return nil, fmt.Errorf("config: %s contains secret fields and is group/world readable — chmod 600 the file (POSIX) or restrict the ACL to the current user (Windows), or move secrets to env references; start with --insecure-permissions to override with a warning (FR-008.003)", path)
			}
		case perr != nil:
			return nil, fmt.Errorf("config: cannot check permissions of %s: %w", path, perr)
		}
	}

	cfg, perr := decodeAndValidate(raw)
	if perr != nil {
		return nil, perr
	}
	cfg.Path = path
	return cfg, nil
}

// decodeAndValidate strictly decodes and validates the raw JSON.
func decodeAndValidate(raw []byte) (*Config, error) {
	dec := jsonDecoder(raw)
	dec.DisallowUnknownFields() // additionalProperties:false (zcode F11 contract)
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config: invalid JSON or unknown field (schema is additionalProperties:false): %w", err)
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	c.applyDefaults()
	return &c, nil
}

// validate enforces every schema-level rule with actionable messages.
func (c *Config) validate() error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("config: schema_version %d not supported (this build supports %d); upgrade unet or migrate the config (zcode F11)", c.SchemaVersion, SchemaVersion)
	}
	if c.ResolutionMode != ResolutionMode {
		return fmt.Errorf("config: resolution_mode must be %q in v1 (no exit-side hostname-forwarding protocol exists over the L3 tunnel; zcode F2)", ResolutionMode)
	}
	if len(c.LocalAddresses) == 0 {
		return errors.New(`config: local_addresses is required (e.g. ["10.66.0.2/32"]) — CreateNetTUN needs the client's tunnel IPs (zcode F1)`)
	}
	for i, p := range c.LocalAddresses {
		if !p.IsValid() {
			return fmt.Errorf("config: local_addresses[%d] is not a valid CIDR prefix", i)
		}
	}
	if err := checkKey("public_key", c.PublicKey); err != nil {
		return err
	}
	if c.PrivateKey != "" {
		if err := checkKey("private_key", c.PrivateKey); err != nil {
			return err
		}
	}
	if c.Peer.PresharedKey != "" {
		if err := checkKey("peer.preshared_key", c.Peer.PresharedKey); err != nil {
			return err
		}
	}
	if err := checkKey("peer.public_key", c.Peer.PublicKey); err != nil {
		return err
	}
	if err := checkHostPort("peer.endpoint", c.Peer.Endpoint); err != nil {
		return err
	}
	if len(c.Peer.AllowedIPs) == 0 {
		return errors.New("config: peer.allowed_ips must contain at least one prefix")
	}
	for i, p := range c.Peer.AllowedIPs {
		if !p.IsValid() {
			return fmt.Errorf("config: peer.allowed_ips[%d] is not a valid CIDR prefix", i)
		}
	}
	if c.Peer.PersistentKeepalive != nil && (*c.Peer.PersistentKeepalive < 0 || *c.Peer.PersistentKeepalive > 65535) {
		return errors.New("config: peer.persistent_keepalive must be 0..65535 seconds")
	}
	if err := c.Obfuscation.validate(); err != nil {
		return err
	}
	if len(c.DNS) == 0 {
		return errors.New("config: dns must list at least one tunnel-side resolver IP (resolution_mode=tunnel_dns)")
	}
	for i, a := range c.DNS {
		if !a.IsValid() {
			return fmt.Errorf("config: dns[%d] is not a valid IP address", i)
		}
	}
	if c.MTU != 0 && (c.MTU < 576 || c.MTU > DefaultMTUCeiling) {
		return fmt.Errorf("config: mtu must be 576..%d (got %d)", DefaultMTUCeiling, c.MTU)
	}
	if err := c.Proxy.validate(); err != nil {
		return err
	}
	if err := c.Gateway.validate(); err != nil {
		return err
	}
	return c.Reconnect.validate(&c.reconnectPresent)
}

func (o ObfuscationParams) validate() error {
	for name, v := range map[string]*int{"jc": o.JC, "jmin": o.JMin, "jmax": o.JMax} {
		if v != nil && *v < 1 {
			return fmt.Errorf("config: obfuscation.%s must be >=1 when present (UAPI rejects <=0); omit the key entirely to disable (zcode F4)", name)
		}
	}
	if o.JMin != nil && o.JMax != nil && *o.JMin > *o.JMax {
		return errors.New("config: obfuscation.jmin must be <= jmax (zcode F4)")
	}
	for _, v := range []int{o.S1, o.S2, o.S3, o.S4} {
		if v < 0 || v > 65535 {
			return errors.New("config: obfuscation.s1..s4 must be 0..65535")
		}
	}
	ranges, err := parseHeaderRanges([]string{string(o.H1), string(o.H2), string(o.H3), string(o.H4)})
	if err != nil {
		return err
	}
	for i, r := range ranges {
		for j := i + 1; j < len(ranges); j++ {
			if r.lo <= ranges[j].hi && ranges[j].lo <= r.hi {
				return fmt.Errorf("config: obfuscation.h%d and h%d ranges overlap — header ranges must be pairwise disjoint (zcode F4)", i+1, j+1)
			}
		}
	}
	for _, tag := range []string{o.I1, o.I2, o.I3, o.I4, o.I5} {
		if len(tag) > 1024 {
			return errors.New("config: obfuscation.i1..i5 tag sequence exceeds 1024 bytes")
		}
	}
	return nil
}

type hdrRange struct{ lo, hi uint32 }

// parseHeaderRanges validates H values: uint32 (>=5, WireGuard message types
// 1-4 must not collide) or "min-max" range strings (device/magic-header.go,
// zcode F4).
func parseHeaderRanges(vals []string) ([]hdrRange, error) {
	out := make([]hdrRange, 0, len(vals))
	for i, v := range vals {
		if v == "" {
			return nil, fmt.Errorf("config: obfuscation.h%d is required", i+1)
		}
		if strings.Contains(v, "-") {
			parts := strings.SplitN(v, "-", 2)
			lo, err1 := strconv.ParseUint(parts[0], 10, 32)
			hi, err2 := strconv.ParseUint(parts[1], 10, 32)
			if err1 != nil || err2 != nil || lo > hi {
				return nil, fmt.Errorf("config: obfuscation.h%d %q is not a valid \"min-max\" range (device/magic-header.go format)", i+1, v)
			}
			if lo < 5 {
				return nil, fmt.Errorf("config: obfuscation.h%d range starts below 5 — WireGuard message types 1-4 must not collide (zcode F4)", i+1)
			}
			out = append(out, hdrRange{uint32(lo), uint32(hi)})
			continue
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil || n < 5 {
			return nil, fmt.Errorf("config: obfuscation.h%d %q must be a uint32 >=5 (WireGuard types 1-4 reserved) or a \"min-max\" range (zcode F4)", i+1, v)
		}
		out = append(out, hdrRange{uint32(n), uint32(n)})
	}
	return out, nil
}

func (p ProxyConfig) validate() error {
	if p.Listen != "" && !strings.HasPrefix(p.Listen, "unix://") && !strings.HasPrefix(p.Listen, "@") {
		if err := checkHostPort("proxy.listen", p.Listen); err != nil {
			return err
		}
	}
	if p.AuthEnabled {
		if p.Username == "" {
			return errors.New("config: proxy.username is required when auth_enabled=true (zcode F6)")
		}
		if p.Password == "" && p.PasswordEnv == "" {
			return errors.New("config: proxy.password or proxy.password_env is required when auth_enabled=true (zcode F6)")
		}
		if p.Password != "" && p.PasswordEnv != "" {
			return errors.New("config: proxy.password and proxy.password_env are mutually exclusive")
		}
	}
	if p.PasswordEnv != "" && !envNameRe.MatchString(p.PasswordEnv) {
		return fmt.Errorf("config: proxy.password_env %q is not a valid environment variable name", p.PasswordEnv)
	}
	return nil
}

func (g GatewayConfig) validate() error {
	if g.Address == "" {
		return nil // gateway block optional (SOCKS5-only usage, T2)
	}
	if err := checkHostPort("gateway.address", g.Address); err != nil {
		return err
	}
	if g.CapabilityTokenEnv != "" && !envNameRe.MatchString(g.CapabilityTokenEnv) {
		return fmt.Errorf("config: gateway.capability_token_env %q is not a valid environment variable name", g.CapabilityTokenEnv)
	}
	if g.CapabilityToken != "" && g.CapabilityTokenEnv != "" {
		return errors.New("config: gateway.capability_token and gateway.capability_token_env are mutually exclusive")
	}
	seen := map[string]bool{}
	for i, m := range g.AllowList {
		if m.PublicPort < 1 || m.PublicPort > 65535 {
			return fmt.Errorf("config: gateway.allow_list[%d].public_port must be 1..65535", i)
		}
		if m.LocalPort < 1 || m.LocalPort > 65535 {
			return fmt.Errorf("config: gateway.allow_list[%d].local_port must be 1..65535", i)
		}
		if m.LocalHost == "" || m.LocalHost == "*" || strings.ContainsAny(m.LocalHost, "*?") {
			return fmt.Errorf("config: gateway.allow_list[%d].local_host must be a concrete host — wildcards forbidden (FR-006.001)", i)
		}
		proto := m.Proto
		if proto == "" {
			proto = "tcp"
		}
		if proto != "tcp" && proto != "udp" {
			return fmt.Errorf("config: gateway.allow_list[%d].proto must be tcp or udp", i)
		}
		key := strconv.Itoa(m.PublicPort) + "/" + proto
		if seen[key] {
			return fmt.Errorf("config: gateway.allow_list has duplicate public_port %s entries", key)
		}
		seen[key] = true
	}
	return nil
}

func (r *ReconnectConfig) validate(present *bool) error {
	*present = r.RetryForever || r.MaxAttempts > 0 || r.MaxTotalDurationMS > 0
	if r.RetryForever && (r.MaxAttempts > 0 || r.MaxTotalDurationMS > 0) {
		return errors.New("config: reconnect.retry_forever is mutually exclusive with max_attempts/max_total_duration_ms (zcode F7)")
	}
	for name, v := range map[string]int{
		"initial_backoff_ms": r.InitialBackoffMS, "max_backoff_ms": r.MaxBackoffMS,
		"max_total_duration_ms": r.MaxTotalDurationMS, "max_attempts": r.MaxAttempts,
		"keepalive_interval_ms": r.KeepaliveIntervalMS, "keepalive_timeout_ms": r.KeepaliveTimeoutMS,
		"dial_timeout_ms": r.DialTimeoutMS,
	} {
		if v < 0 {
			return fmt.Errorf("config: reconnect.%s must be >= 0", name)
		}
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.Peer.PersistentKeepalive == nil {
		ka := DefaultPersistentKeepalive
		c.Peer.PersistentKeepalive = &ka
	}
	if c.MTU == 0 {
		c.MTU = c.Obfuscation.DefaultMTU()
	}
	if c.Proxy.Listen == "" {
		c.Proxy.Listen = "127.0.0.1:1080"
	}
	for i := range c.Gateway.AllowList {
		if c.Gateway.AllowList[i].Proto == "" {
			c.Gateway.AllowList[i].Proto = "tcp"
		}
	}
	r := &c.Reconnect
	if r.InitialBackoffMS == 0 {
		r.InitialBackoffMS = DefaultInitialBackoffMS
	}
	if r.MaxBackoffMS == 0 {
		r.MaxBackoffMS = DefaultMaxBackoffMS
	}
	if r.KeepaliveIntervalMS == 0 {
		r.KeepaliveIntervalMS = DefaultKeepaliveIntervalMS
	}
	if r.KeepaliveTimeoutMS == 0 {
		r.KeepaliveTimeoutMS = DefaultKeepaliveTimeoutMS
	}
	if r.DialTimeoutMS == 0 {
		r.DialTimeoutMS = DefaultDialTimeoutMS
	}
	if !r.RetryForever {
		if r.MaxTotalDurationMS == 0 && r.MaxAttempts == 0 {
			r.MaxTotalDurationMS = DefaultMaxTotalDurationMS
		}
		if r.Jitter == nil {
			t := true
			r.Jitter = &t
		}
	}
}

// checkKey validates base64 WireGuard keys (32 bytes).
func checkKey(field, v string) error {
	b, err := decodeKey(v)
	if err != nil {
		return fmt.Errorf("config: %s: %w", field, err)
	}
	if len(b) != KeyLength {
		return fmt.Errorf("config: %s must decode to %d bytes (got %d)", field, KeyLength, len(b))
	}
	return nil
}

// checkHostPort validates host:port via net.SplitHostPort with strict port
// range (zcode F15; schema regex is the first line of defense).
func checkHostPort(field, v string) error {
	host, portStr, err := splitHostPort(v)
	if err != nil {
		return fmt.Errorf("config: %s %q: %w", field, v, err)
	}
	if host == "" {
		return fmt.Errorf("config: %s %q: empty host", field, v)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("config: %s %q: port must be 1..65535", field, v)
	}
	return nil
}

// SaveOwnerOnly writes data to path with owner-only permissions, tightening
// an existing file's permissions first (FR-008.002).
func SaveOwnerOnly(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return tightenOwnerOnly(path)
}
