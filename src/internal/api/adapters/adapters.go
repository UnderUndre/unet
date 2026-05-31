package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/underundre/unet/internal/api/v1"
	"github.com/underundre/unet/internal/config"
	"github.com/underundre/unet/internal/proxy"
	"github.com/underundre/unet/internal/tunnel"
)

type TunnelAdapter struct {
	mgr    *tunnel.Manager
	cfgMgr *config.Manager
}

func NewTunnelAdapter(mgr *tunnel.Manager, cfgMgr *config.Manager) *TunnelAdapter {
	return &TunnelAdapter{mgr: mgr, cfgMgr: cfgMgr}
}

func (a *TunnelAdapter) Status() string {
	return a.mgr.Status()
}

func (a *TunnelAdapter) IsConnected() bool {
	return a.mgr.IsConnected()
}

func (a *TunnelAdapter) GetConfig() *v1.TunnelConfigView {
	cfg := a.cfgMgr.Get()
	return &v1.TunnelConfigView{
		LocalIP:        cfg.Tunnel.LocalIP,
		ServerIP:       cfg.Tunnel.ServerIP,
		ServerEndpoint: cfg.Tunnel.ServerEndpoint,
		Status:         cfg.Tunnel.Status,
		ConnectedAt:    cfg.Tunnel.ConnectedAt,
	}
}

type RouteAdapter struct {
	cfgMgr *config.Manager
	caddy  *proxy.CaddyClient
	dns    *proxy.DNSManager
}

func NewRouteAdapter(cfgMgr *config.Manager, caddy *proxy.CaddyClient, dns *proxy.DNSManager) *RouteAdapter {
	return &RouteAdapter{cfgMgr: cfgMgr, caddy: caddy, dns: dns}
}

func (a *RouteAdapter) List(ctx context.Context) ([]v1.RouteView, error) {
	cfg := a.cfgMgr.Get()
	baseDomain := cfg.DNS.Zone

	result := make([]v1.RouteView, 0, len(cfg.ExposedPorts))
	for _, p := range cfg.ExposedPorts {
		fqdn := p.HostHeader
		if baseDomain != "" {
			fqdn = p.HostHeader + "." + baseDomain
		}
		result = append(result, v1.RouteView{
			ID:        p.ID,
			Subdomain: p.HostHeader,
			LocalPort: p.Internal,
			Status:    p.Status,
			FQDN:      fqdn,
			CreatedAt: p.CreatedAt,
		})
	}
	return result, nil
}

func (a *RouteAdapter) Create(ctx context.Context, subdomain string, port int) (*v1.RouteView, error) {
	cfg := a.cfgMgr.Get()
	if cfg.Tunnel.Status != "connected" {
		return nil, fmt.Errorf("tunnel not connected")
	}

	for _, ep := range cfg.ExposedPorts {
		if ep.HostHeader == subdomain {
			return nil, fmt.Errorf("subdomain conflict: %s already in use", subdomain)
		}
	}

	routeID := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)
	status := "active"

	localIP := cfg.Tunnel.LocalIP
	targetAddr := fmt.Sprintf("127.0.0.1:%d", port)
	px := tunnel.NewLocalTCPProxy(localIP, targetAddr)
	proxyPort, err := px.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start local proxy: %w", err)
	}

	upstreamDial := fmt.Sprintf("%s:%d", localIP, proxyPort)
	host := subdomain
	baseDomain := cfg.DNS.Zone
	if baseDomain != "" && !strings.HasSuffix(subdomain, baseDomain) {
		host = subdomain + "." + baseDomain
	}

	if err := a.caddy.AddRoute(ctx, host, upstreamDial); err != nil {
		status = "error"
	}

	dnsCreated := false
	if status == "active" {
		if err := a.dns.CreateRecord(ctx, subdomain); err != nil {
			status = "error"
		} else {
			dnsCreated = true
		}
	}

	newPort := config.ExposedPort{
		ID:         routeID,
		Protocol:   "http",
		Internal:   port,
		HostHeader: subdomain,
		Status:     status,
		CreatedAt:  now,
	}

	dup := false
	if err := a.cfgMgr.Update(func(c *config.RootConfig) {
		for _, ep := range c.ExposedPorts {
			if ep.HostHeader == subdomain {
				dup = true
				return
			}
		}
		c.ExposedPorts = append(c.ExposedPorts, newPort)
	}); err != nil {
		a.caddy.RemoveRoute(ctx, host)
		if dnsCreated {
			a.dns.DeleteRecord(ctx, subdomain)
		}
		return nil, fmt.Errorf("failed to save route: %w", err)
	}

	if dup {
		a.caddy.RemoveRoute(ctx, host)
		if dnsCreated {
			a.dns.DeleteRecord(ctx, subdomain)
		}
		return nil, fmt.Errorf("subdomain conflict: %s already in use", subdomain)
	}

	fqdn := subdomain
	if baseDomain != "" {
		fqdn = subdomain + "." + baseDomain
	}

	return &v1.RouteView{
		ID:        routeID,
		Subdomain: subdomain,
		LocalPort: port,
		Status:    status,
		FQDN:      fqdn,
		CreatedAt: now,
	}, nil
}

