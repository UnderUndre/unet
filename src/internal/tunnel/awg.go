package tunnel

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// AWGCli wraps the local `awg-quick` (and `awg`) binaries, providing
// platform-aware helpers for bringing interfaces up/down, syncing
// runtime configuration, and querying interface state.
//
// On Linux the interface name matches the config file basename (e.g. "awg0").
// On macOS awg-quick auto-assigns a utun* name; on Windows a GUID is used.
// After every `Up` call the caller should call `DiscoverInterface` to learn
// the actual OS-level name.
type AWGCli struct {
	// QuickPath is the absolute path to the awg-quick binary discovered at
	// daemon startup. If empty the helper falls back to bare "awg-quick"
	// and relies on the process PATH.
	QuickPath string

	// AwgPath is the absolute path to the awg binary (used for `awg show`,
	// `awg genkey`, `awg pubkey`).
	AwgPath string
}

// NewAWGCli discovers the local awg-quick and awg binaries. It returns an
// error if neither is found on PATH.
func NewAWGCli() (*AWGCli, error) {
	c := &AWGCli{}

	quick, err := exec.LookPath("awg-quick")
	if err != nil {
		if runtime.GOOS == "windows" {
			slog.Info("tunnel: awg-quick not found on Windows (expected, uses GUI/service client)")
		} else {
			return nil, fmt.Errorf("tunnel: awg-quick not found on PATH: %w", err)
		}
	} else {
		c.QuickPath = quick
	}

	awg, err := exec.LookPath("awg")
	if err != nil {
		slog.Warn("tunnel: 'awg' binary not found on PATH; show/genkey operations will fail", "err", err)
	} else {
		c.AwgPath = awg
	}

	slog.Info("tunnel: AWG CLI initialized", "quick", c.QuickPath, "awg", c.AwgPath)
	return c, nil
}

// Up runs `awg-quick up <confPath>` and waits up to timeout for completion.
// confPath is the path to the WireGuard/AmneziaWG .conf file (without the
// .conf extension on some platforms; the caller must provide the correct
// value for the OS).
func (c *AWGCli) Up(ctx context.Context, confPath string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := c.quickCommand(ctx, "up", confPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tunnel: awg-quick up %s: %w\n%s", confPath, err, out)
	}
	slog.Info("tunnel: interface brought up", "conf", confPath)
	return nil
}

// Down runs `awg-quick down <confPath>`.
func (c *AWGCli) Down(ctx context.Context, confPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := c.quickCommand(ctx, "down", confPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Down is best-effort; log but don't hard-fail.
		slog.Warn("tunnel: awg-quick down failed", "conf", confPath, "err", err, "output", string(out))
		return fmt.Errorf("tunnel: awg-quick down %s: %w\n%s", confPath, err, out)
	}
	slog.Info("tunnel: interface brought down", "conf", confPath)
	return nil
}

// Show runs `awg show <iface>` and returns the raw output.  If the
// interface does not exist the error wraps os.ErrNotExist (detected via
// exit-code heuristics).
func (c *AWGCli) Show(ctx context.Context, iface string) (string, error) {
	if c.AwgPath == "" {
		return "", fmt.Errorf("tunnel: awg binary not available")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := c.awgCommand(ctx, "show", iface)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tunnel: awg show %s: %w\n%s", iface, err, out)
	}
	return string(out), nil
}

// ShowDump runs `awg show <iface> dump` and returns the tab-separated dump
// output.  The first line is the interface; subsequent lines are peers.
func (c *AWGCli) ShowDump(ctx context.Context, iface string) (string, error) {
	if c.AwgPath == "" {
		return "", fmt.Errorf("tunnel: awg binary not available")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := c.awgCommand(ctx, "show", iface, "dump")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tunnel: awg show %s dump: %w\n%s", iface, err, out)
	}
	return string(out), nil
}

// Strip runs `awg-quick strip <confPath>` and returns the stripped config
// (runtime state minus [Peer] sections).  This is used locally for the
// syncconf flow when the daemon is running on the same host as the AWG
// interface.
func (c *AWGCli) Strip(ctx context.Context, confPath string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := c.quickCommand(ctx, "strip", confPath)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tunnel: awg-quick strip %s: %w", confPath, err)
	}
	return string(out), nil
}

