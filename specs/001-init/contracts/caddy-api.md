# Caddy Admin API Contract

This document specifies how the Unet Go daemon interacts with the Caddy admin REST API on the VPS to dynamically expose local ports.

## Endpoint & Auth

- **URL**: `http://<tunnel.serverIp>:2019` — bound exclusively to the WireGuard-internal IP on the VPS (e.g., `10.8.1.1:2019`). Reachable ONLY by authenticated WireGuard peers.
- **Auth modes** (selected by user, see `data-model.md` `caddyApi.authMode`):
  - **`"ip-only"`** (default) — no application-layer auth. WG peer authentication IS the security boundary.
  - **`"mtls"`** — client TLS certificate required. Daemon generates a self-signed cert on first connect, registers DER pubkey in Caddy `admin.remote.access_control[].public_keys` via the initial IP-only bootstrap call, then switches to mTLS for all subsequent calls.
- **No Bearer token support.** Caddy v2 admin API has no native Bearer middleware (confirmed via upstream docs; the `Authorization: Bearer ...` header mentioned in upstream Caddy docs refers exclusively to outbound Cloudflare DNS API calls, not inbound admin auth).
- **Content-Type**: `application/json`.

## Endpoint: `POST /config/apps/http/servers/srv0/routes`

Add a new reverse-proxy route.

**Payload Example**:

```json
{
  "match": [
    { "host": ["app.mydomain.com"] }
  ],
  "handle": [
    {
      "handler": "reverse_proxy",
      "upstreams": [
        { "dial": "10.8.1.2:3000" }
      ]
    }
  ],
  "terminal": true
}
```

**Response `200 OK`** on success (Caddy admin API does not return JSON bodies for `POST /config/...`).

## Route Removal — Host-Match (Mutex-Guarded)

Positional `DELETE /routes/<index>` is unsafe under concurrent operations (index shifts between GET and DELETE). The daemon MUST hold a mutex around the GET → match-by-host → DELETE sequence.

### Step 1: `GET /config/apps/http/servers/srv0/routes`

Fetch all routes; find the index where `match[0].host[0]` equals the target subdomain.

### Step 2: `DELETE /config/apps/http/servers/srv0/routes/<matched-index>`

Delete using the index found in Step 1.

**Concurrency guarantee**: the mutex covers GET + DELETE as a single critical section. No other goroutine in the daemon process can mutate Caddy routes during this window. See `internal/proxy/caddy.go` (T016).

**Example flow**:

```text
GET  /config/apps/http/servers/srv0/routes
  → [ { match: [{ host: ["app.mydomain.com"] }] },
      { match: [{ host: ["api.mydomain.com"] }] } ]
  → Target "api.mydomain.com" → index 1

DELETE /config/apps/http/servers/srv0/routes/1
  → 200 OK
```

## mTLS Provisioning Flow (when `authMode == "mtls"`)

**Key principle (post-F2)**: mTLS client public keys are registered **via SSH + `docker exec`** editing of Caddy's config file on the server, NOT via the Caddy admin API. This avoids the chicken-and-egg lockout where the second peer cannot POST its own pubkey through an admin endpoint that now requires mTLS. Privilege model: pubkey registration is gated by SSH-root access (same privilege as adding a WireGuard peer), not by an existing mTLS cert.

### Per-Peer mTLS Provisioning (runs as part of every peer-add flow)

For each new peer (first AND subsequent):

1. **Daemon generates a fresh client cert** locally:
   - ECDSA P-256 keypair, self-signed, validity 10 years.
   - Stores `mtlsClientCertPem` + `mtlsClientKeyPem` in local config (file mode `0600` POSIX / ACL deny-others Windows; per FR-011 these are secret fields and `RedactedString()` MUST mask them in logs).
   - Computes DER-encoded SubjectPublicKeyInfo, base64-encodes it → `<peer-pubkey-b64>`.

2. **Daemon appends the pubkey to Caddy's config file on the VPS via SSH + `docker exec`** — see `appendix-peer-add-flow.md` §2.6 for the concrete command sequence. The amendment touches Caddy's `autosave.json`:

   ```jsonc
   // /config/caddy/autosave.json — partial diff
   {
     "admin": {
       "listen": "10.8.1.1:2019",          // initial: plaintext on WG-internal
       "remote": {
         "access_control": [
           {
             "public_keys": [
               "<peer1-pubkey-b64>",        // existing peers preserved
               "<peer2-pubkey-b64>",
               "<new-peer-pubkey-b64>"      // appended in this op
             ],
             "permissions": [
               { "paths": ["/config/*"], "methods": ["GET","POST","DELETE","PATCH","PUT"] }
             ]
           }
         ]
       }
     }
   }
   ```

3. **Daemon signals Caddy to reload**:
   ```bash
   ssh <vps> "docker exec unet-caddy caddy reload --config /config/caddy/autosave.json --adapter json"
   ```
   Caddy reloads without dropping in-flight HTTPS requests.

4. **First peer special-case (TLS flip)** — on the very first mTLS-enabled peer, after step 3 succeeds, the daemon ALSO updates `admin.listen` to a TLS-wrapped listener (via SSH + edit of `autosave.json`, then reload). Subsequent connections by ALL peers now require mTLS. Subsequent peer-add cycles skip this step (it's idempotent — already-TLS stays TLS).

5. **Daemon switches HTTP client to mTLS** — uses `tls.Config{Certificates: [...], InsecureSkipVerify: false}`, pins the Caddy server cert fingerprint (read from `autosave.json` over SSH on first connect and cached locally). All subsequent admin API calls go through the mTLS channel.

### Recovery from Client Cert Loss

If the local client cert is destroyed (e.g., `~/.unet/` wiped, machine reinstall without backup), the daemon CANNOT reach the Caddy admin endpoint over mTLS. Recovery path:

1. **User authenticates via SSH** (they still have VPS root credentials — these live in a separate location).
2. **Daemon re-runs the per-peer mTLS provisioning flow** (steps 1-3 above) — generates a fresh cert, appends to `autosave.json` via SSH, reloads Caddy.
3. **Stale entry cleanup (optional)** — the lost cert's pubkey remains in `public_keys[]` indefinitely. Periodically (e.g. on each peer-rotate operation) the daemon SHOULD prune `public_keys[]` entries that no longer correspond to active peers. Track the binding `peer_id → pubkey` in `clientsTable` metadata.

**No "downgrade-to-IP-only" recovery path is exposed**, because that would let any WG peer demote security. If the user explicitly wants to disable mTLS, they `ssh` and manually edit `autosave.json`.

## Error Handling

| Caddy Response | Daemon Action |
|----------------|---------------|
| `200 OK` | Continue |
| `400 Bad Request` (malformed JSON) | Log payload, do NOT retry, surface "internal error" to UI |
| `409 Conflict` | Likely concurrent write; retry once after re-acquiring mutex |
| Connection refused / timeout | Tunnel likely down — surface "tunnel not connected" via `/api/status` |
| `403 Forbidden` (when in mTLS mode) | Client cert mismatch — surface mTLS-recovery instructions |
