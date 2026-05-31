# Invite Protocol

**Spec**: `specs/006-peer-onboarding/spec.md`
**Created**: 2026-05-28

---

## Overview

Invite links allow a user to share a peer's WireGuard configuration with a recipient who does not have access to the admin UI. Two modes: HMAC-signed URL (default) and short-code (advanced).

Both modes share:
- Encrypted config blob (AES-256-GCM, daemon-secret key) — raw WG config never in URL or code.
- Configurable TTL (5 min to 72 hours, default 24 hours).
- One-time consumption by default (`max_uses: 1`).
- Rate-limited validation to prevent brute-force.

---

## Mode 1: HMAC-Signed URL

### URL Format

```
https://<daemon-host>/invite/<peer-id>?t=<base64url(token)>&e=<unix-expiry>&s=<base64url(hmac)>
```

**Components**:
- `<daemon-host>`: localhost:8080 (or VPS host for remote daemon). Recipient must be able to reach this host.
- `<peer-id>`: UUID of the peer. Not secret — identifies which peer config to decrypt.
- `t`: 32-byte random token, base64url-encoded. Generated via `crypto/rand`.
- `e`: Unix timestamp (seconds) of expiry. Same as `expires_at` in invite record.
- `s`: HMAC-SHA256 signature over `token || peer_id || expiry_unix`, base64url-encoded. Key = daemon HMAC secret (derived from daemon secret).

### Generation

```
1. Generate token = random(32 bytes)
2. Compute expires_at = now() + ttl_seconds
3. Compute message = token + peer_id_string + expires_at_unix_string
4. Compute signature = HMAC-SHA256(daemon_hmac_key, message)
5. Encode: t = base64url(token), e = expires_at_unix, s = base64url(signature)
6. Construct URL
7. Store invite record in invites.jsonl:
   { token_hash: sha256(token), peer_id, encrypted_config_blob, expires_at, consumed_at: null, ... }
```

### Validation (on recipient access)

```
1. Parse URL: extract t, e, s
2. Decode: token = base64url_decode(t), expiry = int(e), signature = base64url_decode(s)
3. Check expiry: if now() > expiry → 410 invite_expired
4. Recompute message = token + peer_id + expiry_string
5. Recompute expected_sig = HMAC-SHA256(daemon_hmac_key, message)
6. Constant-time compare: expected_sig == signature
   - If mismatch → 403 invite_invalid
7. Look up invite by token_hash = sha256(token)
8. Check consumed_at: if not null AND use_count >= max_uses → 410 invite_consumed
9. Decrypt config blob using daemon secret key
10. Mark consumed: set consumed_at = now(), consumed_by_ip = request_ip, use_count++
11. Return config to recipient
```

### Security properties

- **Token entropy**: 256 bits (32 bytes from `crypto/rand`). Not guessable.
- **HMAC key**: derived from daemon secret (same key used for config encryption). Not exposed to client.
- **Constant-time comparison**: prevents timing side-channel on signature check.
- **One-time**: consumed_at set on first valid access. Subsequent access returns 410.
- **Time-bounded**: expiry checked before signature validation. Expired invites rejected early.
- **No config in URL**: URL contains token + HMAC. Config is stored encrypted server-side.

---

## Mode 2: Short Code

### Code Format

8-digit numeric string. Range: 10000000–99999999 (90M possible codes).

### Generation

```
1. Generate code_int = random_int(10000000, 99999999)  // crypto/rand
2. code_string = str(code_int)
3. Store invite record in invites.jsonl:
   { code_hash: sha256(code_string), peer_id, encrypted_config_blob, expires_at, consumed_at: null, ... }
4. Return code_string to user (shown ONCE, like API token)
```

### Validation (on recipient entry)

