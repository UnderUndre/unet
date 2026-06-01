package qr

import (
	"encoding/base64"
	"strings"
	"testing"
)

const sampleConfig = `[Interface]
PrivateKey = abc123def456
Address = 10.0.0.2/32
DNS = 1.1.1.1

[Peer]
PublicKey = xyz789uvw012
Endpoint = 203.0.113.1:51820
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25`

func TestGenerateQR(t *testing.T) {
	t.Parallel()

	result, err := Generate(sampleConfig, 256)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if result == nil {
		t.Fatal("result must not be nil")
	}
	if len(result.PNG) == 0 {
		t.Error("PNG bytes must not be empty")
	}
	if len(result.PNG) < 100 {
		t.Errorf("PNG seems too small: %d bytes", len(result.PNG))
	}
	if !strings.HasPrefix(result.DeeplinkURI, "wireguard://import?config=") {
		t.Errorf("DeeplinkURI = %q, expected wireguard:// prefix", result.DeeplinkURI)
	}
	if result.ConfigText != sampleConfig {
		t.Error("ConfigText should match input")
	}
}

func TestGenerateQR_EmptyConfig(t *testing.T) {
	t.Parallel()

	_, err := Generate("", 256)
	if err == nil {
		t.Fatal("Generate with empty config should return error")
	}
}

func TestBuildDeeplink(t *testing.T) {
	t.Parallel()

	link, err := BuildDeeplink(sampleConfig)
	if err != nil {
		t.Fatalf("BuildDeeplink failed: %v", err)
	}

	if !strings.HasPrefix(link, "wireguard://import?config=") {
		t.Errorf("link = %q, expected wireguard://import?config= prefix", link)
	}

	encoded := strings.TrimPrefix(link, "wireguard://import?config=")
	decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(encoded)
	if err != nil {
		t.Fatalf("failed to decode base64url: %v", err)
	}

	if string(decoded) != sampleConfig {
		t.Errorf("decoded config mismatch:\ngot:  %q\nwant: %q", string(decoded), sampleConfig)
	}
}

func TestBuildDeeplink_NoPadding(t *testing.T) {
	t.Parallel()

	link, err := BuildDeeplink(sampleConfig)
	if err != nil {
		t.Fatalf("BuildDeeplink failed: %v", err)
	}

	encoded := strings.TrimPrefix(link, "wireguard://import?config=")

	if strings.Contains(encoded, "=") {
		t.Errorf("base64url encoded value should not contain = padding, got: %s", encoded)
	}
}

func TestDeeplinkRoundTrip(t *testing.T) {
	t.Parallel()

	link, err := BuildDeeplink(sampleConfig)
	if err != nil {
		t.Fatalf("BuildDeeplink failed: %v", err)
	}

	encoded := strings.TrimPrefix(link, "wireguard://import?config=")
	decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64url decode failed: %v", err)
	}

	if string(decoded) != sampleConfig {
		t.Errorf("round-trip failed:\ngot:  %q\nwant: %q", string(decoded), sampleConfig)
	}
}
