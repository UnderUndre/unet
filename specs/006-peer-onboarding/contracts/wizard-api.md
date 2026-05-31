# Wizard API Contract

**Spec**: `specs/006-peer-onboarding/spec.md`
**Extends**: `specs/002-api-control-plane/contracts/api.openapi.yaml`
**Created**: 2026-05-28

---

## Overview

Wizard API endpoints are mounted on the same listener as spec 002's Control Plane API. All endpoints are prefixed with `/v1/wizard/`. Additional peer-related endpoints (`/v1/peers/{id}/qr`, `/v1/peers/{id}/invite`) and `/v1/routes/expose` extend the existing 002 surface.

**Auth**: Wizard endpoints require Bearer token (PAT or JWT) with `write` scope. Loopback requests (localhost admin UI) bypass auth — same as 002's loopback exemption.

**Cross-spec follow-up**: These endpoints should be added to `specs/002-api-control-plane/contracts/api.openapi.yaml` as a documentation update during implementation.

---

## Endpoints

### `POST /v1/wizard/sessions`

Start a new wizard session.

**Request**: Empty body.

**Response** `200`:
```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "current_step": "welcome",
  "status": "in_progress",
  "progress_pct": 0,
  "started_at": "2026-05-28T10:00:00Z"
}
```

**Response** `409`:
```json
{
  "error": "session_exists",
  "message": "A wizard session already exists. Resume it.",
  "context": {
    "session_id": "existing-uuid",
    "current_step": "ssh"
  }
}
```

**Notes**:
- Single session model. If session exists, client must resume or abandon.
- Creates `~/.unet/wizard-state.json`.

---

### `GET /v1/wizard/sessions/{id}`

Get current wizard session state. Used for resume.

**Response** `200`:
```json
{
  "session_id": "550e8400-...",
  "current_step": "domain_mode",
  "status": "in_progress",
  "progress_pct": 50,
  "started_at": "2026-05-28T10:00:00Z",
  "last_saved_at": "2026-05-28T10:02:30Z",
  "inputs": {
    "ssh": {
      "host": "1.2.3.4",
      "port": 22,
      "user": "root",
      "auth_type": "key",
      "key_path": "~/.ssh/id_ed25519"
    }
  },
  "preflight_result": { "...": "..." },
  "domain_check_result": null
}
```

**Response** `404`:
```json
{
  "error": "session_not_found",
  "message": "No wizard session with that ID. Start a new one."
}
```

**Notes**:
- Returns full state including all accumulated inputs.
- Sensitive fields (SSH key content, Cloudflare token) are redacted in the response. Only metadata (key path, token prefix) is returned.
- Client uses `current_step` to determine which step component to render.

---

### `DELETE /v1/wizard/sessions/{id}`

Abandon wizard session. Deletes `wizard-state.json`.

**Response** `200`:
```json
{
  "session_id": "550e8400-...",
  "status": "abandoned"
}
```

---

### `POST /v1/wizard/sessions/{id}/steps/{step}`

Submit step input and validate. Transitions wizard state forward if valid.

**Steps**: `welcome`, `ssh`, `domain_mode`, `domain_check`, `cloudflare`, `create_peer`

**Request for `ssh` step**:
```json
{
  "host": "1.2.3.4",
  "port": 22,
  "user": "root",
  "auth_type": "key",
  "key_path": "~/.ssh/id_ed25519",
  "password": null
}
```

**Request for `domain_mode` step**:
```json
{
  "mode": "byo"
}
```

**Request for `domain_check` step**:
```json
{
  "domain": "example.com"
}
```

**Request for `cloudflare` step**:
```json
{
  "token": "op:cf-token-value",
  "enabled": true
}
```

**Request for `create_peer` step**:
```json
{
  "peer_name": "phone",
  "expose_port": {
    "local_port": 3000,
    "subdomain": "app"
  }
}
```

**Response** `200` (step accepted):
```json
{
  "session_id": "550e8400-...",
  "step": "ssh",
  "status": "completed",
  "next_step": "preflight",
  "progress_pct": 25
}
```

**Response** `422` (validation failure):
```json
{
  "error": "ssh_auth_failed",
  "message": "SSH authentication failed: publickey rejected",
  "context": {
    "step": "ssh",
    "host": "1.2.3.4",
    "user": "root"
  }
}
```

**Error codes by step**:

