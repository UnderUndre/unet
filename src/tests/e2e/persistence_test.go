//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/underundre/unet/internal/config"
	"github.com/underundre/unet/internal/provisioner"
	"github.com/underundre/unet/internal/tunnel"
)

const (
	volumeAWGState  = "amnezia-awg-state"
	volumeCaddyData = "caddy-data"
	volumeCaddyConf = "caddy-config"
	awgMountPath    = "/opt/amnezia/awg"
)

func TestComposeGeneratesNamedVolumes(t *testing.T) {
	compose, err := provisioner.GenerateCompose(provisioner.ComposeConfig{
		AWGPort:    31075,
		ManualDNS:  false,
		CaddyImage: "caddy:2-alpine",
	})
	if err != nil {
		t.Fatalf("GenerateCompose() error: %v", err)
	}
	body := string(compose)

	for _, vol := range []string{volumeAWGState, volumeCaddyData, volumeCaddyConf} {
		if !strings.Contains(body, vol+":") {
			t.Errorf("compose missing named volume declaration for %q\nfull output:\n%s", vol, body)
		}
	}

	if !strings.Contains(body, "volumes:") {
		t.Error("compose missing top-level volumes section")
	}

	lines := strings.Split(body, "\n")
	inVolumes := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "volumes:" {
			inVolumes = true
			continue
		}
		if inVolumes && trimmed != "" && !strings.HasPrefix(trimmed, "amnezia-") && !strings.HasPrefix(trimmed, "caddy-") {
			t.Errorf("unexpected non-volume entry in volumes section: %q", trimmed)
		}
	}
}

func TestComposeVolumePathsNotBindMounts(t *testing.T) {
	compose, err := provisioner.GenerateCompose(provisioner.ComposeConfig{
		AWGPort:    31075,
		ManualDNS:  true,
		CaddyImage: "caddy:2-alpine",
	})
	if err != nil {
		t.Fatalf("GenerateCompose() error: %v", err)
	}
	body := string(compose)

	for _, bind := range []string{"./", "/opt/amnezia/awg:/", "/data:/", "/config:/"} {
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "- ") && strings.Contains(trimmed, bind) {
				if strings.Contains(trimed, "/lib/modules") {
					continue
				}
				t.Errorf("found bind mount pattern %q in line: %s", bind, line)
			}
		}
	}

	awgVolumeLine := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, awgMountPath) {
			awgVolumeLine = line
			break
		}
	}
	if awgVolumeLine == "" {
		t.Fatal("compose missing AWG state volume mount")
	}
	if !strings.Contains(awgVolumeLine, volumeAWGState+":"+awgMountPath) {
		t.Errorf("AWG volume should be named volume %s:%s, got: %s", volumeAWGState, awgMountPath, awgVolumeLine)
	}
}

func TestPeerAddTargetsNamedVolumePath(t *testing.T) {
	mock := newMockSSHExecutor()
	mock.OnCommand("cat >> /opt/amnezia/awg/awg0.conf", "", "", nil)
	mock.OnCommand("awg-quick strip", "", "", nil)
	mock.OnCommand("awg show awg0 peers", "dGVzdGNsaWVudHB1YmxpY2tleQ==", "", nil)
	mock.OnCommand("cat /opt/amnezia/awg/clientsTable", "[]", "", nil)
	mock.OnCommand("cat > /opt/amnezia/awg/clientsTable.new", "", "", nil)
	mock.OnCommand("mv /opt/amnezia/awg/clientsTable.new", "", "", nil)

	pm := tunnel.NewPeerManager(mock)

	_, err := pm.AddPeer(context.Background(), tunnel.AddPeerParams{
		VPS: config.VPSConfig{
			Host:          "test-vps",
			ContainerName: "unet-amnezia-awg",
		},
		Interface:          "awg0",
		ClientPublicKey:    "dGVzdGNsaWVudHB1YmxpY2tleQ==",
		ClientIP:           "10.8.1.2",
		PresharedKey:       "dGVzdHBzaw==",
		ClientName:         "test-peer",
		PersistentKeepalive: 25,
	})
	if err != nil {
		t.Fatalf("AddPeer() error: %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()

	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "awg0.conf") && !strings.Contains(cmd, awgMountPath) {
			t.Errorf("peer command targets path outside named volume: %s", cmd)
		}
		if strings.Contains(cmd, "clientsTable") && !strings.Contains(cmd, awgMountPath) {
			t.Errorf("clientsTable command targets path outside named volume: %s", cmd)
		}
	}
}

func TestConfigFilePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	original := config.RootConfig{
		VPS: config.VPSConfig{
			Host:          "persist-test.vps",
			SSHPort:       2222,
			Username:      "root",
			AuthMode:      "key",
			IsProvisioned: true,
		},
		Tunnel: config.TunnelConfig{
			InterfaceName:       "awg0",
			Subnet:              "10.8.1.0/24",
			ServerIP:            "10.8.1.1",
			LocalIP:             "10.8.1.2",
			ServerEndpoint:      "persist-test.vps:31075",
			ServerPublicKey:     "c2VydmVycHVibGlja2V5",
			PresharedKey:        config.SecretString("cHJlc2hhcmVka2V5"),
			PrivateKey:          config.SecretString("cHJpdmF0ZWtleQ=="),
			PublicKey:           "Y2xpZW50cHVibGlja2V5",
			MTU:                 1280,
			PersistentKeepalive: 25,
			Status:              "connected",
			ConnectedAt:         "2026-01-15T10:30:00Z",
		},
		CaddyAPI: config.CaddyAPIConfig{
			Address:    "https://10.8.1.1:2019",
			TLSCert:    "cert-data",
			TLSKey:     config.SecretString("tls-key-data"),
			ClientCert: "client-cert",
			ClientKey:  config.SecretString("client-key-data"),
		},
		ExposedPorts: []config.ExposedPort{
			{Protocol: "tcp", Internal: 8080, HostHeader: "app.example.com"},
		},
		DNS: config.DNSConfig{
			Provider: "cloudflare",
			Token:    config.SecretString("cf-api-token"),
			Zone:     "example.com",
		},
		ServerMirror: `{"lastSyncedAt":"2026-01-15T10:30:00Z","awgConfSha256":"abc123"}`,
		UIToken:      config.SecretString("test-ui-token-12345"),
	}

	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatalf("write error: %v", err)
	}

	readData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}

	var restored config.RootConfig
	if err := json.Unmarshal(readData, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.VPS.Host != original.VPS.Host {
		t.Errorf("VPS.Host: got %q, want %q", restored.VPS.Host, original.VPS.Host)
	}
	if restored.VPS.SSHPort != original.VPS.SSHPort {
		t.Errorf("VPS.SSHPort: got %d, want %d", restored.VPS.SSHPort, original.VPS.SSHPort)
	}
	if restored.Tunnel.InterfaceName != original.Tunnel.InterfaceName {
		t.Errorf("Tunnel.InterfaceName: got %q, want %q", restored.Tunnel.InterfaceName, original.Tunnel.InterfaceName)
	}
	if restored.Tunnel.PresharedKey != original.Tunnel.PresharedKey {
		t.Errorf("Tunnel.PresharedKey: got %q, want %q", restored.Tunnel.PresharedKey, original.Tunnel.PresharedKey)
	}
	if restored.Tunnel.PrivateKey.Plain() != original.Tunnel.PrivateKey.Plain() {
		t.Errorf("Tunnel.PrivateKey mismatch")
	}
	if restored.Tunnel.ServerEndpoint != original.Tunnel.ServerEndpoint {
		t.Errorf("Tunnel.ServerEndpoint: got %q, want %q", restored.Tunnel.ServerEndpoint, original.Tunnel.ServerEndpoint)
	}
	if restored.Tunnel.MTU != original.Tunnel.MTU {
		t.Errorf("Tunnel.MTU: got %d, want %d", restored.Tunnel.MTU, original.Tunnel.MTU)
	}
	if restored.CaddyAPI.Address != original.CaddyAPI.Address {
		t.Errorf("CaddyAPI.Address: got %q, want %q", restored.CaddyAPI.Address, original.CaddyAPI.Address)
	}
	if len(restored.ExposedPorts) != len(original.ExposedPorts) {
		t.Fatalf("ExposedPorts length: got %d, want %d", len(restored.ExposedPorts), len(original.ExposedPorts))
	}
	if restored.ExposedPorts[0].HostHeader != original.ExposedPorts[0].HostHeader {
		t.Errorf("ExposedPorts[0].HostHeader: got %q, want %q", restored.ExposedPorts[0].HostHeader, original.ExposedPorts[0].HostHeader)
	}
	if restored.DNS.Provider != original.DNS.Provider {
		t.Errorf("DNS.Provider: got %q, want %q", restored.DNS.Provider, original.DNS.Provider)
	}
	if restored.DNS.Token.Plain() != original.DNS.Token.Plain() {
		t.Errorf("DNS.Token mismatch")
	}
	if restored.ServerMirror != original.ServerMirror {
		t.Errorf("ServerMirror: got %q, want %q", restored.ServerMirror, original.ServerMirror)
	}
	if restored.UIToken.Plain() != original.UIToken.Plain() {
		t.Errorf("UIToken mismatch")
	}
}

