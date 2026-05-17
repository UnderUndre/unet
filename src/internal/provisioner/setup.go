package provisioner

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/underundre/unet/internal/config"
)

// ProvisionResult holds the outcome of a successful provisioning run.
type ProvisionResult struct {
	ContainerID string        `json:"containerId"`
	AWGConfig   *AWGInitConfig `json:"awgConfig"`
}

// Provision performs the full idempotent provisioning of the VPS:
//  1. SSH to VPS
//  2. Generate AWG config (keypair, port, subnet, obfuscation)
//  3. Generate Dockerfile, start.sh, compose.yml
//  4. Upload files to VPS
//  5. docker compose up -d --build (v2 syntax)
//  6. Wait for container healthy
//  7. Write AWG config into container
//  8. Run awg-quick up awg0 inside container
//  9. Persist results to config
//
// Idempotent: checks container existence, image build, and keys before mutating.
func Provision(ctx context.Context, mgr *config.Manager) (*ProvisionResult, error) {
	cfg := mgr.Get()
	vps := &cfg.VPS

	slog.Info("provisioner: starting provisioning",
		"host", vps.Host,
		"provisioned", vps.IsProvisioned,
	)

	// --- Already provisioned? ---
	if vps.IsProvisioned && vps.ContainerName != "" {
		slog.Info("provisioner: already provisioned, skipping", "container", vps.ContainerName)
		return &ProvisionResult{ContainerID: vps.ContainerName}, nil
	}

	// --- Step 1: Establish SSH connection via pooled Client ---
	client, err := NewClient(*vps)
	if err != nil {
		return nil, fmt.Errorf("provisioner: create SSH client: %w", err)
	}
	defer client.Close()

	slog.Info("provisioner: SSH client ready", "host", vps.Host)

	// --- Step 2: Generate initial AWG config (needed for port/subnet in compose + start.sh) ---
	awgCfg, err := GenerateAWGConfig()
	if err != nil {
		return nil, fmt.Errorf("provisioner: generate AWG config: %w", err)
	}

	// --- Step 3: Generate Dockerfile, start.sh, compose.yml ---
	dfCfg := DockerfileConfig{
		Subnet:          awgCfg.Subnet,
		AWGToolsRelease: "1.0.20250901",
	}
	dockerfile, err := GenerateDockerfile(dfCfg)
	if err != nil {
		return nil, fmt.Errorf("provisioner: generate Dockerfile: %w", err)
	}
	startSh, err := GenerateStartSh(dfCfg)
	if err != nil {
		return nil, fmt.Errorf("provisioner: generate start.sh: %w", err)
	}

	composeCfg := ComposeConfig{
		AWGPort:    awgCfg.ListenPort,
		ManualDNS:  cfg.DNS.Provider == "" || cfg.DNS.Provider == "manual",
		CaddyImage: caddyImage(cfg),
	}
	composeYML, err := GenerateCompose(composeCfg)
	if err != nil {
		return nil, fmt.Errorf("provisioner: generate compose: %w", err)
	}

	// --- Step 4: Create remote directory and upload files ---
	const remoteDir = "/opt/unet"
	if _, _, err := client.ExecuteCommand(ctx, fmt.Sprintf("mkdir -p %s", remoteDir)); err != nil {
		return nil, fmt.Errorf("provisioner: mkdir %s: %w", remoteDir, err)
	}

	files := map[string][]byte{
		"Dockerfile":  dockerfile,
		"start.sh":    startSh,
		"compose.yml": composeYML,
	}
	for name, content := range files {
		if err := client.UploadFile(ctx, remoteDir+"/"+name, content); err != nil {
			return nil, fmt.Errorf("provisioner: upload %s: %w", name, err)
		}
		slog.Debug("provisioner: uploaded", "file", name)
	}

	// --- Step 5: docker compose up -d --build (v2 syntax) ---
	upCmd := fmt.Sprintf("cd %s && docker compose up -d --build", remoteDir)
	if stdout, stderr, err := client.ExecuteCommand(ctx, upCmd); err != nil {
		return nil, fmt.Errorf("provisioner: docker compose up: %w\nstdout: %s\nstderr: %s",
			err, stdout, stderr)
	}
	slog.Info("provisioner: docker compose up completed")

	// --- Step 6: Wait for container healthy ---
	containerName := "unet-amnezia-awg"
	if err := waitForContainerHealthy(ctx, client, containerName); err != nil {
		return nil, fmt.Errorf("provisioner: container healthy: %w", err)
	}

	// --- Step 7: Write AWG config into container ---
	if err := writeAWGConfigToContainer(ctx, client, containerName, awgCfg); err != nil {
		return nil, fmt.Errorf("provisioner: write AWG config to container: %w", err)
	}

	// --- Step 8: Run awg-quick up awg0 inside container ---
	upAwgCmd := fmt.Sprintf("docker exec %s awg-quick up awg0", containerName)
	if stdout, stderr, err := client.ExecuteCommand(ctx, upAwgCmd); err != nil {
		return nil, fmt.Errorf("provisioner: awg-quick up: %w\nstdout: %s\nstderr: %s",
			err, stdout, stderr)
	}
	slog.Info("provisioner: awg-quick up awg0 completed")

	// --- Step 9: Persist results to config ---
	if err := mgr.Update(func(c *config.RootConfig) {
		c.VPS.IsProvisioned = true
		c.VPS.ContainerName = containerName
		c.Tunnel.InterfaceName = "awg0"
		c.Tunnel.Subnet = awgCfg.Subnet
		c.Tunnel.ServerIP = serverIPFromSubnet(awgCfg.Subnet)
		c.Tunnel.ServerEndpoint = fmt.Sprintf("%s:%d", c.VPS.Host, awgCfg.ListenPort)
		c.Tunnel.ServerPublicKey = awgCfg.ServerPublicKey
		c.Tunnel.PresharedKey = config.SecretString(awgCfg.PresharedKey)
		c.Tunnel.Status = "provisioned"
		c.Tunnel.Obfuscation = config.ObfuscationConfig{
			Jc:   awgCfg.Obfuscation.Jc,
			Jmin: awgCfg.Obfuscation.Jmin,
			Jmax: awgCfg.Obfuscation.Jmax,
			S1:   awgCfg.Obfuscation.S1,
			S2:   awgCfg.Obfuscation.S2,
			S3:   awgCfg.Obfuscation.S3,
			S4:   awgCfg.Obfuscation.S4,
			H1:   awgCfg.Obfuscation.H1,
			H2:   awgCfg.Obfuscation.H2,
			H3:   awgCfg.Obfuscation.H3,
			H4:   awgCfg.Obfuscation.H4,
			I1:   awgCfg.Obfuscation.I1,
			I2:   awgCfg.Obfuscation.I2,
			I3:   awgCfg.Obfuscation.I3,
			I4:   awgCfg.Obfuscation.I4,
			I5:   awgCfg.Obfuscation.I5,
		}
	}); err != nil {
		return nil, fmt.Errorf("provisioner: update config: %w", err)
	}

	slog.Info("provisioner: provisioning complete", "container", containerName)
	return &ProvisionResult{
		ContainerID: containerName,
		AWGConfig:   awgCfg,
	}, nil
}

