# Data Model: VPS Lifecycle Management

**Spec**: `specs/003-vps-lifecycle/spec.md`
**Created**: 2026-05-28

---

## Entities

### 1. VPSProfile

SSH coordinates, WireGuard tunnel parameters, and runtime status for the managed VPS. Stored in `~/.unet/vps.json` (separate from `config.json` for clarity — VPS state changes frequently, config is relatively static).

```
VPSProfile {
  host              string    // IPv4, IPv6, or FQDN of the VPS
  port              int       // SSH port (default 22)
  user              string    // SSH username (e.g., "root")
  authMode          AuthMode  // "key" | "password"
  privateKeyPath    string    // Path to SSH private key (e.g., ~/.unet/ssh/id_unet), empty if authMode=password
  knownGoodVersion  string    // Semver of last-known-good unet version on VPS (e.g., "0.3.0")
  lastSeenAt        string    // ISO-8601 UTC — last successful SSH/health contact
  status            VPSStatus // Enum: see below
  composeHash       string    // SHA256 of last-known canonical docker-compose.yml on VPS
  wgEndpoint        string    // host:port for WireGuard tunnel endpoint
  wgServerPublicKey string    // AmneziaWG server public key (base64)
  tunnelSubnet      string    // CIDR string for tunnel network (e.g., "10.8.1.0/24")
  lockedBy          string    // Advisory lock: "<daemonID>:<timestamp>" or "" (unlocked)
}
```

**VPSStatus enum**:

| Value | Meaning |
|-------|---------|
| `active` | Daemon connected, tunnel up, compose healthy |
| `migrating` | VPS is source or target of an active migration |
| `decommissioned` | VPS shut down or released; no longer managed |
| `unreachable` | Health probes failing, reconnect backoff in progress |

**AuthMode enum**:

| Value | Meaning |
|-------|---------|
| `key` | SSH key authentication; `privateKeyPath` must be set |
| `password` | Password authentication; password stored in OS keychain, not in this file |

**Validation rules**:
- `host`: must be valid IPv4, IPv6, or FQDN (RFC 1035). Required.
- `port`: 1–65535. Default 22 if omitted.
- `user`: non-empty string. Required.
- `authMode`: one of `key`, `password`. Required.
- `privateKeyPath`: required when `authMode=key`. File must exist and be readable by daemon process.
- `knownGoodVersion`: valid semver string. Set after successful bootstrap or attach.
- `composeHash`: 64-char lowercase hex string (SHA256). Empty before first bootstrap.
- `wgEndpoint`: `host:port` format, port 1–65535.
- `wgServerPublicKey`: valid base64-encoded WireGuard public key (44 chars).
- `tunnelSubnet`: valid CIDR notation.
- `lockedBy`: format `<UUIDv4>:<ISO-8601>` or empty string.

**Persistence**: `~/.unet/vps.json`. Single JSON object (not array — one daemon manages one VPS). File mode 0600. Atomic write via temp+rename.

**Relationship to 001-init / 002**: Replaces the ad-hoc SSH connection parameters previously scattered across `config.json`. The daemon reads `vps.json` on startup to establish the management connection. Peer, route, and token data remain in `config.json` as defined by 002.

---

### 2. StateBundle (encrypted backup format)

Encrypted export of all operational state for backup and machine-to-machine transfer. Single age-encrypted file containing JSONL records (one JSON object per line). File extension: `.jsonl.age`.

#### Format Overview

The bundle is a single stream: age-encrypt the entire JSONL content (all lines below) with a user-provided passphrase. When decrypted, the result is newline-delimited JSON:

```
Line 1: {"type":"manifest","version":"1.0.0","createdAt":"...","sourceHost":"...","daemonVersion":"...","exportId":"...","peerCount":N,"routeCount":N,"tokenCount":N,"auditEntryCount":N,"payloadHash":"<sha256-of-line-2>"}
Line 2: {"type":"payload","peers":[...],"routes":[...],"tokens":[...],"tunnelConfig":{...},"vpsConnection":{...},"dnsConfig":{...},"auditLog":[...]}
```

The **manifest** (line 1) is the JSONL header record — it carries metadata and content counts. The **payload** (line 2) is the single JSONL data record with all state. Both lines are encrypted together as one age stream. Integrity is provided by age's built-in MAC (no separate signature file needed). The `payloadHash` field in the manifest is SHA-256 of line 2's raw bytes, enabling post-decryption integrity verification.

#### Manifest Header Record (first JSONL line)

