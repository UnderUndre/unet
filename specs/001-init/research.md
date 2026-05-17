# Research: Unet Architecture & Technical Solutions

## 1. Embedding React in a Go Binary

**Decision**: Use Go 1.16+ `//go:embed` with Vite.
**Rationale**: Compiling the React application into static files (HTML/CSS/JS) via Vite and embedding them directly into the Go binary provides the simplest distribution model. The Go standard library `http.FileServer` natively supports serving embedded files via `http.FS`. This eliminates the need to ship separate frontend assets or rely on heavy frameworks like Tauri.
**Alternatives considered**:
- **Tauri / Wails**: Wraps the web view in a native window. Rejected because the daemon runs in the background and managing native GUI windows complicates cross-platform builds unnecessarily. A browser-based localhost UI is sufficient.

## 2. Managing AmneziaWG Interfaces (Client-Side)

**Decision**: Use `awg-quick` (AmneziaWG CLI) via `os/exec` + dynamically-generated `.conf` files.

**Rationale**: AmneziaWG extends WireGuard with handshake-time and packet-shape obfuscation that the standard `wgctrl` Go library cannot configure. Using the official `awg-quick` CLI guarantees 100% compatibility with the AmneziaWG server. The Go daemon generates `.conf` files that match the server's obfuscation profile and invokes `awg-quick up/down`. Error handling via exit codes and stderr parsing. The user must have AmneziaWG client installed (CLI on PATH — verified at daemon startup via `exec.LookPath("awg-quick")`).

### 2.1 Complete Obfuscation Parameter Set

