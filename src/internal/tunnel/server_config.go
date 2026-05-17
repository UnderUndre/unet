package tunnel

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/underundre/unet/internal/config"
)

// ServerConfigParser fetches and parses the AmneziaWG server configuration
// from the VPS via SSH.  It implements the operations described in the
// appendix §1 (Read Server State).
type ServerConfigParser struct {
	sshClient SSHExecutor
}

// SSHExecutor is the interface for executing commands over SSH.
// It is satisfied by *provisioner.Client.
type SSHExecutor interface {
	ExecuteCommand(ctx context.Context, cmd string) (stdout, stderr string, err error)
	ExecuteScript(ctx context.Context, script string) (stdout, stderr string, err error)
}

// ServerConfig holds the parsed server-side AmneziaWG configuration.
type ServerConfig struct {
	// Interface parameters parsed from awg0.conf [Interface] section.
	Address    string // e.g. "10.8.1.1/24"
	ListenPort int    // e.g. 31075

	// Obfuscation parameters from [Interface] section.
	Obfuscation config.ObfuscationConfig

	// ServerPublicKey is the server's WireGuard public key.
	ServerPublicKey string

	// PresharedKey is the shared PSK (Amnezia uses one PSK across all peers).
	PresharedKey string

	// RawConf is the full raw content of awg0.conf as fetched from the server.
	RawConf string

	// ConfSha256 is the SHA256 hash of RawConf for drift detection.
	ConfSha256 string

	// Peers lists the currently active peers as seen by `awg show dump`.
	Peers []PeerDumpEntry
}

// PeerDumpEntry represents one peer line from `awg show <iface> dump`.
type PeerDumpEntry struct {
	PublicKey       string // peer's WireGuard public key
	PresharedKey    string
	Endpoint        string
	AllowedIPs      string // e.g. "10.8.1.2/32"
	LastHandshake   string
	RxBytes         string
	TxBytes         string
	PersistentKeepalive string
}

// NewServerConfigParser creates a new parser with the given SSH executor.
func NewServerConfigParser(sshClient SSHExecutor) *ServerConfigParser {
	return &ServerConfigParser{sshClient: sshClient}
}

// FetchAll performs the full server-state read (appendix §1.1–§1.5):
//   - §1.1 Fetch and parse awg0.conf
//   - §1.2 Fetch server public key
//   - §1.3 Fetch preshared key
//   - §1.4 Fetch existing peers via `awg show dump`
//   - §1.5 Compute drift hash
func (p *ServerConfigParser) FetchAll(ctx context.Context, vps config.VPSConfig, iface string) (*ServerConfig, error) {
	container := vps.ContainerName
	if container == "" {
		container = "unet-amnezia-awg"
	}

	scfg := &ServerConfig{}

	// §1.1 Fetch awg0.conf
	confRaw, err := p.dockerCat(ctx, vps, container, "/opt/amnezia/awg/awg0.conf")
	if err != nil {
		return nil, fmt.Errorf("server_config: fetch awg0.conf: %w", err)
	}
	scfg.RawConf = confRaw

	// §1.5 Compute drift hash
	hash := sha256.Sum256([]byte(confRaw))
	scfg.ConfSha256 = fmt.Sprintf("%x", hash)

	// Parse the [Interface] section.
	if err := parseInterfaceSection(confRaw, scfg); err != nil {
		return nil, fmt.Errorf("server_config: parse awg0.conf: %w", err)
	}

	// §1.2 Fetch server public key
	srvPubKey, err := p.dockerCat(ctx, vps, container, "/opt/amnezia/awg/wireguard_server_public_key.key")
	if err != nil {
		return nil, fmt.Errorf("server_config: fetch server public key: %w", err)
	}
	scfg.ServerPublicKey = strings.TrimSpace(srvPubKey)

	// §1.3 Fetch preshared key
	psk, err := p.dockerCat(ctx, vps, container, "/opt/amnezia/awg/wireguard_psk.key")
	if err != nil {
		return nil, fmt.Errorf("server_config: fetch PSK: %w", err)
	}
	scfg.PresharedKey = strings.TrimSpace(psk)

	// §1.4 Fetch existing peers via `awg show dump`
	peers, err := p.fetchPeerDump(ctx, vps, container, iface)
	if err != nil {
		slog.Warn("server_config: could not fetch peer dump (interface may be down)", "err", err)
		// Non-fatal: interface might not be up yet.
	} else {
		scfg.Peers = peers
	}

	slog.Info("server_config: fetched full server state",
		"address", scfg.Address,
		"listenPort", scfg.ListenPort,
		"peerCount", len(scfg.Peers),
		"confSha256", scfg.ConfSha256[:16]+"...",
	)
	return scfg, nil
}

