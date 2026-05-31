# Feature Specification: VPS Lifecycle Management

**Feature Branch**: `003-vps-lifecycle`
**Created**: 2026-05-27
**Status**: Draft
**Input**: Operational gap analysis — current daemon SSHs into VPS and runs compose (spec 001-init FR-001). No detection of existing install, no recovery on partition, no migration story. Foundation that spec 002-api-control-plane assumes.

## Clarifications

### Session 2026-05-27

- Q: State backup destination? → **Decision: Local file by default + optional sync to S3-compatible (R2/B2/MinIO)** — OSS-friendly default; cloud-sync opt-in for users who survived a laptop-loss scenario.
- Q: Encryption mechanism for state bundle? → **Decision: age + passphrase** — Modern KDF (scrypt), native Go implementation, smallest UX surface, portable across machines.
- Q: VPS version skew tolerance? → A: [NEEDS CLARIFICATION: how many minor versions back is "compatible"? WireGuard config format is stable, but compose schema and Caddy admin API may drift. Recommendation: ±2 minor versions. Beyond that, force reinstall. Exact policy needs implementation validation against real release cadence.]
- Q: Migration strategy? → **Decision: Cutover with DNS-TTL redirect** — Minimal code, predictable 5—15min cutover window, clients reconnect automatically; dual-write deferred.
- Q: Attach vs reinstall when versions match but compose config drifts from canonical? → A: [NEEDS CLARIFICATION: auto-merge, prompt user, or refuse to attach? Recommendation: prompt user with diff view — show what drifted, offer merge (daemon wins) or refuse (user edits VPS manually first). Auto-merge is dangerous if user intentionally modified compose.]

### Session 2026-05-27 (round 1)

| Topic | Decision |
|---|---|
| State backup destination | Local file by default + optional sync to S3-compatible (R2/B2/MinIO) |
| Encryption mechanism for backup bundle | age + passphrase |
| Migration strategy (VPS_A → VPS_B) | Cutover with DNS-TTL redirect |
| Compose canonical source | Embedded in daemon binary |

See inline notes in each FR / section for full rationale.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Clean-VPS Bootstrap (Priority: P1)

As a developer, I run `unet bootstrap <ssh-coords>` on a fresh VPS so that Docker, AmneziaWG, and Caddy are installed and running without any manual configuration.

**Why this priority**: Current FR-001 in spec 001-init covers provisioning but doesn't guarantee idempotency, version detection, or rollback. This story establishes the reliable foundation all other lifecycle operations depend on.

**Independent Test**: Provide a fresh Ubuntu 22.04/24.04 VPS. Run bootstrap. Verify Docker + compose stack running in < 5 minutes. Run bootstrap again — verify zero diff (true idempotency).

**Acceptance Scenarios**:

1. **Given** a fresh Ubuntu VPS with SSH access, **When** I run `unet bootstrap root@1.2.3.4`, **Then** Docker is installed if missing, unet compose stack is pulled/built/started, daemon sees VPS as healthy within 5 minutes.
2. **Given** an already-bootstrapped VPS running current unet version, **When** I run `unet bootstrap root@1.2.3.4` again, **Then** the daemon detects `current` state, verifies compose matches canonical, and exits with zero diff — no containers restarted, no config changed.
3. **Given** a VPS where Docker is partially installed (e.g., `docker` binary exists but `docker compose` fails), **When** I run bootstrap, **Then** the daemon repairs the Docker installation before proceeding to compose deployment.

---

### User Story 2 - Attach to Existing (Priority: P1)

As a developer, I point my daemon at a VPS where unet is already running so that the daemon detects the existing install, attaches, and syncs state without disrupting connected peers.

**Why this priority**: Replacing a laptop, running daemon on a second machine, or recovering from local config loss — these are common operations. Without attach, the user would need to re-provision the VPS, losing all peer state.

