# Implementation Plan: VPS Lifecycle Management

**Spec**: `specs/003-vps-lifecycle/spec.md`
**Branch**: `specs/003-vps-lifecycle`
**Created**: 2026-05-28
**Status**: Draft

---

## Constitution Check

### Principle VI — Cross-AI Review Gate

This is `/speckit.plan` — NOT `/speckit.implement`. No code is being written. The review gate **does not apply** at the planning stage. When this plan proceeds to implementation via `/speckit.implement`, the gate WILL require:

1. `specs/003-vps-lifecycle/reviews/analyze.md` with `verdict: PASS` or `verdict: MEDIUM`.
2. ≥2 external reviewer PASS files from different AI providers.
3. No contradicting `_gate-override.md`.

**Verdict**: PASS (planning stage, gate not yet active).

### Principle VII — Artifact Versioning

The `snapshot-stage.{sh,ps1}` scripts do not exist in this repo (TODO_SNAPSHOT_SCRIPT per constitution §VII). This plan does NOT attempt to call missing scripts.

Per the constitution's graceful-degradation clause: "[if the script is missing] the stage command MUST log a `[snapshot-deferred]` warning but still complete."

Manual tag `plan/003-vps-lifecycle/v1` is encouraged after commit. Note that snapshot-stage tooling is aspirational and cannot be invoked until implemented.

**Verdict**: SKIPPED — tooling not yet available.

### Principle VIII — Knowledge Self-Maintenance

**Drift detected**: `specs/main/architecture.md` does NOT mention bootstrap, attach, detect, migrate, or lifecycle concerns. The architecture describes the daemon as a control plane that SSHes into VPS (spec 001-init FR-001), but lifecycle management (version detection, partition recovery, VPS migration, state backup) is a new concern not reflected in the architecture document.

Specifically, `architecture.md` should be updated to include:

1. A **Lifecycle Layer** between "Daemon Core" and "VPS" that describes bootstrap → attach → health-probe → reconnect → migrate operations.
2. Reference to `specs/003-vps-lifecycle/` in the spec cross-reference table.
3. The four-state detection taxonomy (blank / old / current / incompatible).
4. Compose template embedding strategy (daemon binary as canonical source).

**Follow-up**: Update `architecture.md` to add lifecycle layer description and cross-reference spec 003 before or during implementation.

**Verdict**: NOTE — architecture.md missing lifecycle concern entirely. Non-blocking for plan, tracked as mandatory follow-up before implementation merge.

---

## Technical Approach Summary

### Language & framework

- **Go** (same as existing daemon). No new languages introduced.
- SSH operations via `golang.org/x/crypto/ssh` (same library the daemon already uses for VPS management).
- Encryption via `filippo.io/age` — native Go implementation, scrypt KDF, no CGO, no external binary dependency.
- S3-compatible sync via `github.com/aws/aws-sdk-go-v2` — works with Cloudflare R2, Backblaze B2, MinIO.
- Compose templates via Go `embed.FS` — pinned to daemon version, no network dependency at runtime.
- No external process orchestration frameworks. Lifecycle operations are SSH-session-orchestrated from the daemon process.

### What's reused

- **Daemon core**: Existing SSH connection to VPS, `awg` command execution, Caddy admin API client, config persistence (`~/.unet/config.json`). Lifecycle operations call the same internal functions.
- **Same Go process**: Lifecycle packages are internal packages within the daemon. No separate binary.
- **Existing atomic-write protocol**: State file writes are serialized via the same mechanism the daemon already uses. No new file-locking primitives.
- **Audit log infrastructure**: Extends the JSONL writer pattern from spec 002 with lifecycle-specific action enums.

### What's new

