package auth

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStore_CreateAndGet(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "config.json"))

	now := time.Now()
	expires := now.Add(24 * time.Hour)
	token, plain, err := NewAPIToken("test-admin", ScopeAdmin, "system", &expires)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	if plain == "" {
		t.Fatal("Plain token is empty")
	}

	err = store.Create(token)
	if err != nil {
		t.Fatalf("Failed to store token: %v", err)
	}

	// Test GetByID
	retrieved, err := store.GetByID(token.ID)
	if err != nil {
		t.Fatalf("Failed to get token by ID: %v", err)
	}
	if retrieved.Name != token.Name {
		t.Errorf("Expected name %s, got %s", token.Name, retrieved.Name)
	}

	// Test GetByName
	retrievedByName, err := store.GetByName(token.Name)
	if err != nil {
		t.Fatalf("Failed to get token by name: %v", err)
	}
	if retrievedByName.ID != token.ID {
		t.Errorf("Expected ID %s, got %s", token.ID, retrievedByName.ID)
	}

	// Test uniqueness
	token2, _, _ := NewAPIToken("test-admin", ScopeRead, "system", nil)
	err = store.Create(token2)
	if err != ErrDuplicateName {
		t.Errorf("Expected ErrDuplicateName, got %v", err)
	}
}

func TestStore_UpdateAndSoftDelete(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "config.json"))

	token, _, _ := NewAPIToken("test-token", ScopeRead, "system", nil)
	store.Create(token)

	// Update RequestCount
	token.RequestCount = 5
	err := store.Update(token)
	if err != nil {
		t.Fatalf("Failed to update token: %v", err)
	}

	updated, _ := store.GetByID(token.ID)
	if updated.RequestCount != 5 {
		t.Errorf("Expected request count 5, got %d", updated.RequestCount)
	}

	// SoftDelete
	err = store.SoftDelete(token.ID)
	if err != nil {
		t.Fatalf("Failed to soft delete token: %v", err)
	}

	deleted, _ := store.GetByID(token.ID)
	if deleted.Enabled {
		t.Error("Expected token to be disabled")
	}
}
