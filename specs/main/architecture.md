# unet Architecture

**Version**: 0.2.0 (draft)
**Updated**: 2026-05-28
**Status**: Living document — evolves with each feature spec.

## Product Positioning

**unet** is a self-hosted ngrok/Tailscale alternative built on AmneziaWG + Caddy. It provides secure tunnel connectivity from local machines to a VPS, with DNS-terminated HTTPS ingress for exposing local services publicly.

- **License**: MIT (OSS)
- **Target user**: Solo developer or small team who wants ngrok-like convenience without trusting a third party with their traffic.
- **Planned enterprise tier**: Multi-user management, device count limits, team-based access controls, and premium features (analogous to Tailscale's free/personal/business split). The control plane API (spec 002) is the architectural foundation for this split — external consumers authenticate via API tokens, and the same token infrastructure will serve future multi-user identity.

## Layered Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        ADMIN SURFACE                             │
│              Embedded React UI (Go binary, localhost:8080)       │
│              Spec: 001-init (FR-005, FR-007)                     │
├──────────────────────────────────────────────────────────────────┤
│                        CONTROL PLANE                             │
│        Remote HTTP API — spec 002-api-control-plane              │
│        Authenticated, TLS, network-accessible at :8443           │
│        Routes: /v1/peers, /v1/routes, /v1/tunnel, /v1/tokens,   │
│                /v1/audit, /v1/status, /v1/logs/stream,          │
│                /v1/logs/export                                   │
├──────────────────────────────────────────────────────────────────┤
│                  OPERATIONS (cross-cutting)                      │
│  ┌────────────────────────────┐ ┌────────────────────────────┐  │
│  │  Lifecycle Subsystem       │ │  Observability Subsystem   │  │
│  │  Spec: 003-vps-lifecycle   │ │  Spec: 005-observability   │  │
│  │  Bootstrap · Attach ·      │ │  Structured logs · SSE ·   │  │
│  │  Detect · Migrate ·        │ │  Prometheus :9090 ·        │  │
│  │  Backup · Health ·         │ │  Container aggregation ·   │  │
│  │  Snapshot                  │ │  Log export                │  │
│  └────────────────────────────┘ └────────────────────────────┘  │
├──────────────────────────────────────────────────────────────────┤
│                         DATA PLANE                               │
│  ┌─────────────┐   ┌─────────────┐   ┌──────────────────────┐   │
│  │ AmneziaWG   │   │   Caddy     │   │ net-pause container  │   │
│  │ Transport   │   │ Ingress     │   │ (network namespace)  │   │
│  │ (awg0, UDP) │   │ (HTTPS/443) │   │ shared netns holder  │   │
│  └──────┬──────┘   └──────┬──────┘   └──────────┬───────────┘   │
│         │                 │                      │               │
│         └────────┬────────┘──────────────────────┘               │
│                  │                                                │
│         Docker Compose on VPS (spec 001-init)                    │
│         Persistent volume: amnezia-awg-state                     │
└──────────────────────────────────────────────────────────────────┘
         ▲                                   ▲
         │ WireGuard tunnel (awg0)           │ HTTPS (public)
         │ AmneziaWG obfuscation             │ Caddy reverse proxy
         │ Jc/Jmin/Jmax/S1-S4/H1-H4/I1-I5  │ DNS-01 or HTTP-01 TLS
         │                                   │
   ┌─────┴──────┐                    ┌───────┴───────┐
   │ Local Host │                    │  Internet     │
   │ (daemon)   │◀──localhost API─── │  clients      │
   └────────────┘   (unet-tray.exe)  └───────────────┘
```

## Layer Details

### Data Plane

The data plane handles actual network traffic — tunneling local services through the VPS to the public internet.

**Components**:
- **AmneziaWG container** (`unet-amnezia-awg`): Runs the WireGuard-compatible tunnel with DPI-bypass obfuscation parameters. Listens on a configurable UDP port. Peer state persisted in a Docker named volume (`amnezia-awg-state`) to survive container recreates.
- **Caddy container** (`unet-caddy`): Shares the AmneziaWG container's network namespace (`network_mode: "service:unet-amnezia-awg"`). This allows Caddy to bind to the WG-internal IP and dial client IPs through the tunnel. Terminates TLS (Let's Encrypt via DNS-01 wildcard or HTTP-01 per-subdomain). Dynamically updates routes via its admin REST API.
- **net-pause container**: Holds the shared network namespace alive even if AmneziaWG or Caddy restart. Standard Docker networking pattern for shared-netns setups.

**Spec reference**: `specs/001-init/` (FR-001 through FR-012)

**Daemon API (localhost)**: The local Go daemon manages the data plane via SSH to the VPS and `awg-quick` locally. Its REST API is at `http://localhost:<PORT>/api/*` — unauthenticated, loopback-only. Documented in `specs/001-init/contracts/daemon-api.md`.

### Control Plane

The control plane exposes a network-accessible, authenticated HTTP API for programmatic management of unet resources.

**Purpose**: Enable external consumers (undevops dashboard plugin, third-party tools, future multi-user enterprise tier) to manage peers, routes, and tunnel status without SSH + docker exec.

**Components**:
- **Remote HTTP API**: Separate HTTP listener in the same Go process as the local daemon. Bound to configurable address (default `0.0.0.0:8443`). TLS required. Authenticated via API tokens with scoped permissions (`read`/`write`/`admin`). Path prefix: `/v1/` (no `/api/` outer prefix — Stripe-style routing).
- **Token store**: API tokens stored hashed in `~/.unet/config.json`. Plaintext shown only at creation.
- **Audit log**: Append-only record of all state-changing API actions. Stored locally as JSONL (`~/.unet/audit.jsonl`).

**Route surface** (all under `/v1/`):
- Resource CRUD: `peers`, `routes`, `tunnel/status`, `tokens`
- Operational: `audit`, `status`
- Observability (mounted from spec 005): `logs/stream` (SSE), `logs/export`

**Spec reference**: `specs/002-api-control-plane/spec.md`

**Relationship to daemon API**: The control plane reuses the daemon's VPS connection and state. It does NOT replace the localhost daemon API — both run simultaneously on different listeners with different auth requirements.

```
Remote consumer ──TLS+Bearer──▶ :8443/v1/*     ─┐
                                                 │ same Go process
Local browser ────no auth────▶ :8080/api/*     ─┤
                                                 │
                                                 ▼
                                          Daemon core logic
                                          (state, SSH to VPS, awg-quick)
```

### Lifecycle Operations

Cross-cutting subsystem that manages the full VPS lifecycle — from initial provisioning through health monitoring to migration between hosts. Operations span Control Plane (CLI commands, future API endpoints) and Data Plane (SSH-driven mutations to Docker compose, AmneziaWG config, Caddy routes).

**Capabilities**:
- **Bootstrap**: Idempotent clean-VPS provisioning — Docker install, compose deployment from embedded templates, health verification. Re-running on a current VPS produces zero diff.
- **Attach**: Detect existing install, sync state without disrupting connected peers. Classifies VPS into four states: `blank`, `old`, `current`, `incompatible`.
- **Version detection**: Reads `/opt/unet/version` over SSH, classifies compatibility (±2 minor = attachable, major mismatch = refuse).
- **Migration**: Cutover orchestration with DNS TTL coordination. Phase-tracked state at `~/.unet/migration.json` enables crash recovery.
- **Backup/Restore**: Encrypted state bundles (age + JSONL). Optional S3-compatible sync (R2/B2/MinIO). All-or-nothing import with integrity verification.
- **Health probing**: Periodic ICMP ping + HTTP GET over WG tunnel (not SSH). 15s interval, 3 consecutive failures trigger reconnect with exponential backoff.
- **Snapshot**: Point-in-time config snapshots before mutations. Up to 5 retained, oldest pruned.
- **Compose management**: Canonical `docker-compose.yml` embedded in daemon binary via `go:embed`. Hash-based drift detection against VPS file.

**SSH session pool**: Shared connection pool (max 3 concurrent per VPS, 30s idle timeout) used by all lifecycle packages. Replaces per-command SSH dial from 001-init.

**Spec reference**: `specs/003-vps-lifecycle/spec.md`

### Observability

Cross-cutting subsystem providing structured logging, log streaming, container log aggregation, and Prometheus metrics. All daemon subsystems emit logs through a unified `slog` pipeline; observability outputs flow to files, SSE subscribers, and Prometheus scrapers.

**Capabilities**:
- **Structured logger**: Custom `slog.Handler` replacing all `log.Printf` calls. Dual-write to JSONL files and in-memory ring buffer. Per-component level filtering. Secret redaction (private keys, tokens, passwords → `<redacted>`).
- **Log rotation**: Size-based (100MB default) + date-based rotation via lumberjack. Configurable retention (30 days default). Graceful degradation on disk-full.
- **Container log aggregation**: Docker Engine API follow-stream per managed container (`unet-amnezia-awg`, `unet-caddy`). Re-emitted as structured log records.
- **SSE log stream**: In-memory ring buffer (200 entries) + fan-out hub (up to 10 subscribers). Server-side filtering by level/component/query. Endpoint: `GET /v1/logs/stream` on control plane listener (`:8443`).
- **Log export**: Date-range tarball assembly with optional PII scrubbing (IP masking, peer name anonymization). Endpoint: `GET /v1/logs/export`.
- **Prometheus metrics**: Counter/gauge/histogram collectors for API requests, peer connections, tunnel status, log write latency. Separate listener at `127.0.0.1:9090` (loopback-only by default, external bind requires bearer token).

**Data flow**: All daemon subsystems → `slog` handler → secret redaction → level filter → dual-write (JSONL file + ring buffer → SSE hub → subscribers). Container output → Docker SDK follow → re-emit via `slog` → same dual-write path.

**File layout**: `~/.unet/logs/daemon-YYYY-MM-DD.jsonl` (rotated logs), `~/.unet/audit.jsonl` (API audit, spec 002), `~/.unet/lifecycle-audit.jsonl` (lifecycle audit, spec 003). Unified log stream reads from all three sources.

**Spec reference**: `specs/005-observability/spec.md`

### Admin Surface

The embedded React UI served from the Go binary on localhost. Provides a GUI for configuring VPS, managing tunnel connections, and exposing ports.

**Current state**: Functional for single-user local management (spec 001-init, User Story 4).

**Future evolution**: The admin surface may gain a remote-accessible mode that authenticates against the control plane API tokens, enabling management from any browser (not just localhost). This would be a separate future spec. The current design keeps the UI strictly localhost-only — remote management is API-only.

## Dependency Topology

```
Internet clients
    │
    ▼ (HTTPS)
  Caddy (:443) ──── reverse proxy to ──▶ tunnel client IP:localPort
    │                                       ▲
    │ (admin API on WG IP)                  │ (awg0 tunnel)
    ▼                                       │
  Daemon ─── SSH ──▶ VPS (awg0.conf) ──────┘
    │
    ├── localhost:8080  (daemon API, UI)
    └── 0.0.0.0:8443   (control plane API, TLS + auth)
```

**Key dependency constraints**:
1. Caddy MUST share AmneziaWG's network namespace — it needs `awg0` interface access to dial client IPs. Running Caddy in its own netns is physically incompatible with the routing model.
2. All peer/route mutations go through the daemon → SSH → VPS path. There is no direct VPS API for peer management; the daemon IS the management layer.
3. The control plane API depends on the daemon's SSH connection to the VPS. If SSH is unreachable, the control plane degrades (cached reads, failed writes) but does not crash.
4. Lifecycle operations (spec 003) extend the SSH path with connection pooling, health probing over WG (not SSH), and idempotent compose management. They share the same daemon state mutex.
5. Observability (spec 005) is passive — it reads log output from all subsystems and container stdout/stderr. It does not modify daemon state.

## Technology Stack

| Component        | Technology                              | Notes                                     |
|-----------------|-----------------------------------------|-------------------------------------------|
| Daemon binary   | Go (single binary, embedded React UI)   | `go embed` for UI assets                  |
| Tunnel transport| AmneziaWG (`awg-quick` CLI)             | NOT `wgctrl` — full obfuscation params    |
| Ingress proxy   | Caddy v2 (admin API for dynamic routes) | With `caddy-dns/cloudflare` plugin option |
| DNS automation  | Cloudflare API (optional)               | DNS-01 challenge + A-record management    |
| Containerization| Docker Compose on VPS                   | AmneziaWG + Caddy + net-pause             |
| Local config    | `~/.unet/config.json` (file, 0600)      | Atomic writes via temp+rename             |
| Remote API auth | API tokens (PAT-style, bcrypt-hashed)   | Scoped: read/write/admin                  |
| Remote API TLS  | Self-signed (auto-gen) or CA-signed     | Required for non-loopback                 |
| Structured logs | Go `log/slog` (stdlib)                  | Custom JSON handler, secret redaction     |
| Log rotation    | lumberjack v2                           | Size + date rotation, configurable retention |
| Metrics         | Prometheus `client_golang`              | Text exposition, loopback :9090           |
| Container logs  | Docker Engine API (`docker/client`)     | Follow-stream per container               |
| SSH sessions    | `golang.org/x/crypto/ssh`               | Pooled: max 3 per VPS, 30s idle timeout   |
| State encryption| `filippo.io/age`                        | Backup bundles, scrypt KDF, no CGO        |
| S3-compatible sync | `aws-sdk-go-v2`                      | Optional: R2, B2, MinIO                   |

## Spec Registry

| Spec                          | Scope                                          | Status |
|-------------------------------|------------------------------------------------|--------|
| `specs/001-init/`            | Core data plane, daemon, local UI              | Draft  |
| `specs/002-api-control-plane/`| Remote control plane API (routes, auth, audit) | Draft  |
| `specs/003-vps-lifecycle/`   | VPS lifecycle: bootstrap, attach, migrate, backup, health | Draft  |
| `specs/005-observability/`   | Structured logging, SSE streaming, Prometheus metrics | Draft  |

## Cross-References

- **undevops integration**: unet is consumed by undevops as an "External API Consumer Pattern" plugin. See `C:\Repositories\underundre\underhelpers\undevops\specs\001-init\contracts\plugin-sdk.md` §External API Consumer Pattern.
- **unet daemon API contract**: `specs/001-init/contracts/daemon-api.md` — the localhost API that the control plane API coexists with.
- **unet control plane API spec**: `specs/002-api-control-plane/spec.md` — the remote API surface. Path prefix: `/v1/` (no `/api/` outer prefix).
- **unet VPS lifecycle spec**: `specs/003-vps-lifecycle/spec.md` — bootstrap, attach, detect, migrate, backup, health probing, snapshot.
- **unet observability spec**: `specs/005-observability/spec.md` — structured logging, SSE log streaming, container log aggregation, Prometheus metrics exposition.
