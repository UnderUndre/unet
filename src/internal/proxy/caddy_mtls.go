package proxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/underundre/unet/internal/config"
)

// mTLS-related constants.
const (
	// mTLSCertOrg is the organization name in generated client certificates.
	mTLSCertOrg = "unet-client"

	// mTLSCertValidity is how long a generated client certificate is valid.
	mTLSCertValidity = 365 * 24 * time.Hour
)

// SSHMTLSExecutor is the subset of SSH operations needed for mTLS bootstrap.
type SSHMTLSExecutor interface {
	// ExecuteCommand runs a command on the remote VPS host via SSH.
	ExecuteCommand(ctx context.Context, cmd string) (stdout, stderr string, err error)
	// ExecuteScript runs a script on the remote VPS host via SSH (piped to bash -s).
	ExecuteScript(ctx context.Context, script string) (stdout, stderr string, err error)
}

// GenerateClientCert generates a self-signed client certificate and private
// key suitable for mTLS with the Caddy admin API. Returns PEM-encoded cert
// and key, and persists them in the config.
func (c *CaddyClient) GenerateClientCert() (certPEM, keyPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("proxy: generate ECDSA key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("proxy: generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{mTLSCertOrg},
			CommonName:   "unet-proxy-client",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(mTLSCertValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("proxy: create certificate: %w", err)
	}

	// Encode to PEM.
	certPEM, keyPEM, err = encodeCertPEM(certDER, key)
	if err != nil {
		return "", "", fmt.Errorf("proxy: encode PEM: %w", err)
	}

	// Persist to config.
	saveErr := c.cfgMgr.Update(func(cfg *config.RootConfig) {
		cfg.CaddyAPI.ClientCert = certPEM
		cfg.CaddyAPI.ClientKey = config.SecretString(keyPEM)
	})
	if saveErr != nil {
		slog.Warn("proxy: failed to persist client cert to config", "err", saveErr)
	}

	slog.Info("proxy: generated mTLS client certificate")
	return certPEM, keyPEM, nil
}

// encodeCertPEM encodes a DER certificate and ECDSA private key to PEM strings.
func encodeCertPEM(certDER []byte, key *ecdsa.PrivateKey) (certPEM, keyPEM string, err error) {
	// Encode certificate to PEM.
	certPEM = string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	}))

	// Encode private key to PEM.
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("marshal private key: %w", err)
	}

	keyPEM = string(pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	}))

	return certPEM, keyPEM, nil
}

