package invite

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/underundre/unet/internal/api/v1"
)

const (
	rateLimitMaxAttempts = 5
	rateLimitWindow      = 60 * time.Second
)

type rateLimitEntry struct {
	attempts []time.Time
}

type Handler struct {
	store   *Store
	hmacKey []byte
	encKey  []byte

	rlMu   sync.Mutex
	rlMap  map[string]*rateLimitEntry
}

func NewHandler(store *Store, daemonSecret string) *Handler {
	return &Handler{
		store:   store,
		hmacKey: DeriveKey(daemonSecret + "-hmac"),
		encKey:  DeriveKey(daemonSecret),
		rlMap:   make(map[string]*rateLimitEntry),
	}
}

func (h *Handler) checkRateLimit(ip string) bool {
	h.rlMu.Lock()
	defer h.rlMu.Unlock()

	now := time.Now()
	entry, ok := h.rlMap[ip]
	if !ok {
		h.rlMap[ip] = &rateLimitEntry{attempts: []time.Time{now}}
		return true
	}

	cutoff := now.Add(-rateLimitWindow)
	filtered := make([]time.Time, 0, len(entry.attempts))
	for _, t := range entry.attempts {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}

	if len(filtered) >= rateLimitMaxAttempts {
		entry.attempts = filtered
		return false
	}

	entry.attempts = append(filtered, now)
	h.rlMap[ip] = entry
	return true
}

func (h *Handler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	peerID := r.PathValue("peerId")
	if peerID == "" {
		v1.ErrorResponse(w, http.StatusBadRequest, v1.ErrCodeBadRequest, "peerId is required", nil)
		return
	}

	var req CreateInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		v1.ErrorResponse(w, http.StatusBadRequest, v1.ErrCodeBadRequest, "invalid request body", nil)
		return
	}

	if req.PeerID == "" {
		req.PeerID = peerID
	}
	if req.Mode == "" {
		req.Mode = "hmac"
	}
	if req.TTLSeconds <= 0 {
		req.TTLSeconds = 3600
	}
	if req.MaxUses <= 0 {
		req.MaxUses = 1
	}

	mode := InviteMode(req.Mode)
	if mode != ModeHMAC && mode != ModeShortCode {
		v1.ErrorResponse(w, http.StatusBadRequest, v1.ErrCodeBadRequest, "mode must be hmac or shortcode", nil)
		return
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(req.TTLSeconds) * time.Second)
	inv := &InviteLink{
		ID:        uuid.New().String(),
		PeerID:    req.PeerID,
		Mode:      mode,
		MaxUses:   req.MaxUses,
		UseCount:  0,
		CreatedAt: now,
		CreatedBy: "system",
		ExpiresAt: expiresAt,
	}

	switch mode {
	case ModeHMAC:
		token := uuid.New().String()
		tokenHash := sha256.Sum256([]byte(token))
		inv.TokenHash = hex.EncodeToString(tokenHash[:])

		sig, err := SignURL([]byte(token), req.PeerID, expiresAt.Unix(), h.hmacKey)
		if err != nil {
			v1.ErrorResponse(w, http.StatusInternalServerError, v1.ErrCodeInternalError, "sign failed", nil)
			return
		}
		inv.HMACSig = hex.EncodeToString(sig)

		if err := h.store.Append(inv); err != nil {
			v1.ErrorResponse(w, http.StatusInternalServerError, v1.ErrCodeInternalError, "store failed", nil)
			return
		}

		inviteURL := fmt.Sprintf("/invite/%s?token=%s&expires=%d&sig=%s",
			req.PeerID, token, expiresAt.Unix(), hex.EncodeToString(sig))

		writeJSON(w, http.StatusCreated, map[string]any{
			"id":          inv.ID,
			"peer_id":     inv.PeerID,
			"mode":        string(inv.Mode),
			"invite_url":  inviteURL,
			"token":       token,
			"expires_at":  inv.ExpiresAt.Format(time.RFC3339),
			"max_uses":    inv.MaxUses,
			"created_at":  inv.CreatedAt.Format(time.RFC3339),
		})

	case ModeShortCode:
		code, err := GenerateCode()
		if err != nil {
			v1.ErrorResponse(w, http.StatusInternalServerError, v1.ErrCodeInternalError, "code gen failed", nil)
			return
		}

		tokenHash := sha256.Sum256([]byte(code))
		inv.TokenHash = hex.EncodeToString(tokenHash[:])
		inv.ShortCode = code

		if err := h.store.Append(inv); err != nil {
			v1.ErrorResponse(w, http.StatusInternalServerError, v1.ErrCodeInternalError, "store failed", nil)
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"id":         inv.ID,
			"peer_id":    inv.PeerID,
			"mode":       string(inv.Mode),
			"short_code": inv.ShortCode,
			"expires_at": inv.ExpiresAt.Format(time.RFC3339),
			"max_uses":   inv.MaxUses,
			"created_at": inv.CreatedAt.Format(time.RFC3339),
		})
	}
}