**Independent Test**: Have a VPS with running unet compose and 2+ connected peers. On a fresh local machine, configure SSH coordinates and trigger attach. Verify daemon syncs peer list, tunnel keys, and Caddy routes without disconnecting existing peers.

**Acceptance Scenarios**:

1. **Given** a VPS running unet v0.3.0 with 3 connected peers, **When** a fresh daemon connects and runs attach detection, **Then** the daemon detects state `current`, syncs `awg0.conf` + `clientsTable` + Caddy routes to local state, and the 3 existing peers remain connected throughout.
2. **Given** a VPS running an older unet version (e.g., v0.1.0), **When** the daemon runs attach detection, **Then** the daemon detects state `old`, presents the version gap to the user, and offers upgrade path with explicit confirmation.
3. **Given** a VPS running an incompatible version (e.g., major version ahead or config schema mismatch), **When** the daemon runs attach detection, **Then** the daemon detects state `incompatible`, refuses to attach, and provides clear remediation guidance.

---

### User Story 3 - Partition Recovery (Priority: P2)

As a developer, when my VPS reboots or the network drops, the daemon automatically reconnects on a backoff schedule and restores tunnel state from VPS-side persisted config.

**Why this priority**: Network partitions are inevitable in self-hosted setups. Without auto-recovery, the user must manually intervene every time the VPS reboots or the ISP hiccups. P2 because P1 bootstrap must work first, but recovery is essential for production trust.

**Independent Test**: Establish tunnel. Reboot VPS. Verify daemon auto-reconnects within 30 seconds of VPS becoming reachable. Verify tunnel state (peers, routes) is restored from VPS-persisted config.

**Acceptance Scenarios**:

1. **Given** an active tunnel, **When** the VPS reboots, **Then** the daemon detects tunnel failure, enters exponential backoff (starting 2s, cap 60s), and auto-reconnects within 30 seconds of the VPS becoming reachable.
2. **Given** an active tunnel, **When** the VPS Docker daemon restarts but the host stays up, **Then** the daemon detects compose stack down via health probe, triggers VPS-side `docker compose up -d` over SSH, and restores the tunnel without full bootstrap.
3. **Given** a partition lasting > 10 minutes, **When** connectivity is restored, **Then** the daemon reconnects, re-syncs state from VPS, and surfaces a summary of any changes that occurred during the partition (e.g., "3 peers still connected, 1 route removed externally").

---

### User Story 4 - State Backup/Restore (Priority: P2)

As a developer, I export my unet state to an encrypted file and restore it on a new control machine so that I can resume management with the same peer/route identities.

**Why this priority**: Laptop dies, user gets a new machine, wants to pick up where they left off. Without state export/import, they'd lose all peer identities and need to re-enroll every device. P2 because it's important but not blocking for initial deployment.

**Independent Test**: Export state on Machine A. Copy encrypted bundle to Machine B. Import on Machine B. Verify peer list, route list, and tunnel config are byte-equivalent.

**Acceptance Scenarios**:

1. **Given** a running daemon with 5 peers and 3 exposed routes, **When** I run `unet state export`, **Then** the daemon produces an encrypted bundle file containing all peers, routes, keys, tokens, and VPS connection state.
2. **Given** an encrypted state bundle, **When** I run `unet state import <bundle>` on a fresh machine, **Then** the daemon decrypts, validates integrity, restores all state, and can immediately connect to the VPS without re-provisioning.
3. **Given** a corrupted or tampered state bundle, **When** I attempt import, **Then** the daemon rejects it with a clear integrity error — no partial state applied.

---

### User Story 5 - VPS-to-VPS Migration (Priority: P3)

As a developer, I migrate from VPS_A to VPS_B so that existing clients continue working through the cutover without disconnection.

**Why this priority**: VPS provider changes, hardware upgrades, geographic relocation. Valuable but complex — requires dual-write architecture. P3 because it depends on all P1/P2 stories working reliably first.

