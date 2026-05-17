# Data Model: Unet Configuration

## 1. Local Configuration (`~/.unet/config.json`)

Stores the persistent state of the local daemon across restarts.

```json
{
  "vps": {
    "host": "string",            // IP address or hostname (validated: IPv4/IPv6/FQDN)
    "sshPort": "number",         // Default: 22
    "username": "string",        // Default: root
    "authMode": "string",        // "key" or "password"
    "privateKeyPath": "string",  // Required if authMode == "key"
    "password": "string",        // Required if authMode == "password"; never logged
    "isProvisioned": "boolean",  // Has the setup script run successfully?
    "containerName": "string",   // Docker container name on VPS, e.g. "unet-amnezia-awg"
    "imageBuildHash": "string"   // SHA256 of Dockerfile+entrypoint used; for upgrade detection
  },
  "tunnel": {
    "interfaceName": "string",          // Linux: "awg0"; Windows/macOS: discovered post-up
    "subnet": "string",                 // e.g. "10.8.1.0/24" — read from server config
    "serverIp": "string",               // e.g. "10.8.1.1" — server's address inside the tunnel
    "localIp": "string",                // e.g. "10.8.1.2" — this client's address inside the tunnel
    "serverEndpoint": "string",         // "1.2.3.4:31075" (host:port; port from server config)
    "serverPublicKey": "string",        // VPS WireGuard public key (base64)
    "presharedKey": "string",           // Optional, generated per-peer; if used, must match server
    "privateKey": "string",             // Local AmneziaWG private key (base64); file mode 0600
    "publicKey": "string",              // Local AmneziaWG public key
    "mtu": "number",                    // 1280 default (leaves headroom for junk padding)
    "persistentKeepalive": "number",    // 25 seconds default
    "obfuscation": {
      "Jc":   "number",         // junk packet count
      "Jmin": "number",         // junk min size
      "Jmax": "number",         // junk max size (MUST < MTU)
      "S1":   "number",         // pad: handshake initiation
      "S2":   "number",         // pad: handshake response
      "S3":   "number",         // pad: cookie reply
      "S4":   "number",         // pad: transport
      "H1":   "string",         // header: handshake init — int or "X-Y" range
      "H2":   "string",         // header: handshake response
      "H3":   "string",         // header: cookie reply
      "H4":   "string",         // header: transport data
      "I1":   "string",         // signature/mimicry packet 1 — DSL: <b 0x..>, <r N>, <rd N>, <rc N>, <t>
      "I2":   "string",         // (empty string = not sent)
      "I3":   "string",
      "I4":   "string",
      "I5":   "string"
    },
    "status": "string"          // "disconnected", "connecting", "connected", "error"
  },
  "caddyApi": {
    "endpoint": "string",       // e.g. "http://10.8.1.1:2019" — bound to WG-internal IP
    "authMode": "string",       // "ip-only" or "mtls"
    "mtlsClientCertPem": "string",  // PEM-encoded client cert (only if authMode=="mtls")
    "mtlsClientKeyPem":  "string"   // PEM-encoded client key (only if authMode=="mtls"); file mode 0600
  },
  "exposedPorts": [
    {
      "id": "string",            // UUID v4
      "localPort": "number",     // 1-65535
      "subdomain": "string",     // FQDN, e.g. "app.mydomain.com"; validated against RFC 1035
      "status": "string",        // "active", "pending", "error"
      "lastError": "string"      // error message if status == "error"
    }
  ],
  "dns": {
    "mode": "string",            // "cloudflare" or "manual"
    "cloudflareToken": "string", // Required if mode == "cloudflare"; scopes: Zone:Read + DNS:Edit
    "baseDomain": "string"       // e.g. "mydomain.com"
  },
  "daemon": {
    "port": "number",            // Actual allocated port (default: 8080, falls back if taken)
    "uiToken": "string"          // UUID4 read-protected; embedded into served HTML for /api/* auth
  },
  "serverMirror": {
    "lastSyncedAt": "string",       // ISO 8601 timestamp of last full server-side mirror
    "awgConfRaw": "string",         // Last-known raw awg0.conf from VPS (for diff + volume-loss recovery)
    "awgConfSha256": "string",      // SHA256 of awgConfRaw — drift-detection per FR-010
    "clientsTable": "object",       // Last-known peer registry mirror
    "caddyAdminConfig": "object",   // Last-known /config/caddy/autosave.json with all peers' mTLS pubkeys — required for mTLS-mode volume-loss recovery (see appendix §5.1). null when authMode == "ip-only".
    "serverPrivateKeyB64": "string" // Mirrored server WG private key (file mode 0600 mirror) — required to reconstruct identity after volume loss
  }
}
```

