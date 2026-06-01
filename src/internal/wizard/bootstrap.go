package wizard

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/underundre/unet/internal/logstream"
)

type Bootstrapper interface {
	Run(ctx context.Context, state *WizardState) (*BootstrapResult, error)
}

type PeerCreator interface {
	CreatePeer(ctx context.Context, name string, state *WizardState) (*PeerResult, error)
}

type RouteExposer interface {
	ExposeRoute(ctx context.Context, localPort int, subdomain string, state *WizardState) (*RouteExposeResult, error)
}

type BootstrapResult struct {
	Success       bool   `json:"success"`
	WGEndpoint    string `json:"wg_endpoint,omitempty"`
	ServerPubKey  string `json:"server_pub_key,omitempty"`
	TunnelSubnet  string `json:"tunnel_subnet,omitempty"`
	Duration      string `json:"duration,omitempty"`
}

type PeerResult struct {
	PeerID    string `json:"peer_id"`
	PublicKey string `json:"public_key"`
	LocalIP   string `json:"local_ip"`
}

type RouteExposeResult struct {
	RouteID   string `json:"route_id"`
	Subdomain string `json:"subdomain"`
	FQDN      string `json:"fqdn"`
	URL       string `json:"url"`
}

type BootstrapDeps struct {
	Bootstrapper Bootstrapper
	PeerCreator  PeerCreator
	RouteExposer RouteExposer
	LogHub       *logstream.Hub
	DataDir      string
	SSHPool      SSHPool
}

func (h *Handler) handleCommit(w http.ResponseWriter, r *http.Request) {
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

	if state.CurrentStep != StepCommit {
		writeError(w, http.StatusConflict, "invalid_step",
			fmt.Sprintf("commit can only run from commit step, current: %s", state.CurrentStep),
			map[string]interface{}{"current_step": state.CurrentStep})
		return
	}

	if state.Status != StatusInProgress {
		writeError(w, http.StatusConflict, "invalid_status",
			fmt.Sprintf("session status is %s, expected in_progress", state.Status), nil)
		return
	}

	updated, err := Reducer(state, WizardAction{Type: ActionCommitStart})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reducer_error", err.Error(), nil)
		return
	}

	if err := SaveState(h.dataDir, updated); err != nil {
		writeError(w, http.StatusInternalServerError, "save_error", err.Error(), nil)
		return
	}

	detachedCtx := context.Background()

	go h.runBootstrap(detachedCtx, updated)

	sseURL := fmt.Sprintf("/v1/wizard/sessions/%s/events", id)
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"session_id": id,
		"status":     "committing",
		"sse_url":    sseURL,
	})
}