// FetchConfHash fetches only the SHA256 of the remote awg0.conf for
// lightweight drift detection (appendix §1.5).
func (p *ServerConfigParser) FetchConfHash(ctx context.Context, vps config.VPSConfig) (string, error) {
	container := vps.ContainerName
	if container == "" {
		container = "unet-amnezia-awg"
	}

	// Use sha256sum inside the container.
	cmd := dockerExecCmd(container, "sha256sum /opt/amnezia/awg/awg0.conf")
	stdout, _, err := p.sshClient.ExecuteCommand(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("server_config: fetch conf hash: %w", err)
	}

	// sha256sum output: "<hash>  /path/to/file"
	parts := strings.Fields(stdout)
	if len(parts) < 1 {
		return "", fmt.Errorf("server_config: unexpected sha256sum output: %q", stdout)
	}
	return parts[0], nil
}

// FetchPeers fetches the current peer list from `awg show dump`.
func (p *ServerConfigParser) FetchPeers(ctx context.Context, vps config.VPSConfig, iface string) ([]PeerDumpEntry, error) {
	container := vps.ContainerName
	if container == "" {
		container = "unet-amnezia-awg"
	}
	return p.fetchPeerDump(ctx, vps, container, iface)
}

// ---------- internal helpers ----------

// dockerCat returns the content of a file inside the container via
// `ssh <vps> "docker exec <container> cat <path>"`.
func (p *ServerConfigParser) dockerCat(ctx context.Context, vps config.VPSConfig, container, path string) (string, error) {
	cmd := dockerExecCmd(container, "cat "+shellArg(path))
	stdout, stderr, err := p.sshClient.ExecuteCommand(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("docker cat %s: %w (stderr: %s)", path, err, stderr)
	}
	return stdout, nil
}

// fetchPeerDump runs `awg show <iface> dump` inside the container and
// parses the output into peer entries.
func (p *ServerConfigParser) fetchPeerDump(ctx context.Context, vps config.VPSConfig, container, iface string) ([]PeerDumpEntry, error) {
	cmd := dockerExecCmd(container, fmt.Sprintf("awg show %s dump", shellArg(iface)))
	stdout, _, err := p.sshClient.ExecuteCommand(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("awg show dump: %w", err)
	}
	return parseDumpOutput(stdout), nil
}

// parseDumpOutput parses the tab-separated output of `awg show <iface> dump`.
// The first line is the interface; subsequent lines are peers.
func parseDumpOutput(raw string) []PeerDumpEntry {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) < 2 {
		return nil // No peers (or empty output).
	}

	var peers []PeerDumpEntry
	// Skip first line (interface info).
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) < 8 {
			continue
		}
		peers = append(peers, PeerDumpEntry{
			PublicKey:           fields[0],
			PresharedKey:        fields[1],
			Endpoint:            fields[2],
			AllowedIPs:          fields[3],
			LastHandshake:       fields[4],
			RxBytes:             fields[5],
			TxBytes:             fields[6],
			PersistentKeepalive: fields[7],
		})
	}
	return peers
}

