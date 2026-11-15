package config

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

// jsonDecoder returns a fresh decoder over raw (schema decode entry point).
func jsonDecoder(raw []byte) *json.Decoder {
	return json.NewDecoder(bytes.NewReader(raw))
}

// rawHasSecrets inspects the RAW JSON for secret-bearing values BEFORE any
// structured parse (FR-008.003: refuse before materializing secrets).
// Malformed JSON is not flagged here — decodeAndValidate reports it.
func rawHasSecrets(raw []byte) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	nonEmpty := func(rm json.RawMessage) bool {
		var s string
		return json.Unmarshal(rm, &s) == nil && strings.TrimSpace(s) != ""
	}
	if v, ok := m["private_key"]; ok && nonEmpty(v) {
		return true
	}
	if peerRaw, ok := m["peer"]; ok {
		var peer map[string]json.RawMessage
		if json.Unmarshal(peerRaw, &peer) == nil {
			if v, ok := peer["preshared_key"]; ok && nonEmpty(v) {
				return true
			}
		}
	}
	if pRaw, ok := m["proxy"]; ok {
		var p map[string]json.RawMessage
		if json.Unmarshal(pRaw, &p) == nil {
			if v, ok := p["password"]; ok && nonEmpty(v) {
				return true
			}
		}
	}
	if gRaw, ok := m["gateway"]; ok {
		var g map[string]json.RawMessage
		if json.Unmarshal(gRaw, &g) == nil {
			if v, ok := g["capability_token"]; ok && nonEmpty(v) {
				return true
			}
		}
	}
	return false
}

// decodeKey decodes a base64 WireGuard key (standard encoding, 32 bytes).
func decodeKey(v string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, fmt.Errorf("not valid base64: %w", err)
	}
	return b, nil
}

// splitHostPort wraps net.SplitHostPort with a clearer missing-port error
// (zcode F15: loader-side strict host:port validation).
func splitHostPort(v string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(v)
	if err != nil {
		if strings.Contains(err.Error(), "missing port") {
			return "", "", fmt.Errorf("missing :port")
		}
		return "", "", err
	}
	return host, port, nil
}
