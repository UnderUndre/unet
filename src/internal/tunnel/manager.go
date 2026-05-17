package tunnel

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/underundre/unet/internal/config"
)

// Manager orchestrates the full tunnel lifecycle: reading server state,
// generating client-side configuration, bringing the local AWG interface
// up/down, running a periodic health watchdog, and persisting the server
// mirror for drift detection and volume-loss recovery.
//
// It composes:
//   - AWGCli (T012) for local awg-quick operations
//   - ServerConfigParser (T013a) for SSH-based server config reads
//   - PeerManager (T013b) for SSH-based peer add/remove
type Manager struct {
	cfgMgr  *config.Manager
	awg     *AWGCli
	parser  *ServerConfigParser
	peerMgr *PeerManager

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}

	// status is the current tunnel status string:
	// "disconnected", "connecting", "connected", "error"
	status string
}

// NewManager creates a new tunnel Manager.  The AWGCli, ServerConfigParser,
// and PeerManager are composed from the provided dependencies.
func NewManager(
	cfgMgr *config.Manager,
	awg *AWGCli,
	sshClient SSHExecutor,
) *Manager {
	return &Manager{
		cfgMgr:  cfgMgr,
		awg:     awg,
		parser:  NewServerConfigParser(sshClient),
		peerMgr: NewPeerManager(sshClient),
		status:  "disconnected",
	}
}

