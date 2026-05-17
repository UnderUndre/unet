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
	"sync"
	"time"

	"github.com/underundre/unet/internal/config"
)

// CaddyClient manages reverse-proxy routes on a remote Caddy instance via
// its admin API (http://<tunnel-serverIp>:2019). All requests are routed
// through the WireGuard tunnel by binding the HTTP client's dialer to the
// local tunnel IP.
//
// Operations are guarded by a sync.Mutex to prevent concurrent route
// manipulation which could lead to race conditions when finding/deleting
// routes by position.
type CaddyClient struct {
	cfgMgr *config.Manager
	mu     sync.Mutex

	// baseURL is the Caddy admin API base, e.g. "http://10.8.1.1:2019".
	baseURL string

	// httpClient is the HTTP client used for all Caddy API calls.
	// It binds to the WG local IP so traffic goes through the tunnel.
	httpClient *http.Client

	// sshClient is used for mTLS bootstrap via SSH+docker exec (NOT the admin API).
	sshClient SSHMTLSExecutor
}

// NewCaddyClient creates a new CaddyClient. The HTTP client is configured
// with a 5-second timeout and dials through the WG tunnel interface.
// sshClient is optional (may be nil) — it is required only for BootstrapMTLS.
func NewCaddyClient(cfgMgr *config.Manager, sshClient SSHMTLSExecutor) *CaddyClient {
	c := &CaddyClient{
		cfgMgr:    cfgMgr,
		baseURL:   "", // set lazily on first use
		sshClient: sshClient,
	}

	// Build an HTTP client that routes through the WG tunnel.
	c.httpClient = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: c.dialThroughTunnel,
		},
	}

	return c
}

// dialThroughTunnel dials the Caddy admin API through the WireGuard tunnel
// by explicitly binding the source IP to the local tunnel IP.
func (c *CaddyClient) dialThroughTunnel(ctx context.Context, network, addr string) (net.Conn, error) {
	cfg := c.cfgMgr.Get()
	localIP := cfg.Tunnel.LocalIP

	if localIP == "" {
		return nil, fmt.Errorf("proxy: tunnel local IP not configured")
	}

	// Resolve the target address.
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("proxy: invalid address %q: %w", addr, err)
	}

	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		LocalAddr: &net.TCPAddr{IP: net.ParseIP(localIP)},
	}

	return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
}

// resolveBaseURL returns the Caddy admin API base URL from config.
func (c *CaddyClient) resolveBaseURL() (string, error) {
	cfg := c.cfgMgr.Get()

	// Prefer explicit address from CaddyAPI config.
	if cfg.CaddyAPI.Address != "" {
		return cfg.CaddyAPI.Address, nil
	}

	// Derive from tunnel server IP.
	serverIP := cfg.Tunnel.ServerIP
	if serverIP == "" {
		return "", fmt.Errorf("proxy: tunnel server IP not configured")
	}
	return "http://" + net.JoinHostPort(serverIP, "2019"), nil
}

// baseURLResolved lazily resolves and caches the base URL.
func (c *CaddyClient) baseURLResolved() (string, error) {
	if c.baseURL != "" {
		return c.baseURL, nil
	}
	url, err := c.resolveBaseURL()
	if err != nil {
		return "", err
	}
	c.baseURL = url
	return url, nil
}

// Route represents a single Caddy route with host match and upstream dial.
type Route struct {
	Host        string // matched hostname, e.g. "app.example.com"
	UpstreamDial string // upstream dial target, e.g. "10.8.1.2:3000"
}

// caddyRouteJSON models the JSON structure for a Caddy admin API route.
type caddyRouteJSON struct {
	Match    []caddyMatchJSON    `json:"match"`
	Handle   []caddyHandleJSON   `json:"handle"`
	Terminal bool                `json:"terminal"`
}

type caddyMatchJSON struct {
	Host []string `json:"host"`
}

type caddyHandleJSON struct {
	Handler   string             `json:"handler"`
	Upstreams []caddyUpstreamJSON `json:"upstreams,omitempty"`
}

type caddyUpstreamJSON struct {
	Dial string `json:"dial"`
}

// caddyConfigResponse models the top-level Caddy config JSON returned by
// GET /config/.
type caddyConfigResponse struct {
	Apps caddyAppsJSON `json:"apps"`
}

type caddyAppsJSON struct {
	HTTP caddyHTTPAppJSON `json:"http"`
}

type caddyHTTPAppJSON struct {
	Servers map[string]caddyServerJSON `json:"servers"`
}

type caddyServerJSON struct {
	Routes []caddyRouteJSON `json:"routes"`
}

