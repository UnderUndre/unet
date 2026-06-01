// Package trayapi provides an HTTP client for the unet daemon's localhost API.
package trayapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Default daemon port.
const defaultPort = 8080

// Client communicates with the unet daemon over localhost HTTP.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// DaemonStatus is the response from GET /api/status.
type DaemonStatus struct {
	Tunnel struct {
		Status      string `json:"status"`
		ConnectedAt string `json:"connectedAt,omitempty"`
	} `json:"tunnel"`
	VPS struct {
		Host string `json:"host"`
	} `json:"vps"`
	Ports []struct {
		Subdomain string `json:"subdomain"`
		FQDN      string `json:"fqdn"`
		LocalPort int    `json:"localPort"`
		Status    string `json:"status"`
	} `json:"ports"`
}

// NewClient discovers the daemon port and creates a client.
// Port discovery order: NET_DAEMON_PORT env → daemon config → default 8080.
func NewClient() (*Client, error) {
	port := discoverPort()
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second, // F5 from review: client-side timeout
		},
	}, nil
}

// SetTimeout changes the HTTP client timeout.
func (c *Client) SetTimeout(d time.Duration) {
	c.httpClient.Timeout = d
}

// GetStatus fetches the current daemon status.
func (c *Client) GetStatus(ctx context.Context) (*DaemonStatus, error) {
	statusCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(statusCtx, http.MethodGet, c.baseURL+"/api/status", nil)
	if err != nil {
		return nil, fmt.Errorf("trayapi: status request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trayapi: status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trayapi: status returned %d", resp.StatusCode)
	}

	var status DaemonStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("trayapi: decode status: %w", err)
	}

	return &status, nil
}

// Connect tells the daemon to establish the tunnel.
func (c *Client) Connect(ctx context.Context) error {
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return c.postAction(connectCtx, "/api/tunnel/connect")
}

// Disconnect tells the daemon to tear down the tunnel.
func (c *Client) Disconnect(ctx context.Context) error {
	disconnectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return c.postAction(disconnectCtx, "/api/tunnel/disconnect")
}

// HealthCheck does a lightweight GET /api/status to verify daemon is alive.
func (c *Client) HealthCheck(ctx context.Context) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, c.baseURL+"/api/status", nil)
	if err != nil {
		return false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// GetPortURLs returns the list of exposed port FQDNs for clipboard copy.
func (c *Client) GetPortURLs(ctx context.Context) ([]string, error) {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return nil, err
	}

	var urls []string
	for _, p := range status.Ports {
		if p.Status == "active" && p.FQDN != "" {
			urls = append(urls, "https://"+p.FQDN)
		}
	}
	return urls, nil
}

func (c *Client) postAction(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("trayapi: request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("trayapi: %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("trayapi: %s returned %d: %s", path, resp.StatusCode, string(body))
	}

	return nil
}

// discoverPort finds the daemon port.
func discoverPort() int {
	// 1. Environment variable.
	if envPort := os.Getenv("UNET_DAEMON_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p > 0 {
			return p
		}
	}

	// 2. Daemon config file.
	configDir, err := configDir()
	if err == nil {
		configPath := filepath.Join(configDir, "config.json")
		if data, err := os.ReadFile(configPath); err == nil {
			var cfg struct {
				Daemon struct {
					Port int `json:"port"`
				} `json:"daemon"`
			}
			if json.Unmarshal(data, &cfg) == nil && cfg.Daemon.Port > 0 {
				return cfg.Daemon.Port
			}
		}
	}

	// 3. Default.
	slog.Debug("trayapi: using default port", "port", defaultPort)
	return defaultPort
}

func configDir() (string, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	dir := filepath.Join(localAppData, "unet")
	return dir, nil
}