// Connect performs the full tunnel connection flow:
//  1. Read server state via SSH (T013a)
//  2. Allocate a peer IP
//  3. Generate a client keypair (if not already present)
//  4. Add the peer on the server via SSH (T013b)
//  5. Render and write the local client .conf file
//  6. Bring the interface up via awg-quick (T012)
//  7. Persist the server mirror
//  8. Start the health watchdog
func (m *Manager) Connect(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("tunnel: already connected or connecting")
	}
	m.setStatus("connecting")
	m.mu.Unlock()

	cfg := m.cfgMgr.Get()
	vps := cfg.VPS

	if vps.Host == "" {
		return m.fail("VPS host not configured")
	}
	if !vps.IsProvisioned {
		return m.fail("VPS not provisioned")
	}

	// Step 1 — Read server state.
	srvCfg, err := m.parser.FetchAll(ctx, vps, "awg0")
	if err != nil {
		return m.fail(fmt.Sprintf("fetch server state: %v", err))
	}

	// Step 2 — Generate client keypair if not already present.
	privKey := cfg.Tunnel.PrivateKey.Plain()
	pubKey := cfg.Tunnel.PublicKey
	if privKey == "" || pubKey == "" {
		priv, pub, err := m.awg.GenerateKeyPair(ctx)
		if err != nil {
			return m.fail(fmt.Sprintf("generate keypair: %v", err))
		}
		privKey = priv
		pubKey = pub
		slog.Info("tunnel: generated new keypair", "pub", truncateKey(pub))
	}

	// Step 3 — Allocate next free IP.
	subnet := deriveSubnet(srvCfg.Address)
	if subnet == "" {
		return m.fail("could not derive subnet from server address")
	}
	localIP := cfg.Tunnel.LocalIP
	if localIP == "" {
		localIP, err = AllocateNextIP(subnet, srvCfg.Peers)
		if err != nil {
			return m.fail(fmt.Sprintf("allocate IP: %v", err))
		}
		slog.Info("tunnel: allocated local IP", "ip", localIP)
	}

	// Step 4 — Add peer on the server (if not already added).
	// Check if our pubkey is already on the server.
	alreadyAdded := false
	for _, p := range srvCfg.Peers {
		if p.PublicKey == pubKey {
			alreadyAdded = true
			break
		}
	}

	var ctJSON json.RawMessage
	if !alreadyAdded {
		addResult, err := m.peerMgr.AddPeer(ctx, AddPeerParams{
			VPS:                vps,
			Interface:          "awg0",
			ClientPublicKey:    pubKey,
			ClientIP:           localIP,
			PresharedKey:       srvCfg.PresharedKey,
			ClientName:         hostnameDefault(),
			PersistentKeepalive: 25,
		})
		if err != nil {
			return m.fail(fmt.Sprintf("add peer: %v", err))
		}
		ctJSON = addResult.ClientsTableJSON
	} else {
		slog.Info("tunnel: peer already on server, skipping add")
	}

	// Step 5 — Render and write the local client .conf file.
	endpoint := fmt.Sprintf("%s:%d", vps.Host, srvCfg.ListenPort)
	confPath, err := m.writeClientConf(ClientConfParams{
		InterfaceName:  "awg0",
		PrivateKey:     privKey,
		Address:        localIP + "/24",
		ServerPublicKey: srvCfg.ServerPublicKey,
		Endpoint:       endpoint,
		PresharedKey:   srvCfg.PresharedKey,
		MTU:            1280,
		PersistentKeepalive: 25,
		Obfuscation:    srvCfg.Obfuscation,
	})
	if err != nil {
		return m.fail(fmt.Sprintf("write client conf: %v", err))
	}

	// Step 6 — Bring the interface up.
	if err := m.awg.Up(ctx, confPath, 30*time.Second); err != nil {
		return m.fail(fmt.Sprintf("awg-quick up: %v", err))
	}

	// Discover the actual interface name (macOS/Windows may differ).
	ifaceName, err := m.awg.DiscoverInterface(ctx, "awg0")
	if err != nil {
		slog.Warn("tunnel: could not discover interface name, using default", "err", err)
		ifaceName = "awg0"
	}

	// Step 7 — Persist everything to config.
	if err := m.cfgMgr.Update(func(c *config.RootConfig) {
		c.Tunnel.InterfaceName = ifaceName
		c.Tunnel.Subnet = subnet
		c.Tunnel.ServerIP = deriveServerIP(srvCfg.Address)
		c.Tunnel.LocalIP = localIP
		c.Tunnel.ServerEndpoint = endpoint
		c.Tunnel.ServerPublicKey = srvCfg.ServerPublicKey
		c.Tunnel.PresharedKey = config.SecretString(srvCfg.PresharedKey)
		c.Tunnel.PrivateKey = config.SecretString(privKey)
		c.Tunnel.PublicKey = pubKey
		c.Tunnel.MTU = 1280
		c.Tunnel.PersistentKeepalive = 25
		c.Tunnel.Obfuscation = srvCfg.Obfuscation
		c.Tunnel.Status = "connected"
		c.Tunnel.ConnectedAt = time.Now().UTC().Format(time.RFC3339)

		// Update server mirror.
		mirror := ServerMirrorJSON{
			LastSyncedAt:  time.Now().UTC().Format(time.RFC3339),
			AwgConfRaw:    srvCfg.RawConf,
			AwgConfSha256: srvCfg.ConfSha256,
		}
		if ctJSON != nil {
			mirror.ClientsTable = ctJSON
		}
		mirrorJSON, _ := SerializeServerMirror(&mirror)
		c.ServerMirror = mirrorJSON
	}); err != nil {
		slog.Error("tunnel: failed to persist config", "err", err)
	}

	// Step 8 — Start the health watchdog.
	m.startWatchdog()

	m.mu.Lock()
	m.running = true
	m.status = "connected"
	m.mu.Unlock()

	slog.Info("tunnel: connected",
		"interface", ifaceName,
		"localIP", localIP,
		"endpoint", endpoint,
	)
	return nil
}

// Disconnect brings the tunnel down gracefully:
//  1. Stop the watchdog
//  2. Run awg-quick down
//  3. Update config status
func (m *Manager) Disconnect() error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	// Stop the watchdog.
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()

	// Wait for watchdog to finish.
	if m.done != nil {
		<-m.done
	}

	_ = m.cfgMgr.Get()
	confPath, err := m.clientConfPath()
	if err != nil {
		slog.Warn("tunnel: could not resolve conf path for disconnect", "err", err)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := m.awg.Down(ctx, confPath); err != nil {
			slog.Warn("tunnel: awg-quick down failed", "err", err)
		}
	}

	// Update config status.
	_ = m.cfgMgr.Update(func(c *config.RootConfig) {
		c.Tunnel.Status = "disconnected"
		c.Tunnel.ConnectedAt = ""
	})

	m.mu.Lock()
	m.running = false
	m.status = "disconnected"
	m.mu.Unlock()

	slog.Info("tunnel: disconnected")
	return nil
}

