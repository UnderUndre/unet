package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/underundre/unet/internal/lifecycle/compose"
	"github.com/underundre/unet/internal/ssh"
)

// BootstrapResult holds the outcome of a full bootstrap operation.
type BootstrapResult struct {
	Preflight       *PreflightResult   `json:"preflight"`
	Docker          *DockerInstallResult `json:"docker"`
	Deploy          *DeployResult      `json:"deploy"`
	HealthVerified  bool               `json:"healthVerified"`
	ConnectionParams *ConnectionParams  `json:"connectionParams,omitempty"`
	ZeroDiff        bool               `json:"zeroDiff"` // true when VPS was already current
	Duration        string             `json:"duration"`
}

// ConnectionParams holds the WireGuard connection details discovered from
// a freshly bootstrapped VPS.
type ConnectionParams struct {
	WGEndpoint        string `json:"wgEndpoint"`
	WGServerPublicKey string `json:"wgServerPublicKey"`
	TunnelSubnet      string `json:"tunnelSubnet"`
}

// BootstrapOpts configures the bootstrap operation.
type BootstrapOpts struct {
	// ComposeConfig for rendering the docker-compose.yml.
	ComposeConfig compose.RenderConfig
	// DaemonVersion written to /opt/unet/version.
	DaemonVersion string
}

// Bootstrap orchestrates the full idempotent bootstrap sequence:
//  1. Preflight checks (read-only)
//  2. Snapshot (pre-mutation)
//  3. Docker + compose install
//  4. Compose deploy
//  5. Health verification
//  6. Connection params discovery
//
// On failure after Phase 2, rollback is executed.
func Bootstrap(ctx context.Context, pool *ssh.Pool, opts BootstrapOpts) (*BootstrapResult, error) {
	start := time.Now()
	result := &BootstrapResult{}
	defer func() {
		result.Duration = time.Since(start).Round(time.Second).String()
	}()

	sess, err := pool.Session(ctx)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: get SSH session: %w", err)
	}
	defer pool.Put(sess)

	// Phase 1: Preflight.
	slog.Info("bootstrap: running preflight checks")
	preflight, err := RunPreflight(ctx, sess)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: preflight: %w", err)
	}
	result.Preflight = preflight
	if !preflight.Pass {
		return result, fmt.Errorf("bootstrap: preflight failed")
	}

	// Phase 2: Docker install.
	slog.Info("bootstrap: ensuring Docker is installed")
	dockerResult, err := EnsureDocker(ctx, sess)
	if err != nil {
		return result, fmt.Errorf("bootstrap: Docker install: %w", err)
	}
	result.Docker = dockerResult

	// Phase 3: Deploy compose.
	slog.Info("bootstrap: deploying compose")
	deployResult, err := DeployCompose(ctx, sess, opts.ComposeConfig, opts.DaemonVersion)
	if err != nil {
		// Rollback on deploy failure (after Phase 2 mutations).
		slog.Error("bootstrap: deploy failed, rolling back", "err", err)
		if rbErr := Rollback(ctx, sess); rbErr != nil {
			slog.Error("bootstrap: rollback failed", "err", rbErr)
		}
		return result, fmt.Errorf("bootstrap: deploy: %w", err)
	}
	result.Deploy = deployResult

	if deployResult.Skipped {
		// VPS already current — check health but skip full verify.
		slog.Info("bootstrap: compose unchanged, verifying health")
		result.ZeroDiff = true
		healthy, _ := checkContainerHealth(ctx, sess)
		result.HealthVerified = healthy
		return result, nil
	}

	// Phase 4: Health verification.
	slog.Info("bootstrap: verifying container health")
	healthy, err := verifyHealth(ctx, sess)
	if err != nil {
		slog.Error("bootstrap: health verification failed, rolling back", "err", err)
		if rbErr := Rollback(ctx, sess); rbErr != nil {
			slog.Error("bootstrap: rollback failed", "err", rbErr)
		}
		return result, fmt.Errorf("bootstrap: health verify: %w", err)
	}
	result.HealthVerified = healthy

	// Phase 5: Discover connection params.
	params, err := discoverConnectionParams(ctx, sess)
	if err != nil {
		slog.Warn("bootstrap: could not discover connection params", "err", err)
	} else {
		result.ConnectionParams = params
	}

	return result, nil
}

// verifyHealth polls container status every 5s for up to 120s.
func verifyHealth(ctx context.Context, sess *ssh.Session) (bool, error) {
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}

		healthy, err := checkContainerHealth(ctx, sess)
		if err == nil && healthy {
			return true, nil
		}

		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return false, fmt.Errorf("bootstrap: health verification timed out after 120s")
}

// checkContainerHealth verifies expected containers are running.
func checkContainerHealth(ctx context.Context, sess *ssh.Session) (bool, error) {
	out, err := sess.Run(ctx, "sudo docker ps --filter name=unet- --format '{{.Names}} {{.Status}}' 2>/dev/null")
	if err != nil {
		return false, err
	}

	// Check for required containers.
	hasPause := false
	hasAWG := false
	hasCaddy := false

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "unet-net-pause") && strings.Contains(line, "Up") {
			hasPause = true
		}
		if strings.Contains(line, "unet-amnezia-awg") && strings.Contains(line, "Up") {
			hasAWG = true
		}
		if strings.Contains(line, "unet-caddy") && strings.Contains(line, "Up") {
			hasCaddy = true
		}
	}

	return hasPause && hasAWG && hasCaddy, nil
}

// discoverConnectionParams reads WireGuard connection details from the VPS.
func discoverConnectionParams(ctx context.Context, sess *ssh.Session) (*ConnectionParams, error) {
	params := &ConnectionParams{}

	// Get WG endpoint port from compose/awg config.
	out, err := sess.Run(ctx, "sudo docker exec unet-amnezia-awg cat /opt/amnezia/awg/awg0.conf 2>/dev/null | head -20")
	if err != nil {
		return nil, fmt.Errorf("read awg0.conf: %w", err)
	}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ListenPort = ") {
			port := strings.TrimPrefix(line, "ListenPort = ")
			params.WGEndpoint = "0.0.0.0:" + port
		}
		if strings.HasPrefix(line, "PublicKey = ") {
			params.WGServerPublicKey = strings.TrimPrefix(line, "PublicKey = ")
		}
		if strings.HasPrefix(line, "Address = ") {
			addr := strings.TrimPrefix(line, "Address = ")
			// Convert to CIDR subnet.
			parts := strings.Split(addr, "/")
			if len(parts) == 2 {
				params.TunnelSubnet = addr
			}
		}
	}

	return params, nil
}