func TestConfigSurvivesRestart(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	cfg := config.RootConfig{
		VPS: config.VPSConfig{
			Host:          "restart-test.vps",
			SSHPort:       22,
			Username:      "root",
			IsProvisioned: true,
		},
		Tunnel: config.TunnelConfig{
			Status:         "connected",
			ServerEndpoint: "restart-test.vps:31075",
			LocalIP:        "10.8.1.3",
		},
		ExposedPorts: []config.ExposedPort{
			{Protocol: "tcp", Internal: 3000},
		},
		UIToken: config.SecretString("restart-ui-token"),
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("first marshal error: %v", err)
	}
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatalf("first write error: %v", err)
	}

	firstWrite, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("first read error: %v", err)
	}

	var afterFirst config.RootConfig
	if err := json.Unmarshal(firstWrite, &afterFirst); err != nil {
		t.Fatalf("first unmarshal error: %v", err)
	}

	secondData, err := json.MarshalIndent(afterFirst, "", "  ")
	if err != nil {
		t.Fatalf("second marshal error: %v", err)
	}
	if err := os.WriteFile(cfgPath, secondData, 0o600); err != nil {
		t.Fatalf("second write (simulated restart) error: %v", err)
	}

	secondWrite, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("second read error: %v", err)
	}

	var afterRestart config.RootConfig
	if err := json.Unmarshal(secondWrite, &afterRestart); err != nil {
		t.Fatalf("second unmarshal error: %v", err)
	}

	if afterRestart.VPS.Host != cfg.VPS.Host {
		t.Errorf("after restart VPS.Host: got %q, want %q", afterRestart.VPS.Host, cfg.VPS.Host)
	}
	if afterRestart.Tunnel.Status != cfg.Tunnel.Status {
		t.Errorf("after restart Tunnel.Status: got %q, want %q", afterRestart.Tunnel.Status, cfg.Tunnel.Status)
	}
	if afterRestart.Tunnel.LocalIP != cfg.Tunnel.LocalIP {
		t.Errorf("after restart Tunnel.LocalIP: got %q, want %q", afterRestart.Tunnel.LocalIP, cfg.Tunnel.LocalIP)
	}
	if len(afterRestart.ExposedPorts) != len(cfg.ExposedPorts) {
		t.Errorf("after restart ExposedPorts: got %d, want %d", len(afterRestart.ExposedPorts), len(cfg.ExposedPorts))
	}
	if afterRestart.UIToken.Plain() != cfg.UIToken.Plain() {
		t.Errorf("after restart UIToken mismatch")
	}

	if string(firstWrite) != string(secondWrite) {
		t.Errorf("config data not stable across write/read cycle\nfirst:\n%s\nsecond:\n%s", firstWrite, secondWrite)
	}
}

func TestAWGServerConfigReReadAfterRestart(t *testing.T) {
	conf := sampleAWGConf()

	mock := newMockSSHExecutor()
	mock.OnCommand("/opt/amnezia/awg/awg0.conf", conf, "", nil)
	mock.OnCommand("wireguard_server_public_key.key", "c2VydmVycHVibGlja2V5", "", nil)
	mock.OnCommand("wireguard_psk.key", "dGVzdHBzaw==", "", nil)
	mock.OnCommand("awg show awg0 dump", samplePeerDump(), "", nil)

	vps := config.VPSConfig{
		Host:          "reread-test.vps",
		ContainerName: "unet-amnezia-awg",
	}

	parser := tunnel.NewServerConfigParser(mock)
	first, err := parser.FetchAll(context.Background(), vps, "awg0")
	if err != nil {
		t.Fatalf("first FetchAll() error: %v", err)
	}

	mock2 := newMockSSHExecutor()
	mock2.OnCommand("/opt/amnezia/awg/awg0.conf", conf, "", nil)
	mock2.OnCommand("wireguard_server_public_key.key", "c2VydmVycHVibGlja2V5", "", nil)
	mock2.OnCommand("wireguard_psk.key", "dGVzdHBzaw==", "", nil)
	mock2.OnCommand("awg show awg0 dump", samplePeerDump(), "", nil)

	parser2 := tunnel.NewServerConfigParser(mock2)
	second, err := parser2.FetchAll(context.Background(), vps, "awg0")
	if err != nil {
		t.Fatalf("second FetchAll() (after restart) error: %v", err)
	}

	if first.Address != second.Address {
		t.Errorf("Address mismatch: first=%q second=%q", first.Address, second.Address)
	}
	if first.ListenPort != second.ListenPort {
		t.Errorf("ListenPort mismatch: first=%d second=%d", first.ListenPort, second.ListenPort)
	}
	if first.ServerPublicKey != second.ServerPublicKey {
		t.Errorf("ServerPublicKey mismatch: first=%q second=%q", first.ServerPublicKey, second.ServerPublicKey)
	}
	if first.PresharedKey != second.PresharedKey {
		t.Errorf("PresharedKey mismatch: first=%q second=%q", first.PresharedKey, second.PresharedKey)
	}
	if first.RawConf != second.RawConf {
		t.Errorf("RawConf mismatch: first length=%d second length=%d", len(first.RawConf), len(second.RawConf))
	}
	if first.ConfSha256 != second.ConfSha256 {
		t.Errorf("ConfSha256 mismatch: first=%q second=%q", first.ConfSha256, second.ConfSha256)
	}
	if len(first.Peers) != len(second.Peers) {
		t.Errorf("Peers count mismatch: first=%d second=%d", len(first.Peers), len(second.Peers))
	}
}

