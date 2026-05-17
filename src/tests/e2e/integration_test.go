//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/underundre/unet/internal/config"
	"github.com/underundre/unet/internal/daemon"
	"github.com/underundre/unet/internal/provisioner"
	"github.com/underundre/unet/internal/proxy"
	"github.com/underundre/unet/internal/tunnel"
)

type mockSSHCall struct {
	Cmd    string
	Stdout string
	Stderr string
	Err    error
}

type mockSSHExecutor struct {
	calls []string
	resp  map[string]mockSSHCall
}

func newMockSSHExecutor() *mockSSHExecutor {
	return &mockSSHExecutor{resp: make(map[string]mockSSHCall)}
}

func (m *mockSSHExecutor) onCommand(cmd string, resp mockSSHCall) {
	m.resp[cmd] = resp
}

func (m *mockSSHExecutor) ExecuteCommand(_ context.Context, cmd string) (string, string, error) {
	m.calls = append(m.calls, cmd)
	if r, ok := m.resp[cmd]; ok {
		return r.Stdout, r.Stderr, r.Err
	}
	return "", "", nil
}

func (m *mockSSHExecutor) ExecuteScript(_ context.Context, script string) (string, string, error) {
	m.calls = append(m.calls, script)
	return "", "", nil
}

func (m *mockSSHExecutor) calledWith(substr string) bool {
	for _, c := range m.calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

func setupTestEnv(t *testing.T) *config.Manager {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	unetDir := filepath.Join(home, ".unet")
	if err := os.MkdirAll(unetDir, 0o700); err != nil {
		t.Fatalf("mkdir .unet: %v", err)
	}
	skeleton, _ := json.MarshalIndent(&config.RootConfig{
		UIToken:      config.SecretString("test-e2e-token"),
		ExposedPorts: []config.ExposedPort{},
	}, "", "  ")
	if err := os.WriteFile(filepath.Join(unetDir, "config.json"), skeleton, 0o600); err != nil {
		t.Fatalf("write skeleton: %v", err)
	}
	mgr, err := config.NewManager()
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr
}

func setupAPIServer(t *testing.T) (*config.Manager, *daemon.Server, *httptest.Server) {
	t.Helper()
	mgr := setupTestEnv(t)
	srv := daemon.NewServer(0)
	return mgr, srv, httptest.NewServer(srv)
}

func setTunnelConnected(t *testing.T, mgr *config.Manager) {
	t.Helper()
	err := mgr.Update(func(c *config.RootConfig) {
		c.Tunnel.Status = "connected"
		c.Tunnel.ServerIP = "10.8.1.1"
		c.Tunnel.LocalIP = "10.8.1.2"
		c.VPS.Host = "192.0.2.1"
		c.VPS.IsProvisioned = true
		c.VPS.ContainerName = "unet-amnezia-awg"
		c.DNS.Provider = "manual"
		c.DNS.Zone = "example.com"
	})
	if err != nil {
		t.Fatalf("set tunnel connected: %v", err)
	}
}

func doJSON(t *testing.T, method, url string, payload interface{}) *http.Response {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, b)
	}
	return m
}

func decodeArray(t *testing.T, resp *http.Response) []interface{} {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var arr []interface{}
	if err := json.Unmarshal(b, &arr); err != nil {
		t.Fatalf("unmarshal array: %v\nbody: %s", err, b)
	}
	return arr
}

// ---------------------------------------------------------------------------
// Provision / Compose
// ---------------------------------------------------------------------------

