# Feature Specification: Remote Control Plane API

**Feature Branch**: `specs/002-api-control-plane`
**Created**: 2026-05-27
**Status**: Draft
**Input**: Phase 1/2 architectural decision — unet standalone OSS with stable API consumed by undevops as External API Consumer Pattern plugin.

## Clarifications

### Session 2026-05-27 (round 1)

| Topic | Decision |
|---|---|
| Auth method | PAT (CLI/external) + JWT (admin UI sessions) |
| API path prefix | /v1/ (Stripe-style, no /api/ outer prefix) |
| Backward compat with localhost daemon API | Keep both — localhost unauth, remote auth, common handler |
| Control-plane process placement | Same Go process as daemon, separate port |
| Multi-host | Single-host MVP; multi-host deferred to future spec |

See inline notes below for full rationale.

### Session 2026-05-27 (round 2)

| Topic | Decision |
|---|---|
| Stale data strategy | Cached data returned with `stale: true` boolean field (5s cache age threshold) |
| Rate limit configurability | Hardcoded 60 req/min per token + burst 10 for MVP; per-token configuration deferred |
| Network partition write behavior | Synchronous 503 with structured error; server-side queue deferred |

See inline notes and plan.md "Decisions made beyond spec" for full rationale.

### Resolved Decisions

- Q: Auth method for remote API? → **Decision: Both — opaque PAT-style tokens for CLI/external consumers + JWT for admin UI browser sessions (short TTL, refresh via PAT).** PAT covers programmatic and scripting use cases (GitHub-style UX); JWT gives browser sessions proper expiration and revocation without per-request PAT exposure. Both backed by the same token store.
- Q: API path prefix? → **Decision: `/v1/` (no `/api/` outer prefix).** Stripe-style, cleaner URLs; the daemon's HTTP listener is a dedicated API server, no need for an `/api/` namespace.
- Q: Backward compat with localhost-only daemon API? → **Decision: Keep both — localhost endpoints stay unauthenticated; remote (network-bind) endpoints require auth; both share the same handler implementation.** Preserves all current localhost callers (admin UI, scripts) with zero migration friction; auth is enforced based on bind address at request time.
- Q: Where does the control plane run? → **Decision: Same Go process as daemon, listening on a separate port from the existing localhost HTTP.** One binary, one deploy — Docker-style local socket + remote TCP pattern; preserves "self-host-able WITHOUT undevops" invariant.
- Q: Multi-host story? → **Decision: Single-host MVP in 002 — one daemon manages exactly one VPS; multi-host (one daemon → N VPS) deferred to a future feature spec (007+).** Keeps the data model simple; multi-host requires VPS-as-resource modeling, profile switching, and cross-VPS coordination — not MVP-critical.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - External Tool Lists Peers via API (Priority: P1)

As an external tool (e.g., undevops dashboard), I authenticate with an API token and retrieve a list of all configured peers so that I can display tunnel status in my own UI.

**Why this priority**: Read-only access is the foundation. Without it, no external integration can function. This is the prerequisite for all write operations.

**Independent Test**: Generate an API token. Call `GET /v1/peers` with `Authorization: Bearer <token>`. Verify the response includes peer IDs, names, IP addresses, and connection status.

**Acceptance Scenarios**:

1. **Given** a valid API token with `read` scope, **When** the client calls `GET /v1/peers`, **Then** the response is a JSON array of peer objects with status 200
2. **Given** an invalid or revoked API token, **When** the client calls any API endpoint, **Then** the response is 401 with a structured error body
3. **Given** a valid token with insufficient scope, **When** the client calls a write endpoint, **Then** the response is 403 with scope details in the error body

---

### User Story 2 - External Tool Creates Peer and Receives WireGuard Config (Priority: P1)

As an external tool, I create a new peer (e.g., a mobile device) and receive a ready-to-import WireGuard client configuration file so that the device can join the tunnel network without SSH or manual config editing.

**Why this priority**: Peer creation is the core value-add of the remote API — it's the operation that currently requires SSH + `docker exec` to edit `awg0.conf`. Automating it via API is the entire point of this spec.

**Independent Test**: Authenticate, call `POST /v1/peers` with a peer name. Verify the response includes the full WireGuard client config (including all AmneziaWG obfuscation parameters) and that the peer appears in `GET /v1/peers`.

**Acceptance Scenarios**:

1. **Given** a valid token with `write` scope, **When** the client calls `POST /v1/peers` with `{ "name": "my-phone" }`, **Then** the response is 201 with a peer object including `id`, `name`, `publicKey`, `allowedIp`, and `clientConfig` (full `.conf` content)
2. **Given** a peer name that already exists, **When** the client calls `POST /v1/peers`, **Then** the response is 409 with `error: "peer_name_conflict"`
3. **Given** concurrent peer creation requests, **When** two clients create peers simultaneously, **Then** both succeed with unique IPs, or one receives a retryable error (no silent data corruption)

