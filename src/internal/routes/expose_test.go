package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/underundre/unet/internal/config"
)

type mockDNS struct {
	upsertErr error
	deleteErr error
	upsertCalled bool
	deleteCalled bool
	lastSubdomain string
	lastIP string
}

func (m *mockDNS) UpsertRecord(_ context.Context, subdomain, ip string) error {
	m.upsertCalled = true
	m.lastSubdomain = subdomain
	m.lastIP = ip
	return m.upsertErr
}

func (m *mockDNS) DeleteRecord(_ context.Context, subdomain string) error {
	m.deleteCalled = true
	m.lastSubdomain = subdomain
	return m.deleteErr
}

type mockTunnel struct {
	connected bool
}

func (m *mockTunnel) IsConnected() bool { return m.connected }

func newTestHandler(t *testing.T, tunnelConnected bool) (*Handler, *mockDNS, *mockTunnel) {
	t.Helper()

	_ = t.TempDir()
	mgr, err := config.NewManager()
	if err != nil {
		t.Skipf("cannot create config manager: %v", err)
	}

	dns := &mockDNS{}
	tunnel := &mockTunnel{connected: tunnelConnected}
	h := NewHandler(mgr, dns, tunnel)
	h.SetVPSPublicIP("1.2.3.4")
	return h, dns, tunnel
}

func TestBuildNipioFQDN(t *testing.T) {
	t.Parallel()

	got := BuildNipioFQDN("app", "1.2.3.4")
	want := "app.1-2-3-4.nip.io"
	if got != want {
		t.Errorf("BuildNipioFQDN(%q, %q) = %q, want %q", "app", "1.2.3.4", got, want)
	}
}

func TestBuildNipioFQDNEmptyLabel(t *testing.T) {
	t.Parallel()

	got := BuildNipioFQDN("", "10.0.0.1")
	want := "10-0-0-1.nip.io"
	if got != want {
		t.Errorf("BuildNipioFQDN(%q, %q) = %q, want %q", "", "10.0.0.1", got, want)
	}
}

func TestGenerateSubdomainEmpty(t *testing.T) {
	t.Parallel()

	got := GenerateSubdomain("")
	if !strings.HasPrefix(got, "svc-") {
		t.Errorf("GenerateSubdomain('') = %q, want prefix 'svc-'", got)
	}

	suffix := strings.TrimPrefix(got, "svc-")
	matched, _ := regexp.MatchString(`^[a-z0-9]{4}$`, suffix)
	if !matched {
		t.Errorf("GenerateSubdomain('') suffix = %q, want 4 alphanumeric chars", suffix)
	}
}

func TestGenerateSubdomainWithHint(t *testing.T) {
	t.Parallel()

	got := GenerateSubdomain("myapp")
	if got != "myapp" {
		t.Errorf("GenerateSubdomain('myapp') = %q, want 'myapp'", got)
	}
}

func TestHandleExpose_TunnelNotConnected(t *testing.T) {
	t.Parallel()

	h, _, _ := newTestHandler(t, false)

	body := `{"local_port": 8080}`
	req := httptest.NewRequest(http.MethodPost, "/v1/routes/expose", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleExpose(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusPreconditionFailed)
	}

	var errResp map[string]any
	json.NewDecoder(resp.Body).Decode(&errResp)
	if errResp["error"] != "tunnel_not_connected" {
		t.Errorf("error = %v, want tunnel_not_connected", errResp["error"])
	}
}

