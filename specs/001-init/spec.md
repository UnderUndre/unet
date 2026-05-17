# Feature Specification: Unet Core Architecture

**Feature Branch**: `001-init`  
**Created**: 2026-05-14  
**Status**: Draft  
**Input**: User description: "underundre\helpers\unet\specs\001-init" (Self-hosted Ngrok/Tailscale alternative using AmneziaWG + Caddy with a Go local daemon and embedded web UI)

## Clarifications

### Session 2026-05-15
- Q: What strategy should be used to support AmneziaWG given `wgctrl` incompatibility? → A: Use `awg-quick` via `os/exec` (DPI bypass is strictly required).

### Session 2026-05-16
- Q: Does AmneziaWG provide a server-side management API for adding/removing peers? → A: **No.** Peer management requires SSH + `docker exec` to edit `awg0.conf` and run `awg syncconf` for hot-reload. (Reference: production `amnezia-awg2` container — only `/lib/modules` mount, no exposed sockets/ports beyond the WireGuard UDP listener.)
- Q: Does Caddy v2 support Bearer-token authentication for the admin API? → A: **No.** Native Caddy admin API supports only (a) IP-binding (`listen` directive) and (b) mTLS via `remote.access_control[].public_keys` (base64 DER client certs). Bearer tokens in Caddy docs refer exclusively to outbound Cloudflare API auth, NOT to admin API protection.
- Q: Does AmneziaWG configuration use only `S1, S2, H1-H4` for obfuscation? → A: **No.** Full set is `Jc, Jmin, Jmax, S1-S4, H1-H4 (single or range), I1-I5 (with DSL tags <b>/<r>/<rd>/<rc>/<t>)`. All must propagate from server to client identically.
- Q: Does the AmneziaWG Docker container persist keys across recreates by default? → A: **No.** Reference production setup has no volume mounted on `/opt/amnezia/awg`. Unet must explicitly mount a named Docker volume (`amnezia-awg-state`) to avoid losing all peer state on container recreate.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Easy Server Provisioning (Priority: P1)

As a developer, I want to provide my VPS SSH credentials so that the system automatically sets up AmneziaWG and Caddy without manual configuration.

**Why this priority**: Without the server infrastructure, the local client has nothing to connect to. This is the foundation of the tool.

**Independent Test**: Can be fully tested by providing fresh Ubuntu VPS credentials and verifying that Docker, AmneziaWG, and Caddy are running successfully.

**Acceptance Scenarios**:

1. **Given** a fresh Ubuntu VPS and SSH credentials, **When** I trigger the server setup via the local daemon, **Then** a docker-compose stack with AmneziaWG and Caddy is deployed.
2. **Given** an already configured VPS, **When** I run the setup again, **Then** it gracefully updates or verifies the existing configuration without breaking it.

---

### User Story 2 - Local Tunnel Connection (Priority: P2)

As a developer, I want the local daemon to establish a WireGuard connection to the VPS so that my local machine joins the private network (e.g., 10.8.0.x).

**Why this priority**: The VPN tunnel is required before any traffic routing or port exposure can happen.

**Independent Test**: Can be fully tested by starting the connection and pinging the VPS internal IP (10.8.0.1) from the local machine.

**Acceptance Scenarios**:

1. **Given** a deployed server infrastructure, **When** I start the local daemon, **Then** it automatically applies the WireGuard configuration and establishes a connection.
2. **Given** an active connection, **When** my internet drops and reconnects, **Then** the WireGuard tunnel automatically recovers.

---

### User Story 3 - Local Port Exposure (Priority: P3)

As a developer, I want to specify a local port (e.g., 3000) and a subdomain, so that my local app is accessible publicly via HTTPS without manual reverse proxy configuration.

**Why this priority**: This is the core "Ngrok-like" functionality that delivers the primary value to the end user.

