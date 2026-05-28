# Auth Flows: Remote Control Plane API

**Spec**: `specs/002-api-control-plane/spec.md`
**Created**: 2026-05-27

---

## 1. PAT Creation Flow

```
Admin CLI / Bootstrap
    │
    ▼
POST /v1/tokens
Authorization: Bearer <existing-admin-PAT>
Content-Type: application/json
{ "name": "undevops-prod", "scope": "write" }
    │
    ▼
Token Store:
  1. Generate 32 random bytes → base64url → prefix "unet_"
  2. bcrypt hash (cost 12) the full token string
  3. Store: { id, name, tokenHash, tokenPrefix, scope, ... }
  4. Write audit entry: action=create_token, target=new-token-id
    │
    ▼
Response 201:
{
  "id": "uuid-new",
  "name": "undevops-prod",
  "token": "unet_a1b2c3d4e5f6g7h8i9j0...",   ← PLAINTEXT, SHOWN ONCE
  "scope": "write",
  "createdAt": "2026-05-27T10:00:00Z"
}
    │
    ▼
Admin stores token securely (env var, secret manager, etc.)
Token plaintext is NEVER returned again by any endpoint.
```

### Bootstrap (first-run)

On first daemon start with remote API enabled, if `apiTokens` is empty:

1. Generate an admin-scoped PAT named `bootstrap-admin`.
2. Write it to a file at `~/.unet/bootstrap-token` (mode 0600).
3. Log a one-time message: "Bootstrap token written to ~/.unet/bootstrap-token. Delete after use."
4. On first successful `POST /v1/tokens` using the bootstrap token → delete `~/.unet/bootstrap-token`.

This ensures the admin can create their first real token without already having a token.

---

## 2. PAT Usage Flow

```
External Tool (curl, undevops, etc.)
    │
    │  Every API request:
    ▼
GET /v1/peers
Authorization: Bearer unet_a1b2c3d4e5f6g7h8i9j0...
Host: unet.example.com:8443
    │
    ▼
┌─────────────────────────────────────┐
│  Middleware: auth-by-bind-address    │
│                                     │
│  r.RemoteAddr is loopback?          │
│    YES → skip auth, inject localhost │
│    NO  → extract Bearer token       │
│           starts with "unet_"?      │
│             YES → PAT flow          │
│             NO  → JWT flow          │
└──────────┬──────────────────────────┘
           │ PAT flow
           ▼
┌─────────────────────────────────────┐
│  Middleware: PAT validation          │
│                                     │
│  1. Check in-memory token cache     │
│     (hash → tokenID, verified <5m)  │
│     HIT → inject context, done      │
│                                     │
│  2. MISS → bcrypt verify            │
│     a. Iterate all stored tokenHash │
│     b. bcrypt.CompareHashAndPassword│
│     c. Match found?                 │
│        YES → check enabled, expiry  │
│        NO  → 401                    │
│                                     │
│  3. Update lastUsedAt, requestCount │
│  4. Add to in-memory cache (5m TTL) │
│  5. Inject context:                 │
│     { tokenID, tokenName, scope }   │
└──────────┬──────────────────────────┘
           │
           ▼
┌─────────────────────────────────────┐
│  Middleware: scope enforcement       │
│                                     │
│  Endpoint requires scope X.         │
│  Token has scope Y.                 │
│  Y >= X in hierarchy?               │
│    (admin > write > read)           │
│    YES → proceed                    │
│    NO  → 403 { error, scope info }  │
└──────────┬──────────────────────────┘
           │
           ▼
┌─────────────────────────────────────┐
│  Middleware: rate limiter            │
│                                     │
│  Sliding window: 60 req/min/token   │
│  Burst: 10                          │
│  Within limits? → proceed           │
│  Exceeded? → 429 + Retry-After      │
└──────────┬──────────────────────────┘
           │
           ▼
       Handler
```

---

## 3. JWT Session Establishment

Used by admin UI (browser) to get a short-lived session token.

