# Implementation Plan: Remote Control Plane API

**Spec**: `specs/002-api-control-plane/spec.md`
**Branch**: `specs/002-api-control-plane`
**Created**: 2026-05-27
**Status**: Draft

---

## Constitution Check

### Principle VI — Cross-AI Review Gate

This is `/speckit.plan` — NOT `/speckit.implement`. No code is being written. The review gate **does not apply** at the planning stage. When this plan proceeds to implementation via `/speckit.implement`, the gate WILL require:

1. `specs/002-api-control-plane/reviews/analyze.md` with `verdict: PASS` or `verdict: MEDIUM`.
2. ≥2 external reviewer PASS files from different AI providers.
3. No contradicting `_gate-override.md`.

**Verdict**: PASS (planning stage, gate not yet active).

### Principle VII — Artifact Versioning

The `snapshot-stage.{sh,ps1}` scripts do not exist in this repo (TODO_SNAPSHOT_SCRIPT per constitution §VII). This plan does NOT attempt to call missing scripts.

Per the constitution's graceful-degradation clause: "[if the script is missing] the stage command MUST log a `[snapshot-deferred]` warning but still complete."

Manual tag `plan/002-api-control-plane/v1` is encouraged after commit.

**Verdict**: SKIPPED — tooling not yet available.

### Principle VIII — Knowledge Self-Maintenance

**Drift detected**: `specs/main/architecture.md` already includes a "Control Plane" layer description and references spec 002. However, the architecture diagram shows the remote API path as `/api/v1/*`, while spec 002's locked decision is `/v1/` (no `/api/` prefix). The architecture diagram line:

```
Remote consumer ──TLS+Bearer──▶ :8443/api/v1/*
```

should read:

```
Remote consumer ──TLS+Bearer──▶ :8443/v1/*
```

**Follow-up**: Update `architecture.md` to fix the path prefix before or during implementation.

**Verdict**: NOTE — minor path-prefix drift in architecture diagram. Non-blocking, tracked as follow-up.

---

## Technical Approach Summary

### Language & framework

- **Go** (same as existing daemon). No new languages introduced.
- Standard library `net/http` for the remote listener — same approach as the localhost daemon server.
- No external HTTP framework. The existing daemon likely uses `net/http` + `http.ServeMux` or similar; we extend that pattern.
- TLS via `crypto/tls` + `crypto/x509` for self-signed cert generation on first start.
- Token hashing via `golang.org/x/crypto/bcrypt` (standard, well-audited).

### What's reused

- **Daemon core logic**: VPS SSH connection, `awg` command execution, Caddy admin API client, config persistence (`~/.unet/config.json`). The remote API calls the same internal functions the localhost API calls.
- **Same Go process**: New HTTP listener on separate port (default `0.0.0.0:8443`). Shared access to daemon state.
- **Shared handlers**: Per Clarification #3, both localhost and remote surfaces share handler implementations. Auth is enforced by middleware based on bind address — loopback requests skip auth, network requests require it.

### What's new

| Component | Purpose |
|-----------|---------|
| `internal/api/remote/` | Remote API listener setup, TLS config, route registration |
| `internal/api/middleware/` | Auth middleware (PAT validation, JWT validation, scope enforcement, rate limiting) |
| `internal/auth/` | Token store (CRUD, hash verification), JWT issuer/validator, session management |
| `internal/audit/` | Append-only audit log writer + reader with pagination |
| `internal/api/v1/` | `/v1/` handler registry + endpoint handlers (peers, routes, tunnel, tokens, audit, status) |

### Key decisions locked by spec

1. **Auth**: PAT (opaque tokens, bcrypt-hashed) + JWT (short-lived sessions for admin UI).
2. **Path prefix**: `/v1/` throughout. No `/api/` outer prefix.
3. **Backward compat**: Both localhost and remote surfaces preserved. Same handlers, different auth middleware.
4. **Process**: Same Go binary, separate port.
5. **Multi-host**: Single-host MVP only. No VPS-as-resource modeling.

---

## Project Structure

