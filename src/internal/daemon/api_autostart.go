package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/underundre/unet/internal/platform"
)

// AutostartSettings is the request/response body for autostart toggle.
type AutostartSettings struct {
	Enabled bool `json:"enabled"`
}

// AutostartHandler serves POST /api/v1/settings/autostart.
type AutostartHandler struct {
	autostart platform.AutoStart
	server    *Server
}

// NewAutostartHandler creates a new AutostartHandler.
func NewAutostartHandler(srv *Server) *AutostartHandler {
	return &AutostartHandler{
		autostart: platform.NewAutoStart(),
		server:    srv,
	}
}

// RegisterRoutes registers the autostart settings endpoint.
func (h *AutostartHandler) RegisterRoutes() {
	h.server.HandleFunc("POST /api/v1/settings/autostart", h.handleAutostart)
	h.server.HandleFunc("GET /api/v1/settings/autostart", h.handleAutostartGet)
}

func (h *AutostartHandler) handleAutostart(w http.ResponseWriter, r *http.Request) {
	var req AutostartSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}

	var err error
	if req.Enabled {
		err = h.autostart.Enable()
	} else {
		err = h.autostart.Disable()
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "autostart_error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AutostartSettings{Enabled: h.autostart.IsEnabled()})
}

func (h *AutostartHandler) handleAutostartGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AutostartSettings{Enabled: h.autostart.IsEnabled()})
}