```
1. Recipient enters code at https://<host>/invite?code=<code>
2. Compute code_hash = sha256(code)
3. Look up invite by code_hash
4. Check expiry: if now() > expires_at → 410 invite_expired
5. Check consumed_at: if not null AND use_count >= max_uses → 410 invite_consumed
6. Rate limit check: count attempts for this IP in last 60s
   - If >= 5 → 429 rate_limited (retry_after: 60s)
   - If code_hash has >= 20 failed attempts total → invalidate code → 410 invite_consumed
7. Decrypt config blob
8. Mark consumed
9. Return config to recipient
```

### Security properties

- **Code entropy**: ~26.5 bits (90M space). Much lower than HMAC token. Intended for out-of-band sharing (voice, secure messaging) where the code is ephemeral.
- **Rate limiting**: 5 attempts/IP/min. 20 total failed attempts → code invalidated. Brute-force of 90M space at 5/min would take ~34 years per IP, and code expires in 24h default.
- **No code in URL**: Recipient enters code manually on landing page. Code is not in URL (reduces log/referrer leak).
- **Short-lived**: Default TTL 24h recommended for short codes. Encourage users to use shorter TTL.

---

## Invite Store Format

File: `~/.unet/invites.jsonl` (append-only JSONL)

Each line:
```json
{
  "id": "invite-uuid",
  "peer_id": "peer-uuid",
  "mode": "hmac_url",
  "token_hash": "sha256-hex",
  "encrypted_config_blob": "base64(aes-gcm-ciphertext)",
  "expires_at": "2026-05-29T10:00:00Z",
  "consumed_at": null,
  "consumed_by_ip": null,
  "max_uses": 1,
  "use_count": 0,
  "created_at": "2026-05-28T10:00:00Z",
  "created_by_user": "admin",
  "ttl_seconds": 86400
}
```

**GC**: On daemon start + hourly timer:
1. Scan all lines.
2. Delete lines where `expires_at < now() - 1h` AND (`consumed_at != null` OR `expires_at < now() - 7d`).
3. Rewrite file without deleted lines (temp + rename).

**Index**: In-memory `map[string]int` — `token_hash` (or `code_hash`) → byte offset in JSONL file. Rebuilt on daemon start.

---

## Config Blob Encryption

```go
key = sha256(daemon_secret)[:32]   // AES-256 key from daemon secret
nonce = random(12 bytes)           // AES-GCM nonce
ciphertext = AES-256-GCM-Seal(key, nonce, plaintext=wg_config, aad=nil)
blob = base64(nonce + ciphertext)  // Store as base64 string
```

- Daemon secret: same secret used for HMAC signing. Stored in `~/.unet/config.json` as `daemon_secret` (generated on first daemon start if not present).
- Nonce: 12 bytes, unique per invite (crypto/rand). Prepended to ciphertext.
- AAD: not used (config is self-describing, no additional context needed).
- Plaintext: full WireGuard client config text (including AmneziaWG params).

---

## Rate Limiting

Applied to `GET /invite/{peerId}` and `GET /invite?code=...` endpoints only.

**Per-IP limits**:
- 5 attempts per IP per 60 seconds.
- Tracked in-memory: `map[string][]time.Time` (sliding window).
- Cleared on daemon restart (acceptable — rate limit is transient protection).
- Response: `429 Too Many Requests` with `Retry-After: 60` header.

**Per-invite limits**:
- 20 total failed attempts → invite invalidated (consumed_at set to now()).
- Prevents distributed brute-force across multiple IPs.
- Failed attempt = any validation that fails signature/code check (not expiry or already-consumed).

---

## Consumption Semantics

- **Default**: one-time use (`max_uses: 1`). First successful validation marks consumed.
- **Multi-use**: `max_uses` up to 10. Each valid access increments `use_count`. After `use_count >= max_uses`, further attempts return 410.
- **Consumed check**: happens AFTER signature/code validation (to avoid leaking timing info about consumed vs non-consumed invites).
- **IP logging**: `consumed_by_ip` set on first consumption. For multi-use, only first IP is logged. This is for abuse investigation, not access control.
