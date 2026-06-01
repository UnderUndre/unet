// Package attach handles VPS attach operations: state sync and orchestrator.
package attach

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/underundre/unet/internal/ssh"
	"github.com/underundre/unet/internal/state"
)

// SyncResult holds the outcome of a state sync operation.
type SyncResult struct {
	PeersSynced   int    `json:"peersSynced"`
	RoutesSynced  int    `json:"routesSynced"`
	TunnelSynced  bool   `json:"tunnelSynced"`
	VPSSynced     bool   `json:"vpsSynced"`
	WGEndpoint    string `json:"wgEndpoint,omitempty"`
	WGServerKey   string `json:"wgServerKey,omitempty"`
	TunnelSubnet  string `json:"tunnelSubnet,omitempty"`
	ComposeHash   string `json:"composeHash,omitempty"`
}

// SyncState reads all operational state from the VPS via SSH without
// disrupting connected peers. Populates a VPSProfile for local persistence.
func SyncState(ctx context.Context, sess *ssh.Session, host string, port int, user string, authMode string) (*SyncResult, *state.VPSProfile, error) {
	syncResult := &SyncResult{}
	profile := &state.VPSProfile{
		Host:     host,
		Port:     port,
		User:     user,
		AuthMode: state.AuthMode(authMode),
		Status:   state.VPSStatusActive,
	}

	// 1. Read compose hash.
	hashOut, err := sess.Run(ctx, "sudo cat /opt/unet/.compose-hash 2>/dev/null")
	if err == nil {
		profile.ComposeHash = strings.TrimSpace(hashOut)
		syncResult.ComposeHash = profile.ComposeHash
	}

	// 2. Read version file.
	verOut, err := sess.Run(ctx, "sudo cat /opt/unet/version 2>/dev/null")
	if err == nil {
		profile.KnownGoodVersion = strings.TrimSpace(verOut)
	}

	// 3. Read AWG config from container.
	awgOut, err := sess.Run(ctx, "sudo docker exec unet-amnezia-awg cat /opt/amnezia/awg/awg0.conf 2>/dev/null | head -30")
	if err == nil {
		parseAWGConfig(awgOut, profile, syncResult)
		syncResult.TunnelSynced = true
	}

	// 4. Read client/peer list from container.
	peersJSON, err := sess.Run(ctx, "sudo docker exec unet-amnezia-awg cat /opt/amnezia/awg/clients.json 2>/dev/null")
	if err == nil && strings.TrimSpace(peersJSON) != "" {
		var peers []map[string]any
		if err := json.Unmarshal([]byte(peersJSON), &peers); err == nil {
			syncResult.PeersSynced = len(peers)
		}
	}

	// 5. Read Caddy routes via admin API on WG IP.
	routeOut, err := sess.Run(ctx, "curl -s http://"+ssh.ShellEscape(profile.TunnelSubnet)+":2019/config/apps/http/servers/ 2>/dev/null | head -100")
	_ = routeOut
	if err == nil {
		// Parse routes from Caddy config. Simplified — actual parsing depends on
		// Caddy JSON structure which varies by version.
		syncResult.RoutesSynced = 0
	}

	profile.LastSeenAt = time.Now().UTC().Format(time.RFC3339Nano)
	syncResult.VPSSynced = true

	return syncResult, profile, nil
}

// parseAWGConfig extracts connection parameters from awg0.conf content.
func parseAWGConfig(content string, profile *state.VPSProfile, result *SyncResult) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ListenPort = ") {
			port := strings.TrimPrefix(line, "ListenPort = ")
			result.WGEndpoint = "0.0.0.0:" + port
			profile.WGEndpoint = result.WGEndpoint
		}
		if strings.HasPrefix(line, "PublicKey = ") {
			result.WGServerKey = strings.TrimPrefix(line, "PublicKey = ")
			profile.WGServerPublicKey = result.WGServerKey
		}
		if strings.HasPrefix(line, "Address = ") {
			addr := strings.TrimPrefix(line, "Address = ")
			result.TunnelSubnet = addr
			profile.TunnelSubnet = addr
		}
	}
}

// SaveSyncedProfile persists the synced VPSProfile locally.
func SaveSyncedProfile(profile *state.VPSProfile) error {
	if err := state.SaveVPSProfile(profile); err != nil {
		return fmt.Errorf("attach: save VPSProfile: %w", err)
	}
	slog.Info("attach: saved VPSProfile", "host", profile.Host)
	return nil
}