| Step | Error code | HTTP | Description |
|------|-----------|------|-------------|
| ssh | `ssh_connection_refused` | 422 | TCP connect failed |
| ssh | `ssh_auth_failed` | 422 | SSH key/password rejected |
| ssh | `ssh_no_sudo` | 422 | User lacks passwordless sudo |
| ssh | `ssh_no_docker` | 422 | Docker not running (warning, not blocking) |
| domain_mode | `invalid_mode` | 422 | Mode not in {byo, nipio} |
| domain_check | `dns_lookup_failed` | 422 | Domain does not resolve |
| domain_check | `a_record_mismatch` | 422 | A-record doesn't point to VPS IP (warning) |
| cloudflare | `cf_token_invalid` | 422 | Token rejected by Cloudflare API |
| cloudflare | `cf_token_missing_scope` | 422 | Token lacks Zone:Read or DNS:Edit |
| cloudflare | `cf_zone_not_found` | 422 | Domain not found in Cloudflare account |
| create_peer | `peer_name_invalid` | 422 | Name doesn't match `^[a-zA-Z0-9_-]{1,64}$` |

**Notes**:
- Step submission is idempotent — re-submitting a completed step with same inputs is a no-op.
- Re-submitting with different inputs re-runs validation and updates state.
- Steps must be submitted in order. Submitting `domain_check` before `domain_mode` returns `409 invalid_step_order`.

---

### `POST /v1/wizard/sessions/{id}/preflight`

Run preflight checks against SSH coordinates provided in ssh step.

**Request**: Empty body (uses SSH coords from wizard state).

**Response** `200`:
```json
{
  "session_id": "550e8400-...",
  "preflight_result": {
    "target_host": "1.2.3.4",
    "checked_at": "2026-05-28T10:01:00Z",
    "distro": "ubuntu",
    "distro_version": "22.04",
    "arch": "x86_64",
    "disk_free_gb": 48.2,
    "ram_mb": 2048,
    "has_sudo": true,
    "has_docker": true,
    "docker_running": true,
    "port_443_free": true,
    "port_80_free": true,
    "port_wg_free": true,
    "compatible": true,
    "warnings": [],
    "blocking_failures": []
  },
  "progress_pct": 37
}
```

**Response** `422` (blocking failure):
```json
{
  "error": "preflight_failed",
  "message": "Unsupported OS: centos. Supported: Ubuntu 22.04/24.04, Debian 12.",
  "context": {
    "preflight_result": { "..." : "..." },
    "blocking_failures": ["Unsupported distro: centos 9"]
  }
}
```

**Notes**:
- Preflight runs asynchronously (takes 5-15s). Client should poll or use SSE for progress.
- If `compatible == false` AND `blocking_failures` is non-empty, client cannot proceed.
- If `warnings` is non-empty, client shows warnings and asks user to confirm.

---

### `POST /v1/wizard/sessions/{id}/commit`

Final commit — triggers bootstrap, creates first peer, exposes first URL.

**Request**: Empty body (uses all accumulated wizard state).

**Response** `200` (success):
```json
{
  "session_id": "550e8400-...",
  "status": "committed",
  "peer": {
    "id": "peer-uuid",
    "name": "phone",
    "public_key": "...",
    "allowed_ip": "10.8.0.2"
  },
  "qr": {
    "png_base64": "...",
    "deeplink_uri": "wireguard://import?config=...",
    "config_text": "[Interface]\n..."
  },
  "first_url": "https://app.example.com",
  "bootstrap_duration_ms": 125000,
  "total_duration_ms": 240000
}
```

**Response** `422` (bootstrap failure):
```json
{
  "error": "bootstrap_failed",
  "message": "VPS bootstrap failed: Docker compose up timed out (120s)",
  "context": {
    "rollback_performed": true,
    "vps_state": "clean"
  }
}
```

**Notes**:
- This is the irreversible step. After commit, no back-navigation.
- Long-running (2-5 min). Client must show progress via SSE log stream.
- On failure, rollback is automatic (spec 003 bootstrapper handles this). VPS returns to clean state.
- `wizard-state.json` is deleted on success.

---

### `POST /v1/peers/{peerId}/qr`

Generate QR code and deeplink for an existing peer. Extends 002's peer endpoints.

**Request**:
```json
{
  "size": 256
}
```

**Response** `200`:
```json
{
  "peer_id": "peer-uuid",
  "qr_png_base64": "iVBORw0KGgo...",
  "deeplink_uri": "wireguard://import?config=W0ludGVyZmFjZV0...",
  "config_text": "[Interface]\nPrivateKey = ...\nAddress = 10.8.0.2/32\n...",
  "generated_at": "2026-05-28T10:05:00Z"
}
```

**Response** `404`:
```json
{
  "error": "peer_not_found",
  "message": "No peer with ID peer-uuid"
}
```

**Notes**:
- `config_text` is generated from the peer's stored client config (002 already generates this on peer creation).
- QR is generated server-side (not client-side) to ensure consistent encoding.
- `size` parameter: 128, 256, or 512. Default 256.

---

### `POST /v1/peers/{peerId}/invite`

