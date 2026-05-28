package provisioner

import (
	"bytes"
	"fmt"
	"text/template"
)

// ComposeConfig holds parameters for docker-compose.yml generation.
type ComposeConfig struct {
	// AWGPort is the AmneziaWG listen port (mapped as ${UNET_AWG_PORT}).
	AWGPort int
	// ManualDNS is true when DNS mode is "manual" (HTTP-01 challenge
	// needs port 80/tcp).  When false (Cloudflare DNS-01 mode), port 80
	// is omitted from the compose file.
	ManualDNS bool
	// CaddyImage is the container image for the caddy service.
	// "unet/caddy-cloudflare:local" when dns.mode=="cloudflare",
	// "caddy:2-alpine" otherwise.
	CaddyImage string
}

// composeTmpl is the docker-compose.yml template.  It uses Go
// text/template so the caller can render to a string or file.
//
// Pause-container pattern (RV1): A lightweight "unet-net-pause" service
// (busybox sleep infinity) owns the network namespace and declares all
// port mappings.  Both unet-amnezia-awg and unet-caddy attach via
// network_mode: "service:unet-net-pause".  This ensures that if
// amnezia-awg restarts, Caddy does NOT lose its network stack — the
// pause container holds it stable.  This is the same pattern used by
// Kubernetes pods.
var composeTmpl = template.Must(template.New("compose").Parse(`services:
  unet-net-pause:
    image: busybox:1.36-uclibc
    container_name: unet-net-pause
    command: ["sleep", "infinity"]
    ports:
      - "{{.AWGPort}}:{{.AWGPort}}/udp"
      - "443:443/tcp"{{ if .ManualDNS }}
      - "80:80/tcp"{{ end }}
    restart: unless-stopped

  unet-amnezia-awg:
    build:
      context: .
      dockerfile: Dockerfile
    image: unet/amnezia-awg:local
    container_name: unet-amnezia-awg
    network_mode: "service:unet-net-pause"
    depends_on:
      - unet-net-pause
    cap_add:
      - NET_ADMIN
      - SYS_MODULE
    sysctls:
      - net.ipv4.ip_forward=1
      - net.ipv6.conf.all.forwarding=1
    volumes:
      - amnezia-awg-state:/opt/amnezia/awg
      - /lib/modules:/lib/modules:ro
    restart: unless-stopped

  unet-caddy:
    image: {{.CaddyImage}}
    container_name: unet-caddy
    network_mode: "service:unet-net-pause"
    depends_on:
      - unet-net-pause
    volumes:
      - caddy-data:/data
      - caddy-config:/config
    restart: unless-stopped

volumes:
  amnezia-awg-state:
  caddy-data:
  caddy-config:
`))

// GenerateCompose renders a docker-compose.yml from cfg and returns it
// as a byte slice.
func GenerateCompose(cfg ComposeConfig) ([]byte, error) {
	if cfg.AWGPort <= 0 || cfg.AWGPort > 65535 {
		return nil, fmt.Errorf("provisioner: invalid AWGPort %d (must be 1..65535)", cfg.AWGPort)
	}
	if cfg.CaddyImage == "" {
		cfg.CaddyImage = "caddy:2-alpine"
	}

	var buf bytes.Buffer
	if err := composeTmpl.Execute(&buf, cfg); err != nil {
		return nil, fmt.Errorf("provisioner: compose template: %w", err)
	}
	return buf.Bytes(), nil
}
