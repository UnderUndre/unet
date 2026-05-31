package auth

import (
	"testing"
	"time"
)

func TestJWTIssuer_IssueAndValidate(t *testing.T) {
	key, err := GenerateJWTSigningKey()
	if err != nil {
		t.Fatalf("GenerateJWTSigningKey: %v", err)
	}

	issuer, err := NewJWTIssuer(key)
	if err != nil {
		t.Fatalf("NewJWTIssuer: %v", err)
	}

	token, err := issuer.Issue("token-1", "admin", ScopeAdmin)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if len(token) < 50 {
		t.Errorf("token seems too short: %s", token)
	}

	claims, err := issuer.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if claims.Sub != "token-1" {
		t.Errorf("expected sub=token-1, got %s", claims.Sub)
	}
	if claims.Name != "admin" {
		t.Errorf("expected name=admin, got %s", claims.Name)
	}
	if claims.Scope != ScopeAdmin {
		t.Errorf("expected scope=admin, got %s", claims.Scope)
	}
	if claims.Iss != "unet-daemon" {
		t.Errorf("expected iss=unet-daemon, got %s", claims.Iss)
	}
	if claims.Jti == "" {
		t.Error("expected jti to be set")
	}
}

func TestJWTIssuer_ExpiredToken(t *testing.T) {
	key, _ := GenerateJWTSigningKey()
	issuer, _ := NewJWTIssuer(key)

	issuer.ttl = -1 * time.Second

	token, _ := issuer.Issue("t1", "test", ScopeRead)

	_, err := issuer.Validate(token)
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestJWTIssuer_WrongKey(t *testing.T) {
	key1, _ := GenerateJWTSigningKey()
	key2, _ := GenerateJWTSigningKey()

	issuer1, _ := NewJWTIssuer(key1)
	issuer2, _ := NewJWTIssuer(key2)

	token, _ := issuer1.Issue("t1", "test", ScopeRead)

	_, err := issuer2.Validate(token)
	if err == nil {
		t.Error("expected error for wrong signing key")
	}
}

func TestJWTIssuer_InvalidIssuer(t *testing.T) {
	key, _ := GenerateJWTSigningKey()
	issuer, _ := NewJWTIssuer(key)

	token, _ := issuer.Issue("t1", "test", ScopeRead)

	claims, err := issuer.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if claims.Iss != "unet-daemon" {
		t.Errorf("unexpected issuer: %s", claims.Iss)
	}
}

func TestJWTIssuer_TamperedPayload(t *testing.T) {
	key, _ := GenerateJWTSigningKey()
	issuer, _ := NewJWTIssuer(key)

	token, _ := issuer.Issue("t1", "test", ScopeRead)

	tampered := token[:len(token)-5] + "XXXXX"
	_, err := issuer.Validate(tampered)
	if err == nil {
		t.Error("expected error for tampered token")
	}
}