func TestServerMirrorRoundTrip(t *testing.T) {
	original := tunnel.ServerMirrorJSON{
		LastSyncedAt:     "2026-01-15T10:30:00Z",
		AwgConfRaw:       sampleAWGConf(),
		AwgConfSha256:    "abc123def456789",
		ClientsTable:     json.RawMessage(`[{"name":"laptop","publicKey":"dGVzdA==","ip":"10.8.1.2"}]`),
		CaddyAdminConfig: json.RawMessage(`{"address":"https://10.8.1.1:2019"}`),
		ServerPrivateKey: "c2VydmVycHJpdmF0ZWtleQ==",
	}

	serialized, err := tunnel.SerializeServerMirror(&original)
	if err != nil {
		t.Fatalf("SerializeServerMirror() error: %v", err)
	}

	parsed, err := tunnel.ParseServerMirror(serialized)
	if err != nil {
		t.Fatalf("ParseServerMirror() error: %v", err)
	}

	if parsed.LastSyncedAt != original.LastSyncedAt {
		t.Errorf("LastSyncedAt: got %q, want %q", parsed.LastSyncedAt, original.LastSyncedAt)
	}
	if parsed.AwgConfRaw != original.AwgConfRaw {
		t.Errorf("AwgConfRaw mismatch: got length %d, want %d", len(parsed.AwgConfRaw), len(original.AwgConfRaw))
	}
	if parsed.AwgConfSha256 != original.AwgConfSha256 {
		t.Errorf("AwgConfSha256: got %q, want %q", parsed.AwgConfSha256, original.AwgConfSha256)
	}
	if parsed.ServerPrivateKey != original.ServerPrivateKey {
		t.Errorf("ServerPrivateKey: got %q, want %q", parsed.ServerPrivateKey, original.ServerPrivateKey)
	}

	var origCT, parsedCT []map[string]interface{}
	if err := json.Unmarshal(original.ClientsTable, &origCT); err != nil {
		t.Fatalf("unmarshal original clientsTable: %v", err)
	}
	if err := json.Unmarshal(parsed.ClientsTable, &parsedCT); err != nil {
		t.Fatalf("unmarshal parsed clientsTable: %v", err)
	}
	if len(origCT) != len(parsedCT) {
		t.Fatalf("clientsTable length: got %d, want %d", len(parsedCT), len(origCT))
	}
	if origCT[0]["name"] != parsedCT[0]["name"] {
		t.Errorf("clientsTable[0].name: got %v, want %v", parsedCT[0]["name"], origCT[0]["name"])
	}

	reSerialized, err := tunnel.SerializeServerMirror(parsed)
	if err != nil {
		t.Fatalf("re-SerializeServerMirror() error: %v", err)
	}
	if serialized != reSerialized {
		t.Errorf("mirror not stable across serialize/parse cycle\nfirst:\n%s\nsecond:\n%s", serialized, reSerialized)
	}
}

func sampleAWGConf() string {
	return fmt.Sprintf(`[Interface]
Address = 10.8.1.1/24
ListenPort = 31075
PrivateKey = c2VydmVycHJpdmF0ZWtleQ==
Jc = 4
Jmin = 50
Jmax = 1000
S1 = 0
S2 = 0
S3 = 0
H1 = 1
H2 = 2
H3 = 3
H4 = 4

[Peer]
PublicKey = dGVzdGNsaWVudHB1YmxpY2tleQ==
PresharedKey = dGVzdHBzaw==
AllowedIPs = 10.8.1.2/32
`)
}

func samplePeerDump() string {
	return "awg0\tc2VydmVycHJpdmF0ZWtleQ==\t\tdGVzdHBzaw==\t10.8.1.1/24\t31075\t0\t0\t0\toff\n" +
		"dGVzdGNsaWVudHB1YmxpY2tleQ==\tdGVzdHBzaw==\t1.2.3.4:12345\t10.8.1.2/32\t2026-01-15 10:30:00\t1024\t2048\t25\toff\n"
}
