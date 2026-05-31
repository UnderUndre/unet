package auth

import (
	"testing"
	"time"
)

func TestNewAPIToken(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(1 * time.Hour)

	token, plain, err := NewAPIToken("test", ScopeRead, "system", &expiresAt)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	if len(plain) < 37 { // unet_ + 32 chars base64url = ~48 chars
		t.Errorf("Plain token too short: %s", plain)
	}

	if plain[:5] != "unet_" {
		t.Errorf("Plain token must start with unet_, got %s", plain)
	}

	if token.TokenPrefix != plain[:8] {
		t.Errorf("Token prefix mismatch: expected %s, got %s", plain[:8], token.TokenPrefix)
	}

	if !VerifyToken(plain, token.TokenHash) {
		t.Error("TokenHash does not match plain token")
	}

	if token.Name != "test" {
		t.Errorf("Expected name 'test', got %s", token.Name)
	}
}

func TestNewAPIToken_PastExpiration(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(-1 * time.Hour)

	_, _, err := NewAPIToken("test", ScopeRead, "system", &expiresAt)
	if err != ErrInvalidDate {
		t.Errorf("Expected ErrInvalidDate for past expiration, got %v", err)
	}
}
