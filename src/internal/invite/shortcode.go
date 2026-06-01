package invite

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
)

func GenerateCode() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("rand read: %w", err)
	}

	n := binary.BigEndian.Uint64(buf[:])
	code := 10000000 + int(n%uint64(90000000))
	if code < 10000000 || code > 99999999 {
		code = 10000000 + int(n%90000000)
	}

	_ = math.MaxInt64
	return fmt.Sprintf("%08d", code), nil
}