Generate invite link for an existing peer. Extends 002's peer endpoints.

**Request**:
```json
{
  "mode": "hmac_url",
  "ttl_seconds": 86400,
  "max_uses": 1
}
```

**Response** `200` (HMAC mode):
```json
{
  "id": "invite-uuid",
  "peer_id": "peer-uuid",
  "mode": "hmac_url",
  "url": "https://localhost:8080/invite/peer-uuid?t=base64url-token&e=1716883200&s=base64url-hmac",
  "expires_at": "2026-05-29T10:00:00Z",
  "max_uses": 1
}
```

**Response** `200` (short-code mode):
```json
{
  "id": "invite-uuid",
  "peer_id": "peer-uuid",
  "mode": "short_code",
  "code": "84736291",
  "entry_url": "https://localhost:8080/invite",
  "expires_at": "2026-05-29T10:00:00Z",
  "max_uses": 1
}
```

**Response** `404`:
```json
{
  "error": "peer_not_found",
  "message": "No peer with ID peer-uuid"
}
```

**Notes**:
- `ttl_seconds`: min 300 (5 min), max 259200 (72 hours), default 86400 (24 hours).
- `max_uses`: min 1, max 10, default 1.
- Invite URL contains encrypted config blob — raw WG config never in URL.
- Short code is returned in plaintext ONCE (like 002's token creation). Not retrievable again.

---

### `GET /invite/{peerId}`

Invite landing page. Validates invite token/code, serves peer config.

**Query params** (HMAC mode):
- `t` — base64url-encoded random token
- `e` — Unix timestamp expiry
- `s` — base64url-encoded HMAC signature

**Query params** (short-code mode):
- `code` — 8-digit numeric code

**Response** `200` (valid invite):
```json
{
  "peer_name": "phone",
  "qr_png_base64": "...",
  "deeplink_uri": "wireguard://import?config=...",
  "config_text": "[Interface]\n...",
  "config_download_url": "/invite/{peerId}/download?t=...&e=...&s=...",
  "os_detected": "android",
  "wg_client_download_url": "https://play.google.com/store/apps/details?id=com.wireguard.android",
  "consumed": false
}
```

**Response** `410` (consumed):
```json
{
  "error": "invite_consumed",
  "message": "This invite link has already been used."
}
```

**Response** `410` (expired):
```json
{
  "error": "invite_expired",
  "message": "This invite link has expired."
}
```

**Response** `403` (invalid signature):
```json
{
  "error": "invite_invalid",
  "message": "Invalid invite link signature."
}
```

**Response** `429` (rate limited):
```json
{
  "error": "rate_limited",
  "message": "Too many attempts. Try again later.",
  "context": {
    "retry_after_seconds": 60
  }
}
```

**Notes**:
- This endpoint serves the HTML landing page in the admin UI (not a JSON API). JSON shown above is the data contract for the page component.
- Rate limit: 5 attempts per IP per minute. After 20 failed attempts per invite, invalidate.
- OS detection: `User-Agent` header parsing. Link to correct WG client download.
- First view marks invite as consumed (for `max_uses == 1`). Client-side JS confirms config was displayed/downloaded.

---

### `GET /invite/{peerId}/download`

Download peer config as `.conf` file. Same auth as invite landing page.

**Response** `200`:
- Content-Type: `application/octet-stream`
- Content-Disposition: `attachment; filename="<peer-name>.conf"`
- Body: raw WireGuard config text

**Response** errors: same as `GET /invite/{peerId}` (410, 403, 429).

---

### `POST /v1/routes/expose`

One-click port exposure. Creates route + DNS atomically.

**Request**:
```json
{
  "local_port": 3000,
  "subdomain": "app"
}
```

**Response** `201`:
```json
{
  "route": {
    "id": "route-uuid",
    "subdomain": "app",
    "local_port": 3000,
    "fqdn": "app.example.com",
    "status": "active",
    "created_at": "2026-05-28T10:06:00Z"
  },
  "url": "https://app.example.com"
}
```

**Response** `409` (subdomain conflict):
```json
{
  "error": "route_conflict",
  "message": "Subdomain 'app' is already in use",
  "context": {
    "suggestions": ["app2", "app-3000", "svc-a3f7"]
  }
}
```

**Response** `412` (tunnel not connected):
```json
{
  "error": "tunnel_not_connected",
  "message": "Cannot create routes without an active tunnel"
}
```

**Notes**:
- `subdomain` is optional. If omitted, auto-generates `svc-<random-4>`.
- Atomic operation: route creation + DNS record (if Cloudflare mode) succeed or fail together.
- If DNS creation fails after route creation, route is rolled back.
- nip.io mode: no DNS creation needed. Subdomain appended to `<wg-ip-dashed>.nip.io`.
