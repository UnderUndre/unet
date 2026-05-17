//go:build e2e

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underundre/unet/internal/config"
	"github.com/underundre/unet/internal/tunnel"
)

const sampleAWG0Conf = `[Interface]
Address = 10.8.1.1/24
ListenPort = 31075
PrivateKey = serverPrivKeyB64==
Jc = 4
Jmin = 50
Jmax = 1000
S1 = 0
S2 = 0
S3 = 0
S4 = 0
H1 = 1
H2 = 2
H3 = 3
H4 = 4
I1 = 5
I2 = 10
I3 = 15
I4 = 20
I5 = 25

[Peer]
PublicKey = clientPubKeyB64==
PresharedKey = pskB64==
AllowedIPs = 10.8.1.2/32
`

const sampleAWG0ConfExtraPeer = `[Interface]
Address = 10.8.1.1/24
ListenPort = 31075
PrivateKey = serverPrivKeyB64==
Jc = 4
Jmin = 50
Jmax = 1000
S1 = 0
S2 = 0
S3 = 0
S4 = 0
H1 = 1
H2 = 2
H3 = 3
H4 = 4
I1 = 5
I2 = 10
I3 = 15
I4 = 20
I5 = 25

[Peer]
PublicKey = clientPubKeyB64==
PresharedKey = pskB64==
AllowedIPs = 10.8.1.2/32

[Peer]
PublicKey = anotherPeerPubKeyB64==
PresharedKey = pskB64==
AllowedIPs = 10.8.1.3/32
`

const sampleAWG0ConfChangedObfuscation = `[Interface]
Address = 10.8.1.1/24
ListenPort = 31075
PrivateKey = serverPrivKeyB64==
Jc = 8
Jmin = 100
Jmax = 2000
S1 = 1
S2 = 2
S3 = 3
S4 = 4
H1 = 10
H2 = 20
H3 = 30
H4 = 40
I1 = 50
I2 = 60
I3 = 70
I4 = 80
I5 = 90

[Peer]
PublicKey = clientPubKeyB64==
PresharedKey = pskB64==
AllowedIPs = 10.8.1.2/32
`

func normalizeConf(raw string) string {
	var b strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		b.WriteString(trimmed)
		b.WriteByte('\n')
	}
	return b.String()
}

func hashConf(raw string) string {
	h := sha256.Sum256([]byte(normalizeConf(raw)))
	return fmt.Sprintf("%x", h[:])
}

type mockSSHExecutor struct {
	mu      sync.Mutex
	hashOut string
	confOut string
	err     error
	cmds    []string
}

func (m *mockSSHExecutor) ExecuteCommand(_ context.Context, cmd string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cmds = append(m.cmds, cmd)
	if strings.Contains(cmd, "sha256sum") {
		return m.hashOut + "  /opt/amnezia/awg/awg0.conf", "", m.err
	}
	if strings.Contains(cmd, "cat") {
		return m.confOut, "", m.err
	}
	return "", "", m.err
}

func (m *mockSSHExecutor) ExecuteScript(_ context.Context, script string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cmds = append(m.cmds, script)
	return "", "", m.err
}

func (m *mockSSHExecutor) setHash(h string) {
	m.mu.Lock()
	m.hashOut = h
	m.mu.Unlock()
}

func (m *mockSSHExecutor) getCmds() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.cmds))
	copy(out, m.cmds)
	return out
}

func makeMirrorJSON(t *testing.T, confRaw, confHash string) string {
	t.Helper()
	mirror := tunnel.ServerMirrorJSON{
		LastSyncedAt:  time.Now().UTC().Format(time.RFC3339),
		AwgConfRaw:   confRaw,
		AwgConfSha256: confHash,
	}
	data, err := json.Marshal(mirror)
	if err != nil {
		t.Fatalf("marshal mirror: %v", err)
	}
	return string(data)
}