func TestHandleExpose_InvalidPort(t *testing.T) {
	t.Parallel()

	h, _, _ := newTestHandler(t, true)

	body := `{"local_port": 0}`
	req := httptest.NewRequest(http.MethodPost, "/v1/routes/expose", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleExpose(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleExpose_Success(t *testing.T) {
	t.Parallel()

	h, dns, _ := newTestHandler(t, true)

	body := `{"local_port": 3000, "subdomain": "myapp"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/routes/expose", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleExpose(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusCreated, w.Body.String())
	}

	var result ExposeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Route.Subdomain != "myapp" {
		t.Errorf("subdomain = %q, want 'myapp'", result.Route.Subdomain)
	}
	if result.Route.LocalPort != 3000 {
		t.Errorf("local_port = %d, want 3000", result.Route.LocalPort)
	}
	if result.Route.FQDN != "myapp.1-2-3-4.nip.io" {
		t.Errorf("fqdn = %q, want 'myapp.1-2-3-4.nip.io'", result.Route.FQDN)
	}
	if result.URL != "https://myapp.1-2-3-4.nip.io" {
		t.Errorf("url = %q, want 'https://myapp.1-2-3-4.nip.io'", result.URL)
	}
	if !dns.upsertCalled {
		t.Error("DNS upsert was not called")
	}
	if dns.lastIP != "1.2.3.4" {
		t.Errorf("DNS upsert IP = %q, want '1.2.3.4'", dns.lastIP)
	}
}

func TestHandleExpose_AutoGenerateSubdomain(t *testing.T) {
	t.Parallel()

	h, _, _ := newTestHandler(t, true)

	body := `{"local_port": 8080}`
	req := httptest.NewRequest(http.MethodPost, "/v1/routes/expose", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleExpose(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	var result ExposeResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if !strings.HasPrefix(result.Route.Subdomain, "svc-") {
		t.Errorf("auto-generated subdomain = %q, want 'svc-' prefix", result.Route.Subdomain)
	}
}

func TestHandleExpose_ConflictDetection(t *testing.T) {
	t.Parallel()

	h, _, _ := newTestHandler(t, true)

	body1 := `{"local_port": 3000, "subdomain": "taken"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/routes/expose", strings.NewReader(body1))
	w1 := httptest.NewRecorder()
	h.HandleExpose(w1, req1)

	if w1.Result().StatusCode != http.StatusCreated {
		t.Fatalf("first request: status = %d, want %d", w1.Result().StatusCode, http.StatusCreated)
	}

	body2 := `{"local_port": 4000, "subdomain": "taken"}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/routes/expose", strings.NewReader(body2))
	w2 := httptest.NewRecorder()
	h.HandleExpose(w2, req2)

	resp := w2.Result()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("conflict request: status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}

	var errResp map[string]any
	json.NewDecoder(resp.Body).Decode(&errResp)

	if errResp["error"] != "route_conflict" {
		t.Errorf("error = %v, want route_conflict", errResp["error"])
	}

	ctx, _ := errResp["context"].(map[string]any)
	suggestions, _ := ctx["suggestions"].([]any)
	if len(suggestions) < 1 {
		t.Error("expected suggestions in conflict response")
	}
}

func TestHandleExpose_AtomicRollback(t *testing.T) {
	t.Parallel()

	h, dns, _ := newTestHandler(t, true)

	dns.upsertErr = fmt.Errorf("DNS provider timeout")

	body := `{"local_port": 5000, "subdomain": "rollback-test"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/routes/expose", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleExpose(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (DNS failure)", resp.StatusCode, http.StatusInternalServerError)
	}

	cfg := h.cfgMgr.Get()
	for _, ep := range cfg.ExposedPorts {
		if ep.HostHeader == "rollback-test" {
			t.Error("route should have been rolled back but still exists in config")
		}
	}
}

func TestHandleExpose_InvalidSubdomain(t *testing.T) {
	t.Parallel()

	h, _, _ := newTestHandler(t, true)

	tests := []struct {
		name      string
		subdomain string
	}{
		{"uppercase", "MyApp"},
		{"spaces", "my app"},
		{"starts_with_hyphen", "-app"},
		{"too_long", strings.Repeat("a", 64)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := fmt.Sprintf(`{"local_port": 8080, "subdomain": %q}`, tt.subdomain)
			req := httptest.NewRequest(http.MethodPost, "/v1/routes/expose", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.HandleExpose(w, req)

			if w.Result().StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want %d for subdomain %q", w.Result().StatusCode, http.StatusBadRequest, tt.subdomain)
			}
		})
	}
}