func (h *Handler) runBootstrap(ctx context.Context, state *WizardState) {
	slog.Info("wizard/bootstrap: starting", "session_id", state.SessionID)

	var bootResult *BootstrapResult
	var peerResult *PeerResult
	var routeResult *RouteExposeResult

	bootResult, err := h.bootstrapDeps.Bootstrapper.Run(ctx, state)
	if err != nil {
		slog.Error("wizard/bootstrap: bootstrapper failed", "error", err, "session_id", state.SessionID)
		h.commitFailure(state, err.Error())
		return
	}

	peerResult, err = h.bootstrapDeps.PeerCreator.CreatePeer(ctx, state.Inputs.FirstPeerName, state)
	if err != nil {
		slog.Error("wizard/bootstrap: peer creation failed", "error", err, "session_id", state.SessionID)
		h.commitFailure(state, fmt.Sprintf("peer creation failed: %v", err))
		return
	}

	if state.Inputs.FirstPortExpose != nil {
		port := state.Inputs.FirstPortExpose.LocalPort
		subdomain := state.Inputs.FirstPortExpose.Subdomain

		routeResult, err = h.bootstrapDeps.RouteExposer.ExposeRoute(ctx, port, subdomain, state)
		if err != nil {
			slog.Error("wizard/bootstrap: route exposure failed", "error", err, "session_id", state.SessionID)
			h.commitFailure(state, fmt.Sprintf("route exposure failed: %v", err))
			return
		}
	}

	loaded, loadErr := LoadState(h.dataDir)
	if loadErr != nil {
		slog.Error("wizard/bootstrap: failed to reload state for success", "error", loadErr)
		h.commitFailure(state, "internal state error")
		return
	}

	updated, reduceErr := Reducer(loaded, WizardAction{Type: ActionCommitSuccess})
	if reduceErr != nil {
		slog.Error("wizard/bootstrap: reducer error on success", "error", reduceErr)
		return
	}

	type commitSuccessContext struct {
		PeerID      string `json:"peer_id"`
		PeerName    string `json:"peer_name"`
		LocalIP     string `json:"local_ip"`
		RouteURL    string `json:"route_url"`
		RouteFQDN   string `json:"route_fqdn"`
		WGEndpoint  string `json:"wg_endpoint"`
		ServerPubKey string `json:"server_pub_key"`
	}

	routeURL := ""
	routeFQDN := ""
	if routeResult != nil {
		routeURL = routeResult.URL
		routeFQDN = routeResult.FQDN
	}

	ctx_data := commitSuccessContext{
		PeerID:       peerResult.PeerID,
		PeerName:     state.Inputs.FirstPeerName,
		LocalIP:      peerResult.LocalIP,
		RouteURL:     routeURL,
		RouteFQDN:    routeFQDN,
		WGEndpoint:   bootResult.WGEndpoint,
		ServerPubKey: bootResult.ServerPubKey,
	}

	_ = ctx_data

	if err := SaveState(h.dataDir, updated); err != nil {
		slog.Error("wizard/bootstrap: failed to save success state", "error", err)
		return
	}

	slog.Info("wizard/bootstrap: completed successfully",
		"session_id", state.SessionID,
		"peer_id", peerResult.PeerID,
		"route_url", routeResult.URL,
	)
}

func (h *Handler) commitFailure(state *WizardState, errMsg string) {
	loaded, loadErr := LoadState(h.dataDir)
	if loadErr != nil {
		slog.Error("wizard/bootstrap: cannot reload state for failure", "error", loadErr)
		return
	}

	updated, reduceErr := Reducer(loaded, WizardAction{
		Type:  ActionCommitFailure,
		Error: errMsg,
	})
	if reduceErr != nil {
		slog.Error("wizard/bootstrap: reducer error on failure", "error", reduceErr)
		return
	}

	if err := SaveState(h.dataDir, updated); err != nil {
		slog.Error("wizard/bootstrap: failed to save failure state", "error", err)
	}
}

func (h *Handler) handleSSEEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	state, err := LoadState(h.dataDir)
	if err != nil || state == nil || state.SessionID != id {
		writeError(w, http.StatusNotFound, "not_found", "session not found", nil)
		return
	}

	if h.bootstrapDeps.LogHub == nil {
		writeError(w, http.StatusInternalServerError, "no_log_hub", "log streaming not available", nil)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_error", "streaming not supported", nil)
		return
	}

	filter := logstream.SubFilter{
		Components: map[string]bool{"wizard": true, "bootstrap": true},
	}
	sub, _ := h.bootstrapDeps.LogHub.Subscribe(filter)
	defer h.bootstrapDeps.LogHub.Unsubscribe(sub.ID())

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	fmt.Fprintf(w, "event: system\ndata: {\"type\":\"connected\",\"session_id\":\"%s\"}\n\n", id)
	flusher.Flush()

	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-sub.Channel():
			if !ok {
				return
			}
			var event map[string]any
			if json.Unmarshal(data, &event) != nil {
				continue
			}
			eventType := "log"
			if e, ok := event["event"].(string); ok {
				eventType = e
			}
			dataJSON, _ := json.Marshal(event["data"])
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, dataJSON)
			flusher.Flush()

			current, loadErr := LoadState(h.dataDir)
			if loadErr == nil && current != nil && (current.CurrentStep == StepSuccess || current.CurrentStep == StepError) {
				statusJSON, _ := json.Marshal(map[string]any{
					"step":   string(current.CurrentStep),
					"status": string(current.Status),
				})
				fmt.Fprintf(w, "event: done\ndata: %s\n\n", statusJSON)
				flusher.Flush()
				return
			}
		case <-heartbeat.C:
			fmt.Fprintf(w, "event: heartbeat\ndata: {\"ts\":\"%s\"}\n\n", time.Now().UTC().Format(time.RFC3339))
			flusher.Flush()
		}
	}
}
