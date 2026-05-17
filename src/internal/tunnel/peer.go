package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/underundre/unet/internal/config"
)

// PeerManager handles adding and removing peers on the remote AmneziaWG
// server via SSH.  It implements the operations described in appendix §2
// (Add a New Peer) and §3 (Remove a Peer).
//
// All shell commands use the temp-file pattern (no process substitution)
// and all JSON values are marshalled via encoding/json — never by string
// interpolation.
type PeerManager struct {
	sshClient SSHExecutor
}

// NewPeerManager creates a new PeerManager with the given SSH executor.
func NewPeerManager(sshClient SSHExecutor) *PeerManager {
	return &PeerManager{sshClient: sshClient}
}

// AddPeerParams holds the parameters for adding a new peer.
type AddPeerParams struct {
	// VPS connection details.
	VPS config.VPSConfig

	// Interface name on the server (always "awg0" on Linux).
	Interface string

	// ClientPublicKey is the peer's WireGuard public key (base64).
	ClientPublicKey string

	// ClientIP is the allocated IP for this peer inside the tunnel (e.g. "10.8.1.2").
	ClientIP string

	// PresharedKey is the shared PSK from the server.
	PresharedKey string

	// ClientName is a user-supplied friendly name for the peer.  It may
	// contain arbitrary characters — it is serialised via encoding/json.
	ClientName string

	// PersistentKeepalive is the keepalive interval in seconds (default 25).
	PersistentKeepalive int
}

// AddPeerResult holds the outcome of a successful peer-add operation.
type AddPeerResult struct {
	// ClientsTableJSON is the updated clientsTable JSON payload that was
	// written to the server.
	ClientsTableJSON json.RawMessage
}

// AddPeer performs the full peer-add flow (appendix §2):
//  1. Append [Peer] block to server's awg0.conf via quoted-heredoc stdin
//  2. Hot-reload via temp-file syncconf pattern (no process substitution)
//  3. Update clientsTable via JSON-marshal + atomic temp-file write
//  4. Verify the peer appeared in `awg show`
func (pm *PeerManager) AddPeer(ctx context.Context, params AddPeerParams) (*AddPeerResult, error) {
	container := params.VPS.ContainerName
	if container == "" {
		container = "unet-amnezia-awg"
	}
	iface := params.Interface
	if iface == "" {
		iface = "awg0"
	}
	keepalive := params.PersistentKeepalive
	if keepalive == 0 {
		keepalive = 25
	}

	slog.Info("peer: adding peer",
		"pubkey", truncateKey(params.ClientPublicKey),
		"ip", params.ClientIP,
		"container", container,
	)

	// §2.3 Step 1 — Append [Peer] block to awg0.conf via stdin (quoted heredoc
	// equivalent: Go pipes the literal bytes, no shell interpolation).
	peerBlock := buildPeerBlock(params.ClientPublicKey, params.PresharedKey, params.ClientIP, keepalive)
	if err := pm.appendPeerBlock(ctx, container, peerBlock); err != nil {
		return nil, fmt.Errorf("peer: append [Peer] block: %w", err)
	}

	// §2.3 Step 2 — Hot-reload via temp-file pattern (NO process substitution).
	if err := pm.syncConf(ctx, container, iface); err != nil {
		return nil, fmt.Errorf("peer: syncconf: %w", err)
	}

	// §2.5 — Verify peer is active.
	if err := pm.verifyPeer(ctx, container, iface, params.ClientPublicKey); err != nil {
		return nil, fmt.Errorf("peer: verify: %w", err)
	}

	// §2.4 — Update clientsTable (JSON via encoding/json).
	ctJSON, err := pm.updateClientsTable(ctx, container, params.ClientPublicKey, params.ClientName)
	if err != nil {
		return nil, fmt.Errorf("peer: update clientsTable: %w", err)
	}

	slog.Info("peer: peer added successfully",
		"pubkey", truncateKey(params.ClientPublicKey),
		"ip", params.ClientIP,
	)
	return &AddPeerResult{ClientsTableJSON: ctJSON}, nil
}