func (a *RouteAdapter) Delete(ctx context.Context, id string) error {
	cfg := a.cfgMgr.Get()
	var target *config.ExposedPort
	for i := range cfg.ExposedPorts {
		if cfg.ExposedPorts[i].ID == id {
			t := cfg.ExposedPorts[i]
			target = &t
			break
		}
	}

	if target == nil {
		return fmt.Errorf("route not found: %s", id)
	}

	host := target.HostHeader
	baseDomain := cfg.DNS.Zone
	if baseDomain != "" && !strings.HasSuffix(target.HostHeader, baseDomain) {
		host = target.HostHeader + "." + baseDomain
	}

	a.caddy.RemoveRoute(ctx, host)
	a.dns.DeleteRecord(ctx, target.HostHeader)

	return a.cfgMgr.Update(func(c *config.RootConfig) {
		idx := -1
		for i, ep := range c.ExposedPorts {
			if ep.ID == id {
				idx = i
				break
			}
		}
		if idx >= 0 {
			c.ExposedPorts = append(c.ExposedPorts[:idx], c.ExposedPorts[idx+1:]...)
		}
	})
}

type PeerAdapter struct {
	cfgMgr *config.Manager
	mgr    *tunnel.Manager
}

func NewPeerAdapter(cfgMgr *config.Manager, mgr *tunnel.Manager) *PeerAdapter {
	return &PeerAdapter{cfgMgr: cfgMgr, mgr: mgr}
}

func (a *PeerAdapter) List(ctx context.Context) ([]v1.PeerView, error) {
	cfg := a.cfgMgr.Get()
	mirror, err := tunnel.ParseServerMirror(cfg.ServerMirror)
	if err != nil {
		return []v1.PeerView{}, nil
	}

	if mirror.ClientsTable == nil {
		return []v1.PeerView{}, nil
	}

	var clients []tunnel.ClientEntry
	if err := json.Unmarshal(mirror.ClientsTable, &clients); err != nil {
		return []v1.PeerView{}, nil
	}

	result := make([]v1.PeerView, 0, len(clients))
	for _, c := range clients {
		result = append(result, v1.PeerView{
			ID:         c.ClientID,
			Name:       c.UserData.ClientName,
			CreatedVia: "api",
			CreatedAt:  c.UserData.CreationDate,
		})
	}

	return result, nil
}

func (a *PeerAdapter) GetByID(ctx context.Context, id string) (*v1.PeerDetailView, error) {
	peers, err := a.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, p := range peers {
		if p.ID == id || strings.HasPrefix(p.Name, id) {
			return &v1.PeerDetailView{PeerView: p}, nil
		}
	}

	return nil, fmt.Errorf("peer not found: %s", id)
}

func (a *PeerAdapter) Create(ctx context.Context, name string) (*v1.PeerDetailView, error) {
	if !a.mgr.IsConnected() {
		return nil, fmt.Errorf("tunnel not connected — cannot create peer")
	}

	pubKey, err := a.mgr.AddPeerForRemote(ctx, name)
	if err != nil {
		return nil, err
	}

	cfg := a.cfgMgr.Get()
	subnet := cfg.Tunnel.Subnet
	if subnet == "" {
		return &v1.PeerDetailView{
			PeerView: v1.PeerView{
				ID:         pubKey,
				Name:       name,
				CreatedVia: "api",
				CreatedAt:  time.Now().UTC().Format(time.RFC3339),
			},
		}, nil
	}

	return &v1.PeerDetailView{
		PeerView: v1.PeerView{
			ID:         pubKey,
			Name:       name,
			CreatedVia: "api",
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		},
	}, nil
}

func (a *PeerAdapter) Delete(ctx context.Context, id string) error {
	if !a.mgr.IsConnected() {
		return fmt.Errorf("tunnel not connected — cannot delete peer")
	}

	return a.mgr.RemovePeerByPublicKey(ctx, id)
}
