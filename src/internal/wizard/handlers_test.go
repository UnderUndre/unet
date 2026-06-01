package wizard

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

	"github.com/underundre/unet/internal/wizard/dnscheck"
)

type stubSSHPool struct{}

func (s *stubSSHPool) Connect(_ context.Context, _ SSHConfig) (SSHSession, error) {
	return &stubSSHSession{}, nil
}

type stubSSHSession struct{}

func (s *stubSSHSession) Run(_ context.Context, cmd string) (string, string, error) {
	switch {
	case cmd == "cat /etc/os-release":
		return "ID=ubuntu\nVERSION_ID=\"22.04\"\n", "", nil
	case cmd == "df -h /":
		return "Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda1       100G   20G   80G  20% /\n", "", nil
	case cmd == "sudo -n true":
		return "", "", nil
	case strings.HasPrefix(cmd, "docker"):
		return "Server:\nContainers: 5\n", "", nil
	default:
		return "", "", nil
	}
}

func (s *stubSSHSession) Close() error { return nil }

func setupMux(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	dataDir := t.TempDir()
	mux := http.NewServeMux()
	RegisterRoutes(mux, dataDir, &stubSSHPool{}, BootstrapDeps{}, &dnscheck.DefaultResolver{}, "127.0.0.1")
	return mux, dataDir
}

func doRequest(t *testing.T, mux *http.ServeMux, method, path string, body interface{}) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Result()
}

func decodeJSON(t *testing.T, body io.Reader) map[string]interface{} {
	t.Helper()
	var v map[string]interface{}
	if err := json.NewDecoder(body).Decode(&v); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	return v
}

func TestCreateSession(t *testing.T) {
	t.Parallel()

	mux, _ := setupMux(t)

	resp := doRequest(t, mux, "POST", "/v1/wizard/sessions", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	v := decodeJSON(t, resp.Body)
	if v["session_id"] == nil || v["session_id"] == "" {
		t.Fatal("session_id must be present")
	}
	if v["current_step"] != "welcome" {
		t.Errorf("current_step = %v, want welcome", v["current_step"])
	}
}

func TestCreateSessionConflict(t *testing.T) {
	t.Parallel()

	mux, _ := setupMux(t)

	resp1 := doRequest(t, mux, "POST", "/v1/wizard/sessions", nil)
	resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first POST status = %d, want %d", resp1.StatusCode, http.StatusOK)
	}

	resp2 := doRequest(t, mux, "POST", "/v1/wizard/sessions", nil)
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("second POST status = %d, want %d", resp2.StatusCode, http.StatusConflict)
	}

	v := decodeJSON(t, resp2.Body)
	if v["error"] != "session_exists" {
		t.Errorf("error = %v, want session_exists", v["error"])
	}
}

func TestGetSession(t *testing.T) {
	t.Parallel()

	mux, _ := setupMux(t)

	createResp := doRequest(t, mux, "POST", "/v1/wizard/sessions", nil)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	sid, _ := created["session_id"].(string)
	if sid == "" {
		t.Fatal("no session_id in create response")
	}

	getResp := doRequest(t, mux, "GET", "/v1/wizard/sessions/"+sid, nil)
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", getResp.StatusCode, http.StatusOK)
	}

	v := decodeJSON(t, getResp.Body)
	if v["session_id"] != sid {
		t.Errorf("session_id = %v, want %s", v["session_id"], sid)
	}
	if v["current_step"] == nil {
		t.Error("current_step missing from GET response")
	}
	if v["status"] == nil {
		t.Error("status missing from GET response")
	}
}

func TestGetSessionNotFound(t *testing.T) {
	t.Parallel()

	mux, _ := setupMux(t)

	resp := doRequest(t, mux, "GET", "/v1/wizard/sessions/bogus-id-not-exist", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestDeleteSession(t *testing.T) {
	t.Parallel()

	mux, _ := setupMux(t)

	createResp := doRequest(t, mux, "POST", "/v1/wizard/sessions", nil)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	sid, _ := created["session_id"].(string)

	delResp := doRequest(t, mux, "DELETE", "/v1/wizard/sessions/"+sid, nil)
	defer delResp.Body.Close()

	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE status = %d, want %d", delResp.StatusCode, http.StatusOK)
	}

	v := decodeJSON(t, delResp.Body)
	if v["status"] != "abandoned" {
		t.Errorf("status = %v, want abandoned", v["status"])
	}
}

func TestStepSubmit_Welcome(t *testing.T) {
	t.Parallel()

	mux, _ := setupMux(t)

	createResp := doRequest(t, mux, "POST", "/v1/wizard/sessions", nil)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	sid, _ := created["session_id"].(string)

	stepResp := doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/welcome", nil)
	defer stepResp.Body.Close()

	if stepResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", stepResp.StatusCode, http.StatusOK)
	}

	v := decodeJSON(t, stepResp.Body)
	if v["next_step"] != "ssh" {
		t.Errorf("next_step = %v, want ssh", v["next_step"])
	}
}

func TestStepSubmit_SSH(t *testing.T) {
	t.Parallel()

	mux, _ := setupMux(t)

	createResp := doRequest(t, mux, "POST", "/v1/wizard/sessions", nil)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	sid, _ := created["session_id"].(string)

	doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/welcome", nil).Body.Close()

	sshBody := map[string]interface{}{
		"host":      "192.168.1.1",
		"port":      22,
		"user":      "root",
		"auth_type": "key",
	}
	sshResp := doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/ssh", sshBody)
	defer sshResp.Body.Close()

	if sshResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %v", sshResp.StatusCode, http.StatusOK, decodeJSON(t, sshResp.Body))
	}

	v := decodeJSON(t, sshResp.Body)
	if v["next_step"] != "preflight" {
		t.Errorf("next_step = %v, want preflight", v["next_step"])
	}
}

