package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
)

// APIToken represents an authentication credential for remote API access.
type APIToken struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	TokenHash    string    `json:"tokenHash"`
	TokenPrefix  string    `json:"tokenPrefix"`
	Scope        Scope     `json:"scope"`
	CreatedBy    string    `json:"createdBy"`
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
	RequestCount int64     `json:"requestCount"`
	Enabled      bool      `json:"enabled"`
}

var (
	ErrDuplicateName = errors.New("peer_name_conflict: token name already exists")
	ErrInvalidDate   = errors.New("invalid expiration date: must be in the future")
	ErrTokenNotFound = errors.New("not_found: token not found")
)

// GenerateTokenString creates a new random token string.
// Format: unet_<base64url>
func GenerateTokenString() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return "unet_" + base64.RawURLEncoding.EncodeToString(b)
}

// NewAPIToken initializes a new APIToken and its plaintext representation.
func NewAPIToken(name string, scope Scope, createdBy string, expiresAt *time.Time) (*APIToken, string, error) {
	if expiresAt != nil && expiresAt.Before(time.Now()) {
		return nil, "", ErrInvalidDate
	}

	plainToken := GenerateTokenString()
	hash, err := HashToken(plainToken)
	if err != nil {
		return nil, "", err
	}

	// tokenPrefix is the first 8 characters of the raw token (unet_ + 3 chars)
	prefixLen := 8
	if len(plainToken) < prefixLen {
		prefixLen = len(plainToken)
	}

	token := &APIToken{
		ID:           uuid.New().String(),
		Name:         name,
		TokenHash:    hash,
		TokenPrefix:  plainToken[:prefixLen],
		Scope:        scope,
		CreatedBy:    createdBy,
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    expiresAt,
		RequestCount: 0,
		Enabled:      true,
	}

	return token, plainToken, nil
}