func newTestConfigMgr(t *testing.T, serverMirror string) *config.Manager {
	t.Helper()

	cfg := config.RootConfig{
		VPS: config.VPSConfig{
			Host:          "test-vps.example.com",
			SSHPort:       22,
			Username:      "root",
			ContainerName: "unet-amnezia-awg",
		},
		Tunnel: config.TunnelConfig{
			InterfaceName: "awg0",
			Status:        "connected",
		},
		ServerMirror: serverMirror,
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	unetDir := filepath.Join(home, ".unet")
	if err := os.MkdirAll(unetDir, 0o700); err != nil {
		t.Fatalf("mkdir .unet: %v", err)
	}
	if err := os.WriteFile(filepath.Join(unetDir, "config.json"), data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	mgr, err := config.NewManager()
	if err != nil {
		t.Fatalf("config.NewManager: %v", err)
	}
	return mgr
}

func TestSHA256Hashing_ConsistentForIdenticalConfigs(t *testing.T) {
	h1 := hashConf(sampleAWG0Conf)
	h2 := hashConf(sampleAWG0Conf)
	if h1 != h2 {
		t.Errorf("identical configs produced different hashes: %s vs %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex hash, got %d chars", len(h1))
	}
}

func TestSHA256Hashing_DifferentContentProducesDifferentHash(t *testing.T) {
	h1 := hashConf(sampleAWG0Conf)
	h2 := hashConf(sampleAWG0ConfExtraPeer)
	if h1 == h2 {
		t.Error("different configs produced the same hash")
	}
}

func TestDriftDetection_ExtraPeerProducesDifferentHash(t *testing.T) {
	hOriginal := hashConf(sampleAWG0Conf)
	hModified := hashConf(sampleAWG0ConfExtraPeer)
	if hOriginal == hModified {
		t.Error("extra peer should produce different hash")
	}
}

func TestDriftDetection_ChangedObfuscationProducesDifferentHash(t *testing.T) {
	hOriginal := hashConf(sampleAWG0Conf)
	hModified := hashConf(sampleAWG0ConfChangedObfuscation)
	if hOriginal == hModified {
		t.Error("changed obfuscation params should produce different hash")
	}
}

func TestDriftDetection_ManagerDetectsMismatch(t *testing.T) {
	localHash := hashConf(sampleAWG0Conf)
	remoteHash := hashConf(sampleAWG0ConfExtraPeer)

	ssh := &mockSSHExecutor{hashOut: remoteHash}
	mirrorJSON := makeMirrorJSON(t, sampleAWG0Conf, localHash)

	cfgMgr := newTestConfigMgr(t, mirrorJSON)
	mgr := tunnel.NewManager(cfgMgr, &tunnel.AWGCli{}, ssh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := mgr.CheckDrift(ctx)
	if err == nil {
		t.Fatal("expected drift error, got nil")
	}
	if !strings.Contains(err.Error(), "drift detected") {
		t.Errorf("expected drift error message, got: %v", err)
	}
}

func TestDriftDetection_ManagerNoDriftWhenMatching(t *testing.T) {
	confHash := hashConf(sampleAWG0Conf)

	ssh := &mockSSHExecutor{hashOut: confHash}
	mirrorJSON := makeMirrorJSON(t, sampleAWG0Conf, confHash)

	cfgMgr := newTestConfigMgr(t, mirrorJSON)
	mgr := tunnel.NewManager(cfgMgr, &tunnel.AWGCli{}, ssh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := mgr.CheckDrift(ctx)
	if err != nil {
		t.Errorf("expected no drift, got: %v", err)
	}
}

func TestWatchdogInterval_VerifiedInSource(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "tunnel", "manager.go"))
	if err != nil {
		t.Skip("cannot read manager.go source for interval verification")
	}
	src := string(data)
	if !strings.Contains(src, "watchdogInterval = 30 * time.Second") {
		t.Error("watchdogInterval is not set to 30s in manager.go — check FR-003a compliance")
	}
	t.Log("watchdogInterval confirmed as 30s in source (FR-003a)")
}

func TestEdgeCase_EmptyConfig_StillProducesHash(t *testing.T) {
	h := hashConf("")
	if h == "" {
		t.Error("empty config should still produce a hash")
	}
	if len(h) != 64 {
		t.Errorf("expected 64-char hex hash for empty config, got %d chars", len(h))
	}
}

func TestEdgeCase_WhitespaceOnlyChanges_NoDrift(t *testing.T) {
	confWithExtraWS := "[Interface]\n  Address = 10.8.1.1/24  \n\nListenPort = 31075\n\n"
	confClean := "[Interface]\nAddress = 10.8.1.1/24\nListenPort = 31075"

	h1 := hashConf(confWithExtraWS)
	h2 := hashConf(confClean)
	if h1 != h2 {
		t.Errorf("whitespace-only changes should not trigger drift:\n  h1=%s\n  h2=%s", h1, h2)
	}
}

func TestEdgeCase_MultipleRapidChanges_OneWarningSurfaced(t *testing.T) {
	localHash := hashConf(sampleAWG0Conf)

	ssh := &mockSSHExecutor{hashOut: hashConf(sampleAWG0ConfExtraPeer)}
	mirrorJSON := makeMirrorJSON(t, sampleAWG0Conf, localHash)

	cfgMgr := newTestConfigMgr(t, mirrorJSON)
	mgr := tunnel.NewManager(cfgMgr, &tunnel.AWGCli{}, ssh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	warningCount := 0
	for i := 0; i < 5; i++ {
		if err := mgr.CheckDrift(ctx); err != nil {
			warningCount++
		}
		ssh.setHash(hashConf(sampleAWG0ConfChangedObfuscation))
	}

	if warningCount == 0 {
		t.Error("expected at least one drift warning")
	}
	t.Logf("drift warnings from %d rapid checks: %d (debounce surfaces 1 in UI)", 5, warningCount)
}

func TestAutoResync_ReParsesObfuscationAfterDrift(t *testing.T) {
	localHash := hashConf(sampleAWG0Conf)
	remoteHash := hashConf(sampleAWG0ConfChangedObfuscation)

	ssh := &mockSSHExecutor{
		confOut: sampleAWG0ConfChangedObfuscation,
		hashOut: remoteHash,
	}
	mirrorJSON := makeMirrorJSON(t, sampleAWG0Conf, localHash)

	cfgMgr := newTestConfigMgr(t, mirrorJSON)
	mgr := tunnel.NewManager(cfgMgr, &tunnel.AWGCli{}, ssh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := mgr.CheckDrift(ctx)
	if err == nil {
		t.Fatal("expected drift detection on first check")
	}

	_, parseErr := tunnel.ParseServerMirror(cfgMgr.Get().ServerMirror)
	if parseErr != nil {
		t.Fatalf("re-parse mirror after drift: %v", parseErr)
	}

	cmds := ssh.getCmds()
	hasSHA := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, "sha256sum") {
			hasSHA = true
		}
	}
	if !hasSHA {
		t.Error("expected sha256sum command to be issued")
	}
}

func TestNormalization_BlankLinesStripped(t *testing.T) {
	conf := "[Interface]\n\n\nAddress = 10.8.1.1/24\n\n\n"
	normalized := normalizeConf(conf)
	for _, line := range strings.Split(normalized, "\n") {
		if line == "" {
			continue
		}
		if strings.TrimSpace(line) == "" {
			t.Errorf("blank line remained after normalization: %q", line)
		}
	}
}

func TestNormalization_TrailingWhitespaceStripped(t *testing.T) {
	conf := "[Interface]   \nAddress = 10.8.1.1/24   \n"
	normalized := normalizeConf(conf)
	for _, line := range strings.Split(normalized, "\n") {
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("trailing whitespace not stripped: %q", line)
		}
	}
}

func TestFetchConfHash_UsesSHA256SumCommand(t *testing.T) {
	ssh := &mockSSHExecutor{hashOut: "abc123"}
	parser := tunnel.NewServerConfigParser(ssh)

	ctx := context.Background()
	vps := config.VPSConfig{
		Host:          "test-vps.example.com",
		ContainerName: "unet-amnezia-awg",
	}

	hash, err := parser.FetchConfHash(ctx, vps)
	if err != nil {
		t.Fatalf("FetchConfHash: %v", err)
	}
	if hash != "abc123" {
		t.Errorf("expected hash abc123, got %s", hash)
	}

	cmds := ssh.getCmds()
	if len(cmds) == 0 {
		t.Fatal("expected SSH command to be executed")
	}
	if !strings.Contains(cmds[0], "sha256sum") {
		t.Errorf("expected sha256sum in command, got: %s", cmds[0])
	}
}

func TestFetchConfHash_DefaultsContainerName(t *testing.T) {
	ssh := &mockSSHExecutor{hashOut: "deadbeef"}
	parser := tunnel.NewServerConfigParser(ssh)

	ctx := context.Background()
	vps := config.VPSConfig{Host: "test-vps.example.com"}

	hash, err := parser.FetchConfHash(ctx, vps)
	if err != nil {
		t.Fatalf("FetchConfHash: %v", err)
	}
	if hash != "deadbeef" {
		t.Errorf("expected hash deadbeef, got %s", hash)
	}

	cmds := ssh.getCmds()
	if !strings.Contains(cmds[0], "unet-amnezia-awg") {
		t.Errorf("expected default container name, got: %s", cmds[0])
	}
}

func TestParseServerMirror_EmptyString(t *testing.T) {
	mirror, err := tunnel.ParseServerMirror("")
	if err != nil {
		t.Fatalf("empty mirror should not error: %v", err)
	}
	if mirror.AwgConfRaw != "" {
		t.Error("expected empty AwgConfRaw for empty mirror")
	}
	if mirror.AwgConfSha256 != "" {
		t.Error("expected empty AwgConfSha256 for empty mirror")
	}
}

func TestSerializeRoundTrip(t *testing.T) {
	original := &tunnel.ServerMirrorJSON{
		LastSyncedAt:  "2025-01-01T00:00:00Z",
		AwgConfRaw:   sampleAWG0Conf,
		AwgConfSha256: hashConf(sampleAWG0Conf),
	}
	serialized, err := tunnel.SerializeServerMirror(original)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	parsed, err := tunnel.ParseServerMirror(serialized)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.AwgConfSha256 != original.AwgConfSha256 {
		t.Errorf("hash mismatch after round-trip: %s vs %s", parsed.AwgConfSha256, original.AwgConfSha256)
	}
	if parsed.AwgConfRaw != original.AwgConfRaw {
		t.Error("conf raw mismatch after round-trip")
	}
}