The full set per official `amneziawg-go` documentation (https://github.com/amnezia-vpn/amneziawg-go) — **all must be propagated from server to client identically, or handshake fails**:

| Param | Role | Format | Notes |
|-------|------|--------|-------|
| `Jc` | Junk packet count | int (recommend 4-12) | Pre-handshake noise packets |
| `Jmin` | Junk min size | int (bytes) | Lower bound for junk |
| `Jmax` | Junk max size | int (bytes, **< MTU**) | Upper bound; > MTU = fragmentation |
| `S1` | Padding: handshake initiation | int (bytes) | Adds random tail bytes |
| `S2` | Padding: handshake response | int (bytes) | |
| `S3` | Padding: cookie reply | int (bytes) | |
| `S4` | Padding: transport messages | int (bytes) | |
| `H1` | Header type: handshake init | int OR range `"X-Y"` | Replaces WG default `1`. Ranges = random per-packet pick within range. Must not overlap with H2/H3/H4. |
| `H2` | Header type: handshake response | int OR range | Replaces WG default `2` |
| `H3` | Header type: cookie reply | int OR range | Replaces WG default `3` |
| `H4` | Header type: transport data | int OR range | Replaces WG default `4` |
| `I1` | Signature/mimicry packet #1 | tag DSL (see below) | Sent before handshake |
| `I2..I5` | Signature/mimicry packets 2-5 | tag DSL | Sent in order; unspecified = skipped |

**`I*` tag DSL** (concatenable):

| Tag | Meaning |
|-----|---------|
| `<b 0x[hex]>` | Static bytes, hex-encoded |
| `<r N>` | N random bytes |
| `<rd N>` | N random digits `0-9` |
| `<rc N>` | N random ASCII chars `a-zA-Z` |
| `<t>` | 4-byte UNIX timestamp |

Real production example (observed on `amnezia-awg2` container):

```
I1 = <b 0x084481800001000300000000077469636b65747306776964676574096b696e6f706f69736b0272750000010001c00c0005000100000039001806776964676574077469636b6574730679616e646578c025c0390005000100000039002b1765787465726e616c2d7469636b6574732d776964676574066166697368610679616e646578036e657400c05d000100010000001c000457fafe25>
```

→ This is a hex-encoded DNS-response packet impersonating a lookup for `tickets.widget.kinopoisk.ru` — DPI sees a "valid DNS reply" before the actual handshake.

### 2.2 Server Config Retrieval — No API Exists

**Critical architectural reality**: AmneziaWG provides **no server-side management API**. The Amnezia Desktop client manages servers via SSH + `docker exec`. Unet must follow the same pattern:

```
1. SSH to VPS
2. docker exec <container> cat /opt/amnezia/awg/awg0.conf
3. Parse Junk/S/H/I/Address/ListenPort + PublicKey
4. Use values to construct local client .conf
```

### 2.3 Peer Add/Remove — Hot-Reload Pattern

Two CLI primitives used together:

- `awg-quick strip <conf-path>` — emit conf with `wg-quick`-extras (Address, DNS, MTU, Pre/PostUp/Down) stripped
- `awg syncconf <iface> <stripped-conf>` — apply config diff WITHOUT dropping active peer sessions

Standard hot-reload pattern (per `amneziawg-tools` docs):

```bash
awg syncconf awg0 <(awg-quick strip /opt/amnezia/awg/awg0.conf)
```

Per-peer add (without rewriting the whole file):

```bash
awg set awg0 peer <CLIENT_PUBKEY> \
  preshared-key <(echo <CLIENT_PSK>) \
  allowed-ips 10.8.1.2/32 \
  persistent-keepalive 25
```

Per-peer remove:

```bash
awg set awg0 peer <CLIENT_PUBKEY> remove
```

**Alternatives considered**:
- `wgctrl` Go library — rejected, cannot configure AmneziaWG `J*/S*/H*/I*` parameters. Standard WG handshake fails against obfuscated server.
- Fork `wgctrl` with AmneziaWG support — rejected, ongoing maintenance burden on a low-level networking library.
- `wireguard-go` userspace fallback — rejected for client, but it IS what `amneziawg-go` is forked from. On macOS, `awg-quick` invokes `amneziawg-go` userspace; on Linux, it can use the kernel module `amneziawg-linux-kernel-module` when loaded.

### 2.4 Platform Interface Naming

- **Linux**: Interface name = filename of `.conf` (e.g., `/etc/amneziawg/awg0.conf` → `awg0`). Convention: `awg0`.
- **macOS**: `awg-quick` uses `utun*` automatic naming (kernel-assigned).
- **Windows**: AmneziaWG Windows installer creates TUN adapter with system-generated GUID name. Unet daemon must NOT hardcode `wg0`; must enumerate active interfaces via OS APIs after `awg-quick up`.

### 2.5 PersistentKeepalive + MTU

- `PersistentKeepalive = 25` (seconds) MUST be set on client peer — without it, NAT mapping drops after idle and reconnect requires manual interaction.
- `MTU` in `[Interface]`: set to **1280** to leave headroom for AmneziaWG junk-padding overhead. Default WG (1420) causes fragmentation when Junk `S*` padding pushes packets near link MTU.

## 3. Server-Side AmneziaWG Container (Dockerfile Reference)

**Decision**: Unet provisions the AmneziaWG server by building a custom Alpine-based image directly on the VPS (no public registry). Image is constructed from upstream `amneziawg-tools` GitHub release + `amneziawg-go` userspace binary.

**Rationale**: This matches the upstream Amnezia Desktop client model. Pulling from a public registry adds a fingerprint (registry traffic) which DPI can flag.

### 3.1 Reference Dockerfile (reconstructed from production image layers)

```dockerfile
FROM alpine:3.19

# system tuning
RUN apk add --no-cache bash curl dumb-init && \
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

# amneziawg-tools (awg, awg-quick CLI)
ARG AWGTOOLS_RELEASE=1.0.20250901
RUN apk --no-cache add iproute2 iptables && \
    cd /usr/bin/ && \
    wget https://github.com/amnezia-vpn/amneziawg-tools/releases/download/v${AWGTOOLS_RELEASE}/alpine-3.19-amneziawg-tools.zip && \
    unzip -j alpine-3.19-amneziawg-tools.zip && \
    chmod +x /usr/bin/awg /usr/bin/awg-quick && \
    ln -s /usr/bin/awg /usr/bin/wg && \
    ln -s /usr/bin/awg-quick /usr/bin/wg-quick

# amneziawg-go userspace daemon (build separately, copy in)
COPY amneziawg-go /usr/bin/amneziawg-go

LABEL maintainer="unet/amneziawg"

ENTRYPOINT ["dumb-init", "/opt/amnezia/start.sh"]
```

### 3.2 Reference `start.sh` (entrypoint)

```bash
#!/bin/bash
# /opt/amnezia/start.sh — provisioned by unet daemon via SSH on initial setup

set -e
CONF=/opt/amnezia/awg/awg0.conf
SUBNET="${UNET_SUBNET:-10.8.1.0/24}"

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
```

### 3.3 Persistence — Docker Volume Strategy

**Upstream Amnezia keeps `/opt/amnezia/awg/` inside the container** (no volume) — recreate destroys all keys and peers. **Unet must NOT replicate this footgun.** Mount a named volume:

```yaml
# docker-compose.yml fragment (generated by provisioner T008)
services:
  # netns OWNER. Publishes ALL inbound ports (its own UDP + Caddy's TCP)
  # because Caddy shares this netns and cannot declare ports of its own.
  unet-amnezia-awg:
    image: unet/amnezia-awg:local
    container_name: unet-amnezia-awg
    cap_add: [NET_ADMIN, SYS_MODULE]
    sysctls:
      - net.ipv4.ip_forward=1
      - net.ipv6.conf.all.forwarding=1
    ports:
      - "${UNET_AWG_PORT}:${UNET_AWG_PORT}/udp"   # AmneziaWG handshake
      - "443:443/tcp"                              # Caddy HTTPS (proxied via shared netns)
      - "80:80/tcp"                                # Caddy HTTP — only needed for ACME HTTP-01 (manual DNS mode); REMOVE in cloudflare mode
    volumes:
      - amnezia-awg-state:/opt/amnezia/awg
      - /lib/modules:/lib/modules:ro
    restart: unless-stopped

  # netns SHARER. No own network identity — uses unet-amnezia-awg's netns,
  # which means Caddy sees awg0, eth0, lo as if they were its own. NO `ports:`
  # block here — that's owned by the amnezia service above.
  unet-caddy:
    image: unet/caddy-cloudflare:local   # caddy:2-alpine + caddy-dns/cloudflare (built locally if dns.mode=="cloudflare"); plain caddy:2-alpine otherwise
    container_name: unet-caddy
    network_mode: "service:unet-amnezia-awg"
    depends_on:
      - unet-amnezia-awg
    volumes:
      - caddy-data:/data            # Let's Encrypt certs; MUST persist
      - caddy-config:/config        # autosave.json (admin API state)
    restart: unless-stopped

volumes:
  amnezia-awg-state:
  caddy-data:
  caddy-config:
```

**Networking note**: `network_mode: "service:unet-amnezia-awg"` makes Caddy join amnezia's Linux netns. Side-effects: Caddy cannot declare its own `ports:` (only the netns owner can), cannot use compose `networks:` aliases, and `docker exec unet-caddy ip a` will show amnezia's interfaces (including `awg0`). This is exactly what FR-008 needs — Caddy binds admin endpoint to `awg0`'s IP `10.8.1.1:2019` and dials clients via `awg0`.

**Defense-in-depth**: the unet daemon ALSO mirrors `awg0.conf` + `clientsTable` in `~/.unet/server-mirror.json` so it can fully rebuild server state from local config if the volume is lost.

## 4. Dynamic Caddy Routing

**Decision**: Use Caddy's admin REST API, bound to the WireGuard-internal IP (e.g., `10.8.1.1:2019`).

**Rationale**: Caddy's admin API allows live route mutation (`POST /config/apps/http/servers/srv0/routes`) without restarting the service. Binding to the WG-internal address means only authenticated WG peers can reach the admin endpoint.

### 4.1 Authentication Reality Check

The original spec proposed Bearer-token auth. **Caddy v2 has no native Bearer-token middleware for the admin API.** Per `caddyserver.com/docs/json/admin/remote`, the only two supported defense layers are:

1. **IP-binding** (`listen: "10.8.1.1:2019"`) — default in unet; assumes WG peers are trusted
2. **mTLS** via `remote.access_control[].public_keys` (base64 DER client certs)

We adopt **IP-binding as the primary defense**, and **mTLS as an opt-in defense-in-depth layer** for multi-peer deployments where one WG client could attack another. Bearer-token claims are removed from the spec.

### 4.2 Route Removal — Mutex-Guarded, Host-Match

Positional `DELETE /routes/<index>` is unsafe under concurrent operations (index shifts between GET and DELETE). The Go daemon holds a mutex around the GET → match-by-`match[0].host[0]` → DELETE sequence. See `internal/proxy/caddy.go` and contracts/caddy-api.md.

**Alternatives considered**:
- Writing a `Caddyfile` and `SIGHUP` — rejected; less elegant, prone to file corruption under concurrent writes, requires shell scripting on the VPS.
- HTTP Basic auth via reverse-proxy in front of Caddy admin — rejected for v1; overkill.

## 5. SSH and Docker-Compose Automation

**Decision**: Use `golang.org/x/crypto/ssh` with key OR password auth to upload and execute a bash provisioning script (idempotent).

**Rationale**: Native Go SSH avoids depending on system `ssh` binaries. The Go daemon establishes a session, uploads:
- `Dockerfile` (AmneziaWG build)
- `start.sh` (entrypoint)
- `docker-compose.yml` (orchestration)

…then executes `docker compose up -d --build` (v2 plugin syntax). Idempotency: every step checks current state (does container exist? is image built? are keys present?) before mutating.

**Alternatives considered**:
- **Ansible** — rejected, requires Python + Ansible on user's machine.
- **Cloud-init** — rejected, only works at VPS first-boot; doesn't apply to existing VPSes.

## 6. VPS Firewall (UFW) Configuration

**Decision**: Provisioner detects UFW state; if active, opens UDP `<ListenPort>` and TCP `443` automatically. If `ufw` not installed, skip with a notice.

**Rationale**: Many cloud images (DigitalOcean, Linode, Hetzner) ship UFW preconfigured. Without explicit rules, the AmneziaWG handshake fails with no clear diagnostic. Caddy needs TCP/443 for Let's Encrypt + HTTPS traffic.

Commands executed when UFW present:

```bash
ufw allow ${UNET_AWG_PORT}/udp comment 'unet AmneziaWG'
ufw allow 443/tcp comment 'unet Caddy HTTPS'
# 80/tcp only needed if using HTTP-01 challenge (manual DNS mode)
ufw allow 80/tcp comment 'unet Caddy ACME HTTP-01'
```

## 7. DNS Modes (Cloudflare vs Manual)

**Decision**: Two modes, user-selectable.

### 7.1 Cloudflare Automated Mode

- Cloudflare API token with scopes `Zone:Read` + `DNS:Edit` (scope-limited token, NOT global API key).
- Caddy is configured with `caddy-dns/cloudflare` plugin → issues **wildcard certs via DNS-01** (`*.basedomain`).
- Single wildcard cert covers all exposed subdomains → no per-subdomain Let's Encrypt rate-limit risk.
- Unet daemon also auto-creates A-record for each exposed subdomain (or wildcard A-record once).

### 7.2 Manual / Bring-Your-Own DNS Mode

- User pre-configures `*.basedomain` A-record manually.
- Caddy uses HTTP-01 challenge per-subdomain.
- **Rate-limit risk**: Let's Encrypt limits `~50 certs/week per registered domain`. Exposing many short-lived subdomains hits this fast.
- This trade-off is documented in `quickstart.md`.

**Rejected alternative**: Use Caddy's built-in ACME without DNS provider in either mode — Cloudflare mode without the DNS plugin can't do DNS-01 challenge, so wildcard certs become unreachable.

## 8. Why AmneziaWG (vs. Tailscale/Headscale)

**Decision**: AmneziaWG.

**Rationale**:
- **DPI bypass is a hard requirement** (FR-003, success criterion SC-004). Tailscale uses standard WireGuard transport — fingerprintable by modern DPI (TSPU, GFW). Tailscale's DERP relays use HTTPS but still terminate to WireGuard, which obfuscation-aware DPI flags.
- **Headscale** (open-source Tailscale control plane) inherits the same WireGuard transport limitation.
- AmneziaWG ships obfuscation as a first-class feature (`J*/S*/H*/I*`), specifically engineered to evade VPN-detection DPI in restrictive networks.
- Trade-off accepted: no NAT-traversal coordination (DERP-style), so VPS MUST have a publicly reachable IP. This is acceptable for unet's target use case (developer with rented VPS).

## 9. Local Daemon Persistence (JSON, atomic writes)

**Decision**: Single JSON file at `~/.unet/config.json`, written via temp-file + rename (POSIX atomic).

**Rationale**: Single-user, low write frequency (only on user-initiated changes). JSON is human-inspectable for debugging. Atomic-write recipe (temp file + `os.Rename`) handles crash-mid-write on POSIX; on Windows, `MoveFileEx` with `MOVEFILE_REPLACE_EXISTING` provides the same guarantee.

**File permissions**: `0600` on POSIX (`os.Chmod`); Windows ACL `Owner: Read/Write, Everyone: Deny` (via `golang.org/x/sys/windows`).

**Alternatives considered**:
- **BoltDB / SQLite** — rejected for v1; over-engineered for single-user, ~10-100 write/day load. Re-evaluate if concurrent writes become a real problem.
- **Encrypted-at-rest** (e.g., via OS keychain) — rejected for v1; OS file perms are sufficient guard for a single-user binary. Documented as accepted MVP risk.

## 10. Daemon HTTP Server Binding

**Decision**: Bind daemon HTTP to `127.0.0.1:<port>` only (loopback).

**Rationale**: The daemon API has no authentication (accepted MVP risk; see `contracts/daemon-api.md`). Loopback binding is the only thing protecting it from network attacks. Binding `0.0.0.0` would expose VPS-provisioning APIs to any host on the network. Future: optional UUID-token auth read from `~/.unet/token` (file mode 0600), embedded into the React HTML at serve time.
