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

// DockerInstallResult describes the outcome of Docker + compose installation.
type DockerInstallResult struct {
	DockerInstalled   bool   `json:"dockerInstalled"`
	ComposeInstalled  bool   `json:"composeInstalled"`
	WasAlreadyPresent bool   `json:"wasAlreadyPresent"`
	DockerVersion     string `json:"dockerVersion,omitempty"`
	ComposeVersion    string `json:"composeVersion,omitempty"`
}

// EnsureDocker installs Docker and the compose plugin if not already present.
// Idempotent: skips if both are already available.
func EnsureDocker(ctx context.Context, sess *ssh.Session) (*DockerInstallResult, error) {
	result := &DockerInstallResult{}

	// Check Docker.
	dockerOut, err := sess.Run(ctx, "command -v docker")
	if err == nil && strings.TrimSpace(dockerOut) != "" {
		result.DockerInstalled = true
		verOut, _ := sess.Run(ctx, "docker --version")
		result.DockerVersion = strings.TrimSpace(verOut)
	} else {
		slog.Info("bootstrap: Docker not found, installing")
		installCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		out, _, err := sess.RunScript(installCtx, `set -euo pipefail
curl -fsSL https://get.docker.com | sh
`)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: Docker install failed: %w\noutput: %s", err, out)
		}
		result.DockerInstalled = true

		verOut, _ := sess.Run(ctx, "docker --version")
		result.DockerVersion = strings.TrimSpace(verOut)
	}

	// Check compose plugin.
	composeOut, err := sess.Run(ctx, "docker compose version")
	if err == nil && strings.TrimSpace(composeOut) != "" {
		result.ComposeInstalled = true
		result.ComposeVersion = strings.TrimSpace(composeOut)
	} else {
		slog.Info("bootstrap: compose plugin not found, installing")
		installCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()

		out, _, err := sess.RunScript(installCtx, `set -euo pipefail
apt-get update -qq
apt-get install -y -qq docker-compose-plugin
`)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: compose plugin install failed: %w\noutput: %s", err, out)
		}
		result.ComposeInstalled = true

		verOut, _ := sess.Run(ctx, "docker compose version")
		result.ComposeVersion = strings.TrimSpace(verOut)
	}

	result.WasAlreadyPresent = result.DockerInstalled && result.ComposeInstalled
	return result, nil
}

// DeployResult describes the outcome of compose deployment.
type DeployResult struct {
	ComposeUploaded bool   `json:"composeUploaded"`
	ComposeHash     string `json:"composeHash"`
	ContainersUp    bool   `json:"containersUp"`
	Skipped         bool   `json:"skipped"` // true when hash matched, no changes
}

// DeployCompose renders the compose file from embedded templates, compares
// with the VPS version, and deploys if changed. Idempotent.
func DeployCompose(ctx context.Context, sess *ssh.Session, cfg compose.RenderConfig, version string) (*DeployResult, error) {
	result := &DeployResult{}

	// Render compose from embedded templates.
	composeYAML, err := compose.Render(cfg)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: render compose: %w", err)
	}
	expectedHash := compose.Hash(composeYAML)
	result.ComposeHash = expectedHash

	// Create /opt/unet directory.
	if _, err := sess.Run(ctx, "sudo mkdir -p /opt/unet"); err != nil {
		return nil, fmt.Errorf("bootstrap: create /opt/unet: %w", err)
	}

	// Check existing compose hash on VPS.
	existingHash, _ := sess.Run(ctx, "sudo cat /opt/unet/.compose-hash 2>/dev/null")
	existingHash = strings.TrimSpace(existingHash)

	if existingHash == expectedHash {
		// Check if containers are running.
		containerOut, _ := sess.Run(ctx, "sudo docker ps --filter name=unet- --format '{{.Names}}' 2>/dev/null")
		if strings.Contains(containerOut, "unet-net-pause") {
			slog.Info("bootstrap: compose unchanged, skipping deploy")
			result.Skipped = true
			result.ContainersUp = true
			return result, nil
		}
	}

	// Upload compose file.
	slog.Info("bootstrap: uploading compose file", "hash", expectedHash)
	uploadScript := fmt.Sprintf(`cat > /tmp/docker-compose.yml.unet << 'UNET_COMPOSE_EOF'
%s
UNET_COMPOSE_EOF
sudo mv /tmp/docker-compose.yml.unet /opt/unet/docker-compose.yml
echo '%s' | sudo tee /opt/unet/.compose-hash > /dev/null
`, string(composeYAML), expectedHash)

	if out, _, err := sess.RunScript(ctx, uploadScript); err != nil {
		return nil, fmt.Errorf("bootstrap: upload compose: %w\noutput: %s", err, out)
	}
	result.ComposeUploaded = true

	// Write version file.
	verScript := fmt.Sprintf(`echo '%s' | sudo tee /opt/unet/version > /dev/null`, version)
	if out, _, err := sess.RunScript(ctx, verScript); err != nil {
		return nil, fmt.Errorf("bootstrap: write version file: %w\noutput: %s", err, out)
	}

	// Pull images.
	pullCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	slog.Info("bootstrap: pulling images")
	if out, err := sess.Run(pullCtx, "cd /opt/unet && sudo docker compose pull 2>&1"); err != nil {
		// Check for disk full.
		if strings.Contains(out, "no space left on device") {
			// Cleanup partial artifacts.
			sess.Run(ctx, "sudo docker system prune -f 2>/dev/null")
			return nil, fmt.Errorf("bootstrap: disk full during image pull. Free at least 2GB and retry")
		}
		return nil, fmt.Errorf("bootstrap: docker compose pull failed: %w\noutput: %s", err, out)
	}

	// Bring up containers.
	upCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	slog.Info("bootstrap: starting containers")
	if out, err := sess.Run(upCtx, "cd /opt/unet && sudo docker compose up -d 2>&1"); err != nil {
		return nil, fmt.Errorf("bootstrap: docker compose up failed: %w\noutput: %s", err, out)
	}

	result.ContainersUp = true
	return result, nil
}
