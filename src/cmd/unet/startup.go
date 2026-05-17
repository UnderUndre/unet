package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"log/slog"

	"github.com/underundre/unet/internal/config"
	"github.com/underundre/unet/internal/tunnel"
)

// CheckPrivileges verifies the process is running with elevated privileges.
// On Linux it checks for UID 0 (root). On Windows it checks for administrator
// group membership via golang.org/x/sys/windows. Exits with a clear error if
// privileges are insufficient.
func CheckPrivileges() {
	switch runtime.GOOS {
	case "windows":
		checkPrivilegesWindows()
	case "linux", "darwin", "freebsd":
		checkPrivilegesPOSIX()
	default:
		slog.Warn("privilege check not implemented for platform, continuing anyway", "os", runtime.GOOS)
	}
}

func checkPrivilegesPOSIX() {
	if os.Getuid() != 0 {
		slog.Error("unet must run as root (uid 0)")
		fmt.Fprintln(os.Stderr, "Error: unet requires root privileges. Re-run with sudo or as root.")
		os.Exit(1)
	}
	slog.Debug("running with root privileges")
}

// CheckAwgPath verifies that awg-quick (AmneziaWG) is installed and reachable
// on the current PATH. Exits with a clear installation hint if not found.
func CheckAwgPath() {
	path, err := exec.LookPath("awg-quick")
	if err != nil {
		slog.Error("awg-quick not found on PATH")
		fmt.Fprintln(os.Stderr, "Error: awg-quick not found. Install the AmneziaWG client first.")
		fmt.Fprintln(os.Stderr, "  See: https://amnezia.org/en/downloads")
		os.Exit(1)
	}
	slog.Info("found awg-quick", "path", path)
}

// AcquireLock ensures only one instance of unet is running at a time.
// On POSIX it uses a pidfile at ~/.unet/unet.pid. On Windows it uses a
// named mutex. Returns a cleanup function the caller should defer.
func AcquireLock() func() {
	switch runtime.GOOS {
	case "windows":
		return acquireLockWindows()
	default:
		return acquireLockPIDFile()
	}
}

func acquireLockPIDFile() func() {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("cannot determine home directory", "error", err)
		fmt.Fprintln(os.Stderr, "Error: cannot determine home directory for lock file.")
		os.Exit(1)
	}

	lockDir := filepath.Join(home, ".unet")
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		slog.Error("cannot create lock directory", "path", lockDir, "error", err)
		fmt.Fprintf(os.Stderr, "Error: cannot create %s: %v\n", lockDir, err)
		os.Exit(1)
	}

	pidFile := filepath.Join(lockDir, "unet.pid")

	// Check for existing pidfile.
	data, err := os.ReadFile(pidFile)
	if err == nil {
		// File exists — check if the process is still alive.
		existingPID, parseErr := strconv.Atoi(string(data))
		if parseErr == nil {
			proc, _ := os.FindProcess(existingPID)
			// On Unix, FindProcess always succeeds; send signal 0 to check liveness.
			if proc.Signal(syscall.Signal(0)) == nil {
				slog.Error("another instance is already running", "pid", existingPID)
				fmt.Fprintf(os.Stderr, "Error: unet is already running (pid %d). Remove %s if stale.\n", existingPID, pidFile)
				os.Exit(1)
			}
			// Stale pidfile — fall through to overwrite.
			slog.Warn("stale pidfile found, overwriting", "path", pidFile, "old_pid", existingPID)
		}
	}

	// Write current PID.
	pid := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0644); err != nil {
		slog.Error("cannot write pidfile", "path", pidFile, "error", err)
		fmt.Fprintf(os.Stderr, "Error: cannot write %s: %v\n", pidFile, err)
		os.Exit(1)
	}

	slog.Debug("acquired pidfile lock", "path", pidFile, "pid", pid)

	return func() {
		if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove pidfile on shutdown", "path", pidFile, "error", err)
		}
	}
}

// ReconcileStartupState checks that persisted tunnel status matches reality.
// If config says "connected" but the AWG interface is gone (e.g. machine
// rebooted or awg-quick was run manually), it resets the status to
// "disconnected" so the daemon doesn't operate on a stale assumption.
func ReconcileStartupState(cfgMgr *config.Manager, awg *tunnel.AWGCli) {
	cfg := cfgMgr.Get()
	if cfg.Tunnel.Status != "connected" || cfg.Tunnel.InterfaceName == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if awg.InterfaceExists(ctx, cfg.Tunnel.InterfaceName) {
		return
	}

	slog.Warn("startup: tunnel marked connected but interface missing, resetting",
		"interface", cfg.Tunnel.InterfaceName)

	if err := cfgMgr.Update(func(c *config.RootConfig) {
		c.Tunnel.Status = "disconnected"
		c.Tunnel.ConnectedAt = ""
	}); err != nil {
		slog.Error("startup: failed to persist tunnel reconciliation", "error", err)
	}
}
