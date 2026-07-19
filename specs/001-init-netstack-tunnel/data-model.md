# Data Model: 001-init-netstack-tunnel (unet)

**Branch**: `001-init-netstack-tunnel` | **Date**: 2026-07-18 | **Spec**: `spec.md` (same folder)

Config schema and runtime entities for the embedded AmneziaWG netstack core. The on-disk schema is authoritative in `contracts/config.schema.json`; this document describes fields, relations, and runtime objects.

---

## 1. Persistent config entities (local JSON, FR-008)

### 1.1 `TunnelConfig` (root)

| Field | Type | Req | Notes |
| ----- | ---- | --- | ----- |
| `private_key` | string (base64) | cond. | Client WG private key. Auto-generated on first run if absent (FR-008). MAY be omitted from file and supplied via env (`UNET_PRIVATE_KEY`); if present in file, triggers perm enforcement (FR-008.003). **Plaintext-at-rest risk (FR-008.001).** |
| `public_key` | string (base64) | yes | Derived client public key; the sole tunnel identity to the gateway (FR-006). |
| `peer` | `Peer` | yes | Remote AmneziaWG peer / exit. |
| `obfuscation` | `ObfuscationParams` | yes | AmneziaWG DPI-evasion params (FR-003). |
| `resolution_mode` | `remote` \| `tunnel_dns` | yes | Explicit anti-leak DNS mode (FR-009): remote-side resolution or tunnel-side DNS. |
| `dns` | string[] (IP) | cond. | Required when `resolution_mode = tunnel_dns`; tunnel-side resolver(s) reachable *inside* the tunnel (FR-009). |
| `mtu` | int | no | Tunnel MTU; default computed from obfuscation overhead (FR-010). |
| `proxy` | `ProxyConfig` | no | Local SOCKS5/HTTP listener settings (FR-004). |
| `gateway` | `GatewayConfig` | no | Reverse-forward gateway + port allow-list (FR-006). |
| `reconnect` | `ReconnectConfig` | no | Backoff + terminal-failure knobs (FR-007). |

If `resolution_mode = remote`, `dns` MAY be empty. If remote-side resolution is unavailable at runtime, name-based proxied requests fail closed; the loader MUST NOT silently switch to host DNS.

### 1.2 `Peer` (a.k.a. Gateway exit)

| Field | Type | Req | Notes |
| ----- | ---- | --- | ----- |
| `public_key` | string (base64) | yes | Remote peer public key. |
| `endpoint` | string (`host:port`) | yes | Exit endpoint; `host` MAY be a DNS name, re-resolved on roaming (FR-007.002). |
| `allowed_ips` | string[] (CIDR) | yes | Routed prefixes. |
| `preshared_key` | string (base64) | no | Optional WG PSK. |
| `persistent_keepalive` | int (sec) | no | WG keepalive; underpins liveness (FR-007.005). |

### 1.3 `ObfuscationParams` (FR-003)

`jc`, `jmin`, `jmax` (junk packets), `s1`, `s2` (padding), `h1`,`h2`,`h3`,`h4` (header magic) — all ints. Interact with MTU budget (FR-010, research R5) and SC-003 DPI pass.

### 1.4 `ProxyConfig` (FR-004)

| Field | Type | Default | Notes |
| ----- | ---- | ------- | ----- |
| `listen` | string | `127.0.0.1:1080` | Loopback bind = trust boundary. |
| `auth_enabled` | bool | `false` | Off by default; MUST be on for shared hosts (FR-004 caveat). |
| `username` / `password` | string | — | SOCKS5 user/pass + HTTP Basic when `auth_enabled`. `password` is secret-bearing; file permissions apply if present. |
| `password_env` | string | — | Env var name for proxy password, preferred over plaintext config on shared hosts. |
| `shared_host` | bool | `false` | If true, refuse to start unauthenticated (FR-004 multi-user caveat). |

### 1.5 `GatewayConfig` (FR-006)

| Field | Type | Notes |
| ----- | ---- | ----- |
| `address` | string (`host:port`) | Tunnel-internal gateway TCP address for the single long-lived yamux session. |
| `allow_list` | `PortMapping[]` | Default-deny reverse-forward authorization (FR-006.001). Empty/absent ⇒ no reverse-forward permitted. |
| `capability_token` | string | Optional per-tunnel token carried in first yamux control frame (FR-006.002). Secret-bearing; file permissions apply if present. |
| `capability_token_env` | string | Env var name for the capability token, preferred over plaintext config. |

