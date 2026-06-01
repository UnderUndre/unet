package invite

import (
	"crypto/sha256"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Parallel()

	secret := "test-secret-key"
	key := DeriveKey(secret)
	plaintext := `[Interface]
PrivateKey = abc123
Address = 10.0.0.2/32

[Peer]
PublicKey = xyz789
Endpoint = 1.2.3.4:51820`

	encrypted, err := EncryptConfig(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptConfig failed: %v", err)
	}
	if encrypted == "" {
		t.Fatal("encrypted should not be empty")
	}
	if encrypted == plaintext {
		t.Fatal("encrypted should differ from plaintext")
	}

	decrypted, err := DecryptConfig(encrypted, key)
	if err != nil {
		t.Fatalf("DecryptConfig failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("round-trip mismatch:\ngot:  %q\nwant: %q", decrypted, plaintext)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	t.Parallel()

	keyA := DeriveKey("secret-a")
	keyB := DeriveKey("secret-b")
	plaintext := "sensitive data"

	encrypted, err := EncryptConfig(plaintext, keyA)
	if err != nil {
		t.Fatalf("EncryptConfig failed: %v", err)
	}

	_, err = DecryptConfig(encrypted, keyB)
	if err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestEncryptEmptyPlaintext(t *testing.T) {
	t.Parallel()

	key := DeriveKey("some-key")

	encrypted, err := EncryptConfig("", key)
	if err != nil {
		t.Fatalf("EncryptConfig with empty string should not fail at encrypt stage: %v", err)
	}

	decrypted, err := DecryptConfig(encrypted, key)
	if err != nil {
		t.Fatalf("DecryptConfig failed: %v", err)
	}

	if decrypted != "" {
		t.Errorf("expected empty string, got %q", decrypted)
	}
}

func TestHMACSignatureValidation(t *testing.T) {
	t.Parallel()

	hmacKey := []byte("hmac-secret-key-32-bytes-long!!")
	token := []byte("invite-token-abc")
	peerID := "peer-123"
	var expiresAt int64 = 1735689600

	signature, err := SignURL(token, peerID, expiresAt, hmacKey)
	if err != nil {
		t.Fatalf("SignURL failed: %v", err)
	}
	if len(signature) != sha256.Size {
		t.Fatalf("signature length = %d, want %d", len(signature), sha256.Size)
	}

	if !ValidateURL(token, peerID, expiresAt, signature, hmacKey) {
		t.Fatal("valid signature should pass validation")
	}

	tampered := make([]byte, len(signature))
	copy(tampered, signature)
	tampered[0] ^= 0xFF

	if ValidateURL(token, peerID, expiresAt, tampered, hmacKey) {
		t.Fatal("tampered signature should fail validation")
	}
}

func TestHMACConstantTime(t *testing.T) {
	t.Parallel()

	hmacKey := []byte("hmac-secret-key-32-bytes-long!!")
	token := []byte("token")
	peerID := "p1"
	var expiresAt int64 = 9999

	sig1, _ := SignURL(token, peerID, expiresAt, hmacKey)
	sig2, _ := SignURL(token, peerID, expiresAt, hmacKey)

	if !ValidateURL(token, peerID, expiresAt, sig1, hmacKey) {
		t.Fatal("sig1 should validate")
	}
	if !ValidateURL(token, peerID, expiresAt, sig2, hmacKey) {
		t.Fatal("sig2 should validate")
	}

	fakeSig := make([]byte, len(sig1))
	for i := range fakeSig {
		fakeSig[i] = 0xAA
	}

	if ValidateURL(token, peerID, expiresAt, fakeSig, hmacKey) {
		t.Fatal("completely wrong signature should not validate")
	}
}