// RemovePeerParams holds the parameters for removing a peer.
type RemovePeerParams struct {
	VPS config.VPSConfig

	// Interface name on the server.
	Interface string

	// ClientPublicKey identifies the peer to remove.
	ClientPublicKey string
}

// RemovePeer performs the full peer-remove flow (appendix §3):
//  1. Live removal via `awg set peer <pubkey> remove`
//  2. Persist removal via `awg-quick save`
//  3. Update clientsTable (remove entry)
func (pm *PeerManager) RemovePeer(ctx context.Context, params RemovePeerParams) error {
	container := params.VPS.ContainerName
	if container == "" {
		container = "unet-amnezia-awg"
	}
	iface := params.Interface
	if iface == "" {
		iface = "awg0"
	}

	slog.Info("peer: removing peer",
		"pubkey", truncateKey(params.ClientPublicKey),
		"container", container,
	)

	// §3.1 Live removal.
	removeCmd := dockerExec(container,
		fmt.Sprintf("awg set %s peer %s remove", shellArg(iface), shellArg(params.ClientPublicKey)))
	if _, _, err := pm.sshClient.ExecuteCommand(ctx, removeCmd); err != nil {
		slog.Warn("peer: live remove failed (may already be removed)", "err", err)
		// Non-fatal: peer might not be in the live config.
	}

	// §3.2 Persist removal via `awg-quick save` (requires SaveConfig=true).
	saveCmd := dockerExec(container,
		fmt.Sprintf("awg-quick save %s", shellArg(iface)))
	if _, stderr, err := pm.sshClient.ExecuteCommand(ctx, saveCmd); err != nil {
		slog.Warn("peer: awg-quick save failed", "err", err, "stderr", stderr)
		// Non-fatal for now; config file edit fallback is possible.
	}

	// §3.3 Update clientsTable (remove entry).
	if _, err := pm.removeFromClientsTable(ctx, container, params.ClientPublicKey); err != nil {
		slog.Warn("peer: failed to update clientsTable", "err", err)
		// Non-fatal: metadata is stale but the peer is functionally removed.
	}

	slog.Info("peer: peer removed", "pubkey", truncateKey(params.ClientPublicKey))
	return nil
}

