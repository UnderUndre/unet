package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validConfigJSON is a schema-complete, valid TunnelConfig used as the base
// for mutation tests. Keys are valid 32-byte base64 values.
const validConfigJSON = `{
  "schema_version": 1,
  "local_addresses": ["10.66.0.2/32"],
  "public_key": "6spL9TdkorQCKxQ8bVnDwLHwQtE9BN5nRTu5XlWYqg0=",
  "private_key": "uF0KkxdKlCAxcKfa2YRNvongnwC+PNZEqx1C4pUcv3s=",
  "peer": {
    "public_key": "9lRwdFeOiQv0KxTQEfY28LIYQNDwehq/9lRwdFeOiQs=",
    "endpoint": "vps.example.com:51820",
    "allowed_ips": ["10.66.0.0/24"],
    "persistent_keepalive": 15
  },
  "obfuscation": {
    "jc": 6, "jmin": 50, "jmax": 1100,
    "s1": 30, "s2": 20, "s4": 10,
    "h1": "100000", "h2": "200000", "h3": 300000, "h4": "400000-410000"
  },
  "resolution_mode": "tunnel_dns",
  "dns": ["10.66.0.1"],
  "mtu": 1380,
  "proxy": { "auth_enabled": true, "username": "local", "password_env": "UNET_PROXY_PASSWORD" },
  "gateway": {
    "address": "10.66.0.1:7443",
    "capability_token_env": "UNET_GATEWAY_TOKEN",
    "allow_list": [ { "public_port": 443, "proto": "tcp", "local_host": "127.0.0.1", "local_port": 3000 } ]
  },
  "reconnect": { "max_attempts": 8 }
}`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	// Windows: os.WriteFile's mode does not restrict inherited ACLs — the
	// FR-008.002 owner-only DACL must be applied explicitly (as T4's writer
	// does in production).
	if err := tightPerms(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func mustLoad(t *testing.T, path string, opts Options) *Config {
	t.Helper()
	cfg, err := Load(path, opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestLoadValidFull(t *testing.T) {
	cfg := mustLoad(t, writeTemp(t, validConfigJSON), Options{})
	if cfg.SchemaVersion != 1 {
		t.Errorf("schema_version = %d", cfg.SchemaVersion)
	}
	if len(cfg.LocalAddresses) != 1 || cfg.LocalAddresses[0].Addr().String() != "10.66.0.2" {
		t.Errorf("local_addresses parsed wrong: %v", cfg.LocalAddresses)
	}
	// Defaults (zcode F7): reconnect block only set max_attempts → others default.
	if cfg.Reconnect.InitialBackoffMS != DefaultInitialBackoffMS ||
		cfg.Reconnect.MaxBackoffMS != DefaultMaxBackoffMS ||
		cfg.Reconnect.KeepaliveIntervalMS != DefaultKeepaliveIntervalMS ||
		cfg.Reconnect.DialTimeoutMS != DefaultDialTimeoutMS {
		t.Errorf("reconnect defaults not applied: %+v", cfg.Reconnect)
	}
	if cfg.Reconnect.MaxAttempts != 8 {
		t.Errorf("max_attempts carried: %d", cfg.Reconnect.MaxAttempts)
	}
	// FR-008 default keepalive override respected when present.
	if cfg.Peer.PersistentKeepalive == nil || *cfg.Peer.PersistentKeepalive != 15 {
		t.Errorf("persistent_keepalive = %v", cfg.Peer.PersistentKeepalive)
	}
	// allow_list proto defaulted to tcp.
	if cfg.Gateway.AllowList[0].Proto != "tcp" {
		t.Errorf("allow_list proto = %q", cfg.Gateway.AllowList[0].Proto)
	}
}

func TestRejectsUnknownFields(t *testing.T) {
	bad := strings.Replace(validConfigJSON, `"resolution_mode": "tunnel_dns",`,
		`"resolution_mode": "tunnel_dns", "bogus_field": 1,`, 1)
	_, err := Load(writeTemp(t, bad), Options{})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("want unknown-field error, got %v", err)
	}
}

func TestSchemaVersionMismatch(t *testing.T) {
	bad := strings.Replace(validConfigJSON, `"schema_version": 1`, `"schema_version": 2`, 1)
	_, err := Load(writeTemp(t, bad), Options{})
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("want schema_version error, got %v", err)
	}
}

func TestLocalAddressesRequired(t *testing.T) {
	bad := strings.Replace(validConfigJSON, `"local_addresses": ["10.66.0.2/32"],`, ``, 1)
	_, err := Load(writeTemp(t, bad), Options{})
	if err == nil || !strings.Contains(err.Error(), "local_addresses") {
		t.Fatalf("want local_addresses error (zcode F1), got %v", err)
	}
}

func TestResolutionModeTunnelDNSOnly(t *testing.T) {
	bad := strings.Replace(validConfigJSON, `"resolution_mode": "tunnel_dns"`, `"resolution_mode": "remote"`, 1)
	_, err := Load(writeTemp(t, bad), Options{})
	if err == nil || !strings.Contains(err.Error(), "tunnel_dns") {
		t.Fatalf("want tunnel_dns-only error (zcode F2), got %v", err)
	}
}

func TestObfuscationRules(t *testing.T) {
	cases := []struct {
		name string
		find string
		repl string
		want string
	}{
		{"jc zero", `"jc": 6`, `"jc": 0`, ">=1"},
		{"jmin>jmax", `"jmin": 50, "jmax": 1100`, `"jmin": 1100, "jmax": 50`, "jmin"},
		{"h overlap", `"h3": 300000`, `"h3": "100000-150000"`, "overlap"},
		{"h below 5", `"h1": "100000"`, `"h1": 3`, "uint32 >=5"},
	}
	for _, tc := range cases {
		bad := strings.Replace(validConfigJSON, tc.find, tc.repl, 1)
		_, err := Load(writeTemp(t, bad), Options{})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: want error containing %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestProxyAuthRequiresCreds(t *testing.T) {
	bad := strings.Replace(validConfigJSON, `"auth_enabled": true, "username": "local", "password_env": "UNET_PROXY_PASSWORD"`,
		`"auth_enabled": true`, 1)
	_, err := Load(writeTemp(t, bad), Options{})
	if err == nil || !strings.Contains(err.Error(), "proxy.") {
		t.Fatalf("want auth-credentials error (zcode F6), got %v", err)
	}
}

func TestReconnectMutualExclusion(t *testing.T) {
	bad := strings.Replace(validConfigJSON, `"reconnect": { "max_attempts": 8 }`,
		`"reconnect": { "max_attempts": 8, "retry_forever": true }`, 1)
	_, err := Load(writeTemp(t, bad), Options{})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("want mutual-exclusion error (zcode F7), got %v", err)
	}
}

func TestMTUCeiling(t *testing.T) {
	bad := strings.Replace(validConfigJSON, `"mtu": 1380`, `"mtu": 1500`, 1)
	_, err := Load(writeTemp(t, bad), Options{})
	if err == nil || !strings.Contains(err.Error(), "576..") {
		t.Fatalf("want mtu ceiling error (zcode F9), got %v", err)
	}
}

func TestDefaultMTUAccountsForS4(t *testing.T) {
	bad := strings.Replace(validConfigJSON, `"mtu": 1380,`, ``, 1)
	cfg := mustLoad(t, writeTemp(t, bad), Options{})
	// 1500 - 60 - 32 - s4(10) = 1398 (FR-010 / zcode F9).
	if cfg.MTU != 1398 {
		t.Errorf("default MTU = %d, want 1398", cfg.MTU)
	}
}

func TestEndpointValidation(t *testing.T) {
	for _, bad := range []string{"vps.example.com", "vps.example.com:0", "vps.example.com:99999"} {
		badJSON := strings.Replace(validConfigJSON, `"endpoint": "vps.example.com:51820"`, `"endpoint": "`+bad+`"`, 1)
		_, err := Load(writeTemp(t, badJSON), Options{})
		if err == nil {
			t.Fatalf("endpoint %q: want error (zcode F15)", bad)
		}
	}
}

func TestPermissionsRefuseLooseAllowTight(t *testing.T) {
	// FR-008.003: file WITH secrets + loose perms → refuse; --insecure-permissions → warning; tight → ok.
	loose := writeTemp(t, validConfigJSON)
	loosenFile(t, loose)

	if _, err := Load(loose, Options{}); err == nil || !strings.Contains(err.Error(), "secret fields") {
		t.Fatalf("want owner-only refusal, got %v", err)
	}
	if _, err := Load(loose, Options{InsecurePerms: true}); err != nil {
		t.Fatalf("insecure-permissions should warn-and-pass, got %v", err)
	}

	tight := writeTemp(t, validConfigJSON)
	tightenFile(t, tight)
	if _, err := Load(tight, Options{}); err != nil {
		t.Fatalf("tight perms should load: %v", err)
	}
}

func TestEnvRefsAvoidSecretRefusal(t *testing.T) {
	// A config whose ONLY secrets live in env references (private_key moved to
	// UNET_PRIVATE_KEY) must NOT trigger the secret-perm refusal even loose
	// (FR-008.003 relaxation).
	envOnly := strings.Replace(validConfigJSON, `"private_key": "uF0KkxdKlCAxcKfa2YRNvongnwC+PNZEqx1C4pUcv3s=",
  `, ``, 1)
	if strings.Contains(envOnly, `"private_key"`) {
		t.Fatal("test fixture still contains private_key")
	}
	loose := writeTemp(t, envOnly)
	loosenFile(t, loose)
	if _, err := Load(loose, Options{}); err != nil && strings.Contains(err.Error(), "secret fields") {
		t.Fatalf("env-ref-only config should not trigger secret refusal: %v", err)
	}
}

// --- platform-specific file-permission helpers ---

func loosenFile(t *testing.T, path string) {
	t.Helper()
	if err := loosePerms(path); err != nil {
		t.Fatal(err)
	}
}

func tightenFile(t *testing.T, path string) {
	t.Helper()
	if err := tightPerms(path); err != nil {
		t.Fatal(err)
	}
}
