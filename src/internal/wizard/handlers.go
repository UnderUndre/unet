package wizard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/underundre/unet/internal/wizard/dnscheck"
	"github.com/underundre/unet/internal/wizard/preflight"
)

type Handler struct {
	dataDir       string
	sshPool       SSHPool
	bootstrapDeps BootstrapDeps
	dnsResolver   dnscheck.Resolver
	vpsIP         string
}

type apiError struct {
	Error   string      `json:"error"`
	Message string      `json:"message"`
	Context interface{} `json:"context,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, errCode, msg string, ctx interface{}) {
	writeJSON(w, status, apiError{Error: errCode, Message: msg, Context: ctx})
}

func RegisterRoutes(mux *http.ServeMux, dataDir string, sshPool SSHPool, deps BootstrapDeps, dnsResolver dnscheck.Resolver, vpsIP string) {
	h := &Handler{dataDir: dataDir, sshPool: sshPool, bootstrapDeps: deps, dnsResolver: dnsResolver, vpsIP: vpsIP}

	mux.HandleFunc("POST /v1/wizard/sessions", h.handleCreateSession)
	mux.HandleFunc("GET /v1/wizard/sessions/{id}", h.handleGetSession)
	mux.HandleFunc("DELETE /v1/wizard/sessions/{id}", h.handleDeleteSession)
	mux.HandleFunc("POST /v1/wizard/sessions/{id}/steps/{step}", h.handleStepSubmit)
	mux.HandleFunc("POST /v1/wizard/sessions/{id}/preflight", h.handlePreflight)
	mux.HandleFunc("POST /v1/wizard/sessions/{id}/commit", h.handleCommit)
	mux.HandleFunc("GET /v1/wizard/sessions/{id}/events", h.handleSSEEvents)
}

func (h *Handler) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	existing, err := LoadState(h.dataDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_error", err.Error(), nil)
		return
	}

	if existing != nil {
		writeError(w, http.StatusConflict, "session_exists",
			"a wizard session already exists",
			map[string]interface{}{
				"session_id":   existing.SessionID,
				"current_step": existing.CurrentStep,
			})
		return
	}

	state := NewState()
	if err := SaveState(h.dataDir, state); err != nil {
		writeError(w, http.StatusInternalServerError, "save_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, state)
}

func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	state, err := LoadState(h.dataDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_error", err.Error(), nil)
		return
	}

	if state == nil || state.SessionID != id {
		writeError(w, http.StatusNotFound, "not_found", "session not found", nil)
		return
	}

	if state.CurrentStep == StepCommit && state.Status == StatusInProgress {
		writeJSON(w, http.StatusOK, state)
		return
	}

	redacted := redactState(state)
	writeJSON(w, http.StatusOK, redacted)
}

func redactState(s *WizardState) *WizardState {
	copy := *s

	if copy.Inputs.SSH != nil && copy.Inputs.SSH.Password != "" {
		sshCopy := *copy.Inputs.SSH
		sshCopy.Password = "[REDACTED]"
		copy.Inputs.SSH = &sshCopy
	}

	if copy.Inputs.CloudflareToken != "" {
		token := copy.Inputs.CloudflareToken
		if len(token) > 8 {
			copy.Inputs.CloudflareToken = token[:8] + "..."
		} else {
			copy.Inputs.CloudflareToken = token + "..."
		}
	}

	return &copy
}

func (h *Handler) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	state, err := LoadState(h.dataDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_error", err.Error(), nil)
		return
	}

	if state == nil || state.SessionID != id {
		writeError(w, http.StatusNotFound, "not_found", "session not found", nil)
		return
	}

	_, err = Reducer(state, WizardAction{Type: ActionAbandon})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reducer_error", err.Error(), nil)
		return
	}

	if err := DeleteState(h.dataDir); err != nil {
		writeError(w, http.StatusInternalServerError, "delete_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": id,
		"status":     "abandoned",
	})
}

func (h *Handler) handleStepSubmit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	stepStr := r.PathValue("step")

	state, err := LoadState(h.dataDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_error", err.Error(), nil)
		return
	}

	if state == nil || state.SessionID != id {
		writeError(w, http.StatusNotFound, "not_found", "session not found", nil)
		return
	}

	step := WizardStep(stepStr)
	if !IsValidStep(step) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_step",
			fmt.Sprintf("invalid step: %s", stepStr),
			map[string]interface{}{"step": stepStr})
		return
	}

	if state.CurrentStep != step {
		writeError(w, http.StatusConflict, "invalid_step_order",
			fmt.Sprintf("expected step %s but got %s", state.CurrentStep, step),
			map[string]interface{}{
				"expected": state.CurrentStep,
				"got":      step,
			})
		return
	}

	if err := applyStepInput(state, step, r); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error(),
			map[string]interface{}{"step": stepStr})
		return
	}

	if step == StepDomainCheck {
		port80Free := true
		if state.PreflightResult != nil {
			port80Free = state.PreflightResult.Port80Free
		}
		dcr, dnsErr := dnscheck.Validate(r.Context(), h.dnsResolver, state.Inputs.Domain, h.vpsIP, port80Free)
		if dnsErr != nil {
			writeError(w, http.StatusUnprocessableEntity, "dns_check_failed", fmt.Sprintf("DNS validation failed: %v", dnsErr), nil)
			return
		}
		state.DomainCheckResult = &DomainCheckResult{
			Domain:              dcr.Domain,
			Mode:                dcr.Mode,
			ARecordIPs:          dcr.ARecordIPs,
			PointsToVPS:         dcr.PointsToVPS,
			CloudflareDetected:  dcr.CloudflareDetected,
			CloudflareTokenValid: dcr.CloudflareTokenValid,
			TLSStrategy:         dcr.TLSStrategy,
			TLSFeasible:         dcr.TLSFeasible,
			CheckedAt:           dcr.CheckedAt,
			Warnings:            dcr.Warnings,
			Errors:              dcr.Errors,
		}
	}

	nextStep, err := resolveNextStep(state, step)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "transition_error", err.Error(),
			map[string]interface{}{"step": stepStr})
		return
	}

	updated, err := Reducer(state, WizardAction{
		Type: ActionStepComplete,
		Step: nextStep,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reducer_error", err.Error(), nil)
		return
	}

	if err := SaveState(h.dataDir, updated); err != nil {
		writeError(w, http.StatusInternalServerError, "save_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id":   updated.SessionID,
		"step":         step,
		"status":       "completed",
		"next_step":    nextStep,
		"progress_pct": updated.ProgressPct,
	})
}

func applyStepInput(state *WizardState, step WizardStep, r *http.Request) error {
	switch step {
	case StepWelcome:
		return nil

	case StepSSH:
		var input SSHInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		if input.Host == "" {
			return fmt.Errorf("ssh.host is required")
		}
		if input.Port <= 0 {
			return fmt.Errorf("ssh.port is required and must be positive")
		}
		if input.User == "" {
			return fmt.Errorf("ssh.user is required")
		}
		if input.AuthType == "" {
			return fmt.Errorf("ssh.auth_type is required")
		}
		state.Inputs.SSH = &input

	case StepDomainMode:
		var body struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		if body.Mode != "byo" && body.Mode != "nipio" {
			return fmt.Errorf("mode must be 'byo' or 'nipio'")
		}
		state.Inputs.DomainMode = body.Mode
		state.Inputs.NipioEnabled = body.Mode == "nipio"

	case StepDomainCheck:
		var body struct {
			Domain string `json:"domain"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		if body.Domain == "" {
			return fmt.Errorf("domain is required")
		}
		state.Inputs.Domain = body.Domain

	case StepCloudflare:
		var body struct {
			Token   string `json:"token"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		if body.Enabled && body.Token == "" {
			return fmt.Errorf("token is required when cloudflare is enabled")
		}
		state.Inputs.CloudflareToken = body.Token

	case StepCreatePeer:
		var body struct {
			PeerName   string      `json:"peer_name"`
			ExposePort *PortExpose `json:"expose_port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		if body.PeerName == "" {
			return fmt.Errorf("peer_name is required")
		}
		state.Inputs.FirstPeerName = body.PeerName
		state.Inputs.FirstPortExpose = body.ExposePort

	default:
		return fmt.Errorf("step %s does not accept input", step)
	}

	return nil
}

