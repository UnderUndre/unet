package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/underundre/unet/internal/config"
)

// DNSManager manages DNS records for tunnelled services. It supports two
// providers:
//   - "cloudflare": uses the Cloudflare API to create/delete CNAME records.
//   - "manual": assumes the user has pre-configured a wildcard A-record
//     (*.domain.com → VPS IP); CreateRecord/DeleteRecord are no-ops with
//     a warning if the subdomain is not resolvable.
type DNSManager struct {
	cfgMgr *config.Manager

	// httpClient is used for Cloudflare API calls.
	httpClient *http.Client
}

// NewDNSManager creates a new DNSManager.
func NewDNSManager(cfgMgr *config.Manager) *DNSManager {
	return &DNSManager{
		cfgMgr: cfgMgr,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// DNSRecord represents a DNS record returned by or sent to the Cloudflare API.
type DNSRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

// cfAPIResponse is the generic Cloudflare API response envelope.
type cfAPIResponse struct {
	Success  bool          `json:"success"`
	Errors   []cfAPIError  `json:"errors"`
	Messages []string      `json:"messages"`
	Result   json.RawMessage `json:"result"`
}

type cfAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// CreateRecord creates a DNS record for the given subdomain.
// For Cloudflare provider: creates a CNAME record pointing to the VPS IP.
// For manual provider: validates the wildcard exists (no-op, warns if not resolvable).
func (dm *DNSManager) CreateRecord(ctx context.Context, subdomain string) error {
	cfg := dm.cfgMgr.Get()
	provider := cfg.DNS.Provider

	switch strings.ToLower(provider) {
	case "cloudflare":
		return dm.createCloudflareRecord(ctx, cfg, subdomain)
	case "manual", "":
		return dm.validateManualDNS(cfg, subdomain)
	default:
		return fmt.Errorf("proxy/dns: unsupported provider %q", provider)
	}
}

// DeleteRecord removes the DNS record for the given subdomain.
// For Cloudflare provider: finds and deletes the CNAME record.
// For manual provider: no-op (user manages DNS manually).
func (dm *DNSManager) DeleteRecord(ctx context.Context, subdomain string) error {
	cfg := dm.cfgMgr.Get()
	provider := cfg.DNS.Provider

	switch strings.ToLower(provider) {
	case "cloudflare":
		return dm.deleteCloudflareRecord(ctx, cfg, subdomain)
	case "manual", "":
		slog.Info("proxy/dns: manual mode, skipping record deletion", "subdomain", subdomain)
		return nil
	default:
		return fmt.Errorf("proxy/dns: unsupported provider %q", provider)
	}
}

// ---------- Cloudflare implementation ----------

const cfAPIBase = "https://api.cloudflare.com/client/v4"

// createCloudflareRecord creates a CNAME record for the subdomain pointing
// to the VPS IP via the Cloudflare API.
func (dm *DNSManager) createCloudflareRecord(ctx context.Context, cfg *config.RootConfig, subdomain string) error {
	token := cfg.DNS.Token.Plain()
	zoneID := cfg.DNS.Zone
	domain := cfg.DNS.Zone // zone is the domain name (e.g., "example.com")

	if token == "" {
		return fmt.Errorf("proxy/dns: cloudflare API token not configured")
	}
	if zoneID == "" {
		return fmt.Errorf("proxy/dns: cloudflare zone not configured")
	}

	// Build the full record name.
	recordName := subdomain
	if !strings.HasSuffix(subdomain, domain) {
		recordName = subdomain + "." + domain
	}

	// The CNAME target is the VPS IP (or a domain pointing to it).
	// For a CNAME, we need a domain target. Since we're pointing to a VPS IP,
	// we create an A record instead if we have a raw IP.
	vpsIP := cfg.Tunnel.ServerIP
	if vpsIP == "" {
		return fmt.Errorf("proxy/dns: tunnel server IP not configured")
	}

	// Determine record type: use A record when pointing to an IP directly.
	recordType := "A"
	content := vpsIP

	// Check if record already exists.
	existing, err := dm.findCloudflareRecord(ctx, token, zoneID, recordName)
	if err != nil {
		return fmt.Errorf("proxy/dns: check existing record: %w", err)
	}

	if existing != nil {
		// Update existing record if content differs.
		if existing.Content != content {
			return dm.updateCloudflareRecord(ctx, token, zoneID, existing.ID, recordName, recordType, content)
		}
		slog.Info("proxy/dns: record already exists", "name", recordName, "content", content)
		return nil
	}

	// Create new record.
	record := DNSRecord{
		Type:    recordType,
		Name:    recordName,
		Content: content,
		Proxied: false,
		TTL:     1, // 1 = auto
	}

	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("proxy/dns: marshal record: %w", err)
	}

	url := fmt.Sprintf("%s/zones/%s/dns_records", cfAPIBase, zoneID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("proxy/dns: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	slog.Info("proxy/dns: creating DNS record", "name", recordName, "type", recordType, "content", content)

	resp, err := dm.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("proxy/dns: cloudflare api unreachable: %w", err)
	}
	defer resp.Body.Close()

	return dm.handleCFResponse(resp, "create record")
}

// deleteCloudflareRecord finds and deletes the DNS record for the subdomain.
func (dm *DNSManager) deleteCloudflareRecord(ctx context.Context, cfg *config.RootConfig, subdomain string) error {
	token := cfg.DNS.Token.Plain()
	zoneID := cfg.DNS.Zone
	domain := cfg.DNS.Zone

	if token == "" || zoneID == "" {
		return fmt.Errorf("proxy/dns: cloudflare not configured (token or zone missing)")
	}

	recordName := subdomain
	if !strings.HasSuffix(subdomain, domain) {
		recordName = subdomain + "." + domain
	}

	existing, err := dm.findCloudflareRecord(ctx, token, zoneID, recordName)
	if err != nil {
		return fmt.Errorf("proxy/dns: find record: %w", err)
	}
	if existing == nil {
		slog.Info("proxy/dns: record not found, nothing to delete", "name", recordName)
		return nil
	}

	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", cfAPIBase, zoneID, existing.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("proxy/dns: create delete request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	slog.Info("proxy/dns: deleting DNS record", "name", recordName, "id", existing.ID)

	resp, err := dm.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("proxy/dns: cloudflare api unreachable: %w", err)
	}
	defer resp.Body.Close()

	return dm.handleCFResponse(resp, "delete record")
}

// findCloudflareRecord searches for an existing DNS record matching the name.
func (dm *DNSManager) findCloudflareRecord(ctx context.Context, token, zoneID, name string) (*DNSRecord, error) {
	url := fmt.Sprintf("%s/zones/%s/dns_records?name=%s", cfAPIBase, zoneID, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := dm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare api unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cloudflare returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Success bool         `json:"success"`
		Result  []DNSRecord  `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if !result.Success || len(result.Result) == 0 {
		return nil, nil
	}

	// Return the first matching record.
	return &result.Result[0], nil
}

// updateCloudflareRecord updates an existing DNS record via PUT.
func (dm *DNSManager) updateCloudflareRecord(ctx context.Context, token, zoneID, recordID, name, recordType, content string) error {
	record := DNSRecord{
		Type:    recordType,
		Name:    name,
		Content: content,
		Proxied: false,
		TTL:     1,
	}

	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}

	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", cfAPIBase, zoneID, recordID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	slog.Info("proxy/dns: updating DNS record", "name", name, "content", content)

	resp, err := dm.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare api unreachable: %w", err)
	}
	defer resp.Body.Close()

	return dm.handleCFResponse(resp, "update record")
}

// handleCFResponse checks a Cloudflare API response for errors.
func (dm *DNSManager) handleCFResponse(resp *http.Response, operation string) error {
	var cfResp cfAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfResp); err != nil {
		// If we can't decode, at least check the status code.
		if resp.StatusCode >= 400 {
			return fmt.Errorf("proxy/dns: %s: HTTP %d", operation, resp.StatusCode)
		}
		return nil
	}

	if !cfResp.Success {
		var errMsgs []string
		for _, e := range cfResp.Errors {
			errMsgs = append(errMsgs, fmt.Sprintf("%d: %s", e.Code, e.Message))
		}
		return fmt.Errorf("proxy/dns: %s: %s", operation, strings.Join(errMsgs, "; "))
	}

	return nil
}

// ---------- Manual mode ----------

// validateManualDNS performs a DNS lookup to check if the subdomain resolves.
// In manual mode, the user is expected to have configured a wildcard A-record
// (*.domain.com → VPS IP). This function just warns if it doesn't resolve.
func (dm *DNSManager) validateManualDNS(cfg *config.RootConfig, subdomain string) error {
	domain := cfg.DNS.Zone
	if domain == "" {
		slog.Warn("proxy/dns: manual mode but no domain/zone configured, cannot validate")
		return nil
	}

	fqdn := subdomain
	if !strings.HasSuffix(subdomain, domain) {
		fqdn = subdomain + "." + domain
	}

	// Try to resolve the FQDN.
	resolver := &net.Resolver{}
	ips, err := resolver.LookupHost(context.Background(), fqdn)
	if err != nil || len(ips) == 0 {
		slog.Warn("proxy/dns: manual mode: subdomain not resolvable",
			"fqdn", fqdn,
			"hint", "ensure wildcard A-record *.domain.com is configured",
		)
		return nil
	}

	slog.Info("proxy/dns: manual mode: subdomain resolves", "fqdn", fqdn, "ips", ips)
	return nil
}
