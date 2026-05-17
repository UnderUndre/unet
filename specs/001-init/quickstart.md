# Quickstart: Unet Core Architecture

This guide covers how to run the Unet project locally during development.

## Prerequisites

1. **Go 1.22+**: Required for building the backend daemon.
2. **Node.js 20+ & npm/pnpm**: Required for building the Vite/React frontend.
3. **AmneziaWG Client**: Must be installed on your OS. The daemon uses `awg-quick` CLI to manage the tunnel. Download from [amnezia.org](https://amnezia.org/).
4. **Administrator/Root privileges**: Running the resulting binary requires elevated privileges to manipulate network interfaces.

## Building

### Using Makefile (recommended)

```bash
cd src
make build    # builds frontend + Go binary -> src/bin/unet
```

### Manual build

The React UI must be built before compiling the Go daemon, as the Go compiler embeds the output via `go:embed`.

```bash
# Frontend
cd src/web
npm install
npm run build

# Backend
cd ..
go build -o bin/unet ./cmd/unet
```

## Running

```bash
# Linux / macOS
sudo ./bin/unet

# Windows (Run As Administrator in PowerShell or CMD)
.\bin\unet.exe
```

The daemon starts and serves the embedded UI at `http://localhost:8080`.

> **LAN access**: daemon binds `127.0.0.1` by default. To access the UI from another device on the same network, set `UNET_HOST=0.0.0.0`. This is off by default for security — the control API has no authentication beyond the session token.

## Testing the Pipeline

1. **Mock the VPS**: Use a local VM (e.g., Multipass) or a cheap DigitalOcean droplet (Ubuntu 22.04/24.04 LTS, Docker preinstalled) for testing the provisioning scripts.
2. **Setup**: Provide the SSH credentials to the UI. Choose DNS mode:
   - Cloudflare → paste API token with `Zone:Read + DNS:Edit` scopes (never the global API key).
   - Manual → pre-configure wildcard A-record `*.yourdomain.com` pointing to the VPS public IP.
3. **Connect**: Verify the daemon successfully adds the AmneziaWG interface and connects. Ping the server IP (`tunnel.serverIp`) — if that works, the tunnel and obfuscation parameters are correct.
4. **Expose**: Start a local web server.

   ⚠️ **The app MUST bind `0.0.0.0`, NOT `127.0.0.1`.** Caddy on the VPS reaches your local port via the WireGuard interface — apps listening only on loopback are unreachable.

   Examples that bind correctly:
   ```bash
   # Python — default binds 0.0.0.0
   python3 -m http.server 3000

   # Node http-server
   npx http-server -a 0.0.0.0 -p 3000

   # Vite dev server (default is 127.0.0.1 — explicitly override)
   npm run dev -- --host 0.0.0.0

   # Next.js dev (default is 0.0.0.0; just confirm)
   next dev -p 3000

   # Docker — publish to host's 0.0.0.0
   docker run -p 0.0.0.0:3000:3000 <image>
   ```

5. Use the UI to expose port 3000 to a subdomain. Within ~2 seconds the Caddy route should be live; if Cloudflare mode is selected, the A-record is also created automatically.
6. Verify public access from a separate network (e.g., turn off Wi-Fi on your phone and load via cellular).

## Troubleshooting

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| `awg-quick: command not found` | AmneziaWG client not on PATH | Install from [amnezia.org](https://amnezia.org/), then ensure CLI directory is on PATH |
| Daemon won't start, "privileges required" | Not running as root/Administrator | `sudo` on POSIX; "Run As Administrator" on Windows |
| `connection refused` from exposed subdomain | Local app bound to `127.0.0.1` | Rebind app to `0.0.0.0` (see step 4) |
| `Failed authorization limit reached` from Let's Encrypt | Manual DNS mode + many subdomains | Switch to Cloudflare mode (wildcard cert via DNS-01) |
| Tunnel "connected" but ping `tunnel.serverIp` fails | MTU mismatch | Daemon sets `MTU = 1280` automatically; check VPS firewall is open on UDP `<ListenPort>` |
| UI banner: "server config drifted" | Someone (or another tool, e.g., Amnezia Desktop) modified `awg0.conf` on the VPS | Click "Re-sync" — daemon will re-fetch obfuscation params |