func resolveNextStep(state *WizardState, current WizardStep) (WizardStep, error) {
	switch current {
	case StepWelcome:
		return StepSSH, nil

	case StepSSH:
		return StepPreflight, nil

	case StepDomainMode:
		if state.Inputs.DomainMode == "nipio" {
			return StepCreatePeer, nil
		}
		return StepDomainCheck, nil

	case StepDomainCheck:
		if state.DomainCheckResult != nil {
			if state.DomainCheckResult.CloudflareDetected && state.DomainCheckResult.TLSFeasible {
				return StepCloudflare, nil
			}
			if state.DomainCheckResult.TLSFeasible {
				return StepCreatePeer, nil
			}
		}
		return StepCreatePeer, nil

	case StepCloudflare:
		return StepCreatePeer, nil

	case StepCreatePeer:
		return StepCommit, nil

	default:
		return "", fmt.Errorf("no next step defined for %s", current)
	}
}

func (h *Handler) handlePreflight(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	state, err := LoadState(h.dataDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_error", err.Error(), nil)
		return
	}

	if state == nil || state.SessionID != id {
		writeError(w, http.StatusNotFound, "not_found", "session not found", nil)
		return
	}

	if state.CurrentStep != StepPreflight {
		writeError(w, http.StatusConflict, "invalid_step",
			fmt.Sprintf("preflight can only run from preflight step, current: %s", state.CurrentStep),
			map[string]interface{}{"current_step": state.CurrentStep})
		return
	}

	if state.Inputs.SSH == nil {
		writeError(w, http.StatusUnprocessableEntity, "missing_ssh",
			"SSH credentials not set; complete the SSH step first", nil)
		return
	}

	cfg := SSHConfig{
		Host:     state.Inputs.SSH.Host,
		Port:     state.Inputs.SSH.Port,
		User:     state.Inputs.SSH.User,
		AuthType: state.Inputs.SSH.AuthType,
		KeyPath:  state.Inputs.SSH.KeyPath,
		Password: state.Inputs.SSH.Password,
	}

	preflightCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	sess, err := h.sshPool.Connect(preflightCtx, cfg)
	if err != nil {
		writeError(w, http.StatusBadGateway, "ssh_connect_failed",
			fmt.Sprintf("SSH connection failed: %v", err), nil)
		return
	}
	defer sess.Close()

	result, err := preflight.Run(preflightCtx, sess, state.Inputs.SSH.Host)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "preflight_error",
			fmt.Sprintf("Preflight check failed: %v", err), nil)
		return
	}

	pr := convertPreflightResult(result)

	updated, err := Reducer(state, WizardAction{
		Type: ActionPreflight,
		Data: pr,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reducer_error", err.Error(), nil)
		return
	}

	if err := SaveState(h.dataDir, updated); err != nil {
		writeError(w, http.StatusInternalServerError, "save_error", err.Error(), nil)
		return
	}

	if pr.Compatible {
		writeJSON(w, http.StatusOK, pr)
	} else {
		writeJSON(w, http.StatusUnprocessableEntity, pr)
	}
}

func convertPreflightResult(r *preflight.Result) *PreflightResult {
	return &PreflightResult{
		TargetHost:       r.TargetHost,
		CheckedAt:        r.CheckedAt,
		Distro:           r.Distro,
		DistroVersion:    r.DistroVersion,
		Arch:             r.Arch,
		DiskFreeGB:       r.DiskFreeGB,
		RAMMB:            r.RAMMB,
		HasSudo:          r.HasSudo,
		HasDocker:        r.HasDocker,
		DockerRunning:    r.DockerRunning,
		Port443Free:      r.Port443Free,
		Port80Free:       r.Port80Free,
		PortWGFree:       r.PortWGFree,
		Compatible:       r.Compatible,
		Warnings:         r.Warnings,
		BlockingFailures: r.BlockingFailures,
	}
}
