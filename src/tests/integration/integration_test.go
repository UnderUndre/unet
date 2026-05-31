package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/underundre/unet/internal/api/remote"
	"github.com/underundre/unet/internal/api/v1"
	"github.com/underundre/unet/internal/audit"
	"github.com/underundre/unet/internal/auth"
)

type mockTunnel struct {
	status      string
	connected   bool
	localIP     string
	serverIP    string
	endpoint    string
	connectedAt string
}

func (m *mockTunnel) Status() string              { return m.status }
func (m *mockTunnel) IsConnected() bool           { return m.connected }
func (m *mockTunnel) GetConfig() *v1.TunnelConfigView {
	return &v1.TunnelConfigView{
		LocalIP:        m.localIP,
		ServerIP:       m.serverIP,
		ServerEndpoint: m.endpoint,
		Status:         m.status,
		ConnectedAt:    m.connectedAt,
	}
}

type mockPeers struct {
	peers []v1.PeerView
}

func (m *mockPeers) List(ctx context.Context) ([]v1.PeerView, error) {
	return m.peers, nil
}
func (m *mockPeers) GetByID(ctx context.Context, id string) (*v1.PeerDetailView, error) {
	for _, p := range m.peers {
		if p.ID == id {
			return &v1.PeerDetailView{PeerView: p}, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (m *mockPeers) Create(ctx context.Context, name string) (*v1.PeerDetailView, error) {
	p := v1.PeerView{ID: "new-" + name, Name: name, CreatedVia: "api", CreatedAt: time.Now().Format(time.RFC3339)}
	m.peers = append(m.peers, p)
	return &v1.PeerDetailView{PeerView: p}, nil
}
func (m *mockPeers) Delete(ctx context.Context, id string) error {
	for i, p := range m.peers {
		if p.ID == id {
			m.peers = append(m.peers[:i], m.peers[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

type mockRoutes struct {
	routes []v1.RouteView
}

func (m *mockRoutes) List(ctx context.Context) ([]v1.RouteView, error) {
	return m.routes, nil
}
func (m *mockRoutes) Create(ctx context.Context, subdomain string, port int) (*v1.RouteView, error) {
	r := v1.RouteView{ID: "route-" + subdomain, Subdomain: subdomain, LocalPort: port, Status: "active", CreatedAt: time.Now().Format(time.RFC3339)}
	m.routes = append(m.routes, r)
	return &r, nil
}
func (m *mockRoutes) Delete(ctx context.Context, id string) error {
	for i, r := range m.routes {
		if r.ID == id {
			m.routes = append(m.routes[:i], m.routes[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

type testEnv struct {
	handler  http.Handler
	store    *auth.Store
	cache    *auth.TokenCache
	issuer   *auth.JWTIssuer
	auditLog *audit.Logger
	auditDir string
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	dir := t.TempDir()
	store := auth.NewStore(filepath.Join(dir, "config.json"))
	cache := auth.NewTokenCache(store, 5*time.Minute)

	key, _ := auth.GenerateJWTSigningKey()
	issuer, _ := auth.NewJWTIssuer(key)

	auditPath := filepath.Join(dir, "audit.jsonl")
	auditLog, _ := audit.NewLogger(auditPath)
	t.Cleanup(func() { auditLog.Close() })

	tun := &mockTunnel{status: "connected", connected: true, localIP: "10.8.1.1", serverIP: "10.8.1.1", endpoint: "vpn.example.com:51820", connectedAt: time.Now().Format(time.RFC3339)}
	peers := &mockPeers{peers: []v1.PeerView{}}
	routes := &mockRoutes{routes: []v1.RouteView{}}

	handler := remote.RegisterRoutes(&remote.Dependencies{
		TokenStore: store,
		TokenCache: cache,
		JWTIssuer:  issuer,
		AuditLog:   auditLog,
		AuditPath:  auditPath,
		Peers:      peers,
		Routes:     routes,
		Tunnel:     tun,
	})

	return &testEnv{
		handler:  handler,
		store:    store,
		cache:    cache,
		issuer:   issuer,
		auditLog: auditLog,
		auditDir: dir,
	}
}

func (e *testEnv) do(req *http.Request) *http.Response {
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec.Result()
}

func newRemoteRequest(method, target string, body []byte) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, target, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	r.RemoteAddr = "192.168.1.100:12345"
	return r
}

func newLocalhostRequest(method, target string, body []byte) *http.Request {
	r := newRemoteRequest(method, target, body)
	r.RemoteAddr = "127.0.0.1:12345"
	return r
}

func createAdminToken(t *testing.T, store *auth.Store) string {
	t.Helper()
	token, plain, err := auth.NewAPIToken("test-admin", auth.ScopeAdmin, "system", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(token); err != nil {
		t.Fatal(err)
	}
	return plain
}

func TestAuthFlow_PATLifecycle(t *testing.T) {
	env := setupTestEnv(t)
	plain := createAdminToken(t, env.store)

	req := newRemoteRequest("GET", "/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+plain)

	resp := env.do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var tokens []map[string]any
	json.NewDecoder(resp.Body).Decode(&tokens)
	if len(tokens) != 1 {
		t.Errorf("expected 1 token, got %d", len(tokens))
	}
	if tokens[0]["name"] != "test-admin" {
		t.Errorf("expected name test-admin, got %v", tokens[0]["name"])
	}
}

func TestAuthFlow_JWTExchange(t *testing.T) {
	env := setupTestEnv(t)
	plain := createAdminToken(t, env.store)

	body, _ := json.Marshal(map[string]string{})
	req := newRemoteRequest("POST", "/v1/auth/session", body)
	req.Header.Set("Authorization", "Bearer "+plain)
	req.Header.Set("Content-Type", "application/json")

	resp := env.do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var session map[string]any
	json.NewDecoder(resp.Body).Decode(&session)

	jwtToken, ok := session["token"].(string)
	if !ok || jwtToken == "" {
		t.Fatal("expected JWT token in response")
	}

	claims, err := env.issuer.Validate(jwtToken)
	if err != nil {
		t.Fatalf("JWT validation failed: %v", err)
	}
	if claims.Scope != auth.ScopeAdmin {
		t.Errorf("expected admin scope, got %s", claims.Scope)
	}
}

func TestScopeEnforcement(t *testing.T) {
	env := setupTestEnv(t)

	token, plain, _ := auth.NewAPIToken("reader", auth.ScopeRead, "system", nil)
	env.store.Create(token)

	req := newRemoteRequest("GET", "/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+plain)

	resp := env.do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for read scope on admin endpoint, got %d", resp.StatusCode)
	}
}

func TestNoAuth_ExternalIP(t *testing.T) {
	env := setupTestEnv(t)

	req := newRemoteRequest("GET", "/v1/peers", nil)

	resp := env.do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth from external IP, got %d", resp.StatusCode)
	}
}

func TestLoopbackBypass(t *testing.T) {
	env := setupTestEnv(t)

	req := newLocalhostRequest("GET", "/v1/peers", nil)

	resp := env.do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 from loopback (auto-admin), got %d", resp.StatusCode)
	}
}

func TestTokenCRUD(t *testing.T) {
	env := setupTestEnv(t)
	plain := createAdminToken(t, env.store)

	body, _ := json.Marshal(map[string]string{"name": "ci-pipeline", "scope": "write"})
	req := newRemoteRequest("POST", "/v1/tokens", body)
	req.Header.Set("Authorization", "Bearer "+plain)
	req.Header.Set("Content-Type", "application/json")

	resp := env.do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var createResp map[string]any
	json.NewDecoder(resp.Body).Decode(&createResp)

	if createResp["plainToken"] == nil {
		t.Error("plainToken should be returned on creation")
	}
	if createResp["name"] != "ci-pipeline" {
		t.Errorf("expected name ci-pipeline, got %v", createResp["name"])
	}

	newPlain, _ := createResp["plainToken"].(string)

	req2 := newRemoteRequest("GET", "/v1/status", nil)
	req2.Header.Set("Authorization", "Bearer "+newPlain)

	resp2 := env.do(req2)
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("new token should work, got %d", resp2.StatusCode)
	}
}

func TestGetTunnelStatus(t *testing.T) {
	env := setupTestEnv(t)
	plain := createAdminToken(t, env.store)

	req := newRemoteRequest("GET", "/v1/tunnel/status", nil)
	req.Header.Set("Authorization", "Bearer "+plain)

	resp := env.do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var status map[string]any
	json.NewDecoder(resp.Body).Decode(&status)

	if status["status"] != "connected" {
		t.Errorf("expected connected, got %v", status["status"])
	}
	if status["localIp"] != "10.8.1.1" {
		t.Errorf("expected localIp, got %v", status["localIp"])
	}
}

func TestGetSystemStatus(t *testing.T) {
	env := setupTestEnv(t)
	plain := createAdminToken(t, env.store)

	req := newRemoteRequest("GET", "/v1/status", nil)
	req.Header.Set("Authorization", "Bearer "+plain)

	resp := env.do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var status map[string]any
	json.NewDecoder(resp.Body).Decode(&status)

	if status["apiVersion"] != "2026-05-27" {
		t.Errorf("expected apiVersion, got %v", status["apiVersion"])
	}
}

func TestRouteCRUD(t *testing.T) {
	env := setupTestEnv(t)
	plain := createAdminToken(t, env.store)

	body, _ := json.Marshal(map[string]any{"subdomain": "app", "localPort": 3000})
	req := newRemoteRequest("POST", "/v1/routes", body)
	req.Header.Set("Authorization", "Bearer "+plain)
	req.Header.Set("Content-Type", "application/json")

	resp := env.do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	if created["subdomain"] != "app" {
		t.Errorf("expected subdomain app, got %v", created["subdomain"])
	}
}

func TestPeerCRUD(t *testing.T) {
	env := setupTestEnv(t)
	plain := createAdminToken(t, env.store)

	body, _ := json.Marshal(map[string]string{"name": "laptop"})
	req := newRemoteRequest("POST", "/v1/peers", body)
	req.Header.Set("Authorization", "Bearer "+plain)
	req.Header.Set("Content-Type", "application/json")

	resp := env.do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	if created["name"] != "laptop" {
		t.Errorf("expected name laptop, got %v", created["name"])
	}
}
