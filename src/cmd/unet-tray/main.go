// unet-tray is the system tray application for unet. It runs as a separate
// process, communicating with the unet daemon via its localhost HTTP API.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/underundre/unet/internal/traylogic"
)

var Version = "dev"

func main() {
	slog.Info("starting unet-tray", "version", Version)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Singleton guard — abort if another tray instance is running.
	if !traylogic.AcquireSingletonLock() {
		slog.Error("another unet-tray instance is already running")
		os.Exit(1)
	}
	defer traylogic.ReleaseSingletonLock()

	// Start the tray logic orchestrator.
	app, err := traylogic.NewApp(Version)
	if err != nil {
		slog.Error("failed to initialize tray", "error", err)
		os.Exit(1)
	}

	go func() {
		if err := app.Run(ctx); err != nil {
			slog.Error("tray run error", "error", err)
			cancel()
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
		slog.Info("shutting down unet-tray")
	case <-ctx.Done():
	}
	cancel()
}