**`PortMapping`**: `{ public_port: int, local_host: string, local_port: int }`. No wildcards for `local_host`/`local_port` (FR-006.001).

### 1.6 `ReconnectConfig` (FR-007)

`initial_backoff`, `max_backoff`, `jitter`, `max_total_duration` and/or `max_attempts` (terminal-failure give-up, FR-007.004), `retry_forever` (explicit opt-in only; default false), `keepalive_interval`, `keepalive_timeout` (liveness/half-open, FR-007.005), `dial_timeout` (SOCKS5/HTTP connect timeout). At least one finite terminal condition is required unless `retry_forever = true`.

---

## 2. Runtime entities

### 2.1 `TunnelEngine`

Owns the AmneziaWG `device.Device` + `tun/netstack` `Net`. Lifecycle: `init → handshake → up → (drop) → reconnect (WG-level, FR-007.001a) → up`. Emits `terminal-failure` alert on give-up (FR-007.004).

### 2.2 `Socks5Listener` / `HttpProxy` (FR-004, FR-009)

Bound per `ProxyConfig`. For each accepted client conn creates a `ProxyConn`. Name-based targets (`ATYP=0x03`) resolved **in-tunnel / remote-side**, never host resolver; fail closed on no resolver (FR-009). Applies `dial_timeout` and returns proper SOCKS5 failure replies (FR-007.005).

### 2.3 `YamuxSession` / `YamuxStream` (FR-005, FR-006)

Single `YamuxSession` over one long-lived TCP conn inside the tunnel. Each inbound open-stream request validated against `allow_list` (default-deny) and optional `capability_token` before a `YamuxStream` to `local_host:local_port` is created. yamux keepalive enabled; dead session ⇒ yamux-level reconnect (FR-007.001b) without tearing WG down. Per-stream memory bounded by yamux window (research R4, SC-005).

### 2.3.1 `GatewayRoute` / `GatewaySessionRegistry` (FR-006.003)

Gateway-side integration contract: public Caddy route (`public_host`/`public_port`) maps to one client `public_key` and one allowed `public_port`. The gateway maintains an active-session registry keyed by client public key, validates optional capability token/route ownership, and sends a yamux `open` frame to the matching session. If no active session exists or authorization fails, the gateway returns a clear 502/403-style failure instead of opening arbitrary local targets.

### 2.4 `ConfigLoader` (FR-008)

Reads/writes `TunnelConfig`. On write: `0600`/owner-only ACL (FR-008.002). On read: enforces perms when any secret-bearing field is present — `private_key`, `preshared_key`, proxy `password`, `capability_token`, or future token fields — refuse-by-default, `--insecure-permissions` downgrades to warning (FR-008.003). Env-reference fields (`password_env`, `capability_token_env`, `UNET_PRIVATE_KEY`) avoid plaintext-at-rest. Re-applies checks on reload (FR-007.003).

---

## 3. Relations (summary)

```text
TunnelConfig 1─1 Peer
TunnelConfig 1─1 ObfuscationParams
TunnelConfig 1─0..1 ProxyConfig ──> Socks5Listener/HttpProxy ──* ProxyConn
TunnelConfig 1─0..1 GatewayConfig ──1 YamuxSession ──* YamuxStream
GatewayConfig 1─* PortMapping   (allow-list, default-deny)
TunnelEngine (netstack) carries: ProxyConn dials + YamuxSession
```

---

## 4. State & lifecycle notes

- **Two-level liveness (FR-007.001)**: WG-level (datagram/keepalive) and yamux-level (session keepalive) are independent; each has its own reconnect path.
- **Runtime mutation (FR-007.003)**: keys/allow-list changes require explicit reload/restart in v1; no mid-handshake hot-swap.
- **DNS (FR-009)**: only the WG endpoint hostname may resolve out-of-tunnel (control-plane), logged; all user/proxied resolution follows explicit `resolution_mode` and is in-tunnel/remote-side or fails closed.

*End of Data Model.*