// parseInterfaceSection extracts Interface-level fields from an INI-style
// AmneziaWG config file.  It tolerates `# comment` lines (Amnezia uses
// `# I1 = ...` syntax when injection slots are populated).
func parseInterfaceSection(raw string, scfg *ServerConfig) error {
	// Extract only the [Interface] section.
	section := extractSection(raw, "Interface")
	if section == "" {
		return fmt.Errorf("no [Interface] section found")
	}

	obf := &scfg.Obfuscation
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := splitKeyValue(line)
		if !ok {
			continue
		}

		switch key {
		case "Address":
			scfg.Address = value
		case "ListenPort":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("parse ListenPort: %w", err)
			}
			scfg.ListenPort = n
		case "Jc":
			obf.Jc = atoiOrZero(value)
		case "Jmin":
			obf.Jmin = atoiOrZero(value)
		case "Jmax":
			obf.Jmax = atoiOrZero(value)
		case "S1":
			obf.S1 = atoiOrZero(value)
		case "S2":
			obf.S2 = atoiOrZero(value)
		case "S3":
			obf.S3 = atoiOrZero(value)
		case "S4":
			obf.S4 = atoiOrZero(value)
		case "H1":
			obf.H1 = atoiOrZero(value)
		case "H2":
			obf.H2 = atoiOrZero(value)
		case "H3":
			obf.H3 = atoiOrZero(value)
		case "H4":
			obf.H4 = atoiOrZero(value)
		case "I1":
			obf.I1 = atoiOrZero(value)
		case "I2":
			obf.I2 = atoiOrZero(value)
		case "I3":
			obf.I3 = atoiOrZero(value)
		case "I4":
			obf.I4 = atoiOrZero(value)
		case "I5":
			obf.I5 = atoiOrZero(value)
		}
	}
	return nil
}

// extractSection returns the content of the named INI section (everything
// between `[SectionName]` and the next `[...]` header or EOF), excluding
// the header line itself.
func extractSection(raw, sectionName string) string {
	sectionHeader := "[" + sectionName + "]"
	lines := strings.Split(raw, "\n")
	var buf strings.Builder
	inSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == sectionHeader {
			inSection = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") && trimmed != sectionHeader {
			if inSection {
				break // End of our section.
			}
			continue
		}
		if inSection {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	return strings.TrimRight(buf.String(), "\n")
}

// splitKeyValue splits "Key = Value" (with optional whitespace around '=').
// For commented Amnezia params like "# I1 = 42", the caller must strip
// the comment prefix before calling.
var kvRe = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)\s*=\s*(.*)$`)

func splitKeyValue(line string) (key, value string, ok bool) {
	m := kvRe.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], strings.TrimSpace(m[2]), true
}

func atoiOrZero(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// dockerExecCmd builds an SSH command string that runs a command inside
// the AWG container: `ssh <vps> "docker exec <container> <cmd>"`.
// The caller (SSHExecutor) handles the SSH transport.
func dockerExecCmd(container, cmd string) string {
	return fmt.Sprintf("docker exec %s %s", shellArg(container), cmd)
}

// shellArg wraps a string in single quotes for safe shell interpolation.
func shellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ServerMirrorJSON is the JSON structure for the `serverMirror` blob
// persisted in config.json.  It is serialised via encoding/json only.
type ServerMirrorJSON struct {
	LastSyncedAt      string          `json:"lastSyncedAt,omitempty"`
	AwgConfRaw        string          `json:"awgConfRaw,omitempty"`
	AwgConfSha256     string          `json:"awgConfSha256,omitempty"`
	ClientsTable      json.RawMessage `json:"clientsTable,omitempty"`
	CaddyAdminConfig  json.RawMessage `json:"caddyAdminConfig,omitempty"`
	ServerPrivateKey  string          `json:"serverPrivateKeyB64,omitempty"`
}

// ParseServerMirror deserialises the serverMirror JSON string from config.
func ParseServerMirror(raw string) (*ServerMirrorJSON, error) {
	if raw == "" {
		return &ServerMirrorJSON{}, nil
	}
	var m ServerMirrorJSON
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("server_config: parse serverMirror: %w", err)
	}
	return &m, nil
}

// SerializeServerMirror serialises the mirror to JSON.
func SerializeServerMirror(m *ServerMirrorJSON) (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("server_config: serialize serverMirror: %w", err)
	}
	return string(data), nil
}
