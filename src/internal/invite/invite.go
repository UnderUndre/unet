package invite

import "time"

type InviteMode string

const (
	ModeHMAC     InviteMode = "hmac"
	ModeShortCode InviteMode = "shortcode"
)

type InviteLink struct {
	ID          string     `json:"id"`
	PeerID      string     `json:"peer_id"`
	Mode        InviteMode `json:"mode"`
	TokenHash   string     `json:"token_hash"`
	ShortCode   string     `json:"short_code,omitempty"`
	ExpiresAt   time.Time  `json:"expires_at"`
	MaxUses     int        `json:"max_uses"`
	UseCount    int        `json:"use_count"`
	CreatedAt   time.Time  `json:"created_at"`
	CreatedBy   string     `json:"created_by"`
	ClaimedAt   *time.Time `json:"claimed_at,omitempty"`
	ClaimedIP   string     `json:"claimed_ip,omitempty"`
	ConfigEnc   string     `json:"config_enc,omitempty"`
	HMACSig     string     `json:"hmac_sig,omitempty"`
}

type CreateInviteRequest struct {
	PeerID     string `json:"peer_id"`
	Mode       string `json:"mode"`
	TTLSeconds int    `json:"ttl_seconds"`
	MaxUses    int    `json:"max_uses"`
}
