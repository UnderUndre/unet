package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"log/slog"

	"github.com/underundre/unet/internal/api/adapters"
	"github.com/underundre/unet/internal/api/remote"
	"github.com/underundre/unet/internal/audit"
	"github.com/underundre/unet/internal/auth"
	"github.com/underundre/unet/internal/config"
	"github.com/underundre/unet/internal/daemon"
	"github.com/underundre/unet/internal/proxy"
	"github.com/underundre/unet/internal/provisioner"
	"github.com/underundre/unet/internal/tunnel"
)

var Version = "dev"

func main() {
	var (
		port    int
		showVer bool
	)

	flag.IntVar(&port, "port", 8080, "HTTP listen port for the control API")
	flag.BoolVar(&showVer, "version", false, "Print version and exit")
	flag.Parse()

	if showVer {
		slog.Info("unet", "version", Version)
		os.Exit(0)
	}

	slog.Info("starting unet daemon",
		"version", Version,
		"port", port,
	)

	CheckPrivileges()
	CheckAwgPath()
	cleanup := AcquireLock()
	defer cleanup()

	cfgMgr, err := config.NewManager()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	awgCli, err := tunnel.NewAWGCli()
	if err != nil {
		slog.Error("failed to initialise AWG CLI", "error", err)
		os.Exit(1)
	}

	ReconcileStartupState(cfgMgr, awgCli)

	sshExec := &lazySSHClient{cfgMgr: cfgMgr}

	tunnelMgr := tunnel.NewManager(cfgMgr, awgCli, sshExec)

	caddyClient := proxy.NewCaddyClient(cfgMgr, sshExec)
	dnsManager := proxy.NewDNSManager(cfgMgr)

	srv := daemon.NewServer(port)

	vpsHandler := daemon.NewVPSHandler(cfgMgr, srv)
	vpsHandler.RegisterRoutes()

	tunnelHandler := daemon.NewTunnelHandler(tunnelMgr, cfgMgr, srv)
	tunnelHandler.RegisterRoutes()

	dnsHandler := daemon.NewDNSHandler(cfgMgr, srv)
	dnsHandler.RegisterRoutes()

	portsHandler := daemon.NewPortsHandler(cfgMgr, caddyClient, dnsManager, srv)
	portsHandler.RegisterRoutes()

	srv.HandleFunc("GET /api/ssh/hosts", daemon.HandleSSHHosts)

	remoteSrv := setupRemoteAPI(cfgMgr, tunnelMgr, caddyClient, dnsManager)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		slog.Error("failed to start HTTP server", "error", err)
		os.Exit(1)
	}

	if remoteSrv != nil {
		if err := remoteSrv.Start(); err != nil {
			slog.Error("failed to start remote API server", "error", err)
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	slog.Info("shutting down")
	if remoteSrv != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5)
		defer shutdownCancel()
		remoteSrv.Stop(shutdownCtx)
	}
	cancel()
}

func setupRemoteAPI(cfgMgr *config.Manager, tunnelMgr *tunnel.Manager, caddy *proxy.CaddyClient, dns *proxy.DNSManager) *remote.Server {
	cfg := cfgMgr.Get()
	if !cfg.RemoteAPI.Enabled {
		slog.Info("remote API disabled")
		return nil
	}

	configDir, _ := config.ConfigDir()

	tokenStore := auth.NewStore(cfgMgr.Path())
	tokenCache := auth.NewTokenCache(tokenStore, 0)

	jwtKey := string(cfg.Daemon.JWTSigningKey)
	if jwtKey == "" {
		key, err := auth.GenerateJWTSigningKey()
		if err != nil {
			slog.Error("failed to generate JWT key", "error", err)
			return nil
		}
		if err := cfgMgr.Update(func(c *config.RootConfig) {
			c.Daemon.JWTSigningKey = config.SecretString(key)
		}); err != nil {
			slog.Error("failed to persist JWT key", "error", err)
			return nil
		}
		jwtKey = key
	}

	jwtIssuer, err := auth.NewJWTIssuer(jwtKey)
	if err != nil {
		slog.Error("failed to create JWT issuer", "error", err)
		return nil
	}

	auditPath := filepath.Join(configDir, "audit.jsonl")
	auditLogger, err := audit.NewLogger(auditPath)
	if err != nil {
		slog.Error("failed to create audit logger", "error", err)
		return nil
	}

	tunnelAdapter := adapters.NewTunnelAdapter(tunnelMgr, cfgMgr)
	routeAdapter := adapters.NewRouteAdapter(cfgMgr, caddy, dns)
	peerAdapter := adapters.NewPeerAdapter(cfgMgr, tunnelMgr)

	handler := remote.RegisterRoutes(&remote.Dependencies{
		TokenStore: tokenStore,
		TokenCache: tokenCache,
		JWTIssuer:  jwtIssuer,
		AuditLog:   auditLogger,
		AuditPath:  auditPath,
		Peers:      peerAdapter,
		Routes:     routeAdapter,
		Tunnel:     tunnelAdapter,
	})

	return remote.NewServer(cfgMgr, handler)
}

type lazySSHClient struct {
	cfgMgr *config.Manager
	mu     sync.Mutex
	client *provisioner.Client
}

func (l *lazySSHClient) ensureClient(ctx context.Context) (*provisioner.Client, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.client != nil {
		return l.client, nil
	}

	cfg := l.cfgMgr.Get()
	if cfg.VPS.Host == "" {
		return nil, fmt.Errorf("ssh: VPS not configured")
	}

	client, err := provisioner.NewClient(cfg.VPS)
	if err != nil {
		return nil, fmt.Errorf("ssh: create client: %w", err)
	}
	l.client = client
	return l.client, nil
}

func (l *lazySSHClient) ExecuteCommand(ctx context.Context, cmd string) (string, string, error) {
	c, err := l.ensureClient(ctx)
	if err != nil {
		return "", "", err
	}
	return c.ExecuteCommand(ctx, cmd)
}

func (l *lazySSHClient) ExecuteScript(ctx context.Context, script string) (string, string, error) {
	c, err := l.ensureClient(ctx)
	if err != nil {
		return "", "", err
	}
	return c.ExecuteScript(ctx, script)
}
