package auth

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTokenCache_ValidateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "config.json"))

	token, plain, err := NewAPIToken("test-cache", ScopeWrite, "system", nil)
	if err != nil {
		t.Fatalf("NewAPIToken: %v", err)
	}
	if err := store.Create(token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	cache := NewTokenCache(store, 5*time.Minute)

	result, err := cache.Validate(plain)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if result.TokenID != token.ID {
		t.Errorf("expected tokenID %s, got %s", token.ID, result.TokenID)
	}
	if result.TokenName != "test-cache" {
		t.Errorf("expected name test-cache, got %s", result.TokenName)
	}
	if result.Scope != ScopeWrite {
		t.Errorf("expected scope write, got %s", result.Scope)
	}
}

func TestTokenCache_WrongToken(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "config.json"))

	token, _, _ := NewAPIToken("test", ScopeRead, "system", nil)
	store.Create(token)

	cache := NewTokenCache(store, 5*time.Minute)

	_, err := cache.Validate("unet_wrong_token_value_here")
	if err == nil {
		t.Error("expected error for wrong token")
	}
}

func TestTokenCache_CacheHit(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "config.json"))

	token, plain, _ := NewAPIToken("cached", ScopeAdmin, "system", nil)
	store.Create(token)

	cache := NewTokenCache(store, 5*time.Minute)

	result1, err := cache.Validate(plain)
	if err != nil {
		t.Fatalf("first Validate: %v", err)
	}

	result2, err := cache.Validate(plain)
	if err != nil {
		t.Fatalf("second Validate (cache hit): %v", err)
	}

	if result1.TokenID != result2.TokenID {
		t.Error("cache hit should return same token ID")
	}
}

func TestTokenCache_Invalidate(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "config.json"))

	token, plain, _ := NewAPIToken("inv-test", ScopeRead, "system", nil)
	store.Create(token)

	cache := NewTokenCache(store, 5*time.Minute)

	cache.Validate(plain)

	cache.Invalidate(token.TokenPrefix)

	// Token still valid in store, but cache is invalidated — should re-read from store
	result, err := cache.Validate(plain)
	if err != nil {
		t.Fatalf("Validate after invalidate: %v", err)
	}
	if result.TokenID != token.ID {
		t.Error("re-validated token should match")
	}
}

func TestTokenCache_DisabledToken(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "config.json"))

	token, plain, _ := NewAPIToken("disabled", ScopeRead, "system", nil)
	store.Create(token)

	store.SoftDelete(token.ID)

	cache := NewTokenCache(store, 5*time.Minute)

	_, err := cache.Validate(plain)
	if err == nil {
		t.Error("expected error for disabled token")
	}
}