---

### User Story 3 - External Tool Queries Tunnel and Ingress Status (Priority: P1)

As an external tool, I query the current tunnel status (connected/disconnected, peer handshake recency) and ingress route status (active routes, target ports) so that I can surface health in my monitoring dashboard.

**Why this priority**: Status queries are read-only, low-risk, and high-value. They enable monitoring and alerting without any write capability. Natural first integration point.

**Independent Test**: With an active tunnel and at least one exposed port, call `GET /v1/tunnel/status` and `GET /v1/routes`. Verify response data matches the local daemon state visible at `localhost:8080/api/status`.

**Acceptance Scenarios**:

1. **Given** the daemon is running with an active tunnel, **When** the client calls `GET /v1/tunnel/status`, **Then** the response includes `status: "connected"`, local IP, server IP, and connected-at timestamp
2. **Given** no active tunnel, **When** the client calls `GET /v1/tunnel/status`, **Then** the response includes `status: "disconnected"` — NOT an error
3. **Given** two exposed ports, **When** the client calls `GET /v1/routes`, **Then** the response is an array of two route objects with subdomain, local port, and status

---

### User Story 4 - External Tool Creates Ingress Route (Priority: P2)

As an external tool, I create an ingress route to publish a localhost service via the VPS so that a newly deployed application becomes publicly accessible.

**Why this priority**: Route creation is a write operation that depends on peer creation and tunnel connectivity being functional first. P2 because P1 read operations must be solid before writes that mutate production routing.

**Independent Test**: Authenticate with `write` scope, call `POST /v1/routes` with `{ "localPort": 3000, "subdomain": "app" }`. Verify the route is active and `https://app.mydomain.com` resolves to the local service.

**Acceptance Scenarios**:

1. **Given** an active tunnel and valid DNS config, **When** the client creates a route for port 3000 on subdomain "app", **Then** the route is active and Caddy config is updated within 2 seconds
2. **Given** a subdomain already in use, **When** the client creates a route, **Then** the response is 409 with `error: "route_conflict"`
3. **Given** no active tunnel, **When** the client creates a route, **Then** the response is 412 with `error: "tunnel_not_connected"`

---

### User Story 5 - Scoped API Tokens (Priority: P2)

As the unet administrator, I create API tokens with specific permission scopes (read-only, read-write, admin) so that external tools get least-privilege access.

**Why this priority**: Scope enforcement prevents a compromised token from doing more than its consumer needs. Essential before opening the API to third-party tools. P2 because P1 can initially use a single admin token for simplicity.

**Independent Test**: Create a `read`-scoped token. Verify it can call `GET /v1/peers` but NOT `POST /v1/peers`. Create a `write`-scoped token. Verify it can call both.

**Acceptance Scenarios**:

1. **Given** a token with scope `read`, **When** it calls `GET /v1/peers`, **Then** the response is 200
2. **Given** a token with scope `read`, **When** it calls `POST /v1/peers`, **Then** the response is 403
3. **Given** a token with scope `admin`, **When** it calls `POST /v1/tokens` (create token), **Then** the response is 201
4. **Given** a token with scope `write`, **When** it calls `POST /v1/tokens`, **Then** the response is 403

---

### User Story 6 - Token Identity and Audit Log (Priority: P3)

As the unet administrator, I want API tokens to be associated with a user identity and every API action to be recorded in an audit log so that I can trace who did what.

**Why this priority**: Multi-user foundations enable future team/enterprise use. Audit logging is essential for production trust. P3 because a single-admin setup can operate without it initially.

**Independent Test**: Perform an action (create peer) with a named token. Query the audit log. Verify the entry records the token name, action, target peer, and timestamp.

**Acceptance Scenarios**:

1. **Given** a token with an associated identity, **When** the token creates a peer, **Then** an audit log entry is created with actor=token name, action=create_peer, target=peer ID, timestamp
2. **Given** the admin queries audit log via `GET /v1/audit`, **Then** entries are returned in reverse chronological order, paginated, filterable by actor and action type

---

### Edge Cases