// Status returns the current tunnel status string.
func (m *Manager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// IsConnected returns true if the tunnel is currently up.
func (m *Manager) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// CheckDrift compares the remote awg0.conf hash with the persisted mirror
// hash.  Returns nil if they match, or an error describing the drift.
func (m *Manager) CheckDrift(ctx context.Context) error {
	cfg := m.cfgMgr.Get()
	vps := cfg.VPS

	mirror, err := ParseServerMirror(cfg.ServerMirror)
	if err != nil {
		return fmt.Errorf("tunnel: parse mirror: %w", err)
	}
	if mirror.AwgConfSha256 == "" {
		slog.Debug("tunnel: no mirror hash to compare, skipping drift check")
		return nil
	}

	remoteHash, err := m.parser.FetchConfHash(ctx, vps)
	if err != nil {
		return fmt.Errorf("tunnel: fetch remote hash: %w", err)
	}

	if remoteHash != mirror.AwgConfSha256 {
		return fmt.Errorf("tunnel: drift detected (remote=%s local=%s)",
			remoteHash[:16], mirror.AwgConfSha256[:16])
	}
	return nil
}

// ---------- watchdog ----------

const watchdogInterval = 30 * time.Second

// startWatchdog launches a background goroutine that periodically checks
// tunnel health and detects configuration drift.
func (m *Manager) startWatchdog() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	m.mu.Lock()
	m.cancel = cancel
	m.done = done
	m.mu.Unlock()

	go func() {
		defer close(done)
		slog.Info("tunnel: watchdog started", "interval", watchdogInterval)

		ticker := time.NewTicker(watchdogInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("tunnel: watchdog stopped")
				return
			case <-ticker.C:
				m.watchdogTick(ctx)
			}
		}
	}()
}

// watchdogTick performs one health-check cycle:
//   - Verify the local interface still exists
//   - Check for server-side config drift
func (m *Manager) watchdogTick(ctx context.Context) {
	cfg := m.cfgMgr.Get()
	ifaceName := cfg.Tunnel.InterfaceName
	if ifaceName == "" {
		ifaceName = "awg0"
	}

	// Check local interface liveness.
	if !m.awg.InterfaceExists(ctx, ifaceName) {
		slog.Warn("tunnel: watchdog: interface disappeared", "iface", ifaceName)
		m.mu.Lock()
		m.status = "error"
		m.mu.Unlock()
		_ = m.cfgMgr.Update(func(c *config.RootConfig) {
			c.Tunnel.Status = "error"
		})
		return
	}

	// Check drift (best-effort; network may be temporarily unavailable).
	if err := m.CheckDrift(ctx); err != nil {
		slog.Warn("tunnel: watchdog: drift detected", "err", err)
		// Don't change status to error for drift — it's a warning.
		// The UI layer should surface this.
	}
}

// ---------- config file helpers ----------

// ClientConfParams holds the parameters for rendering a client .conf file.
type ClientConfParams struct {
	InterfaceName       string
	PrivateKey          string
	Address             string
	ServerPublicKey     string
	Endpoint            string
	PresharedKey        string
	MTU                 int
	PersistentKeepalive int
	Obfuscation         config.ObfuscationConfig
}