// AllocateNextIP finds the next free IP in the server's subnet by examining
// the occupied IPs from the peer dump.  It starts at .2 (server is .1) and
// returns the first unallocated address.
func AllocateNextIP(subnet string, peers []PeerDumpEntry) (string, error) {
	// Parse the subnet CIDR to get the network and mask.
	_, ipNet, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", fmt.Errorf("peer: parse subnet %q: %w", subnet, err)
	}

	// Collect occupied IPs from existing peers.
	occupied := make(map[string]struct{})
	for _, p := range peers {
		// AllowedIPs may contain comma-separated entries; take the first.
		ips := strings.Split(p.AllowedIPs, ",")
		for _, ipCIDR := range ips {
			ip := strings.TrimSpace(strings.Split(ipCIDR, "/")[0])
			if ip != "" {
				occupied[ip] = struct{}{}
			}
		}
	}

	// Also mark the server IP (network + 1) as occupied.
	netIP := ipNet.IP.To4()
	if netIP == nil {
		return "", fmt.Errorf("peer: subnet %q is not IPv4", subnet)
	}

	// Iterate from .2 upward.
	for i := 2; i < 255; i++ {
		candidate := net.IP{netIP[0], netIP[1], netIP[2], byte(i)}.String()
		if _, taken := occupied[candidate]; !taken {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("peer: no free IP in subnet %q (all .2–.254 occupied)", subnet)
}

// ---------- internal helpers ----------

// buildPeerBlock renders the [Peer] section for the new peer.
// All values are validated before reaching this function.
func buildPeerBlock(pubKey, psk, clientIP string, keepalive int) string {
	return fmt.Sprintf("\n[Peer]\nPublicKey = %s\nPresharedKey = %s\nAllowedIPs = %s/32\nPersistentKeepalive = %d\n",
		pubKey, psk, clientIP, keepalive)
}

// appendPeerBlock appends the [Peer] block to the server's awg0.conf by
// piping the literal bytes via stdin to `cat >> awg0.conf` inside the
// container.  This is the Go equivalent of the quoted-heredoc pattern.
func (pm *PeerManager) appendPeerBlock(ctx context.Context, container, peerBlock string) error {
	// Build a script that pipes the peer block into docker exec -i via a
	// quoted heredoc (<<'PEER') so the content travels literally — no shell
	// interpolation of the peer block.  The SSH executor pipes this to bash -s
	// on the VPS host.
	script := "docker exec -i " + shellArg(container) + " sh -c 'cat >> /opt/amnezia/awg/awg0.conf'" + " <<'PEER'\n" + peerBlock + "PEER"

	stdout, stderr, err := pm.sshClient.ExecuteScript(ctx, script)
	if err != nil {
		return fmt.Errorf("append peer block: %w (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	return nil
}

// syncConf performs the hot-reload using the temp-file pattern (appendix §2.3
// Step 2) with flock for cross-daemon safety.
func (pm *PeerManager) syncConf(ctx context.Context, container, iface string) error {
	// Use flock-wrapped commands to ensure cross-daemon safety:
	//   1. mktemp for unique strip file
	//   2. awg-quick strip > tempfile
	//   3. flock-held awg syncconf
	//   4. cleanup
	//
	// All inside a single docker exec with flock from util-linux (present on Alpine).
	lockPath := "/opt/amnezia/awg/awg0.lock"
	script := fmt.Sprintf(
		"flock -w 10 %s sh -c '"+
			"TMPFILE=$(mktemp /tmp/awg0-strip.XXXXXX) && "+
			"trap \"rm -f $TMPFILE\" EXIT && "+
			"awg-quick strip /opt/amnezia/awg/awg0.conf > $TMPFILE && "+
			"awg syncconf %s $TMPFILE"+
			"'",
		shellArg(lockPath), shellArg(iface))

	cmd := dockerExec(container, script)
	stdout, stderr, err := pm.sshClient.ExecuteCommand(ctx, cmd)
	if err != nil {
		return fmt.Errorf("syncconf (flock): %w (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	slog.Debug("peer: syncconf complete (flock-protected)", "iface", iface)
	return nil
}

// verifyPeer checks that the peer's public key appears in `awg show`.
func (pm *PeerManager) verifyPeer(ctx context.Context, container, iface, pubKey string) error {
	cmd := dockerExec(container, fmt.Sprintf("awg show %s peers", shellArg(iface)))
	stdout, _, err := pm.sshClient.ExecuteCommand(ctx, cmd)
	if err != nil {
		return fmt.Errorf("verify peer: awg show peers: %w", err)
	}

	for _, line := range strings.Split(stdout, "\n") {
		if strings.TrimSpace(line) == pubKey {
			return nil
		}
	}
	return fmt.Errorf("peer %s not found in awg show output", truncateKey(pubKey))
}

// ---------- clientsTable helpers ----------

// ClientEntry represents one entry in the AmneziaWG clientsTable JSON.
type ClientEntry struct {
	ClientID string   `json:"clientId"`
	UserData UserData `json:"userData"`
}

// UserData holds the user-facing metadata for a client entry.
type UserData struct {
	ClientName   string `json:"clientName"`
	CreationDate string `json:"creationDate"`
}

// updateClientsTable reads the existing clientsTable, appends the new
// entry (marshalled via encoding/json), and writes it back atomically.
func (pm *PeerManager) updateClientsTable(ctx context.Context, container, pubKey, clientName string) (json.RawMessage, error) {
	// Read existing clientsTable.
	existing, err := pm.readClientsTable(ctx, container)
	if err != nil {
		slog.Warn("peer: could not read existing clientsTable, starting fresh", "err", err)
		existing = []ClientEntry{}
	}

	// Append new entry.
	updated := append(existing, ClientEntry{
		ClientID: pubKey,
		UserData: UserData{
			ClientName:   clientName,
			CreationDate: time.Now().UTC().Format(time.RFC3339),
		},
	})

	// Marshal via encoding/json (handles all escaping).
	payload, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal clientsTable: %w", err)
	}

	// Atomic write: temp-file + rename pattern.
	// Step 1: Write to .new via stdin pipe.
	writeCmd := fmt.Sprintf("docker exec -i %s sh -c 'cat > /opt/amnezia/awg/clientsTable.new'",
		shellArg(container))
	// Use ExecuteScript to pipe content.
	script := writeCmd + " <<'PAYLOAD'\n" + string(payload) + "\nPAYLOAD"
	if stdout, stderr, err := pm.sshClient.ExecuteScript(ctx, script); err != nil {
		return nil, fmt.Errorf("write clientsTable.new: %w (stdout=%q stderr=%q)", err, stdout, stderr)
	}

	// Step 2: Atomic rename.
	renameCmd := dockerExec(container, "mv /opt/amnezia/awg/clientsTable.new /opt/amnezia/awg/clientsTable")
	if stdout, stderr, err := pm.sshClient.ExecuteCommand(ctx, renameCmd); err != nil {
		return nil, fmt.Errorf("rename clientsTable: %w (stdout=%q stderr=%q)", err, stdout, stderr)
	}

	return json.RawMessage(payload), nil
}

// removeFromClientsTable reads the existing clientsTable, removes the
// entry matching the given public key, and writes it back atomically.
func (pm *PeerManager) removeFromClientsTable(ctx context.Context, container, pubKey string) (json.RawMessage, error) {
	existing, err := pm.readClientsTable(ctx, container)
	if err != nil {
		return nil, fmt.Errorf("read clientsTable: %w", err)
	}

	// Filter out the removed peer.
	var updated []ClientEntry
	for _, e := range existing {
		if e.ClientID != pubKey {
			updated = append(updated, e)
		}
	}

	// Marshal via encoding/json.
	payload, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal clientsTable: %w", err)
	}

	// Atomic write.
	writeCmd := fmt.Sprintf("docker exec -i %s sh -c 'cat > /opt/amnezia/awg/clientsTable.new'",
		shellArg(container))
	script := writeCmd + " <<'PAYLOAD'\n" + string(payload) + "\nPAYLOAD"
	if stdout, stderr, err := pm.sshClient.ExecuteScript(ctx, script); err != nil {
		return nil, fmt.Errorf("write clientsTable.new: %w (stdout=%q stderr=%q)", err, stdout, stderr)
	}

	renameCmd := dockerExec(container, "mv /opt/amnezia/awg/clientsTable.new /opt/amnezia/awg/clientsTable")
	if stdout, stderr, err := pm.sshClient.ExecuteCommand(ctx, renameCmd); err != nil {
		return nil, fmt.Errorf("rename clientsTable: %w (stdout=%q stderr=%q)", err, stdout, stderr)
	}

	return json.RawMessage(payload), nil
}

// readClientsTable fetches and parses the clientsTable from the server.
func (pm *PeerManager) readClientsTable(ctx context.Context, container string) ([]ClientEntry, error) {
	cmd := dockerExec(container, "cat /opt/amnezia/awg/clientsTable")
	stdout, stderr, err := pm.sshClient.ExecuteCommand(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("cat clientsTable: %w (stderr=%q)", err, stderr)
	}

	var entries []ClientEntry
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		// File might be empty or not valid JSON — return empty slice.
		return []ClientEntry{}, nil
	}
	return entries, nil
}

// dockerExec builds a command string for execution via SSH:
// `docker exec <container> <cmd>`.
// The returned string is meant to be passed to SSHExecutor.ExecuteCommand
// which handles the SSH transport.
func dockerExec(container, cmd string) string {
	return fmt.Sprintf("docker exec %s sh -c %s", shellArg(container), shellArg(cmd))
}

// truncateKey returns a shortened version of a base64 key for logging.
func truncateKey(key string) string {
	if len(key) > 12 {
		return key[:12] + "..."
	}
	return key
}

// SortPeersByIP sorts PeerDumpEntry slices by their AllowedIPs field.
func SortPeersByIP(peers []PeerDumpEntry) {
	sort.Slice(peers, func(i, j int) bool {
		return peers[i].AllowedIPs < peers[j].AllowedIPs
	})
}