**Independent Test**: Can be fully tested by running a local web server, exposing it via the UI, and accessing it from a mobile device over cellular data.

**Acceptance Scenarios**:

1. **Given** an active tunnel and a running local app on port 3000, **When** I request exposure for `app.mydomain.com`, **Then** the local daemon configures Caddy dynamically via its REST API.
2. **Given** a newly exposed port, **When** I navigate to `https://app.mydomain.com`, **Then** traffic securely routes to my local app.

---

### User Story 4 - Local Web UI Configuration (Priority: P4)

As a developer, I want to manage my VPS connections and exposed ports via a local web interface (localhost:8080) so that I have a clear, cross-platform GUI.

**Why this priority**: The CLI/daemon works, but the GUI lowers the barrier to entry and improves the UX.

**Independent Test**: Can be fully tested by opening the browser to the daemon's port and configuring a new exposed service.

**Acceptance Scenarios**:

1. **Given** the daemon is running, **When** I navigate to `localhost:8080`, **Then** I see the current status of my VPS, tunnel, and exposed ports.
2. **Given** the daemon is not running with Administrator/Root privileges, **When** I open the UI, **Then** I see a clear error indicating elevated privileges are required for WireGuard.

### Edge Cases

- What happens when the requested local port is already occupied by another service?
- How does system handle Caddy Let's Encrypt rate limits or DNS propagation delays? (In manual DNS mode, hitting `~50 certs/week` per registered domain is realistic.)
- What happens when the local daemon is launched without Administrator/root privileges?
- How does system handle conflicting WireGuard interfaces if the user already has a VPN running?
- What happens if the AmneziaWG Docker container is recreated and the `amnezia-awg-state` volume is lost? (All client peers invalidated; daemon must detect via server-config drift hash and offer re-enrollment.)
- What happens when the server's `awg0.conf` is modified out-of-band (e.g., by Amnezia Desktop Client simultaneously managing the same VPS)? (Drift detection on connect; client must re-pull obfuscation params or refuse to connect on hash mismatch.)
- What happens when the user's local app binds only to `127.0.0.1` rather than `0.0.0.0`? (Caddy upstream `dial: <client-vpn-ip>:<port>` fails; documented in quickstart as a requirement: app MUST bind `0.0.0.0`.)
- What happens when `awg-quick` is not on PATH on the user's machine? (Daemon refuses to start with a clear error pointing to AmneziaWG installation docs.)
- What happens when two daemon instances are launched simultaneously? (Single-instance lock via pidfile / Windows named mutex; second instance exits with clear error.)
- What happens if Cloudflare API token is revoked mid-session? (Daemon surfaces error via `/api/status`; affected exposed ports go to `"status": "error"` with token-rotation guidance.)
- What happens on **first daemon run** when `~/.unet/config.json` does not exist? (Daemon MUST create `~/.unet/` directory with `0700`, write a default config skeleton with empty `exposedPorts: []`, `tunnel.status: "disconnected"`, `vps.isProvisioned: false`, and `daemon.uiToken` populated with a freshly-generated UUIDv4 — then proceed to the first-time-setup UI.)
- What happens when persisted `tunnel.status: "connected"` after a daemon crash, but the kernel-level interface is actually gone? (FR-010 sub-2 reconciliation: `awg show` probe on startup reveals the discrepancy → status reset to `"disconnected"` → re-initiate `awg-quick up`.)
- What happens when the user enters a Unicode subdomain (e.g., `мой-сайт.mydomain.com`)? (FR-012 subdomain regex is ASCII-only; daemon MUST reject with a clear UI message suggesting Punycode conversion. Reason: Caddy-side cert matching, Cloudflare DNS API, and Let's Encrypt all operate on ASCII labels; passing Unicode would silently produce broken routes.)
- What happens at midnight UTC if the user's machine is on a different timezone? (All timestamps in `config.json` and API responses MUST be ISO-8601 with explicit `Z` suffix or `±HH:MM` offset; UI MAY render in user-local TZ but storage is always normalized.)

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST automate the deployment of AmneziaWG and Caddy on a target Linux VPS via SSH, including:
  - Building the AmneziaWG Docker image locally on the VPS from a reference Dockerfile (no public registry pull — see `research.md` §3.1).
  - Generating `docker-compose.yml` that:
    - mounts a persistent Docker named volume (`amnezia-awg-state`) on `/opt/amnezia/awg` to survive container recreates;
    - declares the `unet-caddy` service with `network_mode: "service:unet-amnezia-awg"` so Caddy shares the AmneziaWG container's Linux network namespace (Caddy MUST see the `awg0` interface to bind its admin endpoint to the WG-internal IP and to `dial` client IPs through the tunnel; running Caddy in its own netns is physically incompatible with FR-008);
    - publishes ALL inbound ports (`${UNET_AWG_PORT}/udp`, `443/tcp`, and `80/tcp` in manual DNS mode) on the `unet-amnezia-awg` service since it owns the shared netns and is the only service permitted to declare `ports:` mappings.
  - Configuring iptables FORWARD + MASQUERADE rules for the tunnel subnet and `net.ipv4.ip_forward = 1`.
  - Detecting UFW; if active, opening UDP `<ListenPort>` and TCP `443` (plus `80` if manual DNS mode).
  - Idempotency: every step verifies current state before mutating.
- **FR-002**: System MUST generate and manage AmneziaWG client configurations securely:
  - Generate client keypair locally (`awg genkey` / `awg pubkey`); private key stored in `~/.unet/config.json` (file mode `0600` POSIX / ACL deny-others on Windows).
  - Add the new peer to the server's `awg0.conf` via SSH, then `awg syncconf` for hot-reload (no peer-drop).
  - Mirror the server's `clientsTable` JSON for metadata (clientId, name, creationDate) compatible with Amnezia Desktop Client.
- **FR-003**: System MUST provide a Go-based local daemon that manages the local AmneziaWG interface by invoking `awg-quick` (AmneziaWG CLI) via `os/exec` — NOT `wgctrl` — to support the full obfuscation parameter set required for DPI bypass:
  - `Jc`, `Jmin`, `Jmax` (junk packets)
  - `S1`, `S2`, `S3`, `S4` (message padding for handshake init, response, cookie, transport)
  - `H1`, `H2`, `H3`, `H4` (custom message-type IDs; single int OR range `"X-Y"`; no overlap)
  - `I1`-`I5` (optional signature/mimicry packets with DSL tags `<b 0x..>`, `<r N>`, `<rd N>`, `<rc N>`, `<t>`)
  
  All parameters MUST be fetched from the server's `awg0.conf` via SSH+`docker exec cat` and copied verbatim to the client `.conf`. The daemon MUST also set `PersistentKeepalive = 25` and `MTU = 1280` on the client interface.
- **FR-003a**: System MUST verify `awg-quick` is on PATH at daemon startup via `exec.LookPath`, exiting with a clear installation-instructions error if absent.
- **FR-004**: System MUST dynamically update Caddy routing configurations via Caddy's admin REST API without restarting the Caddy service. Route deletion MUST be host-matched (not positional-index) under a mutex guard.
- **FR-005**: System MUST serve an embedded web interface on a local port (e.g., 8080) from the single compiled binary. The HTTP server MUST bind to `127.0.0.1` only (loopback) — never `0.0.0.0`.
- **FR-006**: System MUST require and verify Administrator/root privileges on the local machine before attempting to manipulate network interfaces.
- **FR-007**: System MUST use a dynamic port allocation strategy for the web UI if the default port is occupied, and MUST enforce single-instance execution via a pidfile (POSIX) or named mutex (Windows).
- **FR-008**: System MUST authenticate Caddy admin REST API calls using one of two mechanisms (selected by user):
  1. **IP-binding (default)** — bind the Caddy admin endpoint to the WG-internal IP (e.g., `http://10.8.1.1:2019`). Only WireGuard peers can reach it. Sufficient for single-peer deployments.
  2. **mTLS (defense-in-depth)** — bind to WG-internal IP AND require client certificate matching `remote.access_control[].public_keys`. Daemon generates a self-signed client cert on first connect and registers its DER-encoded public key via an initial bootstrap call (over the IP-only window before mTLS is enforced).
  
  Bearer-token authentication is NOT supported because Caddy v2 admin API has no native Bearer middleware (confirmed by upstream documentation review).
- **FR-009**: System MUST manage DNS records for the user's domain by offering two configuration modes:
  1. **Cloudflare Automated Mode**: Caddy on the VPS is provisioned with the `caddy-dns/cloudflare` plugin and issues a **wildcard certificate** via DNS-01 challenge (`*.basedomain`) using a Cloudflare API token (scopes: `Zone:Read` + `DNS:Edit` — never the global API key). The daemon also auto-creates/updates A-records for each exposed subdomain. Single wildcard cert avoids per-subdomain rate-limit risk.
     - **Single-label constraint** (per RFC 6125 wildcard semantics): exposed subdomains MUST have **exactly one label** between the leftmost dot and `dns.baseDomain`. Example: `app.mydomain.com` ✅ matches `*.mydomain.com`; `app.dev.mydomain.com` ❌ does NOT match the wildcard cert and the TLS handshake will fail with `SNI mismatch`. The system MUST reject multi-level subdomain registration in this mode at API-level (`POST /api/ports` returns `400 invalid_subdomain_depth` — see `contracts/daemon-api.md`) AND in the UI form.
  2. **Bring Your Own DNS Mode**: Assumes the user has configured a wildcard A-record (`*.domain.com`) pointing to the VPS. Caddy uses HTTP-01 challenge per-subdomain — therefore multi-level subdomains ARE supported in this mode (each gets its own cert). Rate-limit risk (Let's Encrypt limits ~50 certs/week per registered domain) is documented and surfaced in the UI.
- **FR-010**: System MUST detect out-of-band changes to the server's AmneziaWG configuration AND reconcile stale local state. Two sub-requirements:
  1. **Server drift detection**: On every connect (and every 30s during a live session), the daemon MUST re-fetch `awg0.conf` over SSH, hash it (SHA256), and compare against `serverMirror.awgConfSha256`. On mismatch, the daemon MUST re-parse obfuscation parameters and surface a warning in the UI ("Server config drifted — re-syncing").
  2. **Local stale-state reconciliation** (glm residual finding): On daemon startup, BEFORE acting on persisted `tunnel.status`, the daemon MUST verify the tunnel's actual state — `awg show <iface>` to confirm the interface exists and has an active handshake within the last 3 × `PersistentKeepalive` (75 s) seconds. If persisted status is `"connected"` but `awg show` reports no interface OR stale handshake, the daemon MUST treat it as `"disconnected"` and re-initiate `awg-quick up`. Similarly for VPS state: if `vps.isProvisioned == true`, the daemon SHOULD probe `docker ps --filter name=unet-amnezia-awg` over SSH on first connect-attempt; absence triggers re-provisioning prompt in UI (not auto-re-provision).
- **FR-011**: System MUST persist sensitive local state (`~/.unet/config.json`, `~/.unet/token`) with file mode `0600` on POSIX or Windows ACL `Owner:Read+Write, Everyone:Deny`. Atomic writes via temp-file + rename (POSIX `os.Rename` / Windows `MoveFileEx(MOVEFILE_REPLACE_EXISTING)`). Additionally, all secret fields (SSH password, Cloudflare API token, AmneziaWG private keys, Caddy mTLS client key, `uiToken`) MUST be redacted to `<redacted>` in any log output AND masked to `****<last-4>` in API responses returning configuration. This applies to structured loggers (e.g., `slog`, `zerolog`) and error propagation paths — no secret value may appear in `error.Error()`, stack traces, or debug prints.
- **FR-012**: System MUST validate all user-supplied input that flows into shell commands, JSON payloads, or filesystem paths against strict allowlists:
  - **Subdomain** per RFC 1035: each label `[a-z0-9-]`, 1–63 chars, no leading/trailing hyphen, total ≤253 chars. When `dns.mode == "cloudflare"` (wildcard cert), additionally enforce **exactly one label** between the leftmost dot and `dns.baseDomain` (see FR-009 single-label constraint). When `dns.mode == "manual"`, multi-level subdomains are permitted.
  - **Ports** in range `1..65535` (reject `0`, negative, or out-of-range).
  - **SSH host**: IPv4 / IPv6 / FQDN. Reject shell metacharacters (`;<>|\``$\n`) and any byte not in the IP/FQDN-legal set.
  - **JSON-bound values** (e.g., `clientsTable.userData.clientName`, peer labels in Caddy admin config): MUST be marshalled by `encoding/json`, NEVER concatenated via string templates or shell heredocs (antigravity F4). Any value the user can supply must traverse `json.Marshal` exactly once before reaching a shell or file.
  - **Filesystem paths** (e.g., `vps.privateKeyPath`): reject paths containing `..`, NUL bytes, or symlinks pointing outside the user's home directory.

### Key Entities

- **VPS Node**: Remote server. Stores SSH credentials, public IP, container name (e.g. `unet-amnezia-awg`), Dockerfile build hash, installation state.
- **Tunnel**: Active AmneziaWG connection. Stores: dynamically-discovered interface name (Linux: `awg0`; Windows/macOS: post-`up` enumeration), subnet (read from server, e.g. `10.8.1.0/24`), server endpoint with dynamic port (e.g. `1.2.3.4:31075`), local + server keys, **full obfuscation parameter set** (Jc, Jmin, Jmax, S1-S4, H1-H4, I1-I5), MTU (1280), PersistentKeepalive (25s), status.
- **Exposed Service**: Mapping between a public FQDN and a local port on the client side.
- **Server Mirror**: Local copy of the VPS's `awg0.conf` raw text + `clientsTable` JSON + SHA256 hash. Enables drift detection and full server-state reconstruction if the Docker volume is lost.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can provision a fresh Ubuntu VPS (22.04 LTS or 24.04 LTS) with Docker preinstalled and establish a tunnel in under 5 minutes. (Baseline: 1 Gbps VPS link, AmneziaWG image build cached after first run.) First-run cold-start may take longer due to Docker image build (`amneziawg-tools` download + Alpine package install).
- **SC-002**: Exposing a new local port updates the public routing in under 2 seconds (Caddy admin API call + Cloudflare DNS API call in parallel). DNS propagation excluded — explicitly out of scope.
- **SC-003**: The entire client application is distributed as a single executable binary under 30MB (uncompressed, per platform). MUST be verified at CI time with `goweight` or equivalent. Does NOT include the AmneziaWG client runtime (`awg-quick` + `amneziawg-go` on macOS), which is a system prerequisite.
- **SC-004**: The system successfully traverses NAT and DPI using AmneziaWG (`awg-quick` CLI client + custom-built AmneziaWG server container with persistent volume). Validated by connecting from a network where standard WireGuard handshake is known to be blocked (e.g., behind a DPI-equipped firewall).
- **SC-005**: Recreating the AmneziaWG container without losing the named volume MUST preserve all peer state (no client re-enrollment required). Verified by `docker compose down && docker compose up -d` followed by verifying existing clients still connect.
- **SC-006**: Out-of-band server-config changes (e.g., a peer added via Amnezia Desktop client) MUST be detected by the unet daemon on next connect, within one drift-check cycle (≤30s).