```
StateBundleManifest {
  type              string    // Always "manifest" — identifies this as the header record
  version           string    // Semver of the bundle format (e.g., "1.0.0")
  createdAt         string    // ISO-8601 UTC — when the export was created
  sourceHost        string    // VPSProfile.host of the source VPS
  daemonVersion     string    // Semver of the daemon that created the export
  exportId          string    // UUIDv4 — unique identifier for this export
  peerCount         int       // Number of peers in the payload
  routeCount        int       // Number of routes in the payload
  tokenCount        int       // Number of token stubs in the payload
  auditEntryCount   int       // Number of audit log entries in the payload
  payloadHash       string    // SHA-256 hex of the payload line (line 2) — post-decryption integrity
  encryption        object    // {"algorithm":"age-v1","kdf":"scrypt"}
}
```

#### Payload Record (second JSONL line)

The payload is a single JSON object (one JSONL line) with the following fields. Entity shapes reuse definitions from `specs/002-api-control-plane/data-model.md`.

```
StateBundlePayload {
  peers           []Peer            // Array of Peer objects (same shape as 002 Peer)
  routes          []IngressRoute    // Array of IngressRoute objects (same shape as 002 IngressRoute)
  tokens          []APITokenStub    // Token hashes only — NOT plaintext tokens (see security note)
  tunnelConfig    TunnelConfig      // WireGuard tunnel parameters
  vpsConnection   VPSConnectionStub // VPS SSH coords (private key excluded)
  dnsConfig       DNSConfigStub     // DNS mode and domain (token excluded)
  auditLog        []AuditEntry      // Last 100 AuditEntry objects (same shape as 002 AuditEntry)
}
```

**Embedded types**:

```
APITokenStub {
  id            string    // APIToken.id
  name          string    // APIToken.name
  tokenHash     string    // APIToken.tokenHash (bcrypt hash — not the raw token)
  tokenPrefix   string    // APIToken.tokenPrefix
  scope         string    // APIToken.scope
  createdAt     string    // APIToken.createdAt
  enabled       bool      // APIToken.enabled
}

TunnelConfig {
  serverEndpoint    string    // WireGuard endpoint host:port
  port              int       // WireGuard listen port
  wgServerPublicKey string    // Server public key (base64)
  obfuscationParams object    // AmneziaWG obfuscation parameters (Jc, Jmin, Jmax, S1, S2, H1-H4)
  MTU               int       // Tunnel MTU (default 1280)
  keepalive         int       // PersistentKeepalive in seconds (default 25)
}

VPSConnectionStub {
  host      string    // VPSProfile.host
  sshPort   int       // VPSProfile.port
  username  string    // VPSProfile.user
  authMode  string    // VPSProfile.authMode
  // NOTE: privateKeyPath is intentionally excluded — user must re-provide on import
}

DNSConfigStub {
  mode        string    // "cloudflare" | "manual"
  baseDomain  string    // e.g., "mydomain.com"
  // NOTE: Cloudflare API token is intentionally excluded
}
```

**Security note on tokens**: The `tokens` array contains `APITokenStub` objects with `tokenHash` (bcrypt) and `tokenPrefix` but NEVER the raw token value. This means imported tokens cannot be used for API authentication — the user must create new tokens after import. This is intentional: a stolen state bundle alone cannot be used to authenticate against the API.

**Validation rules**:
- `version`: valid semver. On import, major version must match daemon's supported bundle major (same major = compatible). Minor/patch differences are tolerated.
- `exportId`: valid UUIDv4.
- `payloadHash` in manifest must match SHA-256 of decrypted payload line.
- Decryption must succeed (age passphrase correct). age's built-in MAC provides transport-level integrity.
- Decrypted JSONL must have exactly 2 lines: manifest + payload.
- Payload must be valid JSON conforming to `StateBundlePayload` schema.
- All-or-nothing: if any validation fails, NO state is applied.

**Persistence**: Exported to user-specified path via `unet state export --output <path>`. Default filename: `unet-state-<timestamp>.jsonl.age`. Optionally synced to S3-compatible storage (R2/B2/MinIO) if configured. Not persisted in `~/.unet/` by default — the user chooses where to store the bundle.

---

### 3. MigrationPlan

Tracks an in-progress or completed VPS-to-VPS migration. Persisted so the daemon can resume after a crash mid-cutover.

