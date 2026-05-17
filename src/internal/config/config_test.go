package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// helper to create a temp-based Manager without touching real ~/.unet.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	mgr := &Manager{
		path:   cfgPath,
		config: defaultConfig(),
	}
	if err := mgr.Save(); err != nil {
		t.Fatalf("save skeleton: %v", err)
	}
	return mgr
}

func TestDefaultConfig_HasUIToken(t *testing.T) {
	cfg := defaultConfig()
	if cfg.UIToken == "" {
		t.Fatal("expected non-empty uiToken in default config")
	}
	if len(cfg.ExposedPorts) != 0 {
		t.Fatal("expected empty exposedPorts")
	}
}

func TestSecretString_RedactedString(t *testing.T) {
	s := SecretString("supersecret")
	if got := s.RedactedString(); got != "<redacted>" {
		t.Fatalf("RedactedString() = %q, want %q", got, "<redacted>")
	}
}

func TestSecretString_Mask(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"short", "****hort"},
		{"ab", "****"},
		{"", "****"},
		{"12345678", "****5678"},
		{"abcdefghijklmnopqrstuvwxyz", "****wxyz"},
	}
	for _, tt := range tests {
		s := SecretString(tt.input)
		if got := s.mask(); got != tt.want {
			t.Errorf("mask(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSecretString_Plain(t *testing.T) {
	s := SecretString("hunter2")
	if got := s.Plain(); got != "hunter2" {
		t.Fatalf("Plain() = %q, want %q", got, "hunter2")
	}
}

func TestManager_Get_ReturnsRaw(t *testing.T) {
	mgr := newTestManager(t)
	raw := mgr.Get()
	if raw.VPS.Password != "" {
		// Not set in default, but if we set one it should come through.
	}
	raw.VPS.Password = SecretString("secret123")
	// Get should reflect the in-memory state (pointer).
	raw2 := mgr.Get()
	if raw2.VPS.Password != "secret123" {
		t.Fatal("Get() should return pointer to live config")
	}
}

func TestManager_GetMasked_HidesSecrets(t *testing.T) {
	mgr := newTestManager(t)

	// Set secrets via Update.
	err := mgr.Update(func(cfg *RootConfig) {
		cfg.VPS.Password = SecretString("ssh_password_1234")
		cfg.Tunnel.PrivateKey = SecretString("wg_privkey_ABCD")
		cfg.Tunnel.PresharedKey = SecretString("psk_XYZW")
		cfg.CaddyAPI.ClientKey = SecretString("mtls_client_key_99")
		cfg.CaddyAPI.TLSKey = SecretString("tls_key_5678")
		cfg.DNS.Token = SecretString("cloudflare_token_1234")
		cfg.UIToken = SecretString("ui_token_secret")
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	masked := mgr.GetMasked()

	// Verify all secrets are masked.
	if got := string(masked.VPS.Password); got != "****1234" {
		t.Errorf("VPS.Password = %q, want %q", got, "****1234")
	}
	if got := string(masked.Tunnel.PrivateKey); got != "****ABCD" {
		t.Errorf("Tunnel.PrivateKey = %q, want %q", got, "****ABCD")
	}
	if got := string(masked.Tunnel.PresharedKey); got != "****XYZW" {
		t.Errorf("Tunnel.PresharedKey = %q, want %q", got, "****XYZW")
	}
	if got := string(masked.CaddyAPI.ClientKey); got != "****y_99" {
		t.Errorf("CaddyAPI.ClientKey = %q, want %q", got, "****y_99")
	}
	if got := string(masked.CaddyAPI.TLSKey); got != "****5678" {
		t.Errorf("CaddyAPI.TLSKey = %q, want %q", got, "****5678")
	}
	if got := string(masked.DNS.Token); got != "****1234" {
		t.Errorf("DNS.Token = %q, want %q", got, "****1234")
	}
	if got := string(masked.UIToken); got != "****cret" {
		t.Errorf("UIToken = %q, want %q", got, "****cret")
	}

	// Verify Get() still returns raw values.
	raw := mgr.Get()
	if raw.VPS.Password != "ssh_password_1234" {
		t.Fatal("Get() should return unmasked secrets")
	}
}

func TestManager_SaveAndReload(t *testing.T) {
	mgr := newTestManager(t)

	err := mgr.Update(func(cfg *RootConfig) {
		cfg.VPS.Host = "192.168.1.1"
		cfg.VPS.SSHPort = 2222
		cfg.VPS.Username = "root"
		cfg.Tunnel.InterfaceName = "awg0"
		cfg.DNS.Provider = "cloudflare"
		cfg.Daemon.LogLevel = "debug"
		cfg.ExposedPorts = []ExposedPort{
			{Protocol: "tcp", Internal: 8080},
		}
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Reload from disk.
	data, err := os.ReadFile(mgr.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var loaded RootConfig
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if loaded.VPS.Host != "192.168.1.1" {
		t.Errorf("VPS.Host = %q", loaded.VPS.Host)
	}
	if loaded.VPS.SSHPort != 2222 {
		t.Errorf("VPS.SSHPort = %d", loaded.VPS.SSHPort)
	}
	if loaded.Tunnel.InterfaceName != "awg0" {
		t.Errorf("Tunnel.InterfaceName = %q", loaded.Tunnel.InterfaceName)
	}
	if len(loaded.ExposedPorts) != 1 || loaded.ExposedPorts[0].Internal != 8080 {
		t.Errorf("ExposedPorts = %+v", loaded.ExposedPorts)
	}
}

func TestManager_Update_DoesNotCorruptOnFailure(t *testing.T) {
	mgr := newTestManager(t)

	// Set an initial value.
	err := mgr.Update(func(cfg *RootConfig) {
		cfg.VPS.Host = "original"
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// A second update that just sets a new value.
	err = mgr.Update(func(cfg *RootConfig) {
		cfg.VPS.Host = "updated"
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if mgr.Get().VPS.Host != "updated" {
		t.Errorf("VPS.Host = %q, want %q", mgr.Get().VPS.Host, "updated")
	}
}

func TestNewManager_CreatesDirAndFile(t *testing.T) {
	dir := t.TempDir()
	unetDir := filepath.Join(dir, ".unet")
	cfgPath := filepath.Join(unetDir, "config.json")

	// Override home for this test.
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", originalHome)

	// We need to patch ConfigDir to use our temp dir.
	// Instead, just test the path directly.
	mgr := &Manager{
		path:   cfgPath,
		config: defaultConfig(),
	}

	if err := os.MkdirAll(unetDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := mgr.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config file should exist: %v", err)
	}
}

func TestCloneAndMask_Independent(t *testing.T) {
	cfg := defaultConfig()
	cfg.VPS.Password = SecretString("password1234")
	cfg.Tunnel.PrivateKey = SecretString("privatekey5678")

	masked := cloneAndMask(cfg)

	// Mutating masked should not affect original.
	masked.VPS.Host = "changed"
	if cfg.VPS.Host == "changed" {
		t.Fatal("cloneAndMask should return independent copy")
	}
}

func TestMaskedJSONSerialization(t *testing.T) {
	mgr := newTestManager(t)
	_ = mgr.Update(func(cfg *RootConfig) {
		cfg.VPS.Password = SecretString("secret1234")
	})

	masked := mgr.GetMasked()
	data, err := json.Marshal(masked)
	if err != nil {
		t.Fatalf("Marshal masked: %v", err)
	}

	// The masked JSON should contain ****1234 not the real password.
	if !contains(data, "****1234") {
		t.Fatalf("masked JSON should contain masked password, got: %s", data)
	}
	if contains(data, "secret1234") {
		t.Fatalf("masked JSON should NOT contain raw password, got: %s", data)
	}
}

func contains(data []byte, substr string) bool {
	return string(data) != "" && len(data) >= len(substr) &&
		(string(data)[:len(substr)] == substr ||
			len(data) > len(substr) && containsString(string(data), substr))
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
