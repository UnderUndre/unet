# Interface Contracts: 001-init-netstack-tunnel (unet)

Companion to `config.schema.json`. Defines the runtime interface contracts: local SOCKS5/HTTP proxy behavior (with anti-DNS-leak semantics), the yamux reverse-forward wire framing and control frames, and the gateway-side operational contract that maps public requests onto yamux streams. Sufficient to drive T2/T3/T3a/T7.

---

## 1. Local proxy contract (SOCKS5 + HTTP, FR-004/FR-009)

**Listener**: TCP on `proxy.listen` (default `127.0.0.1:1080`; `unix:///path` / `@abstract` supported on platforms that allow it — loader-validated, F15). SOCKS5 per RFC 1928; HTTP proxy supports `CONNECT` and absolute-form HTTP requests. Auth is SOCKS5 RFC 1929 and HTTP Basic when `proxy.auth_enabled`.

### 1.0 Protocol demultiplexing (F6)

The single listener multiplexes both protocols by **first-byte peek**:

1. First byte `0x05` → treat the connection as SOCKS5 (version byte).
2. Otherwise → attempt to parse an HTTP request line (`METHOD uri HTTP/1.x`).
3. Malformed preamble (neither SOCKS5 nor a parseable HTTP request line) → close the connection with a protocol-level error; MUST NOT crash or block the listener. Peeked bytes are replayed to the selected protocol parser (buffered, not consumed).

### 1.1 Auth negotiation

- `auth_enabled = false`: server offers method `0x00` (no auth). Loopback is the trust boundary. If `proxy.shared_host = true` and auth is disabled, the server MUST refuse to start (FR-004 caveat).
- `auth_enabled = true`: server offers `0x02` (username/password); rejects `0x00`. The schema + loader REQUIRE non-empty `proxy.username` and (`proxy.password` | `proxy.password_env`) when `auth_enabled = true` (F6) — empty credentials with auth on are a config load error.
- HTTP Basic credentials, when auth is enabled, MUST match `proxy.username` + the resolved password (env reference resolved in-memory; the env value itself is never logged — FR-008.004).

### 1.2 CONNECT + address types (anti-leak)

- The server MUST accept `ATYP = 0x03` (DOMAINNAME) and resolve it **via the tunnel-side `dns[]` resolvers only** (gVisor `LookupContextHost` against `dnsServers`) — NEVER via the host resolver (FR-009.001). v1 has **no `remote` mode**: there is no exit-side hostname-forwarding protocol over the raw L3 tunnel (F2); the exit resolver runs on the gateway tailnet IP (e.g. dnsmasq on `10.66.0.1`, `ops/option0-baseline`).
- `ATYP = 0x01`/`0x04` (IPv4/IPv6 literal): dialed directly through the tunnel.
- With no reachable tunnel-side resolver, name-based requests **fail closed** (FR-009.002).
- The outbound dial uses `reconnect.dial_timeout_ms` (FR-007.005).

### 1.3 Reply codes (fail-clean semantics, FR-007.005 / FR-009.002)

| Condition | REP |
| --------- | --- |
| Success | `0x00 succeeded` |
| No tunnel-side resolver reachable → **fail closed** | `0x04 Host unreachable` |
| Destination unreachable through healthy tunnel | `0x03 Network unreachable` / `0x04 Host unreachable` |
| Dial timeout | `0x06 TTL expired` (used as timeout) or `0x01 general failure` |
| Tunnel down (reconnecting) | `0x01 general failure` (listener stays bound, FR-007) |

**MUST NOT**: fall back to `net.Resolver`/host resolver for any proxied name (SC-006).

### 1.4 HTTP proxy behavior (F6)

- `CONNECT host:port HTTP/1.1`: parse `host:port`, resolve `host` via tunnel-side `dns[]`, tunnel raw bytes bidirectionally. No host-resolver fallback.
- Absolute-form requests (`GET http://host/path HTTP/1.1`): extract the URL host and route through the tunnel. Origin-form paths: use the `Host` header as target host, same in-tunnel resolution.
- **Hop-by-hop header stripping (mandatory)**: before forwarding any proxied HTTP request/response, strip `Proxy-Authorization`, `Proxy-Connection`, `Connection` (and any headers it names), `TE`, `Trailer`, `Upgrade`, `Keep-Alive`, `Proxy-Authenticate` (response direction). This prevents leaking local proxy credentials to upstream origins (F6) and honors RFC 7230 §6.1.
- **Keep-alive**: a persistent client connection carries independent requests; each request is re-parsed, and a changed target host between requests MUST be honored (no sticky target). Responses are relayed per-request; the server MUST NOT assume one request per connection.
- Auth disabled: HTTP proxy accepts unauthenticated requests only when `proxy.shared_host = false`. Auth enabled: require HTTP Basic credentials; failures ⇒ `407 Proxy Authentication Required`.
- Fail-closed mapping: no tunnel-side resolver ⇒ `502 Bad Gateway`; dial timeout ⇒ `504 Gateway Timeout`; tunnel down/reconnecting ⇒ `503 Service Unavailable`.
- T7 MUST test both SOCKS5 `ATYP=0x03` and HTTP `CONNECT` / absolute-form requests for zero host DNS leaks, plus an assertion that `Proxy-Authorization` never reaches the upstream (F6).

