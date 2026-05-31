package auth

import (
	"golang.org/x/crypto/bcrypt"
)

// HashToken creates a bcrypt hash of the plain token using cost 12.
func HashToken(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyToken checks if the plain token matches the hash using constant-time comparison.
func VerifyToken(plain, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
	return err == nil
}
