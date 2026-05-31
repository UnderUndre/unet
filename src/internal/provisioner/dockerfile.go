package provisioner

import (
	"bytes"
	"fmt"
	"text/template"
)

// DockerfileConfig holds parameters for Dockerfile and start.sh generation.
type DockerfileConfig struct {
	// Subnet is the AmneziaWG tunnel subnet (e.g. "10.8.1.0/24").
	// It is embedded into start.sh for iptables MASQUERADE rules.
	Subnet string

	// AWGToolsRelease is the amneziawg-tools GitHub release version.
	// Defaults to "1.0.20250901" per research.md §3.1.
	AWGToolsRelease string

	// AWGGoRelease is the amneziawg-go git tag to clone and build.
	// Defaults to "v0.2.13".
	AWGGoRelease string
}

// dockerfileTmpl generates the reference Dockerfile for the AmneziaWG image.
// Multi-stage build: stage 1 compiles amneziawg-go from source (git clone),
// stage 2 creates minimal Alpine runtime with amneziawg-tools + amneziawg-go.
// Based on the official Dockerfile from amnezia-vpn/amneziawg-go.
var dockerfileTmpl = template.Must(template.New("dockerfile").Parse(`FROM golang:1.24.4-alpine AS awg-builder
RUN apk add --no-cache git gcc musl-dev
RUN git clone --depth 1 --branch {{.AWGGoRelease}} https://github.com/amnezia-vpn/amneziawg-go.git /awg
WORKDIR /awg
RUN go mod download && \
    go mod verify && \
    go build -ldflags '-linkmode external -extldflags "-fno-PIC -static"' -v -o /usr/bin/amneziawg-go

FROM alpine:3.19

RUN apk add --no-cache bash curl dumb-init unzip wget && \
    apk --update upgrade --no-cache && \
    mkdir -p /opt/amnezia

RUN echo -e " \n\
  fs.file-max = 51200 \n\
  net.core.rmem_max = 67108864 \n\
  net.core.wmem_max = 67108864 \n\
  net.core.netdev_max_backlog = 250000 \n\
  net.core.somaxconn = 4096 \n\
  net.ipv4.tcp_syncookies = 1 \n\
  net.ipv4.tcp_tw_reuse = 1 \n\
  net.ipv4.tcp_fin_timeout = 30 \n\
  net.ipv4.tcp_keepalive_time = 1200 \n\
  net.ipv4.ip_local_port_range = 10000 65000 \n\
  net.ipv4.tcp_max_syn_backlog = 8192 \n\
  net.ipv4.tcp_max_tw_buckets = 5000 \n\
  net.ipv4.tcp_fastopen = 3 \n\
  net.ipv4.tcp_mem = 25600 51200 102400 \n\
  net.ipv4.tcp_rmem = 4096 87380 67108864 \n\
  net.ipv4.tcp_wmem = 4096 65536 67108864 \n\
  net.ipv4.tcp_mtu_probing = 1 \n\
  net.ipv4.tcp_congestion_control = hybla \n\
  " | sed -e 's/^\s\+//g' | tee -a /etc/sysctl.conf && \
    mkdir -p /etc/security && \
    echo -e " * soft nofile 51200 \n * hard nofile 51200" | tee -a /etc/security/limits.conf

ARG AWGTOOLS_RELEASE={{.AWGToolsRelease}}
RUN apk --no-cache add iproute2 iptables && \
    cd /usr/bin/ && \
    wget https://github.com/amnezia-vpn/amneziawg-tools/releases/download/v${AWGTOOLS_RELEASE}/alpine-3.19-amneziawg-tools.zip && \
    unzip -j alpine-3.19-amneziawg-tools.zip && \
    chmod +x /usr/bin/awg /usr/bin/awg-quick && \
    ln -s /usr/bin/awg /usr/bin/wg && \
    ln -s /usr/bin/awg-quick /usr/bin/wg-quick

COPY --from=awg-builder /usr/bin/amneziawg-go /usr/bin/amneziawg-go

COPY start.sh /opt/amnezia/start.sh
RUN chmod +x /opt/amnezia/start.sh

LABEL maintainer="unet/amneziaawg"

ENTRYPOINT ["dumb-init", "/opt/amnezia/start.sh"]
`))

// startShTmpl generates the start.sh entrypoint for the AmneziaWG container.
// It includes iptables FORWARD + MASQUERADE rules parameterised by the tunnel
// subnet (research.md §3.2 + T008c).
var startShTmpl = template.Must(template.New("startsh").Parse(`#!/bin/bash
# /opt/amnezia/start.sh — provisioned by unet daemon via SSH on initial setup

set -e
CONF=/opt/amnezia/awg/awg0.conf
SUBNET="{{.Subnet}}"

# Clean shutdown of any prior interface
awg-quick down "$CONF" 2>/dev/null || true

# Bring up the interface (relies on awg0.conf populated by provisioner)
if [ -f "$CONF" ]; then
    awg-quick up "$CONF"
fi

# Forwarding firewall rules — bound to the AmneziaWG interface
iptables -A INPUT   -i awg0 -j ACCEPT
iptables -A FORWARD -i awg0 -j ACCEPT
iptables -A OUTPUT  -o awg0 -j ACCEPT
iptables -A FORWARD -i awg0 -o eth0 -s "$SUBNET" -j ACCEPT
iptables -A FORWARD -i awg0 -o eth1 -s "$SUBNET" -j ACCEPT 2>/dev/null || true
iptables -A FORWARD -m state --state ESTABLISHED,RELATED -j ACCEPT

# Outbound MASQUERADE — clients reach the public internet through eth0/eth1
iptables -t nat -A POSTROUTING -s "$SUBNET" -o eth0 -j MASQUERADE
iptables -t nat -A POSTROUTING -s "$SUBNET" -o eth1 -j MASQUERADE 2>/dev/null || true

# Keep the container alive
exec tail -f /dev/null
`))

// GenerateDockerfile renders the Dockerfile for the AmneziaWG image.
func GenerateDockerfile(cfg DockerfileConfig) ([]byte, error) {
	if cfg.AWGToolsRelease == "" {
		cfg.AWGToolsRelease = "1.0.20250901"
	}
	if cfg.AWGGoRelease == "" {
		cfg.AWGGoRelease = "v0.2.13"
	}

	var buf bytes.Buffer
	if err := dockerfileTmpl.Execute(&buf, cfg); err != nil {
		return nil, fmt.Errorf("provisioner: dockerfile template: %w", err)
	}
	return buf.Bytes(), nil
}

// GenerateStartSh renders the start.sh entrypoint with iptables rules
// parameterised by the tunnel subnet in cfg.
func GenerateStartSh(cfg DockerfileConfig) ([]byte, error) {
	if cfg.Subnet == "" {
		return nil, fmt.Errorf("provisioner: Subnet is required for start.sh generation")
	}

	var buf bytes.Buffer
	if err := startShTmpl.Execute(&buf, cfg); err != nil {
		return nil, fmt.Errorf("provisioner: start.sh template: %w", err)
	}
	return buf.Bytes(), nil
}