### 1.1 Field Validation Rules

| Field | Constraint |
|-------|------------|
| `vps.host` | IPv4 OR IPv6 (RFC 4291) OR FQDN (RFC 1035). Reject anything containing shell metachars (`;<>|\``$). |
| `vps.sshPort`, `tunnel.serverEndpoint:port` | 1..65535 |
| `exposedPorts[].localPort` | 1..65535 |
| `exposedPorts[].subdomain` | Each label: 1..63 chars, `[a-z0-9-]`, no leading/trailing hyphen. Total ≤253 chars. Must end with `dns.baseDomain`. |
| `dns.cloudflareToken` | Non-empty when `mode == "cloudflare"`; never echoed to logs |
| `tunnel.obfuscation.Jmax` | MUST be `< tunnel.mtu` |
| `tunnel.obfuscation.H1..H4` | No range overlap (validation at sync time) |

### 1.2 File Permissions

| OS | File | Permission |
|----|------|------------|
| Linux/macOS | `~/.unet/config.json` | `0600` (user-only RW) |
| Linux/macOS | `~/.unet/token` | `0600` |
| Windows | `%USERPROFILE%\.unet\config.json` | ACL: Owner=Read+Write; Everyone=Deny |
| Windows | `%USERPROFILE%\.unet\token` | Same ACL |

Atomic write recipe: write to `<file>.tmp` → `os.Rename` (POSIX) / `MoveFileEx(MOVEFILE_REPLACE_EXISTING)` (Windows).

## 2. Server-Side State (Docker)

The remote VPS maintains state via a Docker named volume.

### 2.1 Containers

| Container | Image | Purpose | Network | Persists Via |
|-----------|-------|---------|---------|--------------|
| `unet-amnezia-awg` | `unet/amnezia-awg:local` (built on VPS from reference Dockerfile in `research.md` §3.1) | AmneziaWG VPN endpoint **and netns owner** | Owns its own netns. Publishes `${UNET_AWG_PORT}/udp` (handshake) + `443/tcp` (Caddy HTTPS, since Caddy shares this netns) + `80/tcp` (only if `dns.mode == "manual"` for ACME HTTP-01). Capabilities: `NET_ADMIN`, `SYS_MODULE`. | Volume `amnezia-awg-state` → `/opt/amnezia/awg` |
| `unet-caddy` | `caddy:2-alpine` (with `caddy-dns/cloudflare` plugin if `dns.mode == "cloudflare"`) | TLS-terminating reverse proxy + ACME | **`network_mode: "service:unet-amnezia-awg"`** — shares amnezia's Linux netns. This is mandatory: Caddy must see the `awg0` interface to bind admin endpoint to `10.8.1.1:2019` AND to `dial` client IPs (`10.8.1.x:<localPort>`) which only route through `awg0`. Caddy has NO own `ports:` directive — it cannot, since it does not own a netns. | Volume `caddy-data` → `/data` (Let's Encrypt certs); volume `caddy-config` → `/config` (autosave JSON) |

