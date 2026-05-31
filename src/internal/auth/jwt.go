package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const defaultJWTTTL = 15 * time.Minute

type JWTClaims struct {
	Sub   string `json:"sub"`
	Name  string `json:"name"`
	Scope Scope  `json:"scope"`
	Iss   string `json:"iss"`
	Iat   int64  `json:"iat"`
	Exp   int64  `json:"exp"`
	Jti   string `json:"jti"`
}

type JWTIssuer struct {
	signingKey []byte
	ttl        time.Duration
}

func NewJWTIssuer(signingKeyBase64 string) (*JWTIssuer, error) {
	key, err := base64.StdEncoding.DecodeString(signingKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("auth: decode jwt key: %w", err)
	}
	if len(key) < 32 {
		return nil, fmt.Errorf("auth: jwt key too short (%d bytes, need 32)", len(key))
	}
	return &JWTIssuer{
		signingKey: key,
		ttl:        defaultJWTTTL,
	}, nil
}

func GenerateJWTSigningKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("auth: generate jwt key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

func (j *JWTIssuer) Issue(tokenID, tokenName string, scope Scope) (string, error) {
	now := time.Now().UTC()
	claims := JWTClaims{
		Sub:   tokenID,
		Name:  tokenName,
		Scope: scope,
		Iss:   "unet-daemon",
		Iat:   now.Unix(),
		Exp:   now.Add(j.ttl).Unix(),
		Jti:   uuid.New().String(),
	}

	header := `{"alg":"HS256","typ":"JWT"}`
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: marshal claims: %w", err)
	}

	headerEnc := base64.RawURLEncoding.EncodeToString([]byte(header))
	payloadEnc := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := headerEnc + "." + payloadEnc

	mac := hmac.New(sha256.New, j.signingKey)
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + sig, nil
}

func (j *JWTIssuer) Validate(tokenStr string) (*JWTClaims, error) {
	parts := splitJWT(tokenStr)
	if len(parts) != 3 {
		return nil, fmt.Errorf("auth: invalid jwt format")
	}

	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, j.signingKey)
	mac.Write([]byte(signingInput))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, fmt.Errorf("auth: invalid jwt signature")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("auth: decode jwt payload: %w", err)
	}

	var claims JWTClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("auth: unmarshal claims: %w", err)
	}

	if claims.Iss != "unet-daemon" {
		return nil, fmt.Errorf("auth: invalid jwt issuer")
	}

	if time.Now().UTC().Unix() > claims.Exp {
		return nil, fmt.Errorf("auth: jwt expired")
	}

	return &claims, nil
}

func (j *JWTIssuer) TTL() time.Duration {
	return j.ttl
}

func splitJWT(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			parts = append(parts, s[start:i])
			start = i + 1
			if len(parts) == 2 {
				parts = append(parts, s[start:])
				return parts
			}
		}
	}
	return nil
}
