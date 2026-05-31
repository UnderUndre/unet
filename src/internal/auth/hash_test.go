package auth

import (
	"testing"
	"time"
)

func TestHashAndVerifyToken(t *testing.T) {
	plain := "unet_test_token_string"

	// Test hashing and round trip
	hash, err := HashToken(plain)
	if err != nil {
		t.Fatalf("HashToken failed: %v", err)
	}

	if hash == "" {
		t.Fatal("HashToken returned empty string")
	}

	if hash == plain {
		t.Fatal("HashToken returned the plain string instead of a hash")
	}

	// Verify correct token
	if !VerifyToken(plain, hash) {
		t.Error("VerifyToken failed on correct token")
	}

	// Verify wrong token
	if VerifyToken("unet_wrong_token", hash) {
		t.Error("VerifyToken succeeded on incorrect token")
	}
}

func BenchmarkHashToken(b *testing.B) {
	plain := "unet_test_token_string"
	for i := 0; i < b.N; i++ {
		_, err := HashToken(plain)
		if err != nil {
			b.Fatalf("HashToken failed: %v", err)
		}
	}
}

func TestHashTokenCost(t *testing.T) {
	plain := "unet_test_token_string"
	
	start := time.Now()
	_, err := HashToken(plain)
	if err != nil {
		t.Fatalf("HashToken failed: %v", err)
	}
	duration := time.Since(start)

	if duration > 500*time.Millisecond {
		t.Errorf("HashToken took %v, which is > 500ms (cost 12 should be faster on modern hardware)", duration)
	}
}
