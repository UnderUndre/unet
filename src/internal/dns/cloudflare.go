package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type cfResponse struct {
	Success bool            `json:"success"`
	Errors  []cfAPIError    `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type cfAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type DNSRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

type TokenVerifyResponse struct {
	Status string `json:"status"`
	ID     string `json:"id"`
}

type CloudflareClient struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

func NewCloudflareClient(token string) *CloudflareClient {
	return &CloudflareClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    "https://api.cloudflare.com/client/v4",
		token:      token,
	}
}

func (c *CloudflareClient) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("dns/cloudflare: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(req)
}

func (c *CloudflareClient) handleResponse(resp *http.Response, operation string) (*cfResponse, error) {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dns/cloudflare: %s: read body: %w", operation, err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, fmt.Errorf("dns/cloudflare: %s: api unreachable (503)", operation)
	}

	var cfResp cfResponse
	if err := json.Unmarshal(body, &cfResp); err != nil {
		return nil, fmt.Errorf("dns/cloudflare: %s: HTTP %d: %s", operation, resp.StatusCode, string(body))
	}

	if !cfResp.Success {
		var msgs []string
		for _, e := range cfResp.Errors {
			msgs = append(msgs, fmt.Sprintf("%d: %s", e.Code, e.Message))
		}
		return nil, fmt.Errorf("dns/cloudflare: %s: %s", operation, strings.Join(msgs, "; "))
	}

	return &cfResp, nil
}

func (c *CloudflareClient) ValidateToken(ctx context.Context) error {
	resp, err := c.doRequest(ctx, http.MethodGet, "/user/tokens/verify", nil)
	if err != nil {
		return fmt.Errorf("dns/cloudflare: validate token: %w", err)
	}

	cfResp, err := c.handleResponse(resp, "validate token")
	if err != nil {
		return err
	}

	var verify TokenVerifyResponse
	if err := json.Unmarshal(cfResp.Result, &verify); err != nil {
		return fmt.Errorf("dns/cloudflare: decode token verify response: %w", err)
	}

	if verify.Status != "active" {
		return fmt.Errorf("dns/cloudflare: token status %q, expected \"active\"", verify.Status)
	}

	slog.Info("dns/cloudflare: token validated", "token_id", verify.ID)
	return nil
}

func (c *CloudflareClient) ValidateTokenScopes(ctx context.Context) error {
	resp, err := c.doRequest(ctx, http.MethodGet, "/user/tokens/verify", nil)
	if err != nil {
		return fmt.Errorf("dns/cloudflare: validate token scopes: %w", err)
	}

	cfResp, err := c.handleResponse(resp, "validate token scopes")
	if err != nil {
		return err
	}

	var verify struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(cfResp.Result, &verify); err != nil {
		return fmt.Errorf("dns/cloudflare: decode token verify response: %w", err)
	}

	if verify.Status != "active" {
		return fmt.Errorf("dns/cloudflare: token status %q, expected \"active\"", verify.Status)
	}

	slog.Info("dns/cloudflare: token scopes validated")
	return nil
}

func (c *CloudflareClient) ListRecords(ctx context.Context, zoneID, name, recordType string) ([]DNSRecord, error) {
	path := fmt.Sprintf("/zones/%s/dns_records?type=%s&name=%s", zoneID, recordType, name)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("dns/cloudflare: list records: %w", err)
	}

	cfResp, err := c.handleResponse(resp, "list records")
	if err != nil {
		return nil, err
	}

	var records []DNSRecord
	if err := json.Unmarshal(cfResp.Result, &records); err != nil {
		return nil, fmt.Errorf("dns/cloudflare: decode records: %w", err)
	}

	return records, nil
}

func (c *CloudflareClient) UpsertARecord(ctx context.Context, zoneID, name, ip string) error {
	existing, err := c.ListRecords(ctx, zoneID, name, "A")
	if err != nil {
		return fmt.Errorf("dns/cloudflare: upsert: check existing: %w", err)
	}

	record := DNSRecord{
		Type:    "A",
		Name:    name,
		Content: ip,
		Proxied: false,
		TTL:     120,
	}

	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("dns/cloudflare: upsert: marshal record: %w", err)
	}

	if len(existing) > 0 {
		recordID := existing[0].ID
		path := fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID)
		resp, err := c.doRequest(ctx, http.MethodPut, path, strings.NewReader(string(payload)))
		if err != nil {
			return fmt.Errorf("dns/cloudflare: upsert: update request: %w", err)
		}
		if _, err := c.handleResponse(resp, "update record"); err != nil {
			return err
		}
		slog.Info("dns/cloudflare: updated A record", "name", name, "record_id", recordID)
		return nil
	}

	path := fmt.Sprintf("/zones/%s/dns_records", zoneID)
	resp, err := c.doRequest(ctx, http.MethodPost, path, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("dns/cloudflare: upsert: create request: %w", err)
	}
	if _, err := c.handleResponse(resp, "create record"); err != nil {
		return err
	}

	slog.Info("dns/cloudflare: created A record", "name", name)
	return nil
}

func (c *CloudflareClient) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	path := fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID)
	resp, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("dns/cloudflare: delete record: %w", err)
	}
	if _, err := c.handleResponse(resp, "delete record"); err != nil {
		return err
	}

	slog.Info("dns/cloudflare: deleted record", "record_id", recordID)
	return nil
}

func (c *CloudflareClient) GetZoneID(ctx context.Context, zoneName string) (string, error) {
	path := fmt.Sprintf("/zones?name=%s", zoneName)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("dns/cloudflare: get zone id: %w", err)
	}

	cfResp, err := c.handleResponse(resp, "get zone id")
	if err != nil {
		return "", err
	}

	var zones []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(cfResp.Result, &zones); err != nil {
		return "", fmt.Errorf("dns/cloudflare: decode zones: %w", err)
	}

	if len(zones) == 0 {
		return "", fmt.Errorf("dns/cloudflare: zone %q not found", zoneName)
	}

	return zones[0].ID, nil
}