// AddRoute adds a new reverse-proxy route to Caddy for the given host
// pointing to the upstream dial address. The route is appended via
// POST /config/apps/http/servers/srv0/routes.
func (c *CaddyClient) AddRoute(ctx context.Context, host, upstreamDial string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	baseURL, err := c.baseURLResolved()
	if err != nil {
		return fmt.Errorf("proxy: resolve caddy address: %w", err)
	}

	route := caddyRouteJSON{
		Match: []caddyMatchJSON{
			{Host: []string{host}},
		},
		Handle: []caddyHandleJSON{
			{
				Handler: "reverse_proxy",
				Upstreams: []caddyUpstreamJSON{
					{Dial: upstreamDial},
				},
			},
		},
		Terminal: true,
	}

	body, err := json.Marshal(route)
	if err != nil {
		return fmt.Errorf("proxy: marshal route: %w", err)
	}

	url := baseURL + "/config/apps/http/servers/srv0/routes"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("proxy: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	slog.Info("proxy: adding caddy route", "host", host, "upstream", upstreamDial)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("proxy: caddy api unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("proxy: caddy returned %d: %s", resp.StatusCode, string(respBody))
	}

	slog.Info("proxy: route added", "host", host, "upstream", upstreamDial)
	return nil
}

// RemoveRoute finds a route matching the given host and removes it by
// position. Caddy's admin API requires DELETE by route index, so we first
// list routes to find the matching one, then DELETE it. The mutex prevents
// concurrent find-and-delete races.
func (c *CaddyClient) RemoveRoute(ctx context.Context, host string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	baseURL, err := c.baseURLResolved()
	if err != nil {
		return fmt.Errorf("proxy: resolve caddy address: %w", err)
	}

	// Step 1: Get current routes to find the index of the matching host.
	idx, err := c.findRouteIndexLocked(ctx, baseURL, host)
	if err != nil {
		return fmt.Errorf("proxy: find route for host %q: %w", host, err)
	}

	// Step 2: DELETE by position.
	url := fmt.Sprintf("%s/config/apps/http/servers/srv0/routes/%d", baseURL, idx)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("proxy: create delete request: %w", err)
	}

	slog.Info("proxy: removing caddy route", "host", host, "index", idx)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("proxy: caddy api unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("proxy: caddy returned %d on delete: %s", resp.StatusCode, string(respBody))
	}

	slog.Info("proxy: route removed", "host", host, "index", idx)
	return nil
}

// ListRoutes returns all currently configured routes in Caddy's srv0 server.
// It parses the full config and extracts host + upstream dial pairs.
func (c *CaddyClient) ListRoutes(ctx context.Context) ([]Route, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	baseURL, err := c.baseURLResolved()
	if err != nil {
		return nil, fmt.Errorf("proxy: resolve caddy address: %w", err)
	}

	return c.listRoutesLocked(ctx, baseURL)
}

// findRouteIndexLocked returns the position index of the route matching
// the given host. Must be called with c.mu held.
func (c *CaddyClient) findRouteIndexLocked(ctx context.Context, baseURL, host string) (int, error) {
	routes, err := c.listRoutesLocked(ctx, baseURL)
	if err != nil {
		return -1, err
	}

	for i, r := range routes {
		if strings.EqualFold(r.Host, host) {
			return i, nil
		}
	}

	return -1, fmt.Errorf("no route found for host %q", host)
}

// listRoutesLocked fetches and parses all routes from Caddy's srv0.
// Must be called with c.mu held.
func (c *CaddyClient) listRoutesLocked(ctx context.Context, baseURL string) ([]Route, error) {
	url := baseURL + "/config/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("caddy api unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("caddy returned %d: %s", resp.StatusCode, string(respBody))
	}

	var cfg caddyConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode caddy config: %w", err)
	}

	srv, ok := cfg.Apps.HTTP.Servers["srv0"]
	if !ok {
		return nil, nil // no srv0 server — no routes
	}

	var routes []Route
	for _, r := range srv.Routes {
		route := Route{}

		// Extract host from matchers.
		for _, m := range r.Match {
			for _, h := range m.Host {
				route.Host = h
				break
			}
			if route.Host != "" {
				break
			}
		}

		// Extract upstream dial from handlers.
		for _, h := range r.Handle {
			if h.Handler == "reverse_proxy" && len(h.Upstreams) > 0 {
				route.UpstreamDial = h.Upstreams[0].Dial
				break
			}
		}

		if route.Host != "" {
			routes = append(routes, route)
		}
	}

	return routes, nil
}

// SetHTTPClient replaces the internal HTTP client. Used by mTLS code to
// swap between ip-only and certificate-authenticated clients.
func (c *CaddyClient) SetHTTPClient(client *http.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.httpClient = client
}