**Independent Test**: Set up VPS_A with running unet and 2 connected peers. Trigger migration to VPS_B. Verify peers remain connected during cutover and VPS_B serves traffic after migration completes.

**Acceptance Scenarios**:

1. **Given** a running unet on VPS_A with 2 connected peers, **When** I initiate migration to VPS_B, **Then** the daemon bootstraps VPS_B, enters dual-write mode where both VPS instances receive config updates, and existing peers maintain connectivity throughout.
2. **Given** an active migration in dual-write mode, **When** cutover completes, **Then** the daemon atomically switches to VPS_B as primary, decommissions VPS_A, and all peers reconnect to VPS_B automatically.
3. **Given** a migration interrupted mid-cutover, **When** the daemon restarts, **Then** it detects the incomplete migration state and offers to resume or roll back to VPS_A.

---

### User Story 6 - Version Drift Handling (Priority: P3)

As a developer, when my daemon detects a version mismatch between local and VPS, it presents a safe upgrade path with rollback capability.

**Why this priority**: Self-hosted software rots. The daemon binary gets updated but VPS compose stays old, or vice versa. Needs detection and safe upgrade mechanics. P3 because it's a polish feature — P1 detection-probe taxonomy (blank/old/current/incompatible) handles the basic case.

**Independent Test**: Have daemon v0.4.0 and VPS running v0.2.0. Trigger version check. Verify daemon presents version gap and offers VPS upgrade with rollback.

**Acceptance Scenarios**:

1. **Given** daemon version 0.4.0 and VPS running unet version 0.2.0, **When** the daemon connects, **Then** it detects the 2-minor-version gap, presents a safe upgrade path, and creates a VPS snapshot before upgrading.
2. **Given** an upgrade attempt that fails mid-way, **When** the daemon detects the failure, **Then** it rolls back to the pre-upgrade snapshot and restores the VPS to its previous working state.
3. **Given** daemon version 0.4.0 and VPS running unet version 1.0.0 (major version ahead), **When** the daemon connects, **Then** it refuses to manage the VPS and directs the user to upgrade the local daemon first.

### Edge Cases

- **VPS disk full mid-bootstrap**: Bootstrap writes Docker images + compose files. If disk fills during image build, the daemon MUST detect the `ENOSPC` error, clean up partial artifacts, and report the failure with disk space requirements. MUST NOT leave the VPS in a partially-bootstrapped state.
- **SSH key rotated externally between attach attempts**: Daemon's stored SSH key no longer works. MUST surface clear auth failure, NOT retry indefinitely. Prompt user to update credentials. Audit log records failed auth attempt.
- **VPS unet running but compose file edited by hand**: Detection probe reads actual compose state and compares to canonical. MUST detect drift, NOT silently overwrite user changes. Present diff and ask for resolution. **Decision: Embedded in daemon binary** — Compose version pinned to daemon version, no drift, no network deps; compose patches ship as daemon releases.
- **Concurrent daemons attempting attach to same VPS**: Two daemons cannot manage the same VPS safely. Daemon MUST acquire an advisory lock on the VPS (e.g., a lock file in the compose directory with daemon ID + timestamp). Second daemon receives `conflict` status with the locking daemon's identity and last-seen timestamp.
- **State bundle imported on machine with different OS architecture**: Bundle contains no architecture-specific binaries — only configuration data (JSON, keys, connection params). Import MUST succeed cross-platform. If bundle somehow contains architecture-specific data in the future, import MUST validate and reject with a clear error.
- **Migration interrupted mid-cutover**: Daemon crashes or user machine loses power during the atomic switch from VPS_A to VPS_B. On restart, daemon MUST detect incomplete migration from persisted migration state, assess which VPS is currently serving traffic, and offer resume or rollback. MUST NOT assume migration completed.

## Requirements *(mandatory)*

### Functional Requirements

**Idempotent Bootstrap (P1)**:

