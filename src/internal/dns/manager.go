package dns

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/underundre/unet/internal/config"
)

type CertStrategy string

const (
	CertStrategyWildcardDNS01 CertStrategy = "wildcard_dns01"
	CertStrategyHTTP01        CertStrategy = "http01"
)

type Manager struct {
	cfgMgr   *config.Manager
	cfClient *CloudflareClient
}

func NewManager(cfgMgr *config.Manager) *Manager {
	return &Manager{cfgMgr: cfgMgr}
}

func (m *Manager) cf() *CloudflareClient {
	if m.cfClient == nil {
		m.cfClient = NewCloudflareClient(m.cfgMgr.Get().DNS.Token.Plain())
	}
	return m.cfClient
}

func CertStrategyForMode(mode string) CertStrategy {
	switch mode {
	case "cloudflare":
		return CertStrategyWildcardDNS01
	default:
		return CertStrategyHTTP01
	}
}

func (m *Manager) CaddyTLSConfig(mode, zone string) (map[string]interface{}, error) {
	switch mode {
	case "cloudflare":
		token := m.cfgMgr.Get().DNS.Token.Plain()
		return map[string]interface{}{
			"issuer": map[string]interface{}{
				"module":     "acme",
				"challenges": []string{"dns-01"},
				"provider": map[string]interface{}{
					"name":      "cloudflare",
					"api_token": token,
				},
			},
			"subject": []string{"*." + zone},
		}, nil
	default:
		cfg := m.cfgMgr.Get()
		exposed := countExposed(cfg.ExposedPorts)
		if exposed > 5 {
			slog.Warn("dns/manager: manual mode with many subdomains may hit Let's Encrypt rate limits",
				"zone", zone,
				"exposed_count", exposed,
				"hint", "consider wildcard cert with DNS-01 challenge instead",
			)
		}
		return map[string]interface{}{
			"issuer": map[string]interface{}{
				"module":     "acme",
				"challenges": []string{"http-01"},
			},
		}, nil
	}
}

func (m *Manager) UpsertRecord(ctx context.Context, subdomain, ip string) error {
	cfg := m.cfgMgr.Get()
	mode := cfg.DNS.Provider

	switch mode {
	case "cloudflare":
		if err := m.cf().ValidateTokenScopes(ctx); err != nil {
			return fmt.Errorf("dns/manager: upsert: %w", err)
		}
		zoneID, err := m.cf().GetZoneID(ctx, cfg.DNS.Zone)
		if err != nil {
			return fmt.Errorf("dns/manager: upsert: %w", err)
		}
		fqdn := subdomain + "." + cfg.DNS.Zone
		if err := m.cf().UpsertARecord(ctx, zoneID, fqdn, ip); err != nil {
			return fmt.Errorf("dns/manager: upsert: %w", err)
		}
		slog.Info("dns/manager: upserted A record", "fqdn", fqdn, "ip", ip)
		return nil
	default:
		domain := cfg.DNS.Zone
		if domain == "" {
			slog.Warn("dns/manager: manual mode but no zone configured, cannot validate")
			return nil
		}
		fqdn := subdomain
		if !endsWith(subdomain, domain) {
			fqdn = subdomain + "." + domain
		}
		resolver := &net.Resolver{}
		ips, err := resolver.LookupHost(ctx, fqdn)
		if err != nil || len(ips) == 0 {
			slog.Warn("dns/manager: manual mode: subdomain not resolvable",
				"fqdn", fqdn,
				"hint", "ensure wildcard A-record *.<zone> is configured",
			)
			return nil
		}
		slog.Info("dns/manager: manual mode: subdomain resolves", "fqdn", fqdn, "ips", ips)
		return nil
	}
}

func (m *Manager) DeleteRecord(ctx context.Context, subdomain string) error {
	cfg := m.cfgMgr.Get()
	mode := cfg.DNS.Provider

	switch mode {
	case "cloudflare":
		zoneID, err := m.cf().GetZoneID(ctx, cfg.DNS.Zone)
		if err != nil {
			return fmt.Errorf("dns/manager: delete: %w", err)
		}
		fqdn := subdomain + "." + cfg.DNS.Zone
		records, err := m.cf().ListRecords(ctx, zoneID, fqdn, "A")
		if err != nil {
			return fmt.Errorf("dns/manager: delete: find record: %w", err)
		}
		if len(records) == 0 {
			slog.Warn("dns/manager: no A record found to delete", "fqdn", fqdn)
			return nil
		}
		for _, rec := range records {
			if err := m.cf().DeleteRecord(ctx, zoneID, rec.ID); err != nil {
				return fmt.Errorf("dns/manager: delete record %s: %w", rec.ID, err)
			}
		}
		slog.Info("dns/manager: deleted A record(s)", "fqdn", fqdn, "count", len(records))
		return nil
	default:
		slog.Debug("dns/manager: manual mode: delete is no-op", "subdomain", subdomain)
		return nil
	}
}

func (m *Manager) ValidateConfig(ctx context.Context) error {
	cfg := m.cfgMgr.Get()
	mode := cfg.DNS.Provider

	switch mode {
	case "cloudflare":
		if cfg.DNS.Token == "" {
			return fmt.Errorf("dns/manager: cloudflare mode requires a token")
		}
		if cfg.DNS.Zone == "" {
			return fmt.Errorf("dns/manager: cloudflare mode requires a zone")
		}
		slog.Info("dns/manager: validating cloudflare config",
			"zone", cfg.DNS.Zone,
			"token", maskToken(cfg.DNS.Token),
		)
		if err := m.cf().ValidateToken(ctx); err != nil {
			return fmt.Errorf("dns/manager: validate config: %w", err)
		}
		if err := m.cf().ValidateTokenScopes(ctx); err != nil {
			return fmt.Errorf("dns/manager: validate config: %w", err)
		}
		if _, err := m.cf().GetZoneID(ctx, cfg.DNS.Zone); err != nil {
			return fmt.Errorf("dns/manager: validate config: %w", err)
		}
		slog.Info("dns/manager: cloudflare config valid")
		return nil
	default:
		if cfg.DNS.Zone == "" {
			slog.Warn("dns/manager: manual mode with no zone configured")
		}
		return nil
	}
}

func RateLimitWarning(mode string, exposedCount int) string {
	if mode != "cloudflare" && exposedCount > 5 {
		return fmt.Sprintf(
			"manual DNS mode with %d exposed subdomains may exceed Let's Encrypt HTTP-01 rate limits (~50 certs/week). Consider switching to cloudflare mode for DNS-01 wildcard certs.",
			exposedCount,
		)
	}
	return ""
}

func countExposed(ports []config.ExposedPort) int {
	seen := make(map[string]struct{}, len(ports))
	for _, p := range ports {
		if p.HostHeader != "" {
			seen[p.HostHeader] = struct{}{}
		}
	}
	return len(seen)
}

func maskToken(s config.SecretString) string {
	v := s.Plain()
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