// BootstrapMTLS performs the initial mTLS bootstrap:
//  1. Generate a client certificate (if not already present in config)
//  2. Extract the public key in DER base64 format
//  3. Register the public key in Caddy's admin.remote.access_control[].public_keys
//     using SSH + docker exec to edit the Caddy config file directly (NOT the admin API)
//
// This avoids the race condition where the first peer registers via the admin API,
// Caddy flips to mTLS-only, and the second peer gets permanently locked out.
func (c *CaddyClient) BootstrapMTLS(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cfg := c.cfgMgr.Get()

	// Step 1: Ensure client cert exists.
	certPEM := cfg.CaddyAPI.ClientCert
	keyPEM := cfg.CaddyAPI.ClientKey.Plain()
	if certPEM == "" || keyPEM == "" {
		c.mu.Unlock() // Unlock so GenerateClientCert can acquire it.
		var err error
		certPEM, keyPEM, err = c.GenerateClientCert()
		c.mu.Lock()
		if err != nil {
			return fmt.Errorf("proxy: bootstrap mTLS: generate cert: %w", err)
		}
	}

	// Step 2: Extract DER-encoded public key from the certificate.
	pubKeyDER, err := extractPublicKeyDER(certPEM)
	if err != nil {
		return fmt.Errorf("proxy: bootstrap mTLS: extract public key: %w", err)
	}

	pubKeyB64 := base64.StdEncoding.EncodeToString(pubKeyDER)

	if c.sshClient == nil {
		return fmt.Errorf("proxy: bootstrap mTLS: SSH client not configured (required for secure mTLS provisioning)")
	}

	container := "unet-caddy"

	// Step 3: Read current Caddy config via SSH + docker exec.
	readCmd := fmt.Sprintf("docker exec %s cat /config/caddy/autosave.json", shellArgCaddy(container))
	stdout, _, err := c.sshClient.ExecuteCommand(ctx, readCmd)
	if err != nil {
		return fmt.Errorf("proxy: bootstrap mTLS: read caddy config: %w", err)
	}

	var caddyConfig map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &caddyConfig); err != nil {
		return fmt.Errorf("proxy: bootstrap mTLS: parse caddy config: %w", err)
	}

	// Step 4: Inject the public key into admin.remote.access_control[].public_keys.
	if err := injectMTLSPublicKey(caddyConfig, pubKeyB64); err != nil {
		return fmt.Errorf("proxy: bootstrap mTLS: inject public key: %w", err)
	}

	// First peer special case: check if we need to flip admin.listen to TLS.
	isFirstPeer := !hasTLSListener(caddyConfig)

	// Step 5: Write updated config back via SSH + docker exec.
	updatedJSON, err := json.Marshal(caddyConfig)
	if err != nil {
		return fmt.Errorf("proxy: bootstrap mTLS: marshal config: %w", err)
	}
	writeScript := fmt.Sprintf(
		"docker exec -i %s sh -c 'cat > /config/caddy/autosave.json'",
		shellArgCaddy(container),
	)
	// Use heredoc to pipe content.
	fullScript := writeScript + " <<'UNETFENCE'\n" + string(updatedJSON) + "\nUNETFENCE"
	if _, stderr, err := c.sshClient.ExecuteScript(ctx, fullScript); err != nil {
		return fmt.Errorf("proxy: bootstrap mTLS: write config: %w (stderr=%q)", err, stderr)
	}

	// Step 6: Reload Caddy.
	reloadCmd := fmt.Sprintf("docker exec %s caddy reload --config /config/caddy/autosave.json --adapter json",
		shellArgCaddy(container))
	if _, stderr, err := c.sshClient.ExecuteCommand(ctx, reloadCmd); err != nil {
		return fmt.Errorf("proxy: bootstrap mTLS: caddy reload: %w (stderr=%q)", err, stderr)
	}

	slog.Info("proxy: mTLS public key registered via SSH+docker exec", "isFirstPeer", isFirstPeer)
	return nil
}

// injectMTLSPublicKey modifies the caddyConfig map in-place to add the given
// base64 DER public key to the admin.remote.access_control[].public_keys array.
func injectMTLSPublicKey(caddyConfig map[string]interface{}, pubKeyB64 string) error {
	// Navigate to admin.remote.access_control
	admin, ok := caddyConfig["admin"].(map[string]interface{})
	if !ok {
		admin = make(map[string]interface{})
		caddyConfig["admin"] = admin
	}
	remote, ok := admin["remote"].(map[string]interface{})
	if !ok {
		remote = make(map[string]interface{})
		admin["remote"] = remote
	}

	// Get or create access_control array
	var acList []interface{}
	if acRaw, exists := remote["access_control"]; exists {
		acList, ok = acRaw.([]interface{})
		if !ok {
			acList = []interface{}{}
		}
	}

	// Check if pubkey already exists in any entry
	for _, acEntry := range acList {
		acMap, ok := acEntry.(map[string]interface{})
		if !ok {
			continue
		}
		pks, ok := acMap["public_keys"].([]interface{})
		if !ok {
			continue
		}
		for _, pk := range pks {
			if pkStr, ok := pk.(string); ok && pkStr == pubKeyB64 {
				return nil // already registered
			}
		}
	}

	// Append to first existing entry, or create new one
	if len(acList) > 0 {
		if acMap, ok := acList[0].(map[string]interface{}); ok {
			pks, ok := acMap["public_keys"].([]interface{})
			if !ok {
				pks = []interface{}{}
			}
			acMap["public_keys"] = append(pks, pubKeyB64)
		}
	} else {
		acList = append(acList, map[string]interface{}{
			"public_keys": []interface{}{pubKeyB64},
			"permissions": []interface{}{
				map[string]interface{}{
					"paths":   []interface{}{"/config/*"},
					"methods": []interface{}{"GET", "POST", "DELETE", "PATCH", "PUT"},
				},
			},
		})
		remote["access_control"] = acList
	}
	return nil
}

