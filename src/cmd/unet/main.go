// Package main is the entrypoint for the unet daemon.
//
// Unet is a self-hosted Ngrok/Tailscale alternative built on
// AmneziaWG (WireGuard) and Caddy reverse-proxy.
package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"log/slog"

	"github.com/underundre/unet/internal/config"
	"github.com/underundre/unet/internal/tunnel"
)

// Version is set at build time via -ldflags "-X main.Version=...".
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

	// Startup checks.
	CheckPrivileges()
	CheckAwgPath()
	cleanup := AcquireLock()
	defer cleanup()

	// Initialise subsystems.
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

	// Reconcile persisted tunnel state with reality.
	ReconcileStartupState(cfgMgr, awgCli)

	// TODO: initialise daemon, DNS, tunnel, and proxy subsystems.

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	slog.Info("shutting down")
}