// ---------- container health helpers ----------

// waitForContainerHealthy polls docker inspect until the container reports
// healthy or the context is cancelled.
func waitForContainerHealthy(ctx context.Context, client *Client, containerName string) error {
	const (
		interval = 3 * time.Second
		timeout  = 120 * time.Second
	)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		cmd := fmt.Sprintf(
			"docker inspect --format='{{.State.Health.Status}}' %s 2>/dev/null || echo 'not_found'",
			containerName,
		)
		stdout, _, err := client.ExecuteCommand(ctx, cmd)
		if err != nil {
			return fmt.Errorf("docker inspect: %w", err)
		}

		status := strings.TrimSpace(stdout)
		switch status {
		case "healthy":
			slog.Info("provisioner: container healthy", "container", containerName)
			return nil
		case "not_found":
			slog.Debug("provisioner: container not yet visible, waiting")
		default:
			slog.Debug("provisioner: container status", "status", status)
		}

		time.Sleep(interval)
	}

	return fmt.Errorf("container %s did not become healthy within %v", containerName, timeout)
}

// ---------- AWG config writing ----------

// writeAWGConfigToContainer generates the awg0.conf content from AWGInitConfig
// and writes it into the container at /opt/amnezia/awg/awg0.conf (matching the
// named volume mount from docker-compose.yml).
func writeAWGConfigToContainer(ctx context.Context, client *Client, container string, cfg *AWGInitConfig) error {
	serverIP := serverIPFromSubnet(cfg.Subnet)

	conf := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/24
ListenPort = %d

Jc = %d
Jmin = %d
Jmax = %d
S1 = %d
S2 = %d
S3 = %d
S4 = %d
H1 = %d
H2 = %d
H3 = %d
H4 = %d
`,
		cfg.ServerPrivateKey,
		serverIP,
		cfg.ListenPort,
		cfg.Obfuscation.Jc,
		cfg.Obfuscation.Jmin,
		cfg.Obfuscation.Jmax,
		cfg.Obfuscation.S1,
		cfg.Obfuscation.S2,
		cfg.Obfuscation.S3,
		cfg.Obfuscation.S4,
		cfg.Obfuscation.H1,
		cfg.Obfuscation.H2,
		cfg.Obfuscation.H3,
		cfg.Obfuscation.H4,
	)

	// Ensure the directory exists inside the container, then write config via
	// heredoc piped through docker exec + tee.
	script := fmt.Sprintf(
		"docker exec %s mkdir -p /opt/amnezia/awg && cat <<'AWGEOF' | docker exec -i %s tee /opt/amnezia/awg/awg0.conf > /dev/null\n%s\nAWGEOF",
		container, container, conf)
	stdout, stderr, err := client.ExecuteScript(ctx, script)
	if err != nil {
		return fmt.Errorf("write awg0.conf: %w\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	slog.Info("provisioner: wrote awg0.conf into container", "container", container)
	return nil
}

// ---------- utility ----------

// serverIPFromSubnet converts "10.8.X.0/24" to "10.8.X.1".
func serverIPFromSubnet(subnet string) string {
	base := strings.TrimSuffix(subnet, ".0/24")
	return base + ".1"
}

// caddyImage returns the appropriate Caddy container image based on DNS config.
func caddyImage(cfg *config.RootConfig) string {
	if cfg.DNS.Provider == "cloudflare" {
		return "unet/caddy-cloudflare:local"
	}
	return "caddy:2-alpine"
}
