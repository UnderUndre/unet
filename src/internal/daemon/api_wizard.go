package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/underundre/unet/internal/config"
	"github.com/underundre/unet/internal/invite"
	"github.com/underundre/unet/internal/logstream"
	"github.com/underundre/unet/internal/qr"
	"github.com/underundre/unet/internal/routes"
	"github.com/underundre/unet/internal/tunnel"
	"github.com/underundre/unet/internal/wizard"
	"github.com/underundre/unet/internal/wizard/dnscheck"
)

func wizardDataDir() (string, error) {
	cfgDir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfgDir, "wizard"), nil
}

type daemonSSHPool struct{}

func (p *daemonSSHPool) Connect(_ context.Context, _ wizard.SSHConfig) (wizard.SSHSession, error) {
	return nil, fmt.Errorf("wizard SSH pool: not yet implemented — use existing ssh.Pool")
}

func RegisterWizardRoutes(srv *Server, cfgMgr *config.Manager, tunnelMgr *tunnel.Manager, logHub *logstream.Hub) {
	dataDir, err := wizardDataDir()
	if err != nil {
		slog.Error("daemon: failed to resolve wizard data dir", "error", err)
		dataDir = filepath.Join(".", "wizard")
	}

	sshPool := &daemonSSHPool{}

	bootstrapDeps := wizard.BootstrapDeps{
		DataDir: dataDir,
		SSHPool: sshPool,
		LogHub:  logHub,
	}

	wizardMux := srv.apiMux

	vpsIP := ""
	cfg := cfgMgr.Get()
	if cfg.VPS.Host != "" {
		vpsIP = resolveVPSPublicIP(cfg.VPS.Host)
	}

	wizard.RegisterRoutes(wizardMux, dataDir, sshPool, bootstrapDeps, &dnscheck.DefaultResolver{}, vpsIP)

	qr.RegisterRoutes(wizardMux)

	var storeErr error
	inviteStore, storeErr := invite.NewStore(dataDir)
	if storeErr != nil {
		slog.Error("daemon: failed to create invite store", "error", storeErr)
	} else {
		cfg := cfgMgr.Get()
		daemonSecret := string(cfg.UIToken.Plain())
		if daemonSecret == "" {
			daemonSecret = "default-daemon-secret"
		}
		inviteHandler := invite.NewHandler(inviteStore, daemonSecret)
		invite.RegisterRoutes(wizardMux, inviteHandler)
	}

	routesHandler := routes.NewHandler(cfgMgr, &dnsAdapter{cfgMgr: cfgMgr}, &tunnelAdapter{mgr: tunnelMgr})
	if tunnelMgr != nil {
		cfg := cfgMgr.Get()
		if cfg.VPS.Host != "" {
			routesHandler.SetVPSPublicIP(resolveVPSPublicIP(cfg.VPS.Host))
		}
	}
	routes.RegisterRoutes(wizardMux, routesHandler)

	slog.Info("daemon: wizard routes registered",
		"data_dir", dataDir,
		"invite_store_ok", storeErr == nil,
	)
}

func resolveVPSPublicIP(host string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil || len(addrs) == 0 {
		slog.Warn("daemon: could not resolve VPS host", "host", host, "error", err)
		return ""
	}
	return addrs[0]
}

type dnsAdapter struct {
	cfgMgr *config.Manager
}

func (d *dnsAdapter) UpsertRecord(ctx context.Context, subdomain, ip string) error {
	slog.Info("daemon/dns-adapter: upsert record", "subdomain", subdomain, "ip", ip)
	return nil
}

func (d *dnsAdapter) DeleteRecord(ctx context.Context, subdomain string) error {
	slog.Info("daemon/dns-adapter: delete record", "subdomain", subdomain)
	return nil
}

type tunnelAdapter struct {
	mgr *tunnel.Manager
}

func (t *tunnelAdapter) IsConnected() bool {
	if t.mgr == nil {
		return false
	}
	return t.mgr.IsConnected()
}

func writeWizardError(w http.ResponseWriter, code int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   errType,
		"message": message,
	})
}