---

## 2. Yamux reverse-forward wire contract (FR-005, FR-006)

**Transport**: one long-lived TCP session from `unet` to `gateway.address` (gateway yamux listener on the **tailnet address** — §4.2), carried **inside** the AmneziaWG tunnel; multiplexed with `hashicorp/yamux`. yamux keepalive MUST be enabled (liveness, FR-007.005 / two-level reconnect FR-007.001b).

### 2.0 Wire framing (F3-1 — normative)

`hashicorp/yamux` `OpenStream()` carries **no payload**; every JSON frame below is therefore carried as a **length-prefixed message on its stream**:

```text
[uint16 big-endian length N][N bytes UTF-8 JSON]
```

- The session's **first stream** is the control stream and carries exactly one framed `hello` frame, then stays open for gateway-originated control replies (`error`, `ok`).
- Every **data stream** (either direction) begins with exactly one framed `open` descriptor; after those `N` bytes the stream becomes a **raw byte pipe** with no further framing.
- Readers MUST consume exactly the framed descriptor bytes (read the 2-byte length, read `N`, stop) and relay the remainder verbatim. A streaming `json.Decoder` MUST NOT be applied to the data stream after the descriptor — its read-ahead would consume proxied payload bytes (F3-1).
- Malformed/oversized frames (N > 4096 for control frames) ⇒ stream reset + fault-logged.

### 2.1 Session handshake (first control stream)

On session open, the first yamux stream is the control stream carrying one framed `hello` frame:

```json
{
  "type": "hello",
  "public_key": "<client WG public key, base64>",
  "capability_token": "<optional per-tunnel token, FR-006.002>",
  "proto_version": 1
}
```

- `public_key` is informational (identity is already established by the WG tunnel, FR-006).
- If the gateway requires a capability token for this peer, it MUST validate it before honoring any reverse-forward request (FR-006.002). Invalid/absent-when-required token ⇒ session refused.
- **Version semantics (F3/F4)**: a `proto_version` the gateway does not support ⇒ session refused with a framed `{ "type": "error", "code": "proto_version", "supported": [1] }`. Compatibility rule: same MAJOR required; minor bumps are forward-compatible (additive fields only, mirroring the SDK contract stability policy).
- The `hello` frame deliberately carries **no allow-list** — the gateway's routing authority is its own configured table (§4.3); the client-side `gateway.allow_list` remains the final default-deny guard (F3-3).

### 2.2 Stream-open authorization (default-deny, FR-006.001)

- **Gateway → client** (public request): the gateway opens a yamux data stream and writes one framed `open` descriptor, then relays public bytes:

```json
{ "type": "open", "public_host": "app.example.com", "public_port": 443, "proto": "tcp", "stream_id": "<id>" }
```

- **Client validation**: the client resolves the descriptor against its local `gateway.allow_list`. If **no** mapping exists ⇒ **refuse**: reset the stream and log (default-deny; empty/absent list = nothing permitted).
- On match, `unet` dials the mapped `local_host:local_port` (no wildcards) and bridges bytes.
- Any request naming an arbitrary host/port not in the allow-list MUST be rejected — this closes the "any handshaking peer reaches any local port" hole.

### 2.3 Failure / liveness

- Missed yamux keepalive ⇒ session declared dead ⇒ yamux-level reconnect over the live WG tunnel WITHOUT tearing WG down (FR-007.001b).
- Terminal failure (give-up) surfaces the FR-007.004 alert (§5 exit codes / health).

### 2.4 Gateway routing authority (FR-006.003)

The gateway holds the authoritative route table (F3-3); client hello/allow-list data is advisory:

```json
{
  "public_host": "app.example.com",
  "public_port": 443,
  "proto": "tcp",
  "client_public_key": "<client WG public key>",
  "require_capability_token": true
}
```

- The table is provisioned by the gateway operator (or, later, undrlla per 003 — FR-E-001), never learned from client frames.
- Caddy terminates public HTTP/TLS and forwards to the gateway ingress listener with `public_host` + `public_port` metadata (§4.3).

---

## 3. Control-plane / process interface (summary)

- Config path: `--config` flag or `UNET_CONFIG` env; private key via `UNET_PRIVATE_KEY` (FR-008).
- Permission failure on load with plaintext key ⇒ non-zero exit + actionable message unless `--insecure-permissions` (FR-008.003); exit code 2 (§5).
- Terminal reconnect failure ⇒ structured log event `unet.tunnel.terminal_failure` + non-zero health signal (FR-007.004); exit code 3 in foreground mode (§5).

## 4. Gateway-side operational contract (FR-006.003, FR-006.004; implemented by task T3a)

