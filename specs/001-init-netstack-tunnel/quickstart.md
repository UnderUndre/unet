# Quickstart: 001-init-netstack-tunnel (unet)

Short developer quickstart: generate a config, lock down permissions, run the tunnel, and verify SOCKS5 works with **no DNS leak**. See `spec.md` for requirements and `contracts/` for the schema.

---

## 1. Build

```bash
cd repos/unet
go build -o unet ./cmd/unet   # single static binary, no drivers, no cgo (SC-001)
```

## 2. Generate config (first run)

`unet` auto-generates the client keypair and writes a JSON config on first run when absent (FR-008):

```bash
./unet --config ./config.json --init
```

This writes `config.json` with `0600` / owner-only permissions (FR-008.002).

> **SECURITY — plaintext key at rest (FR-008.001):** by default `private_key` is stored *in plaintext* in `config.json`. Anyone who can read the file owns your tunnel identity. On shared or untrusted hosts, remove `private_key` from the file and supply it via env instead:
>
> ```bash
> export UNET_PRIVATE_KEY="<base64 private key>"
> ```

Fill in the remote `peer` (public key, `endpoint`, `allowed_ips`), `obfuscation` params, and DNS mode per `contracts/config.schema.json`:

- `local_addresses` (required): THIS client's tunnel-side IP(s), e.g. `["10.66.0.2/32"]` — must be covered by the server-side `[Peer] AllowedIPs` for this client (zcode F1: `CreateNetTUN(localAddresses, …)` needs them, otherwise the netstack NIC has no routes).
- `resolution_mode` is `"tunnel_dns"` only in v1: names resolve via the tunnel-side `dns` resolver IPs (e.g. the gateway's tailnet dnsmasq at `10.66.0.1`). There is no `remote` mode — see FR-009 / zcode F2.

Set a finite reconnect terminal condition (`max_total_duration_ms` or `max_attempts`) unless you explicitly choose `retry_forever: true` and accept degraded health while retrying (the schema enforces the mutual exclusion — zcode F7).

## 3. Verify / fix permissions

`unet` refuses to start if any secret-bearing field is present in a world/group-readable file: `private_key`, `preshared_key`, proxy `password`, or `gateway.capability_token` (FR-008.003):

```bash
# POSIX
chmod 600 config.json
# Windows (PowerShell): restrict ACL to current user
icacls .\config.json /inheritance:r /grant:r "$env:USERNAME:(R,W)"
```

To override on a constrained host (loud warning instead of refuse): `--insecure-permissions`.

Prefer env references for additional secrets:

```json
"proxy": { "auth_enabled": true, "username": "local", "password_env": "UNET_PROXY_PASSWORD" },
"gateway": { "capability_token_env": "UNET_GATEWAY_TOKEN" }
```

## 4. Run the tunnel

```bash
./unet --config ./config.json
```

Expected: `amneziawg-go` + `gVisor netstack` initialize with no permission errors (rootless, SC-002); SOCKS5/HTTP listener binds on `127.0.0.1:1080` (FR-004).

## 5. Verify SOCKS5

```bash
curl -x socks5h://127.0.0.1:1080 https://ifconfig.me
```

Use `socks5h` (not `socks5`) so **curl sends the hostname to the proxy** for in-tunnel resolution. The returned IP MUST be the remote exit's IP, not yours.

## 6. Verify NO DNS leak (FR-009 / SC-006)

The critical check: proxied name resolution must happen inside the tunnel, never via the host resolver.

```bash
# Terminal A: watch the host's clear-net path for DNS (UDP/53) on the physical NIC
sudo tcpdump -ni <physical_iface> port 53

# Terminal B: drive a name-based request through the proxy
curl -x socks5h://127.0.0.1:1080 https://example.com
```

**Pass**: Terminal A shows **zero** DNS queries for `example.com` on the clear-net path (resolution occurred remote-side / in-tunnel). If you instead removed the in-tunnel resolver, the request MUST fail closed (SOCKS5 host-unreachable) — it MUST NOT silently resolve via the host.

> Only the WG **endpoint hostname** itself may resolve out-of-tunnel (control-plane, logged); user traffic must not.

## 7. Verify DPI shape (optional, SC-003)

```bash
tcpdump -ni <iface> -w unet.pcap host <endpoint_ip>
# then run nDPI / Wireshark WireGuard dissector over unet.pcap
# Pass: 0% of packets classified as wireguard/wg
```

## 8. Reverse port forward (optional, FR-006)

Add an allow-list entry (default-deny — nothing is forwarded without it):

```json
"gateway": {
  "address": "10.0.0.1:7000",
  "allow_list": [ { "public_port": 443, "local_host": "127.0.0.1", "local_port": 3000 } ]
}
```

Requests to the gateway's public port 443 are forwarded to local `127.0.0.1:3000`; any other target is rejected.

The public Caddy/gateway side must implement the contract in `contracts/interfaces.md`: three listeners (yamux on the tailnet address, ingress on loopback fed by Caddy, admin on loopback — §4.1), length-prefixed wire framing (§2.0), authoritative route table (§4.3), then open the yamux stream. Test negative cases for no active session, token mismatch, and unlisted port before exposing it publicly.

*End of Quickstart.*