// GenKey generates a WireGuard private key using `awg genkey`.
func (c *AWGCli) GenKey(ctx context.Context) (string, error) {
	if c.AwgPath == "" {
		return "", fmt.Errorf("tunnel: awg binary not available")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := c.awgCommand(ctx, "genkey")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tunnel: awg genkey: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// PubKey derives the public key from a private key using `awg pubkey`.
func (c *AWGCli) PubKey(ctx context.Context, privateKey string) (string, error) {
	if c.AwgPath == "" {
		return "", fmt.Errorf("tunnel: awg binary not available")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := c.awgCommand(ctx, "pubkey")
	cmd.Stdin = strings.NewReader(privateKey + "\n")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tunnel: awg pubkey: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GenerateKeyPair creates a fresh private/public keypair using the local
// awg binary.  Returns (privateKey, publicKey, error).
func (c *AWGCli) GenerateKeyPair(ctx context.Context) (string, string, error) {
	priv, err := c.GenKey(ctx)
	if err != nil {
		return "", "", err
	}
	pub, err := c.PubKey(ctx, priv)
	if err != nil {
		return "", "", err
	}
	return priv, pub, nil
}

// InterfaceExists checks whether an AWG interface is currently running
// by running `awg show <iface>` and checking for success.
func (c *AWGCli) InterfaceExists(ctx context.Context, iface string) bool {
	_, err := c.Show(ctx, iface)
	return err == nil
}

// DiscoverInterface attempts to find the actual OS-level interface name
// after an `awg-quick up` operation.  On Linux the name equals the conf
// basename (e.g. "awg0").  On macOS it may be utun*.  On Windows it is a
// GUID string.
//
// The caller should supply the expected/conf basename (e.g. "awg0") which
// is used as the first candidate.  If that fails, the method enumerates
// available interfaces.
func (c *AWGCli) DiscoverInterface(ctx context.Context, expected string) (string, error) {
	// Fast path: on Linux the interface name equals the config basename.
	if c.InterfaceExists(ctx, expected) {
		return expected, nil
	}

	switch runtime.GOOS {
	case "darwin":
		// macOS: awg-quick assigns utun* — enumerate utun interfaces.
		return c.discoverDarwin(ctx)
	case "windows":
		// Windows: GUID naming — try to find via `awg show interfaces`.
		return c.discoverWindows(ctx)
	default:
		// Linux and other Unix: should match expected.
		return expected, nil
	}
}

// discoverDarwin enumerates utun* interfaces on macOS.
func (c *AWGCli) discoverDarwin(ctx context.Context) (string, error) {
	if c.AwgPath == "" {
		return "", fmt.Errorf("tunnel: awg binary not available for interface discovery")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := c.awgCommand(ctx, "show", "interfaces")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tunnel: awg show interfaces: %w", err)
	}
	ifaces := strings.TrimSpace(string(out))
	if ifaces == "" {
		return "", fmt.Errorf("tunnel: no AWG interfaces found on macOS")
	}
	// Return the first interface listed.
	return strings.Split(ifaces, "\n")[0], nil
}

// discoverWindows tries to find the AWG interface on Windows.
//
// Known limitation (RV5): AmneziaWG on Windows uses a Windows Service with
// wintun driver, not the Linux-style awg-quick CLI. The discoverer can read
// interface state via `awg show`, but the Up/Down lifecycle must be managed
// through the official AmneziaWG Windows client or via the wireguard-go
// userspace implementation. See:
//   - https://github.com/amnezia-vpn/amnezia-wg-windows
//   - golang.zx2c4.com/wireguard/windows
//
// For MVP, unet on Windows manages config only and expects the user to bring
// the interface up manually via the AmneziaWG client.
func (c *AWGCli) discoverWindows(ctx context.Context) (string, error) {
	if c.AwgPath == "" {
		return "", fmt.Errorf("tunnel: awg binary not available for interface discovery")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := c.awgCommand(ctx, "show", "interfaces")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tunnel: awg show interfaces (Windows): %w", err)
	}
	ifaces := strings.TrimSpace(string(out))
	if ifaces == "" {
		return "", fmt.Errorf("tunnel: no AWG interfaces found on Windows (ensure AmneziaWG client is running)")
	}
	return strings.Split(ifaces, "\n")[0], nil
}

// ---------- helper constructors ----------

func (c *AWGCli) quickCommand(ctx context.Context, args ...string) *exec.Cmd {
	name := c.QuickPath
	if name == "" {
		name = "awg-quick"
	}
	cmd := exec.CommandContext(ctx, name, args...)
	// Ensure the command runs with the real environment (needed for PATH,
	// and on Windows for System32 access).
	cmd.Env = os.Environ()
	return cmd
}

func (c *AWGCli) awgCommand(ctx context.Context, args ...string) *exec.Cmd {
	name := c.AwgPath
	if name == "" {
		name = "awg"
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	return cmd
}