- **FR-001**: The daemon MUST provide a `bootstrap` operation that installs Docker (if missing), deploys the unet compose stack, and configures the VPS for tunnel operation. Every step MUST verify current state before mutating. Re-running bootstrap on an already-configured VPS at the same version MUST produce zero changes (true idempotency) — no containers restarted, no config files rewritten, no DNS records touched.
- **FR-002**: The daemon MUST create a VPS snapshot (compose state + `awg0.conf` + Docker volume backup hash) before any mutating bootstrap step. This snapshot enables rollback on failure. Snapshot stored as a tar archive in `/opt/unet/snapshots/<timestamp>/` on the VPS. [NEEDS CLARIFICATION: snapshot strategy — full volume backup or just config file copy? Full volume backup is safer but slow on large volumes.]

**Detection Probe Taxonomy (P1)**:

- **FR-003**: The daemon MUST classify VPS state into exactly four categories on connect:
  1. `blank` — no Docker, no unet artifacts, fresh OS
  2. `old` — unet installed but version behind daemon (within compatible range)
  3. `current` — unet installed, version matches daemon, compose matches canonical
  4. `incompatible` — unet installed but major version mismatch or config schema incompatible
- **FR-004**: The detection probe MUST execute over SSH within 10 seconds. It checks: Docker presence, `docker ps --filter name=unet-` output, `/opt/unet/version` file contents, compose file hash vs canonical, and `awg0.conf` presence in the persistent volume.

**Attach Mode (P1)**:

- **FR-005**: When detection probe returns `current`, the daemon MUST attach without re-provisioning: sync `awg0.conf`, `clientsTable`, Caddy route state, and VPS connection metadata to local state. Existing peer connections MUST NOT be disrupted during attach.
- **FR-006**: When detection probe returns `old`, the daemon MUST present the version gap to the user and offer upgrade (FR-011). MUST NOT auto-upgrade without explicit user confirmation. If user declines, daemon MAY attach in read-only mode (monitoring only, no config mutations).

**Health Probe over WG Tunnel (P2)**:

- **FR-007**: The daemon MUST periodically probe VPS health over the WireGuard tunnel (NOT SSH) to detect partition independently of SSH connectivity. Probe interval: 15 seconds. Probe method: ICMP ping to VPS WG IP + HTTP GET to Caddy admin endpoint on WG IP. Three consecutive probe failures trigger reconnect sequence (FR-008).

**Exponential-Backoff Reconnect (P2)**:

- **FR-008**: On tunnel partition detected by health probe failure, the daemon MUST enter exponential backoff reconnect: initial delay 2s, multiplier 2x, cap 60s, jitter ±20% to avoid thundering herd. Each reconnect attempt: verify SSH reachable → verify Docker running → verify compose stack up → re-establish `awg-quick` tunnel. Maximum reconnect duration before surfacing error to user: 10 minutes. After 10 minutes, daemon continues retrying but surfaces degraded status.

**State Export/Import (P2)**:

- **FR-009**: The daemon MUST export all operational state to an encrypted bundle file via `unet state export [--output <path>]`. Bundle contents (JSONL format, one JSON object per line):
  - Header: version, timestamp, daemon version, export ID
  - VPS connection: host, SSH port, username, auth mode (private key excluded — user must re-provide)
  - Tunnel config: server endpoint, port, WG keys (server public key only), obfuscation params, MTU, keepalive
  - Peers: array of {id, name, publicKey, allowedIp, clientConfig (obfuscation params only, no private keys)}
  - Routes: array of {id, subdomain, localPort, status}
  - DNS config: mode, baseDomain (Cloudflare token excluded)
  - Audit log: recent entries (last 100)
  - Footer: SHA256 hash of all preceding lines
- **FR-010**: The daemon MUST import a state bundle via `unet state import <path>`: decrypt, validate footer hash, validate schema version compatibility, restore state to local config. MUST NOT apply partial state on validation failure — all-or-nothing. Imported state overwrites ALL existing local state after user confirmation. Encryption via age with passphrase (resolved Clarifications 2026-05-27 round 1).