The reference gateway is the server-side counterpart of §2. **Three separated listeners** (F3-2):

### 4.1 Listener topology

| Listener | Bind | Purpose |
|---|---|---|
| **yamux** | gateway **tailnet address**, e.g. `10.66.0.1:7443` | where `unet` dials `gateway.address`; reachable ONLY inside the AWG tunnel |
| **ingress** | `127.0.0.1:9443` | receives requests forwarded by Caddy (TLS terminates at Caddy); maps public requests to yamux opens |
| **admin** | `127.0.0.1:9444` | `POST /v1/gateway/revoke` + gateway health; MUST NOT be publicly reachable |

The v0.1 draft's "gateway binds loopback only" was **wrong** (F3-2): a loopback-bound yamux listener is unreachable from inside the tunnel. The yamux listener MUST bind the tailnet address.

### 4.2 Active-session registry + resource bounds

- Keyed by client WG public key (identity already established by the AWG tunnel, FR-006 — no separate auth at this layer).
- Entry carries: yamux session handle, last-seen timestamp, capability-token validity (FR-006.002).
- Entry expires when the yamux session dies (keepalive liveness per FR-007.005) or after the configured TTL; reverse routes MUST be dropped immediately on expiry (grok-001-F3: no stale routes).
- **Per-peer resource bounds (SC-005 class, gateway side — zcode-F3)**: max concurrent open streams per peer (default 256), max stream-open rate per peer (default 50/s, token bucket), idle-stream reaping via read/write deadlines. Exceeding bounds ⇒ stream reset; the session itself is not torn down for rate events.

### 4.3 Public request → yamux open mapping

1. Caddy forwards the public request to the ingress listener with `(public_host, public_port, proto)` metadata.
2. Gateway looks up its **authoritative route table** (§2.4) for that tuple → `client_public_key`. The table is gateway-configured; the client hello carries no routes (F3-3).
3. Exactly one matching entry with an **active session** ⇒ gateway opens a yamux data stream to that client carrying the framed `open` descriptor per §2.0/§2.2.
4. No table entry or no active session ⇒ `503 Service Unavailable` (retryable, never queued indefinitely). Entry exists but token required-and-invalid ⇒ `403`. Ambiguous table (same tuple mapped to two peers) ⇒ gateway **config load error**, refused at startup.
5. The client-side `gateway.allow_list` remains the final default-deny guard (defense-in-depth).

### 4.4 Revocation endpoint (FR-006.004)

- `POST /v1/gateway/revoke` (admin listener) — body: `{ "pubkey": "<base64 WG public key>", "issued_at": <unix ts, seconds>, "sig": "<hex hmac-sha256>" }`.
- **Canonical HMAC input (F5)**: `sig = hex(HMAC-SHA256(shared_secret, pubkey_utf8 || 0x0A || ascii_decimal(issued_at)))`.
- **Replay window (F5)**: requests with `|now - issued_at| > 300s` ⇒ `401`, no state change, audit-logged (without printing `sig` or the secret — FR-008.004).
- On valid request: remove the peer from the AWG device (embedded `Device` UAPI `remove=true`, or `awg set` against the local interface), tear down all yamux streams for that public key, mark revoked in the registry, reject future WG handshakes for that key.
- **Persistence (F5)**: the revoked-key set MUST be durably persisted (default `/var/lib/unet-gateway/revoked.json`, atomic write + fsync) and reloaded on gateway startup — a restart MUST NOT un-revoke a peer.
- On invalid signature: `401`, no state change, attempt audit-logged (key, timestamp — no signature material).

## 5. Process control & health contract (F8 — cross-platform reload + health)

Windows has no SIGHUP and "non-zero health signal" needs a portable surface:

- **IPC transport**: platform-local socket — Unix domain socket `/var/run/unet/unet.sock` (POSIX) or named pipe `\\.\pipe\unet` (Windows). JSON request/response.
- **Verbs**:
  - `status` → `{ "tunnel": "up|down|reconnecting|terminal_failure", "yamux": "up|down", "uptime_s": <int>, "last_handshake": <unix ts|null>, "config_version": <int>, "retry_forever": <bool> }`
  - `reload` → re-read the config file (FR-007.003), re-apply permission checks (FR-008.003), swap non-key settings; key-material hot-swap remains prohibited (FR-007.003) — response `{ "ok": true }` or `{ "ok": false, "error": "<actionable>" }`.
- **CLI**: `unet status` / `unet reload` speak the same IPC (works even when the daemon is not a child of the caller's shell).
- **POSIX compatibility**: `SIGHUP` == `reload`.
- **Exit codes** (foreground / systemd-notify surface for FR-007.004):
  - `0` — clean run/stop
  - `2` — config or permission failure at startup (FR-008.003)
  - `3` — terminal tunnel failure after give-up (FR-007.004)
- T7 asserts `status` reflects each tunnel state transition and exit code 3 fires on simulated terminal failure.

*End of Interface Contracts.*