// writeClientConf renders the AmneziaWG client configuration and writes it
// to the unet config directory.  Returns the path (without extension) for
// use with awg-quick.
func (m *Manager) writeClientConf(params ClientConfParams) (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("[Interface]\n")
	b.WriteString(fmt.Sprintf("PrivateKey = %s\n", params.PrivateKey))
	b.WriteString(fmt.Sprintf("Address = %s\n", params.Address))
	if params.MTU > 0 {
		b.WriteString(fmt.Sprintf("MTU = %d\n", params.MTU))
	}
	b.WriteString("\n[Peer]\n")
	b.WriteString(fmt.Sprintf("PublicKey = %s\n", params.ServerPublicKey))
	b.WriteString(fmt.Sprintf("Endpoint = %s\n", params.Endpoint))
	b.WriteString(fmt.Sprintf("PresharedKey = %s\n", params.PresharedKey))
	b.WriteString("AllowedIPs = 0.0.0.0/0, ::/0\n")
	if params.PersistentKeepalive > 0 {
		b.WriteString(fmt.Sprintf("PersistentKeepalive = %d\n", params.PersistentKeepalive))
	}

	// Obfuscation parameters.
	writeIfNonZero(&b, "Jc", params.Obfuscation.Jc)
	writeIfNonZero(&b, "Jmin", params.Obfuscation.Jmin)
	writeIfNonZero(&b, "Jmax", params.Obfuscation.Jmax)
	writeIfNonZero(&b, "S1", params.Obfuscation.S1)
	writeIfNonZero(&b, "S2", params.Obfuscation.S2)
	writeIfNonZero(&b, "S3", params.Obfuscation.S3)
	writeIfNonZero(&b, "S4", params.Obfuscation.S4)
	writeIfNonZero(&b, "H1", params.Obfuscation.H1)
	writeIfNonZero(&b, "H2", params.Obfuscation.H2)
	writeIfNonZero(&b, "H3", params.Obfuscation.H3)
	writeIfNonZero(&b, "H4", params.Obfuscation.H4)
	writeIfNonZero(&b, "I1", params.Obfuscation.I1)
	writeIfNonZero(&b, "I2", params.Obfuscation.I2)
	writeIfNonZero(&b, "I3", params.Obfuscation.I3)
	writeIfNonZero(&b, "I4", params.Obfuscation.I4)
	writeIfNonZero(&b, "I5", params.Obfuscation.I5)

	content := b.String()
	confFile := filepath.Join(dir, "awg0.conf")
	if err := os.WriteFile(confFile, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("write client conf: %w", err)
	}

	// awg-quick expects the path without .conf extension on some platforms.
	// Return the path WITHOUT the extension as that's what awg-quick up expects.
	confPath := filepath.Join(dir, "awg0")
	slog.Debug("tunnel: wrote client conf", "path", confFile)
	return confPath, nil
}

// clientConfPath returns the path (without .conf extension) for the client
// configuration file, suitable for awg-quick up/down.
func (m *Manager) clientConfPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "awg0"), nil
}

// ---------- helpers ----------

// fail sets the status to error and returns a formatted error.
func (m *Manager) fail(msg string) error {
	m.mu.Lock()
	m.status = "error"
	m.mu.Unlock()
	_ = m.cfgMgr.Update(func(c *config.RootConfig) {
		c.Tunnel.Status = "error"
	})
	return fmt.Errorf("tunnel: %s", msg)
}

// setStatus sets the tunnel status string.
func (m *Manager) setStatus(s string) {
	m.status = s
	_ = m.cfgMgr.Update(func(c *config.RootConfig) {
		c.Tunnel.Status = s
	})
}

// deriveSubnet extracts the subnet CIDR from an Address field.
// e.g. "10.8.1.1/24" → "10.8.1.0/24"
func deriveSubnet(address string) string {
	ip, _, err := net.ParseCIDR(address)
	if err != nil {
		return ""
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.0/24", ip4[0], ip4[1], ip4[2])
}

// deriveServerIP extracts the server IP from an Address field.
// e.g. "10.8.1.1/24" → "10.8.1.1"
func deriveServerIP(address string) string {
	idx := strings.IndexByte(address, '/')
	if idx > 0 {
		return address[:idx]
	}
	return address
}

// hostnameDefault returns the machine's hostname for use as a default
// client name when adding a peer.
func hostnameDefault() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unet-client"
	}
	return h
}

// writeIfNonZero writes "Key = Value\n" to the builder if value is non-zero.
func writeIfNonZero(b *strings.Builder, key string, value int) {
	if value != 0 {
		b.WriteString(fmt.Sprintf("%s = %d\n", key, value))
	}
}

// Ensure imports are used.
var (
	_ = sha256.Sum256
	_ = errors.New
	_ = json.Marshal
	_ = (*config.Manager)(nil)
)