func (h *Handler) HandleLanding(w http.ResponseWriter, r *http.Request) {
	peerID := r.PathValue("peerId")
	if peerID == "" {
		v1.ErrorResponse(w, http.StatusBadRequest, v1.ErrCodeBadRequest, "peerId is required", nil)
		return
	}

	ip := extractIP(r)
	if !h.checkRateLimit(ip) {
		w.Header().Set("Retry-After", "60")
		v1.ErrorResponse(w, http.StatusTooManyRequests, v1.ErrCodeRateLimited, "too many attempts", map[string]any{"retry_after": 60})
		return
	}

	token := r.URL.Query().Get("token")
	expiresStr := r.URL.Query().Get("expires")
	sig := r.URL.Query().Get("sig")
	code := r.URL.Query().Get("code")

	var inv *InviteLink
	var err error

	if token != "" {
		tokenHash := sha256.Sum256([]byte(token))
		hashStr := hex.EncodeToString(tokenHash[:])
		inv, err = h.store.FindByTokenHash(hashStr)
		if err != nil {
			v1.ErrorResponse(w, http.StatusInternalServerError, v1.ErrCodeInternalError, "lookup failed", nil)
			return
		}

		if inv == nil || inv.PeerID != peerID {
			v1.ErrorResponse(w, http.StatusNotFound, v1.ErrCodeNotFound, "invite not found", nil)
			return
		}

		if !time.Now().Before(inv.ExpiresAt) {
			v1.ErrorResponse(w, http.StatusGone, "expired", "invite has expired", nil)
			return
		}

		if inv.MaxUses > 0 && inv.UseCount >= inv.MaxUses {
			v1.ErrorResponse(w, http.StatusGone, "consumed", "invite has been consumed", nil)
			return
		}

		if expiresStr != "" && sig != "" {
			var expires int64
			fmt.Sscanf(expiresStr, "%d", &expires)
			sigBytes, _ := hex.DecodeString(sig)
			if !ValidateURL([]byte(token), peerID, expires, sigBytes, h.hmacKey) {
				v1.ErrorResponse(w, http.StatusForbidden, v1.ErrCodeUnauthorized, "invalid signature", nil)
				return
			}
		}
	} else if code != "" {
		codeHash := sha256.Sum256([]byte(code))
		hashStr := hex.EncodeToString(codeHash[:])
		inv, err = h.store.FindByTokenHash(hashStr)
		if err != nil {
			v1.ErrorResponse(w, http.StatusInternalServerError, v1.ErrCodeInternalError, "lookup failed", nil)
			return
		}

		if inv == nil || inv.PeerID != peerID {
			v1.ErrorResponse(w, http.StatusNotFound, v1.ErrCodeNotFound, "invite not found", nil)
			return
		}

		if !time.Now().Before(inv.ExpiresAt) {
			v1.ErrorResponse(w, http.StatusGone, "expired", "invite has expired", nil)
			return
		}

		if inv.MaxUses > 0 && inv.UseCount >= inv.MaxUses {
			v1.ErrorResponse(w, http.StatusGone, "consumed", "invite has been consumed", nil)
			return
		}
	} else {
		v1.ErrorResponse(w, http.StatusBadRequest, v1.ErrCodeBadRequest, "token or code query param required", nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"peer_id":    inv.PeerID,
		"mode":       string(inv.Mode),
		"expires_at": inv.ExpiresAt.Format(time.RFC3339),
		"created_at": inv.CreatedAt.Format(time.RFC3339),
	})
}

func (h *Handler) HandleDownload(w http.ResponseWriter, r *http.Request) {
	peerID := r.PathValue("peerId")
	if peerID == "" {
		v1.ErrorResponse(w, http.StatusBadRequest, v1.ErrCodeBadRequest, "peerId is required", nil)
		return
	}

	ip := extractIP(r)
	if !h.checkRateLimit(ip) {
		w.Header().Set("Retry-After", "60")
		v1.ErrorResponse(w, http.StatusTooManyRequests, v1.ErrCodeRateLimited, "too many attempts", map[string]any{"retry_after": 60})
		return
	}

	token := r.URL.Query().Get("token")
	code := r.URL.Query().Get("code")

	var inv *InviteLink
	var err error

	if token != "" {
		tokenHash := sha256.Sum256([]byte(token))
		inv, err = h.store.FindByTokenHash(hex.EncodeToString(tokenHash[:]))
	} else if code != "" {
		codeHash := sha256.Sum256([]byte(code))
		inv, err = h.store.FindByTokenHash(hex.EncodeToString(codeHash[:]))
	} else {
		v1.ErrorResponse(w, http.StatusBadRequest, v1.ErrCodeBadRequest, "token or code query param required", nil)
		return
	}

	if err != nil {
		v1.ErrorResponse(w, http.StatusInternalServerError, v1.ErrCodeInternalError, "lookup failed", nil)
		return
	}

	if inv == nil || inv.PeerID != peerID {
		v1.ErrorResponse(w, http.StatusNotFound, v1.ErrCodeNotFound, "invite not found", nil)
		return
	}

	if !time.Now().Before(inv.ExpiresAt) {
		v1.ErrorResponse(w, http.StatusGone, "expired", "invite has expired", nil)
		return
	}

	if inv.MaxUses > 0 && inv.UseCount >= inv.MaxUses {
		v1.ErrorResponse(w, http.StatusGone, "consumed", "invite has been consumed", nil)
		return
	}

	if inv.ConfigEnc == "" {
		v1.ErrorResponse(w, http.StatusNotFound, v1.ErrCodeNotFound, "no config available for this invite", nil)
		return
	}

	configText, err := DecryptConfig(inv.ConfigEnc, h.encKey)
	if err != nil {
		slog.Error("decrypt config failed", "error", err, "invite_id", inv.ID)
		v1.ErrorResponse(w, http.StatusInternalServerError, v1.ErrCodeInternalError, "decrypt failed", nil)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="unet-%s.conf"`, peerID))
	w.Write([]byte(configText))
}

func RegisterRoutes(mux *http.ServeMux, handler *Handler) {
	mux.HandleFunc("POST /v1/peers/{peerId}/invite", handler.HandleCreate)
	mux.HandleFunc("GET /invite/{peerId}", handler.HandleLanding)
	mux.HandleFunc("GET /invite/{peerId}/download", handler.HandleDownload)
}

func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	host := r.RemoteAddr
	return host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