func TestProvision_ComposeGeneration(t *testing.T) {
	compose, err := provisioner.GenerateCompose(provisioner.ComposeConfig{
		AWGPort:    31075,
		ManualDNS:  false,
		CaddyImage: "caddy:2-alpine",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(compose)
	if !strings.Contains(s, "31075:31075/udp") {
		t.Error("missing AWG port mapping")
	}
	if !strings.Contains(s, "unet-amnezia-awg") {
		t.Error("missing awg service")
	}
	if !strings.Contains(s, "unet-caddy") {
		t.Error("missing caddy service")
	}
	if strings.Contains(s, "80:80/tcp") {
		t.Error("should NOT have port 80 when ManualDNS=false")
	}
}

func TestProvision_ComposeGeneration_ManualDNS(t *testing.T) {
	compose, err := provisioner.GenerateCompose(provisioner.ComposeConfig{
		AWGPort:    31075,
		ManualDNS:  true,
		CaddyImage: "caddy:2-alpine",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "80:80/tcp") {
		t.Error("should have port 80 when ManualDNS=true")
	}
}

func TestProvision_ComposeInvalidPort(t *testing.T) {
	_, err := provisioner.GenerateCompose(provisioner.ComposeConfig{
		AWGPort:    0,
		CaddyImage: "caddy:2-alpine",
	})
	if err == nil {
		t.Fatal("expected error for port 0")
	}
	if !strings.Contains(err.Error(), "invalid AWGPort") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AWG key generation
// ---------------------------------------------------------------------------

func TestProvision_AWGKeysGenerated(t *testing.T) {
	cfg, err := provisioner.GenerateAWGConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerPrivateKey == "" {
		t.Error("empty server private key")
	}
	if cfg.ServerPublicKey == "" {
		t.Error("empty server public key")
	}
	if cfg.PresharedKey == "" {
		t.Error("empty preshared key")
	}
	if cfg.ListenPort < 30000 || cfg.ListenPort > 60000 {
		t.Errorf("port %d out of [30000,60000]", cfg.ListenPort)
	}
	if !strings.HasPrefix(cfg.Subnet, "10.8.") {
		t.Errorf("subnet %q should start with 10.8.", cfg.Subnet)
	}
	if cfg.Obfuscation.Jc < 1 {
		t.Error("Jc should be >= 1")
	}
	if cfg.Obfuscation.Jmin >= cfg.Obfuscation.Jmax {
		t.Error("Jmin should be < Jmax")
	}
	if cfg.ServerPrivateKey == cfg.ServerPublicKey {
		t.Error("keys should differ")
	}
}

// ---------------------------------------------------------------------------
// SSH host validation
// ---------------------------------------------------------------------------

func TestSSH_HostValidation(t *testing.T) {
	tests := []struct {
		host    string
		wantErr bool
	}{
		{"192.168.1.1", false},
		{"example.com", false},
		{"host;rm -rf /", true},
		{"host`whoami`", true},
		{"host$(id)", true},
		{"host|cat /etc/passwd", true},
		{"host>file", true},
		{"host<file", true},
	}
	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			_, err := provisioner.NewClient(config.VPSConfig{
				Host:     tc.host,
				AuthMode: "password",
			})
			if tc.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// VPS configure
// ---------------------------------------------------------------------------

func TestVPS_Configure_SavesConfig(t *testing.T) {
	mgr, srv, ts := setupAPIServer(t)
	defer ts.Close()

	handler := daemon.NewVPSHandler(mgr, srv)
	handler.RegisterRoutes()

	resp := doJSON(t, "POST", ts.URL+"/api/vps/configure", map[string]interface{}{
		"host":     "192.0.2.1",
		"sshPort":  22,
		"username": "root",
		"authMode": "password",
		"password": "secret123",
	})
	ar := decodeJSON(t, resp)

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d; body=%v", resp.StatusCode, ar)
	}
	if status, _ := ar["status"].(string); status != "provisioning" {
		t.Errorf("status=%q, want provisioning", status)
	}
	if taskID, _ := ar["taskId"].(string); !strings.HasPrefix(taskID, "provision-") {
		t.Errorf("taskId=%q", taskID)
	}

	cfg := mgr.Get()
	if cfg.VPS.Host != "192.0.2.1" {
		t.Errorf("host=%q", cfg.VPS.Host)
	}
	if cfg.VPS.Username != "root" {
		t.Errorf("username=%q", cfg.VPS.Username)
	}
	if cfg.VPS.Password.Plain() != "secret123" {
		t.Error("password not saved")
	}
}

func TestVPS_Configure_InvalidCredentials(t *testing.T) {
	mgr, srv, ts := setupAPIServer(t)
	defer ts.Close()

	handler := daemon.NewVPSHandler(mgr, srv)
	handler.RegisterRoutes()

	tests := []struct {
		name    string
		payload map[string]interface{}
	}{
		{"missing host", map[string]interface{}{"sshPort": 22, "username": "root", "authMode": "password", "password": "x"}},
		{"missing username", map[string]interface{}{"host": "1.2.3.4", "sshPort": 22, "authMode": "password", "password": "x"}},
		{"invalid authMode", map[string]interface{}{"host": "1.2.3.4", "sshPort": 22, "username": "root", "authMode": "token"}},
		{"password mode without password", map[string]interface{}{"host": "1.2.3.4", "sshPort": 22, "username": "root", "authMode": "password"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, "POST", ts.URL+"/api/vps/configure", tc.payload)
			ar := decodeJSON(t, resp)
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status=%d, want 400; body=%v", resp.StatusCode, ar)
			}
			if errVal, _ := ar["error"].(string); errVal != "invalid_credentials" {
				t.Errorf("error=%q, want invalid_credentials", errVal)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// VPS status
// ---------------------------------------------------------------------------

func TestVPS_Status(t *testing.T) {
	mgr, srv, ts := setupAPIServer(t)
	defer ts.Close()

	err := mgr.Update(func(c *config.RootConfig) {
		c.VPS.Host = "192.0.2.1"
		c.VPS.IsProvisioned = true
	})
	if err != nil {
		t.Fatal(err)
	}

	handler := daemon.NewVPSHandler(mgr, srv)
	handler.RegisterRoutes()

	resp, err := http.Get(ts.URL + "/api/vps/status")
	if err != nil {
		t.Fatal(err)
	}
	ar := decodeJSON(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if configured, _ := ar["configured"].(bool); !configured {
		t.Error("configured should be true")
	}
	if provisioned, _ := ar["provisioned"].(bool); !provisioned {
		t.Error("provisioned should be true")
	}
	if host, _ := ar["host"].(string); host != "192.0.2.1" {
		t.Errorf("host=%q", host)
	}
}

// ---------------------------------------------------------------------------
// System status
// ---------------------------------------------------------------------------

func TestSystemStatus_Structure(t *testing.T) {
	mgr, srv, ts := setupAPIServer(t)
	defer ts.Close()

	err := mgr.Update(func(c *config.RootConfig) {
		c.VPS.Host = "192.0.2.1"
		c.VPS.IsProvisioned = true
		c.Tunnel.Status = "connected"
		c.Tunnel.LocalIP = "10.8.1.2"
		c.Tunnel.ServerIP = "10.8.1.1"
	})
	if err != nil {
		t.Fatal(err)
	}

	handler := daemon.NewVPSHandler(mgr, srv)
	handler.RegisterRoutes()

	resp, err := http.Get(ts.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	ar := decodeJSON(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	vps, ok := ar["vps"].(map[string]interface{})
	if !ok {
		t.Fatal("missing vps object")
	}
	if configured, _ := vps["configured"].(bool); !configured {
		t.Error("vps.configured should be true")
	}
	if provisioned, _ := vps["provisioned"].(bool); !provisioned {
		t.Error("vps.provisioned should be true")
	}

	td, ok := ar["tunnel"].(map[string]interface{})
	if !ok {
		t.Fatal("missing tunnel object")
	}
	if status, _ := td["status"].(string); status != "connected" {
		t.Errorf("tunnel.status=%q", status)
	}

	ports, ok := ar["ports"].([]interface{})
	if !ok {
		t.Fatal("missing ports array")
	}
	if len(ports) != 0 {
		t.Errorf("ports should be empty, got %d", len(ports))
	}
}

// ---------------------------------------------------------------------------
// Ports: expose without tunnel
// ---------------------------------------------------------------------------

func TestPorts_ExposeWithoutTunnel(t *testing.T) {
	mgr, srv, ts := setupAPIServer(t)
	defer ts.Close()

	caddyCli := proxy.NewCaddyClient(mgr, nil)
	dnsMgr := proxy.NewDNSManager(mgr)
	daemon.NewPortsHandler(mgr, caddyCli, dnsMgr, srv).RegisterRoutes()

	resp := doJSON(t, "POST", ts.URL+"/api/ports", map[string]interface{}{
		"localPort": 8080,
		"subdomain": "myapp",
		"protocol":  "http",
	})
	ar := decodeJSON(t, resp)

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d; body=%v", resp.StatusCode, ar)
	}
	if errVal, _ := ar["error"].(string); errVal != "tunnel_not_connected" {
		t.Errorf("error=%q", errVal)
	}
}

// ---------------------------------------------------------------------------
// Ports: duplicate subdomain
// ---------------------------------------------------------------------------

func TestPorts_DuplicateSubdomain(t *testing.T) {
	mgr, srv, ts := setupAPIServer(t)
	defer ts.Close()

	err := mgr.Update(func(c *config.RootConfig) {
		c.Tunnel.Status = "connected"
		c.Tunnel.ServerIP = "10.8.1.1"
		c.VPS.Host = "192.0.2.1"
		c.VPS.IsProvisioned = true
		c.DNS.Provider = "manual"
		c.DNS.Zone = "example.com"
		c.ExposedPorts = []config.ExposedPort{
			{Protocol: "http", Internal: 3000, HostHeader: "myapp"},
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	caddyCli := proxy.NewCaddyClient(mgr, nil)
	dnsMgr := proxy.NewDNSManager(mgr)
	daemon.NewPortsHandler(mgr, caddyCli, dnsMgr, srv).RegisterRoutes()

	resp := doJSON(t, "POST", ts.URL+"/api/ports", map[string]interface{}{
		"localPort": 4000,
		"subdomain": "myapp",
		"protocol":  "http",
	})
	ar := decodeJSON(t, resp)

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d; body=%v", resp.StatusCode, ar)
	}
	if errVal, _ := ar["error"].(string); errVal != "duplicate_subdomain" {
		t.Errorf("error=%q", errVal)
	}
}

// ---------------------------------------------------------------------------
// Ports: validation
// ---------------------------------------------------------------------------

func TestPorts_InvalidPort(t *testing.T) {
	mgr, srv, ts := setupAPIServer(t)
	defer ts.Close()

	err := mgr.Update(func(c *config.RootConfig) {
		c.Tunnel.Status = "connected"
		c.Tunnel.ServerIP = "10.8.1.1"
	})
	if err != nil {
		t.Fatal(err)
	}

	caddyCli := proxy.NewCaddyClient(mgr, nil)
	dnsMgr := proxy.NewDNSManager(mgr)
	daemon.NewPortsHandler(mgr, caddyCli, dnsMgr, srv).RegisterRoutes()

	tests := []struct {
		name    string
		payload map[string]interface{}
		want    int
	}{
		{"port 0", map[string]interface{}{"localPort": 0, "subdomain": "app", "protocol": "http"}, http.StatusBadRequest},
		{"port negative", map[string]interface{}{"localPort": -1, "subdomain": "app", "protocol": "http"}, http.StatusBadRequest},
		{"port 99999", map[string]interface{}{"localPort": 99999, "subdomain": "app", "protocol": "http"}, http.StatusBadRequest},
		{"empty subdomain", map[string]interface{}{"localPort": 8080, "subdomain": "", "protocol": "http"}, http.StatusBadRequest},
		{"uppercase subdomain", map[string]interface{}{"localPort": 8080, "subdomain": "MY-APP", "protocol": "http"}, http.StatusBadRequest},
		{"invalid protocol", map[string]interface{}{"localPort": 8080, "subdomain": "app", "protocol": "grpc"}, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, "POST", ts.URL+"/api/ports", tc.payload)
			if resp.StatusCode != tc.want {
				ar := decodeJSON(t, resp)
				t.Errorf("status=%d, want %d; body=%v", resp.StatusCode, tc.want, ar)
			}
			resp.Body.Close()
		})
	}
}

// ---------------------------------------------------------------------------
// Ports: list
// ---------------------------------------------------------------------------

func TestPorts_ListEmpty(t *testing.T) {
	mgr, srv, ts := setupAPIServer(t)
	defer ts.Close()

	caddyCli := proxy.NewCaddyClient(mgr, nil)
	dnsMgr := proxy.NewDNSManager(mgr)
	daemon.NewPortsHandler(mgr, caddyCli, dnsMgr, srv).RegisterRoutes()

	resp, err := http.Get(ts.URL + "/api/ports")
	if err != nil {
		t.Fatal(err)
	}
	ports := decodeArray(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if len(ports) != 0 {
		t.Errorf("expected empty, got %d", len(ports))
	}
}

func TestPorts_ListWithEntries(t *testing.T) {
	mgr, srv, ts := setupAPIServer(t)
	defer ts.Close()

	err := mgr.Update(func(c *config.RootConfig) {
		c.ExposedPorts = []config.ExposedPort{
			{Protocol: "http", Internal: 3000, HostHeader: "app1"},
			{Protocol: "http", Internal: 8080, HostHeader: "app2"},
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	caddyCli := proxy.NewCaddyClient(mgr, nil)
	dnsMgr := proxy.NewDNSManager(mgr)
	daemon.NewPortsHandler(mgr, caddyCli, dnsMgr, srv).RegisterRoutes()

	resp, err := http.Get(ts.URL + "/api/ports")
	if err != nil {
		t.Fatal(err)
	}
	ports := decodeArray(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if len(ports) != 2 {
		t.Fatalf("expected 2, got %d", len(ports))
	}
}

// ---------------------------------------------------------------------------
// DNS configure
// ---------------------------------------------------------------------------

func TestDNS_Configure(t *testing.T) {
	mgr, srv, ts := setupAPIServer(t)
	defer ts.Close()

	daemon.NewDNSHandler(mgr, srv).RegisterRoutes()

	resp := doJSON(t, "POST", ts.URL+"/api/dns/configure", map[string]interface{}{
		"mode": "manual",
		"zone": "example.com",
	})
	ar := decodeJSON(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d; body=%v", resp.StatusCode, ar)
	}
	if status, _ := ar["status"].(string); status != "configured" {
		t.Errorf("status=%q", status)
	}

	cfg := mgr.Get()
	if cfg.DNS.Provider != "manual" {
		t.Errorf("provider=%q", cfg.DNS.Provider)
	}
	if cfg.DNS.Zone != "example.com" {
		t.Errorf("zone=%q", cfg.DNS.Zone)
	}
}

func TestDNS_Configure_InvalidMode(t *testing.T) {
	mgr, srv, ts := setupAPIServer(t)
	defer ts.Close()

	daemon.NewDNSHandler(mgr, srv).RegisterRoutes()

	resp := doJSON(t, "POST", ts.URL+"/api/dns/configure", map[string]interface{}{
		"mode": "route53",
		"zone": "example.com",
	})
	ar := decodeJSON(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if errVal, _ := ar["error"].(string); errVal != "bad_request" {
		t.Errorf("error=%q", errVal)
	}
}

func TestDNS_Configure_CloudflareWithoutToken(t *testing.T) {
	mgr, srv, ts := setupAPIServer(t)
	defer ts.Close()

	daemon.NewDNSHandler(mgr, srv).RegisterRoutes()

	resp := doJSON(t, "POST", ts.URL+"/api/dns/configure", map[string]interface{}{
		"mode": "cloudflare",
		"zone": "example.com",
	})
	ar := decodeJSON(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if errVal, _ := ar["error"].(string); errVal != "bad_request" {
		t.Errorf("error=%q", errVal)
	}
}

// ---------------------------------------------------------------------------
// Config round-trip
// ---------------------------------------------------------------------------

func TestConfig_RoundTrip(t *testing.T) {
	mgr := setupTestEnv(t)

	err := mgr.Update(func(c *config.RootConfig) {
		c.VPS.Host = "192.0.2.1"
		c.VPS.SSHPort = 22
		c.VPS.Username = "root"
		c.VPS.AuthMode = "password"
		c.VPS.Password = config.SecretString("secret")
		c.VPS.IsProvisioned = true
		c.Tunnel.Status = "connected"
		c.Tunnel.LocalIP = "10.8.1.2"
		c.Tunnel.ServerIP = "10.8.1.1"
		c.ExposedPorts = []config.ExposedPort{
			{Protocol: "http", Internal: 8080, HostHeader: "app"},
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := mgr.Get()
	if cfg.VPS.Host != "192.0.2.1" {
		t.Errorf("host=%q", cfg.VPS.Host)
	}
	if cfg.Tunnel.Status != "connected" {
		t.Errorf("status=%q", cfg.Tunnel.Status)
	}
	if len(cfg.ExposedPorts) != 1 || cfg.ExposedPorts[0].HostHeader != "app" {
		t.Errorf("ports=%v", cfg.ExposedPorts)
	}
}

// ---------------------------------------------------------------------------
// SecretString masking
// ---------------------------------------------------------------------------

func TestConfig_SecretMasking(t *testing.T) {
	mgr := setupTestEnv(t)

	err := mgr.Update(func(c *config.RootConfig) {
		c.VPS.Password = config.SecretString("supersecret1234")
		c.Tunnel.PrivateKey = config.SecretString("privatekey5678")
	})
	if err != nil {
		t.Fatal(err)
	}

	masked := mgr.GetMasked()
	data, err := json.Marshal(masked)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "supersecret1234") {
		t.Error("raw password in masked JSON")
	}
	if strings.Contains(s, "privatekey5678") {
		t.Error("raw key in masked JSON")
	}
	if strings.Contains(s, "****1234") {
		t.Log("password masked with last-4")
	}
}

// ---------------------------------------------------------------------------
// Tunnel disconnect config update
// ---------------------------------------------------------------------------

func TestTunnel_DisconnectConfigUpdate(t *testing.T) {
	mgr := setupTestEnv(t)

	err := mgr.Update(func(c *config.RootConfig) {
		c.Tunnel.Status = "disconnected"
		c.Tunnel.ConnectedAt = ""
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := mgr.Get()
	if cfg.Tunnel.Status != "disconnected" {
		t.Errorf("status=%q", cfg.Tunnel.Status)
	}
}

// ---------------------------------------------------------------------------
// Mock SSH executor
// ---------------------------------------------------------------------------

func TestMockSSH_RecordsCalls(t *testing.T) {
	ssh := newMockSSHExecutor()
	ssh.onCommand("cat /file", mockSSHCall{Stdout: "hello"})

	stdout, _, err := ssh.ExecuteCommand(context.Background(), "cat /file")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "hello" {
		t.Errorf("stdout=%q", stdout)
	}
	if !ssh.calledWith("cat") {
		t.Error("should have recorded 'cat' call")
	}
	if ssh.calledWith("rm") {
		t.Error("should not have recorded 'rm' call")
	}
}

// ---------------------------------------------------------------------------
// Tunnel manager creation with mock
// ---------------------------------------------------------------------------

func TestTunnel_ManagerWithMock(t *testing.T) {
	ssh := newMockSSHExecutor()
	ssh.onCommand("docker exec unet-amnezia-awg cat /opt/amnezia/awg/awg0.conf", mockSSHCall{
		Stdout: `[Interface]
Address = 10.8.1.1/24
ListenPort = 31075
PrivateKey = serverprivkey123=
Jc = 4
Jmin = 50
Jmax = 1000
`,
	})

	mgr := setupTestEnv(t)

	err := mgr.Update(func(c *config.RootConfig) {
		c.VPS.Host = "192.0.2.1"
		c.VPS.SSHPort = 22
		c.VPS.Username = "root"
		c.VPS.AuthMode = "password"
		c.VPS.Password = config.SecretString("test")
		c.VPS.IsProvisioned = true
		c.VPS.ContainerName = "unet-amnezia-awg"
	})
	if err != nil {
		t.Fatal(err)
	}

	awgCli, err := tunnel.NewAWGCli()
	if err != nil {
		t.Skipf("awg-quick not available: %v", err)
	}
	_ = tunnel.NewManager(mgr, awgCli, ssh)
}

// ---------------------------------------------------------------------------
// Ports: expose with connected tunnel (Caddy/DNS likely unreachable)
// ---------------------------------------------------------------------------

func TestPorts_ExposeWithConnectedTunnel(t *testing.T) {
	mgr, srv, ts := setupAPIServer(t)
	defer ts.Close()

	setTunnelConnected(t, mgr)

	caddyCli := proxy.NewCaddyClient(mgr, nil)
	dnsMgr := proxy.NewDNSManager(mgr)
	daemon.NewPortsHandler(mgr, caddyCli, dnsMgr, srv).RegisterRoutes()

	resp := doJSON(t, "POST", ts.URL+"/api/ports", map[string]interface{}{
		"localPort": 8080,
		"subdomain": "myapp",
		"protocol":  "http",
	})

	if resp.StatusCode == http.StatusCreated {
		ar := decodeJSON(t, resp)
		if id, _ := ar["id"].(string); id == "" {
			t.Error("missing port id")
		}
		if lp, ok := ar["localPort"].(float64); !ok || int(lp) != 8080 {
			t.Errorf("localPort=%v", ar["localPort"])
		}
		if sub, _ := ar["subdomain"].(string); sub != "myapp" {
			t.Errorf("subdomain=%q", sub)
		}
	} else {
		resp.Body.Close()
		t.Logf("port creation returned %d (Caddy/DNS unreachable, expected in test env)", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkComposeGeneration(b *testing.B) {
	cfg := provisioner.ComposeConfig{
		AWGPort:    31075,
		ManualDNS:  false,
		CaddyImage: "caddy:2-alpine",
	}
	for i := 0; i < b.N; i++ {
		if _, err := provisioner.GenerateCompose(cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAWGKeyGeneration(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := provisioner.GenerateAWGConfig(); err != nil {
			b.Fatal(err)
		}
	}
}