// hasTLSListener checks if Caddy admin is already configured for TLS.
func hasTLSListener(caddyConfig map[string]interface{}) bool {
	admin, ok := caddyConfig["admin"].(map[string]interface{})
	if !ok {
		return false
	}
	listen, ok := admin["listen"]
	if !ok {
		return false
	}
	// TLS listener is either a string starting with "https://" or a slice containing one.
	switch v := listen.(type) {
	case string:
		return strings.HasPrefix(v, "https://")
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.HasPrefix(s, "https://") {
				return true
			}
		}
	}
	return false
}

// shellArgCaddy quotes a string for safe embedding in a shell command.
func shellArgCaddy(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// extractPublicKeyDER parses a PEM certificate and returns the DER-encoded
// public key (SubjectPublicKeyInfo).
func extractPublicKeyDER(certPEM string) ([]byte, error) {
	block, err := pemDecode(certPEM)
	if err != nil {
		return nil, err
	}

	cert, err := x509.ParseCertificate(block)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}

	return x509.MarshalPKIXPublicKey(cert.PublicKey)
}

// pemDecode strips PEM headers and returns the DER bytes from the first
// PEM block.
func pemDecode(pemStr string) ([]byte, error) {
	// Find the first PEM block.
	beginMarker := "-----BEGIN CERTIFICATE-----"
	endMarker := "-----END CERTIFICATE-----"
	beginIdx := strings.Index(pemStr, beginMarker)
	endIdx := strings.Index(pemStr, endMarker)
	if beginIdx < 0 || endIdx < 0 {
		return nil, fmt.Errorf("no PEM certificate block found")
	}

	b64 := strings.TrimSpace(pemStr[beginIdx+len(beginMarker) : endIdx])
	// Remove whitespace/newlines.
	b64 = strings.ReplaceAll(b64, "\n", "")
	b64 = strings.ReplaceAll(b64, "\r", "")
	b64 = strings.ReplaceAll(b64, " ", "")

	return base64.StdEncoding.DecodeString(b64)
}

// CreateTLSClient creates an *http.Client configured with the stored client
// certificate for mTLS authentication. The client connects through the WG
// tunnel (bound to local tunnel IP) and uses TLS with the client cert.
func (c *CaddyClient) CreateTLSClient() (*http.Client, error) {
	cfg := c.cfgMgr.Get()

	certPEM := cfg.CaddyAPI.ClientCert
	keyPEM := cfg.CaddyAPI.ClientKey.Plain()
	if certPEM == "" || keyPEM == "" {
		return nil, fmt.Errorf("proxy: no client certificate configured, run BootstrapMTLS first")
	}

	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("proxy: load client cert: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		// We're connecting to an IP, so skip hostname verification.
		InsecureSkipVerify: true, //nolint:gosec // Caddy admin is on tunnel IP, not a public hostname
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext:      c.dialThroughTunnel,
			TLSClientConfig: tlsConfig,
		},
	}

	slog.Debug("proxy: created mTLS HTTP client")
	return client, nil
}

// ToggleMTLS switches the CaddyClient between ip-only and mTLS mode.
// When enabled=true, it creates a TLS client with the stored certificate.
// When enabled=false, it reverts to the plain ip-only HTTP client.
func (c *CaddyClient) ToggleMTLS(ctx context.Context, enabled bool) error {
	if enabled {
		tlsClient, err := c.CreateTLSClient()
		if err != nil {
			return fmt.Errorf("proxy: toggle mTLS on: %w", err)
		}
		c.SetHTTPClient(tlsClient)
		slog.Info("proxy: switched to mTLS mode")
	} else {
		ipOnlyClient := &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				DialContext: c.dialThroughTunnel,
			},
		}
		c.SetHTTPClient(ipOnlyClient)
		slog.Info("proxy: switched to ip-only mode")
	}
	return nil
}