```
MigrationPlan {
  id              UUID            // UUIDv4, primary key
  sourceVPS       string          // VPSProfile.host of the source VPS
  targetVPS       string          // VPSProfile.host of the target VPS
  dnsTtlSeconds   int             // DNS TTL for cutover switch (default 300)
  cutoverAt       string          // ISO-8601 — planned cutover time
  status          MigrationStatus // Enum: see below
  snapshotId      string          // Reference to pre-migration snapshot on source VPS
  startedAt       string          // ISO-8601 — when migration began
  completedAt     string          // ISO-8601 — when migration finished (or "")
  error           string          // Error message if status=aborted (or "")
}
```

**MigrationStatus enum**:

| Value | Meaning |
|-------|---------|
| `pending` | Plan created, awaiting execution |
| `bootstrapping` | Target VPS being bootstrapped (FR-001) |
| `syncing` | State being copied from source to target |
| `cutover` | DNS and tunnel being switched to target |
| `draining` | Waiting for old DNS TTL to expire on source |
| `complete` | Migration finished; target is active |
| `aborted` | Migration failed; source may still be active |

**Validation rules**:
- `sourceVPS != targetVPS`: must be different VPS hosts.
- `dnsTtlSeconds`: 60–3600 (1 minute to 1 hour).
- `id`: auto-generated UUIDv4.
- `cutoverAt`: must be in the future when plan is created.
- `snapshotId`: must reference an existing snapshot on the source VPS before migration begins.

**Persistence**: `~/.unet/migration.json`. Single JSON object. File mode 0600. Atomic write via temp+rename. On daemon restart, if this file exists with status not `complete` or `aborted`, the daemon resumes migration assessment.

**Recovery**: If daemon crashes during cutover, on restart it reads `migration.json`, determines which VPS is currently serving traffic (by probing both), and offers resume or rollback.

---

### 4. HealthSnapshot

Ephemeral result of a single health probe cycle. Used for reconnect decision-making. NOT persisted — generated on demand, held in memory.

```
HealthSnapshot {
  timestamp           string    // ISO-8601 UTC — when the probe was taken
  vpsReachable        bool      // SSH connectivity to VPS
  wgHandshakeRecency  string    // Duration since last WG handshake (e.g., "15s") or "never"
  containerStatus     ContainerStatus // Enum: see below
  wgTunnelUp          bool      // WireGuard tunnel interface is up
  errors              []string  // List of error messages from probe (empty if healthy)
}
```

**ContainerStatus enum**:

| Value | Meaning |
|---------|-------------------------------------------|
| `running` | All expected containers up and healthy |
| `degraded` | Some containers running, others restarting or unhealthy |
| `stopped` | Compose stack not running |
| `unknown` | Cannot determine (SSH unreachable) |

**Validation rules**:
- `timestamp`: ISO-8601 UTC, set by daemon at probe time.
- `wgHandshakeRecency`: Go duration string or `"never"`.
- `errors`: each entry non-empty string, max 256 chars.

**Persistence**: None. Ephemeral, in-memory only. Reset on daemon restart.

**Usage**: Consumed by reconnect logic (FR-008). Three consecutive `HealthSnapshot` results with `vpsReachable=false` or `wgTunnelUp=false` trigger `ReconnectState` activation.

---

### 5. ComposeManifest

Describes the canonical Docker Compose template embedded in the daemon binary. Used for drift detection — compare `renderedHash` on VPS against `templateHash` in the binary.

```
ComposeManifest {
  templateVersion   string    // Daemon semver that shipped this template
  templateHash      string    // SHA256 of the embedded docker-compose.yml template
  renderedHash      string    // SHA256 of the rendered compose on VPS (after variable substitution)
  renderPath        string    // Absolute path on VPS (e.g., "/opt/unet/docker-compose.yml")
  lastRenderedAt    string    // ISO-8601 UTC — when compose was last rendered/deployed
}
```

**Embedding**: The canonical `docker-compose.yml` template is embedded in the daemon binary via Go `embed.FS`. This ensures the compose definition is version-pinned to the daemon release — no network dependency, no drift source.

**Validation rules**:
- `templateVersion`: valid semver.
- `templateHash`: 64-char lowercase hex (SHA256). Computed at build time, immutable.
- `renderedHash`: 64-char lowercase hex. Computed by reading the file on VPS over SSH.
- `renderPath`: absolute POSIX path, must start with `/`.
- `lastRenderedAt`: ISO-8601 UTC.

**Persistence**: `templateHash` and `templateVersion` are compile-time constants in the binary. `renderedHash`, `renderPath`, and `lastRenderedAt` are stored in `~/.unet/vps.json` alongside `VPSProfile.composeHash`.

