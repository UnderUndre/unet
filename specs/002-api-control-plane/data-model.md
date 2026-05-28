# Data Model: Remote Control Plane API

**Spec**: `specs/002-api-control-plane/spec.md`
**Created**: 2026-05-27

---

## Entities

### 1. APIToken

Authentication credential for remote API access. Stored hashed in `~/.unet/config.json` under `apiTokens[]`.

```
APIToken {
  id            string    // UUIDv4, primary key
  name          string    // Human-readable label (e.g., "undevops-prod", "ci-pipeline")
  tokenHash     string    // bcrypt hash of the raw token (cost 12)
  tokenPrefix   string    // First 8 chars of raw token for identification (e.g., "unet_a1b2")
  scope         Scope     // Permission level: read | write | admin
  createdBy     string    // "system" for first boot token, or token ID of creator
  createdAt     string    // ISO-8601 UTC
  expiresAt     string    // ISO-8601 UTC, or "" (never expires)
  lastUsedAt    string    // ISO-8601 UTC, updated on each authenticated request
  requestCount  int64     // Monotonically increasing, updated on each authenticated request
  enabled       bool      // Soft-disable without deletion; default true
}
```

**Scope enum**:

| Value | Permissions |
|-------|------------|
| `read` | GET endpoints only (peers, routes, tunnel, status) |
| `write` | All `read` + POST/DELETE on peers and routes |
| `admin` | All `write` + token management, audit log access, cert rotation |

**Hierarchy**: `admin` ⊃ `write` ⊃ `read`.

**Validation rules**:
- `name`: 1–128 chars, `[a-zA-Z0-9_-]` + spaces. Unique across all tokens.
- `scope`: one of `read`, `write`, `admin`. Required.
- `expiresAt`: if set, must be in the future relative to creation time.
- `id`: auto-generated UUIDv4. Not user-settable.
- `tokenHash`: bcrypt hash, never returned in API responses.
- `tokenPrefix`: `unet_` + first 4 chars of base64url-encoded random bytes. Used for UX ("token starting with unet_a1b2...").

**Persistence**: JSON array in `~/.unet/config.json` → `apiTokens`. File mode 0600. Atomic write via temp+rename.

**Token format**: `unet_<32 random bytes, base64url-encoded>`. Total length ~48 chars. The `unet_` prefix distinguishes PAT tokens from JWT tokens in the `Authorization: Bearer` header.

---

### 2. Session (JWT-backed)

Short-lived browser session for admin UI. NOT persisted — stateless JWT.

```
Session (JWT Claims) {
  sub           string    // Token ID that created the session (references APIToken.id)
  name          string    // Token name (from APIToken.name, for audit)
  scope         Scope     // Copied from the creating APIToken.scope
  iat           int64     // Issued-at (Unix epoch)
  exp           int64     // Expiration (iat + TTL, default 15 min)
  jti           string    // JWT ID (UUIDv4, for revocation if needed)
  iss           string    // Always "unet-daemon"
}
```

**JWT structure**:
- Algorithm: HS256
- Signing key: derived from daemon's existing config secret (e.g., `config.daemonSecret` — if not present, generate and persist on first start).
- Header: `{ "alg": "HS256", "typ": "JWT" }`

**Refresh flow**: Client exchanges a PAT for a new JWT before expiry. The PAT must be `admin` or `write` scope. JWT cannot be used to create more JWTs (no self-refresh).

**Revocation**: For v0.1, JWT revocation is not supported (stateless). A revoked PAT cannot mint new JWTs, but existing JWTs run until expiry (max 15 min). Future: JWT ID blocklist.

---

### 3. Peer (extends 001-init)

Extends the peer concept from `specs/001-init/`. The remote API adds API-managed metadata.

```
Peer {
  id              string    // UUIDv4
  name            string    // Human-readable label
  publicKey       string    // WireGuard public key (base64)
  allowedIp       string    // Assigned tunnel IP (e.g., "10.8.1.3/32")
  createdVia      string    // "local" | "api" — how the peer was created
  createdAt       string    // ISO-8601 UTC
  
  // Dynamic (read from awg show, not persisted)
  connected       bool      // Recent handshake within last 3 minutes
  lastHandshake   string    // ISO-8601 UTC or ""
  transferRx      int64     // Bytes received (since last awg show)
  transferTx      int64     // Bytes sent
  
  // WireGuard client config (returned only on POST /v1/peers creation)
  clientConfig    string    // Full .conf content for client import (only in create response)
}
```

**Validation rules**:
- `name`: 1–64 chars, `[a-zA-Z0-9_-]`. Unique across peers.
- `allowedIp`: auto-allocated from tunnel subnet (e.g., `10.8.1.0/24`). Next available IP. Allocation is atomic under the peer-mutation mutex.
- `publicKey`: auto-generated on creation via `awg genkey` / `awg pubkey`. Private key stored in daemon config, included in `clientConfig`.

**Relationship to 001-init**: The `clientsTable` in the daemon's server mirror (from 001-init FR-002) stores peer metadata. The remote API reads from and writes to the same structure. No separate peer store.

---

### 4. IngressRoute (renamed from "Exposed Service" in 001-init)

The remote API uses "route" terminology. Maps 1:1 to `ExposedPort` / `Exposed Service` from 001-init.