**Why shared netns**: a Linux network namespace contains a private set of interfaces, routing table, and iptables rules. If Caddy ran in its own netns, it would see only its veth-to-bridge pair — it would NOT see `awg0` (which lives in amnezia's netns) and could not reach `10.8.1.x` peer addresses. `network_mode: "service:<owner>"` is the standard Docker idiom for "co-locate two services in one netns" (equivalent to `--network=container:<owner>` on the CLI). One side-effect: only the netns owner can declare `ports:` in compose.

### 2.2 AmneziaWG Container State (`/opt/amnezia/awg/`)

| File | Purpose | Owner / Mode |
|------|---------|--------------|
| `awg0.conf` | Server config (all `J*/S*/H*/I*` params + `[Peer]` blocks) | `root:root 0600` |
| `clientsTable` | JSON registry of peer metadata (clientId, name, creationDate) — purely informational | `root:root 0600` |
| `wireguard_server_private_key.key` | Server's WG private key (base64, 32 bytes) | `root:root 0600` |
| `wireguard_server_public_key.key` | Server's WG public key | `root:root 0644` |
| `wireguard_psk.key` | Pre-shared key (shared across all peers in this deployment) | `root:root 0600` |

### 2.3 Caddy Container State (`/data/`, `/config/`)

- `/data/caddy/certificates/` — Let's Encrypt issued certs (PEM); MUST persist across container restarts to avoid hitting ACME rate limits.
- `/config/caddy/autosave.json` — Caddy's persisted runtime config (admin API writes here automatically when `config.persist=true`).

### 2.4 Network Topology

The two server-side containers (`unet-amnezia-awg` and `unet-caddy`) share **one Linux network namespace**, owned by amnezia. Caddy has no own veth/IP — it sees the same interfaces (`lo`, `eth0`, `awg0`) that amnezia does. Both processes appear, from the host's perspective, behind the same `${UNET_AWG_PORT}/udp` + `443/tcp` (+`80/tcp`) port mappings declared on the amnezia service.

```
[Internet]
   │  443/TCP, 80/TCP (ACME HTTP-01 if manual DNS mode)
   │  ${UNET_AWG_PORT}/UDP (AmneziaWG handshake)
   ▼
[VPS host iface: eth0 / eth1]
   │  (all three ports above mapped to the SHARED netns below)
   ▼
┌───────────────────────────────────────────────────────────────┐
│  Shared Linux netns (owned by unet-amnezia-awg)               │
│                                                               │
│   ┌─────────────────────┐    ┌─────────────────────┐          │
│   │ amneziawg-go daemon │    │ caddy (admin+HTTPS) │          │
│   │ awg-quick up awg0   │    │ network_mode:       │          │
│   │ iptables MASQUERADE │    │   service:amnezia   │          │
│   └─────────────────────┘    └─────────────────────┘          │
│           │                            │                      │
│           │  bind awg0                 │  bind awg0 + eth0    │
│           │  10.8.1.1                  │  10.8.1.1:2019 admin │
│           │                            │  0.0.0.0:443 HTTPS   │
│           ▼                            ▼                      │
│   ╔══════════════════════════════════════════════════════╗    │
│   ║  awg0  ←── AmneziaWG handshake ──→  [Client awg0]    ║    │
│   ║  10.8.1.1                            10.8.1.2 (etc)  ║    │
│   ╚══════════════════════════════════════════════════════╝    │
└───────────────────────────────────────────────────────────────┘
                                                │
                                                │  Caddy proxies HTTPS
                                                │  via the same awg0,
                                                │  dial 10.8.1.2:<port>
                                                ▼
                                       [Client local app]
                                       (MUST bind 0.0.0.0,
                                        not 127.0.0.1)
```

**Critical**: this layout makes Caddy's `dial 10.8.1.2:<port>` actually work — without shared netns, the upstream IP would be unreachable from Caddy's perspective. It also means client-side apps see incoming traffic on the `awg0` interface (not loopback), reinforcing the "bind 0.0.0.0" requirement documented in `quickstart.md`.

## 3. Field-by-Field Origin & Sync Direction

| Field | Source of Truth | How Client Obtains |
|-------|-----------------|---------------------|
| `tunnel.interfaceName` | Local OS post-`awg-quick up` | Enumerate post-up |
| `tunnel.subnet`, `tunnel.serverIp` | Server `awg0.conf` `[Interface].Address` | SSH+`docker exec cat` parse |
| `tunnel.serverEndpoint.port` | Server `awg0.conf` `[Interface].ListenPort` | SSH+parse |
| `tunnel.serverPublicKey` | Server `/opt/amnezia/awg/wireguard_server_public_key.key` | SSH+`docker exec cat` |
| `tunnel.presharedKey` | Server `/opt/amnezia/awg/wireguard_psk.key` | SSH+`docker exec cat` |
| `tunnel.obfuscation.*` | Server `awg0.conf` `[Interface]` section | SSH+parse |
| `tunnel.localIp` | Allocated by client; written to server's `[Peer].AllowedIPs` | Daemon picks next free `.N` in subnet |
| `tunnel.privateKey`/`publicKey` | Generated locally by `awg genkey` / `awg pubkey` | Local only |
| `caddyApi.endpoint` | Derived: `http://{tunnel.serverIp}:2019` | Computed |
| `caddyApi.mtls*` (if used) | Daemon-generated on first mTLS bootstrap | Local generation + push to Caddy admin via `public_keys` |

**Server-config drift detection**: On every connect, daemon re-fetches `awg0.conf` over SSH, computes SHA256, compares with `serverMirror.awgConfRaw` hash. Mismatch → re-parse + warn UI (server was modified out-of-band, e.g. by Amnezia Desktop client).
