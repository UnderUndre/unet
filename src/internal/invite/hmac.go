package invite

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
)

func SignURL(token []byte, peerID string, expiresAt int64, hmacKey []byte) ([]byte, error) {
	h := hmac.New(sha256.New, hmacKey)
	h.Write(token)
	h.Write([]byte(peerID))

	var buf [8]byte
	binPutUint64(buf[:], uint64(expiresAt))
	h.Write(buf[:])

	return h.Sum(nil), nil
}

func ValidateURL(token []byte, peerID string, expiresAt int64, signature []byte, hmacKey []byte) bool {
	expected, err := SignURL(token, peerID, expiresAt, hmacKey)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(signature, expected) == 1
}

func binPutUint64(b []byte, v uint64) {
	_ = b[7]
	b[0] = byte(v >> 56)
	b[1] = byte(v >> 48)
	b[2] = byte(v >> 40)
	b[3] = byte(v >> 32)
	b[4] = byte(v >> 24)
	b[5] = byte(v >> 16)
	b[6] = byte(v >> 8)
	b[7] = byte(v)
}