---

### 6. ReconnectState

Tracks the exponential-backoff reconnect sequence triggered by health probe failure (FR-008). Held in memory — reset on daemon restart.

```
ReconnectState {
  startedAt       string          // ISO-8601 UTC — when reconnect sequence began
  attemptCount    int             // Number of reconnect attempts so far
  nextAttemptAt   string          // ISO-8601 UTC — scheduled time for next attempt
  currentDelayMs  int             // Current backoff delay in milliseconds
  lastError       string          // Error from most recent attempt (or "")
  phase           ReconnectPhase  // Enum: see below
}
```

**ReconnectPhase enum**:

| Value | Meaning |
|-------|---------|
| `probing` | Testing SSH and WG reachability |
| `reconnecting` | Re-establishing SSH + WG tunnel |
| `verifying` | Confirming compose stack is healthy |
| `syncing` | Re-syncing state from VPS to local |

**Backoff parameters** (from FR-008):
- Initial delay: 2000ms (2s)
- Multiplier: 2x per attempt
- Cap: 60,000ms (60s)
- Jitter: ±20% applied to each delay
- Maximum duration before surfacing degraded status: 10 minutes (continues retrying)

**Validation rules**:
- `attemptCount`: non-negative integer.
- `currentDelayMs`: 2000–60000 (after jitter, may briefly exceed 60s by up to 20%).
- `phase`: one of the four enum values.

**Persistence**: In-memory only. Reset on daemon restart. When the daemon starts and finds `VPSProfile.status=unreachable`, it creates a new `ReconnectState` and begins probing immediately.

---

### 7. LifecycleEvent (audit)

Immutable record of a VPS lifecycle action. Append-only. Extends the `AuditEntry` structure from `specs/002-api-control-plane/data-model.md` with lifecycle-specific actions.

```
LifecycleEvent {
  id                string    // UUIDv4
  timestamp         string    // ISO-8601 UTC (set by daemon at write time)
  actorTokenId      string    // "daemon" for lifecycle events (no API token involved), or APIToken.id if triggered via API
  actorTokenName    string    // "daemon" or APIToken.name
  action            Action    // Enum: see below (superset of 002's actions)
  targetResourceId  string    // VPSProfile.host (for VPS-scoped actions) or MigrationPlan.id
  sourceIp          string    // "localhost" for CLI-triggered, or client IP if API-triggered
  userAgent         string    // "unet-daemon/<version>" for CLI, or HTTP User-Agent
  metadata          object    // Action-specific context (max 4KB)
}
```