```
IngressRoute {
  id            string    // UUIDv4
  subdomain     string    // e.g., "app" (becomes "app.mydomain.com")
  localPort     int       // 1–65535
  targetPeerIp  string    // Tunnel IP of the peer that owns this route (default: local daemon's IP)
  status        string    // "active" | "error" | "pending"
  createdAt     string    // ISO-8601 UTC
  
  // Dynamic
  caddyRouteId  string    // Caddy admin API route ID (for management)
  dnsRecordId   string    // Cloudflare DNS record ID (if cloudflare mode)
}
```

**Validation rules**:
- `subdomain`: RFC 1035 label rules per FR-012 from spec 001-init. In Cloudflare mode: exactly one label between dot and `baseDomain`. In manual mode: multi-level allowed.
- `localPort`: 1–65535. Must not conflict with existing routes (same port+subdomain combination).
- `subdomain` uniqueness: enforced across all routes.

**Relationship to 001-init**: The existing `exposedPorts[]` in `config.json` IS the route store. The remote API reads/writes the same array. Field mapping:

| 001-init field | Remote API field |
|---|---|
| `exposedPorts[].id` | `IngressRoute.id` |
| `exposedPorts[].localPort` | `IngressRoute.localPort` |
| `exposedPorts[].subdomain` | `IngressRoute.subdomain` |
| `exposedPorts[].status` | `IngressRoute.status` |

---

### 5. AuditEntry

Immutable record of a state-changing API action. Append-only.

```
AuditEntry {
  id                string    // UUIDv4
  timestamp         string    // ISO-8601 UTC (when the action occurred)
  actorTokenId      string    // APIToken.id of the caller
  actorTokenName    string    // APIToken.name (denormalized for query perf)
  action            Action    // Enum: see below
  targetResourceId  string    // ID of the affected resource (peer ID, route ID, token ID)
  sourceIp          string    // Client IP from request
  userAgent         string    // User-Agent header
  metadata          object    // Action-specific context (see below)
}
```

**Action enum**:

| Action | Target resource | Metadata |
|--------|----------------|----------|
| `create_peer` | Peer ID | `{ "peerName": "...", "allowedIp": "..." }` |
| `delete_peer` | Peer ID | `{ "peerName": "..." }` |
| `create_route` | Route ID | `{ "subdomain": "...", "localPort": N }` |
| `delete_route` | Route ID | `{ "subdomain": "..." }` |
| `create_token` | Token ID | `{ "tokenName": "...", "scope": "..." }` |
| `revoke_token` | Token ID | `{ "tokenName": "..." }` |
| `rotate_cert` | "" (empty) | `{ "oldCertExpiry": "...", "newCertExpiry": "..." }` |

**Validation rules**:
- `id`: auto-generated UUIDv4.
- `timestamp`: set by server at write time, not client.
- `action`: must be one of the enum values. New actions require schema update.
- `metadata`: arbitrary JSON object, max 4KB. Not indexed.

**Persistence**: JSONL file at `~/.unet/audit.jsonl`. One JSON object per line. Append-only — no updates, no deletes. File mode 0600.

**Query**: Read by scanning the file. For v0.1 (single-host, modest volume), full scan is acceptable. Pagination via line offset. Filtering in-memory.

---

## Persistence Notes

### Single-host invariant

All data lives on the single host running the daemon. No database server, no distributed state.

| Entity | Storage | Format |
|--------|---------|--------|
| APIToken | `~/.unet/config.json` → `apiTokens` | JSON array |
| Session | Stateless JWT | N/A (not persisted) |
| Peer | `~/.unet/config.json` → existing peer structures + `clientsTable` | JSON |
| IngressRoute | `~/.unet/config.json` → `exposedPorts` | JSON array |
| AuditEntry | `~/.unet/audit.jsonl` | JSONL (one object per line) |
| TLS cert | `~/.unet/cert.pem`, `~/.unet/key.pem` | PEM |
| JWT signing key | `~/.unet/config.json` → `daemon.jwtSigningKey` | Base64-encoded 32-byte key |

### Config.json additions

```json
{
  "remoteApi": {
    "enabled": true,
    "listenAddr": "0.0.0.0:8443",
    "tlsCertPath": "~/.unet/cert.pem",
    "tlsKeyPath": "~/.unet/key.pem"
  },
  "apiTokens": [
    {
      "id": "uuid-1",
      "name": "admin-default",
      "tokenHash": "$2a$12$...",
      "tokenPrefix": "unet_a1b2",
      "scope": "admin",
      "createdBy": "system",
      "createdAt": "2026-05-27T10:00:00Z",
      "expiresAt": "",
      "lastUsedAt": "",
      "requestCount": 0,
      "enabled": true
    }
  ],
  "daemon": {
    "jwtSigningKey": "base64-encoded-32-bytes",
    "existingFields": "..."
  }
}
```

### Atomic write protocol

All writes to `config.json` follow the existing pattern: write to temp file → `fsync` → rename over target. This applies to:
- Token CRUD
- Peer mutations (reuses existing daemon logic)
- Route mutations (reuses existing daemon logic)

Audit log writes: open with `O_APPEND` → write single line → `fsync`. No temp file needed for appends.

### Backup

`~/.unet/` is the single directory to back up. Contains all state. Documented in quickstart.md.