- **VPS unreachable**: Remote API returns cached/stale data for read endpoints (with `stale: true` boolean field when cache age exceeds 5 seconds; clients may inspect and refresh as needed). Write endpoints return 503 with `error: "vps_unreachable"`. **Decision: read endpoints return cached data with a `stale: true` boolean field when cache age exceeds 5 seconds; clients may inspect and refresh as needed** — Matches plan.md decision "Stale data: read endpoints return cached data with stale: true indicator".
- **Token expired/revoked mid-session**: Next request returns 401. Client must re-authenticate or obtain a new token. No grace period.
- **Peer config conflicts (IP exhaustion)**: If the subnet (e.g., `10.8.1.0/24`) runs out of allocatable IPs, `POST /v1/peers` returns 507 with `error: "ip_pool_exhausted"`.
- **Concurrent peer creation**: Must serialize writes to `awg0.conf`. Use a mutex or queue. Concurrent requests must not corrupt the config file or produce duplicate IPs.
- **Network partition between daemon and VPS**: Daemon detects via existing drift-check mechanism (FR-010). Remote API surfaces `vps_status: "partitioned"` in status responses. On VPS unreachability, the daemon returns HTTP 503 with a structured error body (`{error: 'vps_unreachable', retry_after_seconds: N, last_seen: <ts>}`). Client is responsible for retry. Server-side queuing of write operations is out of scope for MVP — tracked as future enhancement.
- **Control plane TLS certificate expiry**: If using self-signed certs, the admin must rotate manually. API should surface cert expiry warning via `GET /v1/status` when cert expires within 30 days.
- **Rate limiting**: The API enforces per-token rate limits. Default: 60 requests/minute per token, burst of 10. Rate-limited responses return 429 with `Retry-After` header. **Decision: hardcoded 60 req/min per token with burst 10 for MVP; per-token configuration deferred to future spec** — Matches plan.md decision "Rate limits: 60 req/min per token, burst 10, hardcoded for MVP. Configurable in future".

## Requirements *(mandatory)*

### Functional Requirements