New code goes under `src/` (or the daemon's internal package root — adjust to match existing layout):

```
src/
├── internal/
│   ├── api/
│   │   ├── remote/                  # NEW: remote listener bootstrap
│   │   │   ├── server.go            # TLS listener setup, Start/Stop
│   │   │   ├── tls.go               # Self-signed cert generation + CA cert loading
│   │   │   └── routes.go            # Mux registration for /v1/*
│   │   ├── middleware/               # NEW: middleware chain
│   │   │   ├── auth.go              # Auth-by-bind-address dispatcher
│   │   │   ├── pat.go               # PAT Bearer token validation
│   │   │   ├── jwt.go               # JWT Bearer token validation
│   │   │   ├── scope.go             # Scope enforcement (read/write/admin)
│   │   │   ├── ratelimit.go         # Per-token rate limiter
│   │   │   └── audit.go             # Audit log middleware (records state-changing calls)
│   │   ├── v1/                      # NEW: /v1/ endpoint handlers
│   │   │   ├── handlers.go          # Handler registry
│   │   │   ├── peers.go             # GET/POST/DELETE /v1/peers, GET /v1/peers/:id
│   │   │   ├── routes.go            # GET/POST/DELETE /v1/routes
│   │   │   ├── tunnel.go            # GET /v1/tunnel/status
│   │   │   ├── tokens.go            # POST/GET/DELETE /v1/tokens
│   │   │   ├── audit.go             # GET /v1/audit
│   │   │   ├── status.go            # GET /v1/status (system health + cert expiry)
│   │   │   └── errors.go            # Structured error response helpers
│   │   └── shared/                  # NEW (or extend existing): shared handler logic
│   │       └── handlers.go          # Common peer/route logic called by both :8080 and :8443
│   ├── auth/                        # NEW: auth subsystem
│   │   ├── token.go                 # Token struct, CRUD operations
│   │   ├── store.go                 # Token persistence (file-based, in config.json)
│   │   ├── hash.go                  # bcrypt hash + verify
│   │   ├── jwt.go                   # JWT issue, validate, refresh
│   │   ├── session.go               # Session struct, TTL management
│   │   └── scope.go                 # Scope enum + permission matrix
│   ├── audit/                       # NEW: audit logging
│   │   ├── logger.go                # Append-only JSONL writer
│   │   ├── reader.go                # Paginated reader with filters
│   │   └── types.go                 # AuditEntry struct, Action enum
│   └── daemon/                      # EXISTING — modified
│       └── main.go                  # Add remote listener startup alongside localhost
├── cmd/
│   └── unet/
│       └── main.go                  # EXISTING — no changes (daemon/main.go handles it)
```

**Files touched**: `internal/daemon/main.go` (add remote listener init). All other files are new.

---

## Component Breakdown

### 1. Remote API Server (`internal/api/remote/`)

Starts a second `net/http.Server` bound to a configurable address (default `0.0.0.0:8443`). Handles TLS: on first start, generates a self-signed ECDSA P-256 cert valid for 365 days, stored at `~/.unet/cert.pem` + `~/.unet/key.pem`. Admin can replace with CA-signed cert. Refuses to start on non-loopback without TLS.

### 2. Auth Middleware (`internal/api/middleware/`)

**Auth-by-bind-address dispatcher**: inspects `r.RemoteAddr`. If the request originates from a loopback address → skip auth (localhost compatibility). If from a network address → enforce auth.

Two auth paths:
- **PAT**: `Authorization: Bearer unet_<opaque>`. Middleware hashes the bearer value and looks up in token store. Sets `tokenID`, `tokenScope`, `tokenName` in request context.
- **JWT**: `Authorization: Bearer eyJ...`. Middleware validates signature, expiry, issuer. Sets `sessionID`, `tokenScope` in context.

**Scope enforcement**: After auth, a second middleware checks the required scope for the endpoint against the token's scope. `admin` > `write` > `read` hierarchy.

**Rate limiter**: Sliding-window counter per token ID. Default 60 req/min, burst 10. Returns 429 with `Retry-After` header.

### 3. Token Store (`internal/auth/`)

Persists tokens in `~/.unet/config.json` under a new `apiTokens` key. Each token stored as: `{ id, name, tokenHash, scope, createdBy, createdAt, expiresAt, lastUsedAt, requestCount, enabled }`.

Token creation: generate 32 random bytes → base64url encode → prefix `unet_` → hash with bcrypt cost 12 → store hash. Return plaintext **once**.

Token verification: constant-time bcrypt compare of submitted bearer token against stored hashes.

JWT: signed with HS256 using a key derived from the daemon's existing config secret. Short TTL (15 min default), refresh via PAT exchange.

### 4. Audit Logger (`internal/audit/`)

Append-only JSONL file at `~/.unet/audit.jsonl`. Each entry: `{ id, timestamp, actorTokenId, actorTokenName, action, targetResourceId, sourceIp, userAgent, metadata }`.

Writer: atomic append (open with O_APPEND, write single line, sync).

Reader: reads file, parses JSONL, supports pagination (offset + limit), filtering by actor, action type, date range. Returns entries in reverse chronological order.

### 5. Handler Layer (`internal/api/v1/`)

Each handler file covers one resource domain. Handlers extract auth context from middleware, validate input, call existing daemon core functions, write audit entries, return structured JSON responses.

**Error responses** all follow: `{ "error": "snake_case_code", "message": "human-readable", "context": { ... } }`.

---

## Data Flow

```
Remote Client (curl, undevops, etc.)
    │
    │ HTTPS (TLS 1.2+)
    ▼
:8443 Listener (net/http.Server)
    │
    ├─── TLS handshake (self-signed or CA cert)
    │
    ├─── Middleware: auth-by-bind-address
    │    ├── loopback? → skip auth, inject "localhost" identity
    │    └── network?  → extract Bearer token
    │         ├── PAT? → bcrypt lookup → inject token context
    │         └── JWT? → validate signature/expiry → inject session context
    │
    ├─── Middleware: scope enforcement
    │    └── required_scope ∈ token_scope? → 403 if no
    │
    ├─── Middleware: rate limiter
    │    └── check sliding window → 429 if exceeded
    │
    ▼
Handler (internal/api/v1/*.go)
    │
    ├── Validate input (Zod-like: Go struct tags + manual checks)
    ├── Call daemon core function (e.g., AddPeer, RemovePeer, CreateRoute)
    │    └── daemon core → SSH to VPS → awg syncconf / Caddy admin API
    ├── Write audit entry (async, non-blocking)
    └── Return JSON response
         │
         ▼
    Daemon State (~/.unet/config.json + VPS state)
```

**Concurrent writes to `awg0.conf`**: The daemon already has (or needs) a mutex serializing peer mutations. The remote API reuses the same mutex. No new concurrency primitive needed — the shared-process design makes this trivial.

---

## Migration / Compat Strategy

### Coexistence with localhost API

The existing localhost API (`:8080/api/*`) and the new remote API (`:8443/v1/*`) coexist without conflict:

| Aspect | Localhost API | Remote API |
|--------|--------------|------------|
| Port | 8080 (existing) | 8443 (new) |
| Path prefix | `/api/` | `/v1/` |
| Auth | None (loopback-only) | PAT/JWT (required for network) |
| TLS | No (plaintext, loopback) | Yes (required for non-loopback) |

### Shared handler logic

Both surfaces call the same internal functions. The `auth-by-bind-address` middleware in the remote server's chain determines auth requirement. On the localhost server, auth middleware is not mounted.

**No endpoint conflicts**: Different ports + different path prefixes = zero collision. The localhost `/api/peers` and remote `/v1/peers` are distinct URLs.

### Endpoint mapping (existing → new)

| Localhost endpoint | Remote equivalent | Notes |
|---|---|---|
| `GET /api/ports` | `GET /v1/routes` | Renamed: "ports" → "routes" (more accurate) |
| `POST /api/ports` | `POST /v1/routes` | Same rename |
| `DELETE /api/ports/:id` | `DELETE /v1/routes/:id` | Same rename |
| `GET /api/tunnel/status` | `GET /v1/tunnel/status` | Same path after prefix swap |
| `GET /api/status` | `GET /v1/status` | Remote version includes cert expiry |
| `GET /api/peers` | NEW | No localhost equivalent (local UI manages differently) |
| `POST /api/peers` | NEW | No localhost equivalent |
| `DELETE /api/peers/:id` | NEW | No localhost equivalent |
| `POST/GET/DELETE /v1/tokens` | NEW | No localhost equivalent |
| `GET /v1/audit` | NEW | No localhost equivalent |

**No overlap**: The localhost API uses `/api/*` paths, the remote API uses `/v1/*` paths. No shared URL space.

---

## Testing Strategy

### Unit tests

| Component | What's mocked | Tool |
|-----------|--------------|------|
| Auth middleware (PAT) | Token store (interface) | `testing` + interfaces |
| Auth middleware (JWT) | JWT signer | `testing` + fixed test keys |
| Scope enforcement | Request context with known scopes | Table-driven tests |
| Rate limiter | Clock (injectable) | `testing` |
| Token store CRUD | Filesystem (temp dir) | `t.TempDir()` |
| Audit logger | Filesystem (temp dir) | `t.TempDir()` |
| Handler logic | Daemon core (interface), audit writer | `testing` + httptest |
| Input validation | N/A (pure functions) | Table-driven tests |

### Integration tests

| Test | What runs real | What's mocked |
|------|---------------|---------------|
| Full auth flow | Token store (real file), JWT signer | VPS SSH, awg commands |
| Peer CRUD via API | Token store, handler, daemon core | SSH to VPS, `awg` CLI |
| Route CRUD via API | Token store, handler, daemon core | SSH to VPS, Caddy admin API |
| Rate limiting | Full middleware chain | Downstream handler |
| TLS cert generation | Real TLS handshake | N/A |

Integration tests run against `httptest.NewServer` with the full middleware chain mounted. No real VPS needed.

### End-to-end tests (manual / CI)

- Start daemon with test config → create PAT via CLI → curl against real server → verify peer appears in VPS config. Requires a real VPS or Docker-based VPS mock. Deferred to implementation phase.

---

## Open Risks

1. **bcrypt verification latency on every request**: bcrypt with cost 12 takes ~250ms. For high-throughput scenarios, this is too slow. Mitigation: cache verified tokens in an in-memory LRU with 5-min TTL. Token revocation invalidates cache entry. Spec doesn't mandate constant-time on every request — just at rest storage. (Decision in plan.md, not in spec.)

2. **Config.json file contention**: Both the localhost API and remote API write to `~/.unet/config.json`. The existing daemon must already serialize writes (or it's a pre-existing bug). Remote API reuses the same serialization mechanism. If none exists, one must be added — this is a prerequisite, not a feature.

3. **Self-signed cert UX**: External tools (undevops) connecting to a self-signed API need to trust the cert. Options: (a) pin the CA fingerprint in the consumer's config, (b) provide the cert file for import, (c) `InsecureSkipVerify` (not recommended). The plan defaults to (a) — the cert fingerprint is shown at creation and can be pinned. (Decision in plan.md, not in spec.)

4. **JWT refresh token security**: If a JWT is compromised, it's valid for its TTL (15 min). No refresh token rotation is specified for v0.1. Acceptable for single-admin MVP. Future: refresh token rotation + family detection.

5. **Concurrent peer creation (IP allocation)**: Spec mandates serialization via mutex. IP allocation must be atomic under the mutex: read current peers → find next IP → write config → sync. Race window is the mutex scope. If the mutex doesn't cover the full read-modify-write cycle, duplicate IPs are possible. Implementation MUST hold the mutex across the entire alloc+write+sync sequence.

6. **Audit log growth**: JSONL file grows unboundedly. For single-host MVP, this is fine (months of API calls ≈ few MB). Future: rotation + archival. Noted as accepted risk for v0.1.

7. **Rate limiting state loss on restart**: In-memory rate limit counters reset on daemon restart. Attacker gets a fresh window. Acceptable for MVP — 60 req/min isn't a security boundary, just abuse reduction. Future: persistent counters or external rate limiter.

---

## Decisions Made in Plan Beyond Spec

| Topic | Decision | Why |
|-------|----------|-----|
| Token cache for bcrypt perf | In-memory LRU cache with 5-min TTL, invalidated on revocation | bcrypt cost 12 = ~250ms per verify; unacceptable for repeated reads |
| JWT signing algorithm | HS256 with key derived from daemon config secret | Simplest; single-admin model doesn't need asymmetric signing |
| JWT TTL | 15 minutes default, configurable | Balance between security and UX |
| Audit log format | JSONL file at `~/.unet/audit.jsonl` | Simple, append-only, grep-friendly, no external deps |
| Rate limit state | In-memory, lost on restart | MVP-appropriate; external rate limiter is future scope |
| Self-signed cert algorithm | ECDSA P-256, 365-day validity | Modern, fast, smaller certs than RSA |
| API version header | `API-Version: 2026-05-27` in responses | Stripe-style date-based versioning; future-proofing |
| Config key for tokens | `apiTokens` array in `~/.unet/config.json` | Co-located with existing config; single file to back up |
| Config key for remote API settings | `remoteApi: { enabled, listenAddr, tlsCert, tlsKey }` | Grouped under dedicated key for clarity |
| Network partition write behavior | Synchronous 503 with structured error body; server-side queue+retry deferred to future spec | MVP simplicity — queuing adds complexity (persistence, retry logic, ordering) without clear v0.1 benefit |
| Loopback :8443 admin scope | Loopback requests on :8443 skip auth and receive `admin` scope unconditionally | Mirrors existing :8080 localhost behavior; single-admin model means shell access = full access already. Documented as accepted security trade-off in spec.md |