| Package | Purpose |
|---------|---------|
| `internal/lifecycle/bootstrap/` | Clean-VPS provisioning, idempotent Docker + compose deployment |
| `internal/lifecycle/attach/` | Detect existing install, sync state without disrupting peers |
| `internal/lifecycle/detect/` | Version detection, four-state classification (blank/old/current/incompatible) |
| `internal/lifecycle/migrate/` | Cutover orchestration with DNS TTL coordination |
| `internal/lifecycle/backup/` | Export/import encrypted state bundles (age + JSONL) |
| `internal/lifecycle/compose/` | Embedded canonical compose templates, hash-based drift detection |
| `internal/lifecycle/health/` | Periodic health probing over WG tunnel, reconnect trigger |
| `internal/lifecycle/snapshot/` | VPS snapshot before mutations, rollback support |
| `internal/state/` | Lifecycle state persistence (migration phases, reconnect state) |
| `internal/ssh/` | SSH session pool (shared across lifecycle operations) |

### Key decisions locked by spec

1. **Bootstrap**: Idempotent — re-running on already-current VPS produces zero diff (FR-001).
2. **Detection taxonomy**: Exactly four states: `blank`, `old`, `current`, `incompatible` (FR-003).
3. **Attach**: Sync state without disrupting peers. `old` → prompt user, no auto-upgrade (FR-005/FR-006).
4. **Health probe**: Over WG tunnel (not SSH). ICMP ping + HTTP GET. 15s interval, 3 consecutive failures trigger reconnect (FR-007).
5. **Reconnect**: Exponential backoff, 2s–60s cap, ±20% jitter (FR-008).
6. **Encryption**: age + passphrase for state bundles (Clarification 2026-05-27).
7. **Migration**: Cutover with DNS-TTL redirect. Dual-write deferred (Clarification 2026-05-27).
8. **Compose source**: Embedded in daemon binary (Clarification 2026-05-27).

---

## Project Structure

