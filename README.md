# Unet

Self-hosted tunneling solution — expose local ports to the internet via your own VPS. Think Ngrok, but you own the infra.

Built on **AmneziaWG** (WireGuard fork with DPI bypass) for encrypted tunnels and **Caddy** for automatic HTTPS reverse proxying. Single Go binary with embedded React UI.

## Features

- **One-click VPS provisioning** — SSH in, install Docker, deploy AmneziaWG + Caddy stack
- **WireGuard tunnel** — AmneziaWG with obfuscation parameters for DPI-resistant connections
- **Port exposure** — pick a local port + subdomain, get `https://subdomain.yourdomain.com` in ~2 seconds
- **Control Plane API** — remote-accessible, authenticated, and audited API for programmatic management (routes, peers, tunnel status)
- **VPS Lifecycle Management** — automated bootstrap, health probing, drift detection, migration, and encrypted state backups
- **Observability** — structured logging, SSE log streaming, container log aggregation, and Prometheus metrics
- **Dashboard** — live tunnel status, exposed ports, DNS configuration
- **mTLS** — mutual TLS between daemon and Caddy admin API
- **DNS modes** — Cloudflare API (auto A-record + wildcard cert) or manual DNS
- **Secret redaction** — all secrets masked in logs and API responses (`****last4`)

## Architecture

See [architecture.md](specs/main/architecture.md) for detailed diagrams and subsystem breakdowns.

## Prerequisites

| Requirement | Why |
|---|---|
| **AmneziaWG client** | `awg-quick` must be on PATH. Download from [amnezia.org](https://amnezia.org/) |
| **Administrator/root** | WireGuard needs elevated privileges to create network interfaces |
| **VPS** (Ubuntu 20.04+) | With SSH access; Docker is installed automatically |
| **Go 1.25+** | Building the daemon |
| **Node.js 20+** | Building the React frontend |
| **Cloudflare account** | Optional. API token with `Zone:Read + DNS:Edit` for auto-DNS |

## Quick Start

### Build from source

```bash
cd src

# Build everything (frontend + backend)
make build

# Or manually:
cd web && npm install && npm run build && cd ..
go build -o bin/unet ./cmd/unet
```

### Run

```bash
# Linux / macOS
sudo ./bin/unet

# Windows — run as Administrator
.\src\bin\unet.exe
```

Open `http://localhost:8080` in your browser.

### Provision & expose

1. Enter VPS SSH credentials in the UI
2. Wait for provisioning to complete (Docker install + compose deploy)
3. Click **Connect** to establish the WireGuard tunnel
4. Pick a local port + subdomain, click **Expose**
5. Your service is live at `https://subdomain.yourdomain.com`

## Configuration

| Path | Purpose |
|---|---|
| `~/.unet/config.json` | Main config (VPS credentials, tunnel params, exposed ports, DNS settings) |

| Env Var | Default | Description |
|---|---|---|
| `UNET_PORT` | `8080` | HTTP listen port for control API |
| `UNET_CONFIG` | `~/.unet/config.json` | Path to config file |
| `UNET_HOST` | `127.0.0.1` | Bind address. Set `0.0.0.0` for LAN access (off by default) |

## Development

```bash
cd src

make dev          # Run Go + Vite dev servers in parallel
make build        # Production build
make test         # Run all tests (Go + web)
make test-go      # Go tests only
make test-web     # Web tests only
make lint         # Lint Go + web
make cross-build  # Build for all platforms
make clean        # Remove build artifacts
```

## Security

- **mTLS** on Caddy admin API — daemon and Caddy authenticate each other via client/server certs
- **File permissions** — `~/.unet/` files created with `0600` (POSIX) or owner-only ACLs (Windows)
- **Secret redaction** — passwords, keys, and tokens masked to `****<last4>` in logs and API responses
- **Localhost-only** — daemon binds `127.0.0.1` by default; no remote access to control API
- **Control Plane Auth** — API tokens with scoped permissions (`read`/`write`/`admin`)
- **SSH** — VPS managed exclusively over SSH (key or password auth)

## License

[MIT](LICENSE)