**Migration Mode (P3)**:

- **FR-011**: The daemon MUST support VPS-to-VPS migration via `unet migrate --target <ssh-coords>`:
  1. Bootstrap VPS_B (FR-001)
  2. Copy `awg0.conf` server config + persistent volume data from VPS_A to VPS_B
  3. Enter dual-write window: all config mutations applied to both VPS instances
  4. Cutover: atomically switch local tunnel to VPS_B, update DNS A-records to VPS_B IP
  5. Decommission VPS_A: stop compose stack, archive state
  Migration uses cutover with DNS-TTL redirect (resolved Clarifications 2026-05-27 round 1). Dual-write deferred to future spec.

**SSH Key Handling (P1)**:

- **FR-012**: The daemon MUST support both password and SSH key authentication for VPS access. SSH private keys MUST be stored in `~/.unet/ssh/` with mode `0700` (directory) and `0600` (key files). Private keys MUST NOT be included in state export bundles — the user must re-provide SSH credentials on import. Keys MUST be validated (format check + test connection) before storage.

**Corrupted Compose Recovery (P2)**:

- **FR-013**: The daemon MUST detect and recover from corrupted compose state on the VPS. Detection triggers: `docker compose` commands fail, containers in restart loop, or health probe returns unexpected status. Recovery: stop compose stack → restore from most recent snapshot (FR-002) → restart compose stack → verify health. If no snapshot exists, fall back to clean bootstrap with user confirmation (existing peer state preserved from Docker volume if intact).

**Snapshot-Before-Bootstrap Rollback (P1)**:

- **FR-014**: Before any mutating bootstrap or upgrade operation, the daemon MUST create a point-in-time snapshot of VPS compose state (FR-002). The daemon MUST maintain up to 5 snapshots on the VPS, pruning oldest. The user MAY trigger rollback to the most recent snapshot via `unet rollback`. Rollback: stop compose → restore snapshot → restart compose → verify health. Rollback is destructive to any state created after the snapshot timestamp.

**Audit Log (P2)**:

- **FR-015**: The daemon MUST record all lifecycle actions in an append-only audit log stored locally at `~/.unet/audit.jsonl` and replicated to VPS at `/opt/unet/audit.jsonl`. Each entry: timestamp (ISO-8601 Z), action (enum: `bootstrap_start`, `bootstrap_complete`, `attach`, `detach`, `upgrade_start`, `upgrade_complete`, `upgrade_rollback`, `snapshot_create`, `rollback`, `migrate_start`, `migrate_cutover`, `migrate_complete`, `partition_detected`, `reconnect_success`, `state_export`, `state_import`), actor (daemon version), target (VPS host), result (`success`/`failure`/`partial`), metadata (JSON). This lifecycle audit log is separate from and additive to the API audit log defined in spec 002-api-control-plane FR-016.

### Key Entities

- **VPSState**: Classification result from detection probe. Attributes: classification (`blank`/`old`/`current`/`incompatible`), detectedVersion, canonicalVersion, composeHash, lastChecked (timestamp), sshReachable (bool), dockerRunning (bool), composeHealthy (bool).
- **VPSSnapshot**: Point-in-time backup of VPS compose state. Attributes: id, createdAt, composeFileHash, awgConfHash, volumeBackupHash, sizeBytes, location (VPS path).
- **LifecycleEvent**: Single entry in the lifecycle audit log. Attributes: timestamp, action (enum), actor, target, result, metadata.
- **MigrationState**: Tracks an in-progress migration. Attributes: id, sourceVPS, targetVPS, phase (`bootstrapping`/`syncing`/`dual_write`/`cutover`/`decommissioning`/`completed`/`failed`), startedAt, snapshotId, error (if failed). Persisted locally so daemon can resume after crash.
- **ReconnectState**: Tracks backoff reconnect sequence. Attributes: startedAt, attemptCount, nextAttemptAt, currentDelayMs, lastError, phase (`probing`/`reconnecting`/`verifying`/`syncing`).

