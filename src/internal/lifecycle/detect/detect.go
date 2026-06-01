// Package detect probes a VPS and classifies its state into one of four
// categories: blank, old, current, incompatible.
package detect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/underundre/unet/internal/lifecycle/compose"
	"github.com/underundre/unet/internal/ssh"
)

// VPSState is the classification result for a VPS.
type VPSState string

const (
	StateBlank        VPSState = "blank"        // No Docker, no version file
	StateOld          VPSState = "old"          // Behind by 1-2 minor versions
	StateCurrent      VPSState = "current"      // Version match + compose hash match
	StateIncompatible VPSState = "incompatible"  // Major version mismatch
)

// ClassifyResult holds the full detection probe result.
type ClassifyResult struct {
	State           VPSState `json:"state"`
	HasDocker       bool     `json:"hasDocker"`
	VPSVersion      string   `json:"vpsVersion,omitempty"`
	DaemonVersion   string   `json:"daemonVersion"`
	ComposeHashMatch bool    `json:"composeHashMatch"`
	VPSComposeHash  string   `json:"vpsComposeHash,omitempty"`
	ExpectedHash    string   `json:"expectedHash,omitempty"`
	HasContainers   bool     `json:"hasContainers"`
	HasAWGConf      bool     `json:"hasAwgConf"`
}

// Classify probes the VPS and classifies its state. Must complete within 10s
// per FR-004. Each SSH command gets an independent context deadline.
func Classify(ctx context.Context, sess *ssh.Session, daemonVersion string, renderCfg compose.RenderConfig) (*ClassifyResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result := &ClassifyResult{
		DaemonVersion: daemonVersion,
	}

	// Probe 1: Docker presence.
	dockerOut, _ := sess.Run(ctx, "command -v docker 2>/dev/null")
	result.HasDocker = strings.TrimSpace(dockerOut) != ""

	// Probe 2: Version file.
	verOut, verErr := sess.Run(ctx, "sudo cat /opt/unet/version 2>/dev/null")
	if verErr == nil {
		result.VPSVersion = strings.TrimSpace(verOut)
	}

	// Probe 3: Compose hash on VPS.
	hashOut, hashErr := sess.Run(ctx, "sudo cat /opt/unet/.compose-hash 2>/dev/null")
	if hashErr == nil {
		result.VPSComposeHash = strings.TrimSpace(hashOut)
	}

	// Probe 4: Expected compose hash from embedded template.
	expected, err := compose.Render(renderCfg)
	if err == nil {
		result.ExpectedHash = compose.Hash(expected)
	}

	result.ComposeHashMatch = result.VPSComposeHash != "" && result.VPSComposeHash == result.ExpectedHash

	// Probe 5: Running containers.
	containerOut, _ := sess.Run(ctx, "sudo docker ps --filter name=unet- --format '{{.Names}}' 2>/dev/null")
	result.HasContainers = strings.Contains(containerOut, "unet-")

	// Probe 6: awg0.conf presence.
	_, awgErr := sess.Run(ctx, "sudo docker exec unet-amnezia-awg test -f /opt/amnezia/awg/awg0.conf 2>/dev/null")
	result.HasAWGConf = awgErr == nil

	// Classification logic.
	result.State = classifyState(result)

	return result, nil
}

func classifyState(r *ClassifyResult) VPSState {
	// Blank: no Docker and no version file.
	if !r.HasDocker && r.VPSVersion == "" {
		return StateBlank
	}

	// No version file but Docker exists — treat as blank (partial/broken install).
	if r.VPSVersion == "" {
		return StateBlank
	}

	// Parse versions.
	vpsVer, err := ParseSemver(r.VPSVersion)
	if err != nil {
		return StateIncompatible
	}
	daemonVer, err := ParseSemver(r.DaemonVersion)
	if err != nil {
		// If daemon version is unparseable, treat VPS as current (trust the build).
		return StateCurrent
	}

	// Major version mismatch → incompatible.
	if vpsVer.Major != daemonVer.Major {
		return StateIncompatible
	}

	// Same major, within ±2 minor → old (attachable).
	if abs(vpsVer.Minor-daemonVer.Minor) <= 2 && vpsVer.Minor != daemonVer.Minor {
		return StateOld
	}

	// Same version + compose hash match → current.
	if vpsVer.Minor == daemonVer.Minor && vpsVer.Patch == daemonVer.Patch && r.ComposeHashMatch {
		return StateCurrent
	}

	// Same minor but different patch, or compose drift → old.
	return StateOld
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// String returns a human-readable description of the VPS state.
func (s VPSState) String() string {
	switch s {
	case StateBlank:
		return "No unet installation detected (blank VPS)"
	case StateOld:
		return "VPS running older unet version (attachable with upgrade offer)"
	case StateCurrent:
		return "VPS running current unet version (ready for attach)"
	case StateIncompatible:
		return "VPS running incompatible unet version (refuse attach)"
	default:
		return fmt.Sprintf("Unknown VPS state: %s", string(s))
	}
}