```
Admin UI (browser)
    │
    ▼
POST /v1/auth/session
Authorization: Bearer unet_<admin-PAT>
    │
    ▼
Server:
  1. Validate PAT (same as PAT usage flow)
  2. Extract token scope and name
  3. Generate JWT:
     - sub: token-id
     - name: token-name
     - scope: token-scope
     - iss: "unet-daemon"
     - iat: now
     - exp: now + 15 minutes
     - jti: uuidv4
  4. Sign with HS256 (jwtSigningKey from config)
    │
    ▼
Response 200:
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "expiresIn": 900,
  "scope": "admin"
}
    │
    ▼
Browser stores JWT (memory or sessionStorage).
Subsequent requests use JWT as Bearer token.
    │
    │  When JWT nears expiry:
    ▼
POST /v1/auth/session
Authorization: Bearer unet_<admin-PAT>    ← re-send PAT
    │
    ▼
New JWT issued. PAT never leaves server storage.
```

### JWT validation on each request

```
Authorization: Bearer eyJ...
    │
    ▼
┌─────────────────────────────────────┐
│  Middleware: JWT validation          │
│                                     │
│  1. Parse token (don't trust claims)│
│  2. Verify HS256 signature          │
│  3. Check exp > now                 │
│  4. Check iss == "unet-daemon"      │
│  5. Extract sub, scope, name        │
│  6. Verify referenced PAT still     │
│     exists and is enabled           │
│     (if PAT revoked → JWT invalid)  │
│  7. Inject context:                 │
│     { sessionID: jti, tokenScope,   │
│       tokenName, parentTokenID }    │
└──────────────────────────────────────┘
```

---

## 4. Token Revocation Flow

```
Admin
    │
    ▼
DELETE /v1/tokens/<token-id>
Authorization: Bearer <admin-PAT>
    │
    ▼
Server:
  1. Validate caller has admin scope
  2. Look up target token by ID
  3. Set token.enabled = false (soft-disable)
     OR remove from store (hard-delete)
     Decision: soft-disable for MVP (preserves audit trail)
  4. Invalidate in-memory cache entry for this token
  5. Write audit entry: action=revoke_token, target=token-id
  6. Any active JWTs referencing this PAT:
     - Will fail on next request (step 6 of JWT validation)
     - Max 15 min before natural expiry
  7. Any in-flight PAT-authenticated requests:
     - Cache entry already invalidated
     - Next request → bcrypt miss → token not found → 401
    │
    ▼
Response 200:
{ "id": "uuid", "status": "revoked" }
```

---

## 5. Auth-by-Bind-Address Logic

The core branching decision that determines whether auth is required.

```
Incoming request on :8443
    │
    ▼
Extract r.RemoteAddr → net.IP
    │
    ▼
Is IP loopback? (127.0.0.1, ::1, or Unix socket)
    │
    ├── YES ──────────────────────────────────────┐
    │   Skip auth middleware entirely               │
    │   Inject "localhost" identity into context:  │
    │   { source: "localhost", scope: "admin" }   │
    │   Proceed directly to handler                │
    │                                               │
    ├── NO ───────────────────────────────────────┐
    │   Apply full auth middleware chain:          │
    │                                              │
    │   Extract Authorization header               │
    │   ├── Missing → 401                          │
    │   ├── Bearer unet_* → PAT validation flow    │
    │   ├── Bearer eyJ* → JWT validation flow      │
    │   └── Other → 401                            │
    │                                              │
    │   Then: scope enforcement → rate limit        │
    │                                              │
    └──────────────────────────────────────────────┘
```

### Why this design

Per Clarification #3: "localhost endpoints stay unauthenticated; remote endpoints require auth; both share the same handler implementation."

The auth-by-bind-address check is implemented as middleware on the remote server's middleware chain. It runs BEFORE any auth middleware, so:

- Loopback connections: auth middleware is skipped → request proceeds as "localhost admin".
- Network connections: auth middleware runs → PAT/JWT validated → scope checked → rate limited.

On the localhost server (`:8080`), no auth middleware is mounted at all. The remote server (`:8443`) has the full chain.

### Trusted proxies

For v0.1, `r.RemoteAddr` is trusted directly (no `X-Forwarded-For` parsing). If the API is behind a reverse proxy, the admin must configure the proxy to set `X-Real-IP` and the daemon must be configured with a `trustedProxies` list. This is a future enhancement — for MVP, direct connection is assumed.

### Security note

Binding the remote API to `127.0.0.1:8443` effectively makes it localhost-only (all requests skip auth). This is intentional — the admin can disable remote access entirely by setting `remoteApi.listenAddr: "127.0.0.1:8443"`.
