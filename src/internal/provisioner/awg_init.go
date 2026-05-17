package provisioner

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"sort"

	"log/slog"

	"golang.org/x/crypto/curve25519"
)

// AWGInitConfig holds all generated server-side AmneziaWG configuration values.
type AWGInitConfig struct {
	ServerPrivateKey string `json:"serverPrivateKey"`
	ServerPublicKey  string `json:"serverPublicKey"`
	PresharedKey     string `json:"presharedKey"`
	ListenPort       int    `json:"listenPort"`
	Subnet           string `json:"subnet"`
	Obfuscation      ObfuscationParams `json:"obfuscation"`
}

// ObfuscationParams holds the full set of AmneziaWG obfuscation parameters.
type ObfuscationParams struct {
	Jc   int `json:"Jc"`
	Jmin int `json:"Jmin"`
	Jmax int `json:"Jmax"`
	S1   int `json:"S1"`
	S2   int `json:"S2"`
	S3   int `json:"S3"`
	S4   int `json:"S4"`
	H1   int `json:"H1"`
	H2   int `json:"H2"`
	H3   int `json:"H3"`
	H4   int `json:"H4"`
	I1   int `json:"I1"`
	I2   int `json:"I2"`
	I3   int `json:"I3"`
	I4   int `json:"I4"`
	I5   int `json:"I5"`
}

// GenerateAWGConfig generates a complete server-side AmneziaWG initial
// configuration including keypair, PSK, listen port, subnet, and
// obfuscation parameters.
func GenerateAWGConfig() (*AWGInitConfig, error) {
	slog.Info("provisioner: generating AmneziaWG initial config")

	// --- Server keypair ---
	privKey, pubKey, err := generateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("provisioner: generate keypair: %w", err)
	}

	// --- Preshared key ---
	psk, err := generatePSK()
	if err != nil {
		return nil, fmt.Errorf("provisioner: generate PSK: %w", err)
	}

	// --- Listen port: random in [30000, 60000] ---
	port, err := randInt(30000, 60000)
	if err != nil {
		return nil, fmt.Errorf("provisioner: pick port: %w", err)
	}

	// --- Subnet: CGNAT 100.64.X.Y/24 (RFC 6598) ---
	subnet, err := pickSubnet()
	if err != nil {
		return nil, fmt.Errorf("provisioner: pick subnet: %w", err)
	}

	// --- Obfuscation parameters ---
	obf, err := generateObfuscation()
	if err != nil {
		return nil, fmt.Errorf("provisioner: generate obfuscation: %w", err)
	}

	cfg := &AWGInitConfig{
		ServerPrivateKey: privKey,
		ServerPublicKey:  pubKey,
		PresharedKey:     psk,
		ListenPort:       port,
		Subnet:           subnet,
		Obfuscation:      obf,
	}

	slog.Info("provisioner: AWG config generated",
		"port", cfg.ListenPort,
		"subnet", cfg.Subnet,
	)
	return cfg, nil
}

// ---------- key generation helpers ----------

// generateKeyPair creates a Curve25519 keypair suitable for WireGuard/AmneziaWG.
// The private key is clamped internally by curve25519.X25519.
func generateKeyPair() (privateKey, publicKey string, err error) {
	var priv [32]byte
	if _, err = rand.Read(priv[:]); err != nil {
		return "", "", fmt.Errorf("read random bytes: %w", err)
	}

	// WireGuard/Noise protocol clamps the private key bits via X25519:
	// clamp private key per WireGuard spec.
	priv[0] &= 248
	priv[31] = (priv[31] & 127) | 64

	var pub [32]byte
	pubSlice, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return "", "", fmt.Errorf("X25519 scalar mult: %w", err)
	}
	copy(pub[:], pubSlice)

	return base64.StdEncoding.EncodeToString(priv[:]),
		base64.StdEncoding.EncodeToString(pub[:]),
		nil
}

// generatePSK creates a 32-byte random pre-shared key.
func generatePSK() (string, error) {
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.StdEncoding.EncodeToString(psk[:]), nil
}

// ---------- port / subnet helpers ----------

// randInt returns a cryptographically random integer in [min, max].
func randInt(min, max int) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()) + min, nil
}

// pickSubnet selects a random /24 subnet in the CGNAT range 100.64.0.0/10
// (RFC 6598). This avoids conflicts with common private ranges
// (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16) used by corporate VPNs
// and consumer routers. Pattern follows Tailscale's approach.
func pickSubnet() (string, error) {
	// 100.64.0.0/10 → first /24 is 100.64.0.0/24, last is 100.127.255.0/24
	// That's 64 * 256 = 16384 possible /24 subnets.
	// Pick a random x in [0, 16383] and compute the /24.
	x, err := randInt(0, 16383)
	if err != nil {
		return "", err
	}
	// 100.64.0.0 = [100, 64, 0, 0]
	// x-th /24: byte0=100, byte1=64 + (x>>8), byte2=x & 0xFF
	byte1 := 64 + (x >> 8)
	byte2 := x & 0xFF
	return fmt.Sprintf("100.%d.%d.0/24", byte1, byte2), nil
}

// ---------- obfuscation helpers ----------

// generateObfuscation builds the full AmneziaWG obfuscation parameter set.
//
//	Jc   = 4   (fixed)
//	Jmin = 10  (fixed)
//	Jmax = 50  (fixed)
//	S1-S4 = random 1-byte values (0-255)
//	H1-H4 = random non-overlapping integers in [1, 2147483647]
//	I1-I5 = 0  (empty / unused)
func generateObfuscation() (ObfuscationParams, error) {
	var o ObfuscationParams

	o.Jc = 4
	o.Jmin = 10
	o.Jmax = 50

	// S1-S4: random bytes in [0, 255]
	sVals, err := randomDistinct(4, 0, 255)
	if err != nil {
		return o, err
	}
	o.S1, o.S2, o.S3, o.S4 = sVals[0], sVals[1], sVals[2], sVals[3]

	// H1-H4: random distinct positive int32 values
	hVals, err := randomDistinct(4, 1, 2147483647)
	if err != nil {
		return o, err
	}
	o.H1, o.H2, o.H3, o.H4 = hVals[0], hVals[1], hVals[2], hVals[3]

	// I1-I5: zero (empty)
	o.I1, o.I2, o.I3, o.I4, o.I5 = 0, 0, 0, 0, 0

	return o, nil
}

// randomDistinct returns n distinct random integers in [min, max].
func randomDistinct(n, min, max int) ([]int, error) {
	if max-min+1 < n {
		return nil, fmt.Errorf("range too small for %d distinct values", n)
	}

	seen := make(map[int]struct{}, n)
	result := make([]int, 0, n)

	for len(result) < n {
		v, err := randInt(min, max)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			result = append(result, v)
		}
	}

	sort.Ints(result)
	return result, nil
}