**Authentication (P1)**:
- **FR-001**: The remote API MUST authenticate all requests via API tokens sent as `Authorization: Bearer *** header. Unauthenticated requests MUST be rejected with 401. **[security-note]**: Loopback requests (`127.0.0.1` / `::1`) skip auth and receive admin scope — see "Accepted Security Trade-offs: Loopback :8443 → unconditional admin scope".
- **FR-002**: API tokens MUST be stored hashed (bcrypt or argon2) at rest. The plaintext token is shown only once at creation time.
- **FR-003**: Token management endpoints (`POST /v1/tokens`, `GET /v1/tokens`, `DELETE /v1/tokens/:id`) MUST require `admin` scope. Token creation accepts `name`, `scope` (`read` | `write` | `admin`), and optional `expiresAt`.

**Peers CRUD (P1)**:
- **FR-004**: `GET /v1/peers` MUST return all configured peers with: id, name, public key, allowed IP, connected status (derived from recent handshake), and creation timestamp. Requires `read` scope.
- **FR-005**: `POST /v1/peers` MUST create a new peer on the VPS (add to `awg0.conf`, run `awg syncconf`), allocate the next available IP in the tunnel subnet, and return the complete WireGuard client config including all AmneziaWG obfuscation parameters. Requires `write` scope.
- **FR-006**: `GET /v1/peers/:id` MUST return a single peer's detail including its current handshake status and data transfer stats (if available from `awg show`). Requires `read` scope.
- **FR-007**: `DELETE /v1/peers/:id` MUST remove the peer from `awg0.conf`, run `awg syncconf`, and return 200. The peer's WireGuard connection is immediately terminated. Requires `write` scope.

**Ingress Routes CRUD (P2)**:
- **FR-008**: `GET /v1/routes` MUST return all active ingress routes with: id, subdomain, local port, target peer IP, Caddy route status, and creation timestamp. Requires `read` scope.
- **FR-009**: `POST /v1/routes` MUST create a Caddy ingress route and (if Cloudflare mode) create the DNS A-record. Validates subdomain format per FR-012 rules from spec 001-init. Requires `write` scope.
- **FR-010**: `DELETE /v1/routes/:id` MUST remove the Caddy route and (if Cloudflare mode) remove the DNS record. Returns 200. Requires `write` scope.

**Tunnel Status (P1)**:
- **FR-011**: `GET /v1/tunnel/status` MUST return current tunnel status (`connected`/`disconnected`/`connecting`/`error`), local IP, server IP, server endpoint, and connected-at timestamp. Requires `read` scope.

**Transport Security (P1)**:
- **FR-012**: The remote API listener MUST require TLS. On first start, generate a self-signed certificate. Admin MAY replace with a CA-signed cert via configuration. The listener MUST NOT start in plaintext mode on a network-accessible interface.
- **FR-013**: The API listener address MUST be configurable (default: `0.0.0.0:8443`). Setting to `127.0.0.1` disables remote access (reverts to localhost-only behavior).

**Error Responses (P1)**:
- **FR-014**: All error responses MUST use a consistent JSON structure: `{ "error": "<snake_case_code>", "message": "<human-readable>", "context": { ... } }`. HTTP status codes MUST be semantically correct (401, 403, 404, 409, 412, 500, 503).

**Rate Limiting (P2)**:
- **FR-015**: The API SHOULD enforce per-token rate limits. Default: 60 requests/minute per token, burst of 10. Rate-limited responses return 429 with `Retry-After` header. Hardcoded for MVP; per-token configuration deferred to future spec.

**Audit Logging (P3)**:
- **FR-016**: Every state-changing API call (peer create/delete, route create/delete, token create/revoke) MUST be recorded in an append-only audit log with: timestamp, actor (token name + ID), action, target resource ID, and request metadata (source IP, user-agent).
- **FR-017**: `GET /v1/audit` MUST return audit entries paginated (default 50, max 200), filterable by actor and action type. Requires `admin` scope.

### Key Entities

- **APIToken**: Authentication credential for the remote API. Attributes: id, name (human-readable), tokenHash, scope (`read`/`write`/`admin`), createdBy, createdAt, expiresAt, lastUsedAt, requestCount, enabled.
- **AuditEntry**: Immutable record of an API action. Attributes: id, timestamp, actorTokenId, actorTokenName, action (enum: `create_peer`, `delete_peer`, `create_route`, `delete_route`, `create_token`, `revoke_token`, `rotate_cert`), targetResourceId, sourceIp, userAgent, metadata (JSON).

## Assumptions

- **Same-process deployment**: The control plane API runs in the same Go process as the existing local daemon, on a separate HTTP listener. This avoids IPC complexity and lets the API reuse the daemon's VPS connection and state.
- **Daemon state is source of truth**: The API reads from and writes to the same `~/.unet/config.json` and VPS state that the local daemon uses. No separate database.
- **Single-admin model for v0.1**: All tokens are created by a single administrator. Multi-user identity is deferred to a future spec.
- **TLS is required for remote access**: No plaintext HTTP on non-loopback interfaces. Self-signed certs are acceptable for initial releases; the admin can provision CA-signed certs.
- **Existing daemon API unchanged**: The localhost `/api/*` endpoints remain as-is. The remote API is an additive layer, not a replacement.

## Out of Scope (for this spec)

- Admin UI redesign or new UI views (separate future spec)
- AmneziaWG protocol-level changes (out of scope for any API spec)
- Multi-tenant data isolation / orgs / teams (separate future spec)
- Billing, quotas, device limits (enterprise tier concern)
- gRPC API (may be added as a future consideration — the HTTP REST API is the v0.1 scope)
- WebSocket/SSE streaming for real-time status updates (polling is sufficient for v0.1)
- OAuth2 / OIDC integration (PAT-style tokens for v0.1)
- Multi-host / multi-VPS management (one daemon = one VPS for now)

## Accepted Security Trade-offs

### Loopback :8443 → unconditional admin scope

Requests originating from loopback (`127.0.0.1` / `::1`) on the remote API port `:8443` are treated as **admin-scoped without authentication**, mirroring the existing localhost-only daemon API (`:8080`) behavior.

**Rationale**: Single-admin model assumes the operator has full shell access to the daemon host. Any local process with shell access can already manipulate the daemon's filesystem state and tokens directly, so requiring auth for loopback would only block legitimate local tooling without raising the security floor.

**Implications**:
- Anyone with shell access to the daemon host has full control.
- Multi-user systems (rare for unet) should NOT run the daemon as a shared service.
- Future enterprise tier (multi-user) will redefine loopback auth (likely require token even on loopback, plus per-user identity).

**Status**: Accepted for v0.1 OSS scope.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A valid `read`-scoped token can list all peers via `GET /v1/peers` in under 200ms (P95) when the daemon is co-located on the same host as the API consumer
- **SC-002**: 100% of write endpoints (`POST /v1/peers`, `POST /v1/routes`, `DELETE /v1/peers/:id`, `DELETE /v1/routes/:id`) require authentication and enforce scope
- **SC-003**: Creating a new peer via `POST /v1/peers` returns a ready-to-import WireGuard client config in under 3 seconds (P95), including the VPS `awg syncconf` round-trip
- **SC-004**: The remote API rejects all unauthenticated requests with 401 and all insufficient-scope requests with 403 — zero bypass paths in automated security testing
- **SC-005**: The API operates correctly when the VPS is unreachable: read endpoints return cached data with a `stale: true` indicator, write endpoints return 503 within the configured timeout
- **SC-006**: API token plaintext is shown exactly once at creation and never again — not in logs, not in `GET /v1/tokens` responses, not in error messages
- **SC-007**: The self-signed TLS certificate is generated automatically on first start, and the API refuses to listen on non-loopback without TLS
- **SC-008**: The existing localhost daemon API at `localhost:8080/api/*` continues to function unchanged after the remote API is enabled — zero regressions in local UI functionality
- **SC-009**: Concurrent `POST /v1/peers` requests (up to 5 simultaneous) complete without data corruption or duplicate IP assignment
- **SC-010**: Audit log entries are immutable and queryable via `GET /v1/audit` within 1 second of the action occurring
