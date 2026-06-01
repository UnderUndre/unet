package qr

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/underundre/unet/internal/api/v1"
)

type Handler struct{}

func NewHandler() *Handler {
	return &Handler{}
}

func (h *Handler) HandleGenerate(w http.ResponseWriter, r *http.Request) {
	peerID := r.PathValue("peerId")
	if peerID == "" {
		v1.ErrorResponse(w, http.StatusBadRequest, v1.ErrCodeBadRequest, "peerId is required", nil)
		return
	}

	stubConfig := fmt.Sprintf("[Interface]\nPrivateKey = (stub)\nAddress = 10.0.0.%s/24\nDNS = 1.1.1.1\n\n[Peer]\nPublicKey = (stub)\nEndpoint = (stub):51820\nAllowedIPs = 0.0.0.0/0", peerID)

	result, err := Generate(stubConfig, 512)
	if err != nil {
		v1.ErrorResponse(w, http.StatusInternalServerError, v1.ErrCodeInternalError, "failed to generate QR code", map[string]any{"error": err.Error()})
		return
	}

	resp := map[string]any{
		"peer_id":        peerID,
		"qr_png_base64":  base64.StdEncoding.EncodeToString(result.PNG),
		"deeplink_uri":   result.DeeplinkURI,
		"config_text":    result.ConfigText,
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func RegisterRoutes(mux *http.ServeMux) {
	h := NewHandler()
	mux.HandleFunc("POST /v1/peers/{peerId}/qr", h.HandleGenerate)
}