## Assumptions

- **Same SSH path as 001-init**: All VPS operations go through SSH + `docker exec`. No VPS-side agent or API. This constraint is inherited from spec 001-init — the daemon IS the management layer.
- **Compose stack is canonical source**: The daemon embeds the canonical `docker-compose.yml` and Dockerfile. Any deviation on the VPS is drift that must be detected and resolved.
- **Version file on VPS**: The daemon writes `/opt/unet/version` on bootstrap containing the semantic version. This is the version signal for detection probes. Format: `<major>.<minor>.<patch>\n`.
- **Single daemon per VPS**: Only one daemon may manage a VPS at a time (enforced by advisory lock, FR edge case 4). Multi-daemon coordination is out of scope.
- **State bundle excludes secrets**: Private keys, SSH passwords, and API tokens are NOT included in export bundles. The user must re-provide credentials on import. This is a deliberate security tradeoff — a stolen state bundle alone cannot compromise the VPS.
- **VPS OS target**: Ubuntu 22.04 LTS and 24.04 LTS only for v0.1. Other distros are future scope.

## Out of Scope (for this spec)

- Cloud VPS auto-provisioning via provider APIs (DigitalOcean, Hetzner, AWS EC2) — future spec
- AmneziaWG protocol-level changes
- Multi-region routing or geographic failover
- GUI for lifecycle management (bootstrap/attach/migrate are CLI-first; UI integration in future spec)
- Automated scheduled backups (manual export only for v0.1)
- Multi-VPS management (one daemon = one VPS, inherited from architecture.md)

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: `unet bootstrap` completes on a fresh AWS t3.micro equivalent (2 vCPU, 1GB RAM, Ubuntu 22.04) in under 5 minutes with Docker image build cached. First-run cold start (no cache) under 10 minutes.
- **SC-002**: Attach-detection (FR-003 probe) completes in under 10 seconds over SSH on a VPS with latency ≤ 200ms.
- **SC-003**: Partition recovery (FR-008) reconnects and restores tunnel state in under 30 seconds after the VPS becomes reachable, assuming the compose stack is already running on the VPS side.
- **SC-004**: State bundle import produces a byte-equivalent peer/route set — every peer ID, public key, allowed IP, and route mapping in the imported state matches the exported state exactly. Verified by `unet state diff <bundle> <current>` producing empty output.
- **SC-005**: VPS-to-VPS migration (FR-011) causes zero client disconnections during the dual-write cutover window. Measured by: connected peer's `latest handshake` timestamp remains within `PersistentKeepalive` (25s) throughout migration. **Note**: With cutover migration (resolved), clients will disconnect briefly during DNS TTL window (5—15 min). SC revised: migration completes within DNS TTL window, peers reconnect automatically.
- **SC-006**: Re-running `unet bootstrap` on an already-bootstrapped VPS at the same version produces no diff: `docker compose` reports no config changes, no containers restarted, `awg0.conf` unchanged. Verified by comparing pre/post state hashes.

## Cross-References

- **Extends**: `specs/001-init/` — FR-001 bootstrap replaces the provisioning logic in 001-init FR-001 with idempotent, version-aware lifecycle management. Compose definition, peer management, and Caddy integration remain as defined in 001-init.
- **Used by**: `specs/002-api-control-plane/` — the remote API assumes a running VPS and daemon. Lifecycle events (bootstrap, attach, reconnect) are prerequisites for the API to function. The lifecycle audit log (FR-015) is additive to the API audit log (002 FR-016).
- **Foundation for**: `specs/004-desktop-integration/` — network change detection triggers reconnect using FR-008 backoff mechanism. `specs/006-peer-onboarding/` — wizard uses bootstrap for first-time VPS setup.