New code goes under `src/internal/` (daemon's internal package root):

```
src/
├── internal/
│   ├── lifecycle/
│   │   ├── bootstrap/                  # NEW: clean-VPS provisioning
│   │   │   ├── bootstrap.go            # Bootstrap() entry point, idempotent orchestrator
│   │   │   ├── preflight.go            # Preflight checks: uname, disk, sudo, OS version
│   │   │   ├── docker.go               # Docker installation (idempotent), compose deploy
│   │   │   ├── rollback.go             # Rollback on bootstrap failure
│   │   │   └── bootstrap_test.go
│   │   ├── attach/                     # NEW: detect existing install, sync state
│   │   │   ├── attach.go               # Attach() entry point
│   │   │   ├── probe.go                # Detection probe over SSH
│   │   │   ├── sync.go                 # State sync: awg0.conf, clientsTable, Caddy routes
│   │   │   └── attach_test.go
│   │   ├── detect/                     # NEW: version detection, classification
│   │   │   ├── detect.go               # Classify() → VPSState (blank/old/current/incompatible)
│   │   │   ├── version.go              # Version parsing, comparison, compatibility range
│   │   │   └── detect_test.go
│   │   ├── migrate/                    # NEW: cutover orchestration
│   │   │   ├── migrate.go              # Migrate() entry point, phase orchestrator
│   │   │   ├── cutover.go              # DNS update, tunnel switch, atomic cutover
│   │   │   ├── decommission.go         # VPS_A cleanup: stop compose, archive state
│   │   │   └── migrate_test.go
│   │   ├── backup/                     # NEW: export/import encrypted bundles
│   │   │   ├── export.go               # State bundle export (JSONL assembly)
│   │   │   ├── import.go               # State bundle import (parse, validate, restore)
│   │   │   ├── encrypt.go              # age encryption/decryption wrapper
│   │   │   ├── s3.go                   # Optional S3-compatible sync (R2/B2/MinIO)
│   │   │   ├── bundle.go               # Bundle format types, manifest header, payloadHash
│   │   │   └── backup_test.go
│   │   ├── compose/                    # NEW: embedded canonical compose templates
│   │   │   ├── compose.go              # Render(), Hash(), Drift() — template operations
│   │   │   ├── templates/
│   │   │   │   ├── docker-compose.yml.tmpl   # Canonical compose definition
│   │   │   │   └── Dockerfile.amnezia.tmpl   # AmneziaWG Dockerfile template
│   │   │   └── compose_test.go
│   │   ├── health/                     # NEW: periodic health probing
│   │   │   ├── prober.go               # Periodic probe loop: ICMP ping + HTTP GET
│   │   │   ├── reconnect.go            # Exponential backoff reconnect sequence
│   │   │   └── health_test.go
│   │   └── snapshot/                   # NEW: VPS snapshot before mutations
│   │       ├── snapshot.go             # Create(), Restore(), List(), Prune()
│   │       └── snapshot_test.go
│   ├── state/                          # NEW: lifecycle state persistence
│   │   ├── state.go                    # LifecycleState struct, Load/Save
│   │   ├── migration.go                # MigrationState: phase tracking for crash recovery
│   │   └── state_test.go
│   ├── ssh/                            # NEW: SSH session management (shared)
│   │   ├── client.go                   # SSH client factory, key validation
│   │   ├── session.go                  # Session wrapper: Run(), Output(), Close()
│   │   ├── pool.go                     # Connection pool: reuse, max 3 concurrent, idle timeout
│   │   └── ssh_test.go
│   └── daemon/                         # EXISTING — modified
│       └── main.go                     # Add lifecycle command registration, health prober startup
├── cmd/
│   └── unet/
│       └── main.go                     # EXISTING — add CLI subcommands: bootstrap, attach, migrate, rollback, state export/import
```

**Files touched**: `internal/daemon/main.go` (add lifecycle init + health prober startup), `cmd/unet/main.go` (add CLI subcommands). All other files are new.

**Dependencies added**:
- `filippo.io/age` — age encryption (pure Go, no CGO)
- `github.com/aws/aws-sdk-go-v2/service/s3` — S3-compatible sync
- `github.com/aws/aws-sdk-go-v2/config` — S3 config loading
- `golang.org/x/crypto/ssh` — likely already present, confirm
- `golang.org/x/crypto/ssh/knownhosts` — known_hosts validation

---

## Component Breakdown

### 1. Bootstrapper (`internal/lifecycle/bootstrap/`)

Clean-VPS provisioning engine. Idempotent by design — every step verifies current state before mutating. Entry point `Bootstrap(ctx, sshCoords, opts)` orchestrates: preflight checks (OS version, disk space ≥ 2GB free, sudo access, existing Docker state) → Docker installation (idempotent: detect `docker` binary + `docker compose` plugin, install only if missing or broken) → compose render (from embedded templates, render to `/opt/unet/docker-compose.yml`) → `docker compose up -d` → wait for health probe success → return. On any step failure, rollback to pre-mutation snapshot (FR-014). Re-running on a current VPS (detected as `current` by detect package) exits immediately with zero diff — no containers restarted, no config rewritten (FR-001).

### 2. Attacher (`internal/lifecycle/attach/`)

Detect existing install and sync state without disrupting connected peers. Entry point `Attach(ctx, sshCoords)` runs detection probe via SSH (classify VPS state), then syncs `awg0.conf` + `clientsTable` + Caddy routes to local daemon state. When probe returns `current`, attach proceeds silently. When `old`, presents version gap and offers upgrade with user confirmation — refuses auto-upgrade (FR-006). When `incompatible`, refuses attach entirely. When `blank`, redirects to bootstrap. Peer connections on VPS are never disrupted: attach reads state via `docker exec` and SSH file reads, never restarts containers. Advisory lock on VPS prevents concurrent daemon attachment conflicts.

### 3. Version Detector (`internal/lifecycle/detect/`)

Reads `/opt/unet/version` from VPS over SSH, classifies VPS state into the four-state taxonomy per FR-003. `Classify(ctx, session) → VPSState` checks: Docker presence, `docker ps --filter name=unet-` output, version file contents, compose file hash vs canonical (from embedded templates), `awg0.conf` presence. Compatibility range: ±2 minor versions is "old" (attachable with upgrade offer). Major version mismatch = "incompatible" (refuse). No version file + no Docker = "blank". Version matches + compose hash matches = "current". Probe MUST complete within 10 seconds (FR-004).

### 4. Health Prober (`internal/lifecycle/health/`)

Periodic probe loop running in daemon background. Probes VPS health over the WireGuard tunnel (NOT SSH — independent partition detection). Two transport methods: ICMP ping to VPS WG IP (preferred) + HTTP GET to Caddy admin endpoint on WG IP (fallback). Interval: 15 seconds. Three consecutive failures trigger reconnect sequence (FR-007). Reconnect uses exponential backoff: 2s initial, 2x multiplier, 60s cap, ±20% jitter (FR-008). Each reconnect attempt: verify SSH reachable → verify Docker running → verify compose stack up → re-establish WG tunnel. After 10 minutes of continuous failure, daemon surfaces degraded status but continues retrying. On reconnect success, re-syncs state from VPS and surfaces partition summary.

### 5. Backup Exporter (`internal/lifecycle/backup/export.go`)

Assembles daemon state into a single `.jsonl.age` file (age-encrypted JSONL stream). Line 1 = manifest header record (`type: manifest`, version, timestamp, daemon version, export ID, entity counts, payloadHash). Line 2 = payload object (VPS connection params with host/port/username — private key excluded, tunnel config with server endpoint/port/WG public key/obfuscation params, peers array, routes array, DNS config — token excluded, audit log — last 100 entries). Integrity: manifest's `payloadHash` = SHA-256 of decrypted line 2 bytes. Encrypts entire JSONL stream with age using user-provided passphrase via `filippo.io/age` library. Optional S3 sync via `aws-sdk-go-v2` (compatible with R2/B2/MinIO) — user configures endpoint + credentials in daemon config. Export path defaults to `~/.unet/exports/unet-state-<timestamp>.jsonl.age`.

### 6. Backup Importer (`internal/lifecycle/backup/import.go`)

Decrypts age-encrypted `.jsonl.age` file using user-provided passphrase. Parses line 1 as manifest header, validates `payloadHash` against decrypted line 2 bytes (SHA-256) — if mismatch, rejects bundle with clear integrity error, NO partial state applied (FR-010). Validates schema version compatibility (bundle version field checked against daemon version — ±2 minor compatibility range). Restores state to local config: overwrites peers, routes, tunnel config, DNS config. Requires explicit user confirmation before overwrite. Private keys, SSH credentials, and API tokens NOT restored (user must re-provide). All-or-nothing: on any validation failure, zero state changes applied.

### 7. Migrator (`internal/lifecycle/migrate/`)

Cutover orchestration: entry point `Migrate(ctx, sourceVPS, targetVPS)` coordinates the full migration flow. Phase 1 — bootstrap VPS_B in parallel with ongoing VPS_A operations (FR-001 on new host). Phase 2 — export state from VPS_A (awg0.conf + Docker volume data). Phase 3 — import state to VPS_B. Phase 4 — cutover: DNS TTL-aware A-record update pointing to VPS_B IP, switch local WG tunnel to VPS_B endpoint. Phase 5 — decommission VPS_A: stop compose stack, archive state. Migration state persisted at `~/.unet/migration.json` with phase tracking — enables crash recovery: daemon restart detects incomplete migration and offers resume or rollback (FR-011). Cutover window depends on DNS TTL (5–15 min typical). Dual-write mode explicitly deferred per spec clarification.

### 8. Compose Manager (`internal/lifecycle/compose/`)

Embeds canonical `docker-compose.yml` and `Dockerfile.amnezia` templates in the daemon binary via Go `embed.FS`. Templates are pinned to daemon version — no network dependency, no external template fetch. `Render(ctx, vars)` produces compose YAML from templates with variable substitution (WG port, obfuscation params, etc.). `Hash()` computes SHA256 of rendered compose for drift comparison against VPS file. `Drift(ctx, session)` reads VPS compose file, compares hash to canonical — detects manual edits by user. On drift detection: present diff, offer merge (daemon canonical wins) or refuse (user must resolve manually). Auto-merge is explicitly NOT offered — manual edits are intentional and should not be silently overwritten.

### 9. Snapshot Manager (`internal/lifecycle/snapshot/`)

Creates point-in-time snapshots of VPS state before any mutating operation (bootstrap, upgrade). Snapshot contents for MVP: config-only — `awg0.conf`, `docker-compose.yml`, `clientsTable` JSON, `/opt/unet/version`. NOT full Docker volumes (size/speed tradeoff — full volumes on 1GB VPS would be impractical). Stored as tar archive at `/opt/unet/snapshots/<timestamp>/` on VPS. Maintains up to 5 snapshots, pruning oldest. Rollback: stop compose → restore snapshot tar → restart compose → verify health. Snapshot creation and restore are SSH-orchestrated operations.

### 10. SSH Session Pool (`internal/ssh/`)

Shared SSH connection pool used by all lifecycle packages. Reuses connections across operations to avoid SSH handshake overhead per command. Pool configuration: max 3 concurrent sessions per VPS, idle timeout 30 seconds, automatic reconnection on session failure. Wraps `golang.org/x/crypto/ssh` with higher-level `Run(cmd)` and `Output(cmd)` methods. Supports both password and key authentication (FR-012). Key storage at `~/.unet/ssh/` with proper permissions (0700 directory, 0600 key files). Connection validation on pool entry: test connection before returning session to caller.

---

## Data Flow

### Bootstrap Sequence

```
User: unet bootstrap root@1.2.3.4
    │
    ├─── SSH dial (key or password auth)
    │    └─── Validate connection, add to session pool
    │
    ├─── Detection probe (detect.Classify)
    │    ├── Check /opt/unet/version
    │    ├── Check docker presence
    │    ├── Check docker ps --filter name=unet-
    │    ├── Check compose hash vs canonical
    │    └── Return VPSState: blank/old/current/incompatible
    │
    ├─── IF current → verify compose hash match → exit (zero diff)
    │
    ├─── Preflight checks (bootstrap.preflight)
    │    ├── uname -m (arch check: x86_64 or aarch64)
    │    ├── df -h / (disk space ≥ 2GB free)
    │    ├── sudo -n true (sudo access)
    │    └── cat /etc/os-release (Ubuntu 22.04/24.04 check)
    │
    ├─── Create snapshot (snapshot.Create)
    │    └── tar czf /opt/unet/snapshots/<ts>/snapshot.tar.gz {config files}
    │
    ├─── Docker install (bootstrap.docker) — idempotent
    │    ├── which docker → if missing, install via get.docker.com
    │    ├── docker compose version → if missing, install plugin
    │    └── Verify: docker info + docker compose version
    │
    ├─── Compose render (compose.Render)
    │    ├── Render embedded docker-compose.yml.tmpl with vars
    │    ├── Compute SHA256 of rendered output
    │    └── Upload to VPS: /opt/unet/docker-compose.yml (if hash differs)
    │
    ├─── Deploy (bootstrap.docker)
    │    └── SSH: cd /opt/unet && docker compose up -d
    │
    ├─── Health probe wait (health.Prober.WaitForHealthy)
    │    ├── ICMP ping to WG IP (15s interval, up to 120s timeout)
    │    └── HTTP GET to Caddy admin on WG IP
    │
    └─── Write /opt/unet/version with daemon semver
         │
         ▼
    SUCCESS: VPS bootstrapped, tunnel ready
    OR
    FAILURE: rollback to snapshot, report error
```

### Migration Sequence

```
User: unet migrate --target root@5.6.7.8
    │
    ├─── Persist MigrationState (phase: bootstrapping)
    │    to ~/.unet/migration.json
    │
    ├─── Phase 1: Bootstrap VPS_B (parallel)
    │    └── bootstrap.Bootstrap(ctx, targetCoords)
    │         (full bootstrap sequence above)
    │
    ├─── Phase 2: Export from VPS_A
    │    ├── Read /opt/unet/awg0.conf via SSH
    │    ├── Read clientsTable via docker exec
    │    └── Read Docker volume data (config-only MVP)
    │
    ├─── Phase 3: Import to VPS_B
    │    ├── Write awg0.conf to VPS_B via SSH
    │    ├── Write clientsTable via docker exec
    │    └── Restart compose stack on VPS_B
    │
    ├─── Persist MigrationState (phase: cutover)
    │
    ├─── Phase 4: Cutover
    │    ├── DNS: Update A-record to VPS_B IP
    │    │   (TTL-aware: wait up to TTL duration)
    │    ├── Local: Switch WG endpoint to VPS_B
    │    └── Verify: health probe against VPS_B succeeds
    │
    ├─── Phase 5: Decommission VPS_A
    │    ├── SSH: docker compose down
    │    ├── SSH: tar czf archive of /opt/unet/
    │    └── Audit log: migrate_complete
    │
    └─── Persist MigrationState (phase: completed)
         │
         ▼
    SUCCESS: VPS_B serving, VPS_A archived
    OR on failure at any phase:
    Rollback to VPS_A, persist MigrationState (phase: failed)
```

### Health Probe + Reconnect Flow

```
[Background goroutine — runs continuously while daemon is active]
    │
    ├─── Every 15s: probe VPS
    │    ├── ICMP ping to VPS WG IP
    │    └── HTTP GET to http://<WG-IP>:2019/config/ (Caddy admin)
    │
    ├─── IF success → reset failure counter
    │
    ├─── IF failure → increment counter
    │    └── IF counter ≥ 3 → trigger reconnect
    │         │
    │         ├── delay = min(2 * 2^attempt, 60) * jitter(±20%)
    │         ├── SSH reachable? → IF no → wait, retry
    │         ├── Docker running? → IF no → docker compose up -d
    │         ├── Compose stack healthy? → IF no → restore snapshot
    │         ├── Re-establish WG tunnel (awg-quick up)
    │         ├── Verify health probe succeeds
    │         ├── Re-sync state from VPS
    │         └── IF >10min → surface degraded status to user
    │
    └─── On reconnect success:
         ├── Audit log: reconnect_success
         └── Surface partition summary to user
```

---

## Migration / Compat Strategy

### Coexistence with existing 001-init daemon code

The lifecycle packages extend the existing daemon without replacing its core functions. Relationship:

| Aspect | 001-init (existing) | 003-lifecycle (new) |
|--------|---------------------|---------------------|
| VPS provisioning | One-shot SSH + compose deploy | Idempotent bootstrap with detection + rollback |
| Peer management | Direct `awg` commands via SSH | Reuses same functions, adds health monitoring |
| State persistence | `~/.unet/config.json` | Same file, adds `~/.unet/migration.json` + lifecycle state keys |
| SSH connections | Per-command dial | Session pool (reuse, timeout, auto-reconnect) |

**Lifecycle ops coordinate with existing daemon code via shared state mutex.** The daemon already serializes config writes — lifecycle operations acquire the same mutex for any state mutation. No new concurrency primitives needed.

### SSH session management

All lifecycle packages use the shared SSH session pool (`internal/ssh/pool.go`). This replaces the per-command SSH dial pattern from 001-init with connection reuse:

- Max 3 concurrent sessions per VPS host
- Idle timeout: 30 seconds (connection returned to pool, re-established on next request)
- Automatic reconnection on session failure (TCP reset, timeout)
- Session validation: test `echo ok` before returning session to caller

### State file writes

State writes are serialized via the existing atomic-write protocol (write to temp file, rename). New state files:

- `~/.unet/migration.json` — migration phase tracking for crash recovery
- `~/.unet/reconnect.json` — reconnect state (attempt count, next attempt time)
- `~/.unet/exports/` — directory for state export bundles

All use the same atomic-write pattern. No concurrent write conflicts possible.

### Backward compatibility

- Existing `~/.unet/config.json` schema is extended, not broken. New keys are additive.
- VPS-side `/opt/unet/version` file is new — existing VPS instances without it will classify as `blank` (no version file = pre-lifecycle daemon). Attach logic handles this gracefully.
- Compose template embedding: first daemon version with lifecycle support writes the canonical compose. If VPS has a non-canonical compose, drift detection triggers user prompt — never silent overwrite.

---

## Testing Strategy

### Unit tests

| Component | What's mocked | Tool |
|-----------|--------------|------|
| Bootstrapper | SSH session (interface), Docker client | `testing` + interface mocks |
| Detection/classification | SSH session output (version file, docker ps, compose hash) | Table-driven tests with fixed outputs |
| Version comparator | N/A (pure function) | Table-driven: blank/old/current/incompatible for various version pairs |
| Compose renderer | N/A (pure function: template + vars → YAML) | Golden file comparison |
| Compose drift detector | SSH session (remote hash) | Table-driven: match/mismatch/missing |
| Backup encrypt/decrypt | N/A (age library call) | Round-trip: encrypt → decrypt → compare |
| Backup bundle assembly | N/A (pure function) | JSONL parse + SHA256 verify |
| Backup import validation | Corrupted bundles, version mismatches | Table-driven: valid/invalid/tampered |
| Health prober | ICMP/HTTP (interface), clock | `testing` + injectable clock |
| Reconnect backoff | Clock (injectable) | Verify delay sequence: 2, 4, 8, 16, 32, 60, 60... |
| Migration phase tracker | Filesystem (temp dir) | `t.TempDir()` |
| SSH session pool | SSH client (interface) | `testing` + mock connections |
| Snapshot create/restore | SSH session (interface) | Table-driven with tar fixture |

### Integration tests

| Test | What runs real | What's mocked |
|------|---------------|---------------|
| Full bootstrap flow | Preflight + compose render + state writes | SSH to VPS (mock responses), Docker CLI |
| Attach + sync | State sync logic, config file writes | SSH to VPS (mock awg output, docker exec output) |
| Export → import round-trip | age encryption, JSONL assembly, payloadHash integrity | Filesystem (temp dir) |
| Health probe → reconnect trigger | Prober loop with injectable clock, reconnect sequence | ICMP/HTTP transport, SSH session |
| Migration phase persistence | MigrationState JSON write/read, crash recovery | SSH to VPS |
| S3 sync | aws-sdk-go-v2 client (real client, mock server) | S3 endpoint (httptest mock) |

Integration tests run against mock SSH responses via a `Session` interface. No real VPS needed.

### End-to-end tests (manual / CI)

- **Docker-in-Docker**: Use a Docker container as a fake VPS. Bootstrap against it, verify Docker-in-Docker compose stack starts. Requires privileged mode or Docker-outside-of-Docker socket mount.
- **Ephemeral VPS in CI**: Future — spin up a real VPS (Hetzner cloud API), run full bootstrap + attach + migrate, tear down. Deferred to implementation phase.
- **Manual smoke test**: Developer runs `unet bootstrap` against a real $5/month VPS, verifies end-to-end.

---

## Open Risks

1. **SSH key handling — passphrase-protected keys**: Daemon runs unattended. Passphrase-protected SSH keys require either ssh-agent forwarding (needs agent running) or interactive prompt (breaks unattended operation). Mitigation: require key-only auth without passphrase for daemon-managed keys. Document this clearly. User-provided keys with passphrase = interactive-only.

2. **Disk-full mid-bootstrap (ENOSPC)**: Docker image pull or build can fill a small VPS disk. Daemon MUST detect ENOSPC, clean up partial artifacts (dangling images, partial compose files), and report failure with minimum disk requirement. Risk: cleanup itself might fail if disk is truly full. Mitigation: reserve 500MB headroom check in preflight — refuse bootstrap if < 2GB free.

3. **Age key/passphrase loss — unrecoverable**: If user forgets the passphrase for an encrypted state bundle, the data is gone. Period. No recovery mechanism possible by design (scrypt KDF). MUST document this prominently in CLI help and during export. Consider warning on export: "Store this passphrase securely. Lost passphrase = lost data."

4. **DNS TTL surprises during migration**: Cutover window depends on DNS TTL that the user may not control (e.g., registrar-enforced minimum TTL, CDN-level caching, ISP DNS cache ignoring TTL). Mitigation: pre-migration check — resolve current DNS, warn if TTL > 300s. Lower TTL 24h before migration if possible. Document expected cutover window as 2× TTL.

5. **Concurrent daemon attachment conflicts**: Two daemons attempting to manage the same VPS. Advisory lock file on VPS (`/opt/unet/daemon.lock`) with daemon ID + timestamp. Second daemon receives `conflict` status. Risk: stale lock after daemon crash — lock includes timestamp, second daemon can override if lock is >30 minutes old with confirmation.

6. **Compose template version skew across daemon releases**: Daemon v0.3.0 embeds compose template v0.3.0. VPS running v0.2.0 has compose template v0.2.0. Detection catches this, but upgrade path requires compose replacement. Risk: user customized compose beyond our template variables — customizations lost on upgrade. Mitigation: drift detection + diff presentation before upgrade.

7. **Health probe false positives — network jitter vs actual partition**: ICMP ping loss on 15s interval with 3-consecutive-failure trigger = 45s of sustained packet loss. On some networks (mobile, satellite), this threshold may be too aggressive. Risk: unnecessary reconnect storms. Mitigation: make probe interval and failure threshold configurable. Default is conservative for stable connections.

8. **Snapshot size on small VPS**: Full volume snapshots on a 1GB RAM / 20GB disk VPS are impractical. Config-only snapshots (awg0.conf + compose.yml + clientsTable) are small (~100KB) but don't capture Docker volume data (peer connection state, logs). Trade-off: config-only for MVP, full volume as opt-in for users who need it. Document limitation clearly.

---

## Decisions Made in Plan Beyond Spec

| Topic | Decision | Why |
|-------|----------|-----|
| SSH session pooling | Reuse connections, max 3 concurrent per VPS, idle timeout 30s | Avoid SSH handshake overhead per command (~200ms per handshake). Pool keeps connections warm for frequent health probes and state sync operations. 3 concurrent = enough for parallel operations without exhausting VPS SSH MaxSessions. |
| Compose template embedding | Use Go `embed.FS` — pinned to daemon version, no network deps | Eliminates runtime dependency on template fetch. Daemon binary is the single source of truth. Template version = daemon version. Simplifies deployment and testing. |
| Snapshot strategy (MVP) | Config-only (awg0.conf + compose.yml + clientsTable), not full Docker volumes | Size/speed tradeoff: config snapshot ~100KB, completes in <1s. Full volume snapshot on 20GB disk = minutes. Config-only covers 95% of rollback scenarios (compose drift, config corruption). Full volume backup deferred to future spec. |
| Health probe transport | ICMP ping preferred, HTTP fallback | ICMP is lightweight and directly tests tunnel connectivity. But some VPS providers block ICMP. Fallback to HTTP GET on Caddy admin endpoint (always available since Caddy runs in compose). Either success = healthy. |
| Migration state persistence | `~/.unet/migration.json` with phase tracking | Crash recovery: daemon restart reads migration.json, detects incomplete migration, offers resume or rollback. Phase enum: bootstrapping → syncing → cutover → decommissioning → completed/failed. |
| Backup format versioning | Semver in manifest header, `v1.0.0` for initial release | Forward compatibility: future daemon versions can read v1.0.0 bundles and migrate schema. Header line: `{"type":"manifest","version":"1.0.0","exportedAt":"...","daemonVersion":"..."}`. |
| Detection probe timeout | 10 seconds hard timeout (per FR-004) | SSH commands with context deadline. Single probe runs: version file read, docker check, compose hash. Each command gets independent timeout. Total probe must complete in 10s. |
| Advisory lock format | `/opt/unet/daemon.lock` — JSON file: `{daemonID, hostname, timestamp}` | Simple, human-readable, SSH-accessible. Second daemon can inspect lock to identify the owner. Stale lock override after 30 minutes with user confirmation. |
| Reconnect backoff jitter | ±20% random jitter on each delay | Prevents thundering herd if multiple daemons (unlikely in single-daemon model, but defensive) or reconnection loops from exactly synchronized timing. |
| Preflight disk space threshold | ≥ 2GB free on `/` | Docker images + compose build artifacts need ~1.5GB on fresh install. 2GB provides 500MB headroom for config files and logs. Checked in preflight, refuses bootstrap if insufficient. |