**Action enum** (lifecycle-specific, additive to 002's actions):

| Action | Target resource | Metadata example |
|--------|----------------|------------------|
| `bootstrap_start` | VPS host | `{ "detectedState": "blank", "daemonVersion": "0.3.0" }` |
| `bootstrap_complete` | VPS host | `{ "duration": "4m12s", "stepsCompleted": 8 }` |
| `attach` | VPS host | `{ "detectedState": "current", "peersSynced": 5, "routesSynced": 3 }` |
| `detach` | VPS host | `{ "reason": "user_initiated" }` |
| `upgrade_start` | VPS host | `{ "fromVersion": "0.2.0", "toVersion": "0.3.0", "snapshotId": "..." }` |
| `upgrade_complete` | VPS host | `{ "fromVersion": "0.2.0", "toVersion": "0.3.0", "duration": "2m30s" }` |
| `upgrade_rollback` | VPS host | `{ "reason": "compose_health_check_failed", "snapshotId": "..." }` |
| `snapshot_create` | VPS host | `{ "snapshotId": "...", "sizeBytes": 12345678, "location": "/opt/unet/snapshots/..." }` |
| `rollback` | VPS host | `{ "snapshotId": "...", "previousComposeHash": "..." }` |
| `migrate_start` | MigrationPlan.id | `{ "sourceVPS": "1.2.3.4", "targetVPS": "5.6.7.8" }` |
| `migrate_cutover` | MigrationPlan.id | `{ "dnsUpdated": true, "tunnelSwitched": true }` |
| `migrate_complete` | MigrationPlan.id | `{ "duration": "12m", "peersMigrated": 5 }` |
| `partition_detected` | VPS host | `{ "lastHealthyAt": "...", "consecutiveFailures": 3 }` |
| `reconnect_success` | VPS host | `{ "attempts": 7, "totalDowntime": "3m42s" }` |
| `state_export` | VPS host | `{ "exportId": "...", "bundlePath": "/path/to/bundle.jsonl.age", "sizeBytes": 45678 }` |
| `state_import` | VPS host | `{ "exportId": "...", "peersRestored": 5, "routesRestored": 3 }` |

**Validation rules**:
- `id`: auto-generated UUIDv4.
- `timestamp`: set by daemon at write time, not client.
- `action`: must be one of the lifecycle action enum values.
- `metadata`: arbitrary JSON object, max 4KB. Not indexed.
- `targetResourceId`: non-empty string.

**Persistence**: JSONL file at `~/.unet/lifecycle-audit.jsonl`. One JSON object per line. Append-only — no updates, no deletes. File mode 0600. **Distinct from** 002's `~/.unet/audit.jsonl` — lifecycle events are in a separate file to keep concerns separated. Both files are replicated to the VPS at `/opt/unet/audit.jsonl` (API) and `/opt/unet/lifecycle-audit.jsonl` (lifecycle).

---

## Persistence Notes

### File inventory

All files in `~/.unet/`. JSON for most, age-encrypted for state bundles.

| Entity | Storage | Format |
|--------|---------|--------|
| VPSProfile | `~/.unet/vps.json` | JSON object |
| ComposeManifest (runtime fields) | `~/.unet/vps.json` → alongside VPSProfile | JSON (merged) |
| MigrationPlan | `~/.unet/migration.json` | JSON object |
| StateBundle | User-specified path (e.g., `~/unet-backup.jsonl.age`) | age-encrypted JSONL (manifest header + payload, 2 lines) |
| HealthSnapshot | In-memory only | N/A (not persisted) |
| ReconnectState | In-memory only | N/A (not persisted) |
| LifecycleEvent | `~/.unet/lifecycle-audit.jsonl` | JSONL (one object per line) |
| SSH private keys | `~/.unet/ssh/id_unet` (and variants) | PEM (OpenSSH format) |

### Atomic write protocol

All JSON file writes follow the same pattern as 002: write to temp file → `fsync` → rename over target. This applies to:
- `vps.json` (VPSProfile mutations)
- `migration.json` (MigrationPlan updates)

Lifecycle audit writes: open with `O_APPEND` → write single line → `fsync`. No temp file needed for appends.

### File permissions

- POSIX: mode `0600` on all JSON/JSONL files. `~/.unet/ssh/` directory mode `0700`, key files mode `0600`.
- Windows: ACL deny-others on all files in `~/.unet/`. Ensure only the daemon's user account has read/write access.

### State bundle format details

A `.jsonl.age` file, when decrypted, contains exactly 2 JSONL lines:

```
Line 1: {"type":"manifest","version":"1.0.0","createdAt":"...","sourceHost":"...","daemonVersion":"...","exportId":"...","peerCount":N,"routeCount":N,"tokenCount":N,"auditEntryCount":N,"payloadHash":"<sha256-hex>","encryption":{"algorithm":"age-v1","kdf":"scrypt"}}
Line 2: {"type":"payload","peers":[...],"routes":[...],"tokens":[...],"tunnelConfig":{...},"vpsConnection":{...},"dnsConfig":{...},"auditLog":[...]}
```

The entire file (both lines) is age-encrypted as a single stream. The `payloadHash` field in the manifest (line 1) contains the SHA-256 hex digest of line 2's raw bytes, providing post-decryption integrity verification. age's built-in MAC provides transport-level integrity — no separate signature or footer line is needed.

### Cross-spec entity reuse

This spec reuses entity shapes from `specs/002-api-control-plane/data-model.md`:

| 003 usage | 002 entity | Notes |
|-----------|-----------|-------|
| `StateBundlePayload.peers` | `Peer` | Identical shape — `{id, name, publicKey, allowedIp, createdVia, createdAt, ...}` |
| `StateBundlePayload.routes` | `IngressRoute` | Identical shape — `{id, subdomain, localPort, targetPeerIp, status, createdAt, ...}` |
| `StateBundlePayload.tokens` | `APIToken` (stub) | Hashes only — `tokenHash` and `tokenPrefix`, never raw token |
| `StateBundlePayload.auditLog` | `AuditEntry` | Identical shape — `{id, timestamp, actorTokenId, action, ...}` |
| `LifecycleEvent` | `AuditEntry` | Same structure, extended action enum, separate file |

This reuse ensures that import/export can round-trip state without field mapping or transformation.
