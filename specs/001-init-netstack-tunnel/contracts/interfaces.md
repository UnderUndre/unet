# Interface Contracts: 001-init-netstack-tunnel (unet)

Companion to `config.schema.json`. Defines the runtime interface contracts: local SOCKS5/HTTP proxy behavior (with anti-DNS-leak semantics), the yamux reverse-forward control frame, and the gateway/Caddy counterpart that maps public requests onto yamux streams. Sufficient to drive T2/T3/T7.

---

## 1. Local proxy contract (SOCKS5 + HTTP, FR-004/FR-009)

**Listener**: TCP on `proxy.listen` (default `127.0.0.1:1080`). SOCKS5 per RFC 1928; HTTP proxy supports `CONNECT` and absolute-form HTTP requests. Auth is SOCKS5 RFC 1929 and HTTP Basic when `proxy.auth_enabled`.

### 1.1 Auth negotiation

- `auth_enabled = false`: server offers method `0x00` (no auth). Loopback is the trust boundary. If `proxy.shared_host = true` and auth is disabled, the server MUST refuse to start (FR-004 caveat).
- `auth_enabled = true`: server offers `0x02` (username/password); rejects `0x00`.

### 1.2 CONNECT + address types (anti-leak)

- The server MUST accept `ATYP = 0x03` (DOMAINNAME) and resolve it **in-tunnel / remote-side** — NEVER via the host resolver (FR-009.001).
  - Preferred: forward the domain to the remote exit for resolution.
  - Else: resolve via a `dns` resolver **through** the netstack tunnel.
- `ATYP = 0x01`/`0x04` (IPv4/IPv6 literal): dialed directly through the tunnel.
- The outbound dial uses `reconnect.dial_timeout_ms` (FR-007.005).

### 1.3 Reply codes (fail-clean semantics, FR-007.005 / FR-009.002)

| Condition | REP |
| --------- | --- |
| Success | `0x00 succeeded` |
| In-tunnel resolution unavailable (no remote-side + no `dns`) → **fail closed** | `0x04 Host unreachable` |
| Destination unreachable through healthy tunnel | `0x03 Network unreachable` / `0x04 Host unreachable` |
| Dial timeout | `0x06 TTL expired` (used as timeout) or `0x01 general failure` |
| Tunnel down (reconnecting) | `0x01 general failure` (listener stays bound, FR-007) |

**MUST NOT**: fall back to `net.Resolver`/host resolver for any proxied name (SC-006).

### 1.4 HTTP proxy behavior

- `CONNECT host:port HTTP/1.1`: parse `host:port`, resolve `host` according to `resolution_mode` (`remote` or `tunnel_dns`), then tunnel bytes through netstack. No host-resolver fallback.
- Absolute-form requests (`GET http://host/path HTTP/1.1`): extract the URL host and route through the tunnel. If the request uses an origin-form path, use the `Host` header as the target host and apply the same in-tunnel resolution.
- Auth disabled: HTTP proxy accepts unauthenticated requests only when `proxy.shared_host = false`. Auth enabled: require HTTP Basic credentials matching `proxy.username` + password/env reference.
- Fail-closed mapping: no in-tunnel/remote resolver ⇒ `502 Bad Gateway`; dial timeout ⇒ `504 Gateway Timeout`; tunnel down/reconnecting ⇒ `503 Service Unavailable`; unauthorized ⇒ `407 Proxy Authentication Required`.
- T7 MUST test both SOCKS5 `ATYP=0x03` and HTTP `CONNECT` / absolute-form requests for zero host DNS leaks.

---

## 2. Yamux reverse-forward control frame contract (FR-005, FR-006)

**Transport**: one long-lived TCP session from `unet` to `gateway.address`, carried **inside** the AmneziaWG tunnel; multiplexed with `hashicorp/yamux`. yamux keepalive MUST be enabled (liveness, FR-007.005 / two-level reconnect FR-007.001b).

### 2.1 Session handshake (first control stream)

On session open, the first yamux stream is a control stream carrying a single JSON control frame:

```json
{
  "type": "hello",
  "public_key": "<client WG public key, base64>",
  "capability_token": "<optional per-tunnel token, FR-006.002>",
  "proto_version": 1
}
```

- `public_key` is informational (identity is already established by the WG tunnel, FR-006).
- If `gateway.capability_token` is configured, the gateway/authorizer MUST validate it before honoring any allow-list entry (FR-006.002). Invalid/absent-when-required token ⇒ session refused.

### 2.2 Stream-open authorization (default-deny, FR-006.001)

Each subsequent inbound open-stream request carries a target descriptor:

```json
{ "type": "open", "public_port": 443, "stream_id": "<id>" }
```

- The server resolves `public_port` against `gateway.allow_list`. If **no** mapping exists ⇒ **refuse**: reset the stream and log (default-deny; empty/absent list = nothing permitted).
- On match, `unet` dials the mapped `local_host:local_port` (no wildcards) and bridges bytes.
- Any request naming an arbitrary host/port not in the allow-list MUST be rejected — this closes the "any handshaking peer reaches any local port" hole.

### 2.3 Failure / liveness

- Missed yamux keepalive ⇒ session declared dead ⇒ yamux-level reconnect over the live WG tunnel WITHOUT tearing WG down (FR-007.001b).
- Terminal failure (give-up) surfaces the FR-007.004 alert.

### 2.4 Gateway / Caddy counterpart contract (FR-006.003)

The public gateway MAY live outside `repos/unet`, but v1 depends on this contract:

```json
{
  "public_host": "app.example.com",
  "public_port": 443,
  "client_public_key": "<client WG public key>",
  "require_capability_token": true
}
```

- Caddy terminates public HTTP/TLS and routes the request to the gateway adapter with `public_host` + `public_port` metadata.
- The gateway adapter looks up an active `YamuxSession` by `client_public_key`. If no session is active, it returns `502 Bad Gateway` without retrying arbitrary clients.
- The gateway validates public-port ownership and, when configured, the per-tunnel `capability_token` before sending any `open` frame. Unauthorized route/token mismatch returns `403 Forbidden`.
- On success, the gateway opens a yamux stream to the client and sends `{ "type": "open", "public_port": <port>, "stream_id": "<id>" }`.
- The client still enforces its local `gateway.allow_list`; gateway authorization is a first guard, client allow-list is the final default-deny guard.
- T7 MUST include an integration fixture that proves: public HTTP request -> gateway route lookup -> yamux `open` -> local `127.0.0.1:<port>` response, plus negative cases for no session, token mismatch, and unlisted port.

---

## 3. Control-plane / process interface (summary)

- Config path: `--config` flag or `UNET_CONFIG` env; private key via `UNET_PRIVATE_KEY` (FR-008).
- Permission failure on load with plaintext key ⇒ non-zero exit + actionable message unless `--insecure-permissions` (FR-008.003).
- Terminal reconnect failure ⇒ structured log event `unet.tunnel.terminal_failure` + non-zero health signal (FR-007.004).

*End of Interface Contracts.*