func TestStepSubmit_InvalidOrder(t *testing.T) {
	t.Parallel()

	mux, _ := setupMux(t)

	createResp := doRequest(t, mux, "POST", "/v1/wizard/sessions", nil)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	sid, _ := created["session_id"].(string)

	resp := doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/domain_check", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}

	v := decodeJSON(t, resp.Body)
	if v["error"] != "invalid_step_order" {
		t.Errorf("error = %v, want invalid_step_order", v["error"])
	}
}

func TestStepSubmit_InvalidPeerName(t *testing.T) {
	t.Parallel()

	mux, dataDir := setupMux(t)

	createResp := doRequest(t, mux, "POST", "/v1/wizard/sessions", nil)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	sid, _ := created["session_id"].(string)

	doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/welcome", nil).Body.Close()

	sshBody := map[string]interface{}{
		"host":      "1.2.3.4",
		"port":      22,
		"user":      "root",
		"auth_type": "key",
	}
	doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/ssh", sshBody).Body.Close()

	forceStateToStep(t, dataDir, sid, StepCreatePeer)

	peerBody := map[string]interface{}{
		"peer_name": "",
	}
	resp := doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/create_peer", peerBody)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusUnprocessableEntity, body)
	}
}

func TestStepBack(t *testing.T) {
	t.Parallel()

	mux, dataDir := setupMux(t)

	createResp := doRequest(t, mux, "POST", "/v1/wizard/sessions", nil)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	sid, _ := created["session_id"].(string)

	doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/welcome", nil).Body.Close()

	sshBody := map[string]interface{}{
		"host":      "1.2.3.4",
		"port":      22,
		"user":      "root",
		"auth_type": "key",
	}
	doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/ssh", sshBody).Body.Close()

	forceStateToStep(t, dataDir, sid, StepSSH)

	state, err := LoadState(dataDir)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state.CurrentStep != StepSSH {
		t.Fatalf("current_step = %q, want ssh before back test", state.CurrentStep)
	}

	backResp := doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/welcome", nil)
	defer backResp.Body.Close()

	if backResp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(backResp.Body)
		t.Fatalf("step_back via step_submit: status = %d, body: %s", backResp.StatusCode, body)
	}
}

func TestPreflight(t *testing.T) {
	t.Parallel()

	mux, dataDir := setupMux(t)

	createResp := doRequest(t, mux, "POST", "/v1/wizard/sessions", nil)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	sid, _ := created["session_id"].(string)

	doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/welcome", nil).Body.Close()

	sshBody := map[string]interface{}{
		"host":      "1.2.3.4",
		"port":      22,
		"user":      "root",
		"auth_type": "key",
	}
	doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/ssh", sshBody).Body.Close()

	forceStateToStep(t, dataDir, sid, StepPreflight)

	preflightResp := doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/preflight", nil)
	defer preflightResp.Body.Close()

	if preflightResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(preflightResp.Body)
		t.Fatalf("preflight status = %d, want %d; body: %s", preflightResp.StatusCode, http.StatusOK, body)
	}
}

func TestSensitiveFieldRedaction(t *testing.T) {
	t.Parallel()

	mux, dataDir := setupMux(t)

	createResp := doRequest(t, mux, "POST", "/v1/wizard/sessions", nil)
	var created map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()

	sid, _ := created["session_id"].(string)

	doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/welcome", nil).Body.Close()

	sshBody := map[string]interface{}{
		"host":      "1.2.3.4",
		"port":      22,
		"user":      "root",
		"auth_type": "password",
		"password":  "super-secret-pass",
	}
	doRequest(t, mux, "POST", "/v1/wizard/sessions/"+sid+"/steps/ssh", sshBody).Body.Close()

	state, err := LoadState(dataDir)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	state.Inputs.CloudflareToken = "cf-1234567890abcdef"
	if err := SaveState(dataDir, state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	getResp := doRequest(t, mux, "GET", "/v1/wizard/sessions/"+sid, nil)
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", getResp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(getResp.Body)
	bodyStr := string(body)

	if strings.Contains(bodyStr, "super-secret-pass") {
		t.Error("SSH password leaked in response body")
	}
	if !strings.Contains(bodyStr, "[REDACTED]") {
		t.Error("SSH password not redacted — expected [REDACTED]")
	}
	if strings.Contains(bodyStr, "cf-1234567890abcdef") {
		t.Error("full Cloudflare token leaked in response body")
	}
	if !strings.Contains(bodyStr, "cf-12345...") {
		t.Error("Cloudflare token not prefix-redacted — expected cf-12345...")
	}
}

func forceStateToStep(t *testing.T, dataDir, sid string, step WizardStep) {
	t.Helper()

	state, err := LoadState(dataDir)
	if err != nil {
		t.Fatalf("load state for force: %v", err)
	}
	if state == nil || state.SessionID != sid {
		t.Fatalf("session %s not found for force", sid)
	}

	state.CurrentStep = step
	state.ProgressPct = stepProgress[step]

	path := filepath.Join(dataDir, "wizard-state.json")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal forced state: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write forced state: %v", err)
	}
}
