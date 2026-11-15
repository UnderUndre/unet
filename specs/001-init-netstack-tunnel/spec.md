# Feature Specification: 001-init-netstack-tunnel (Unet Embedded AmneziaWG Netstack Core)

**Feature Branch**: `001-init-netstack-tunnel`
**Created**: 2026-07-18
**Status**: Ready for implement (v1 scope: FR-001..FR-010 incl. gateway contract FR-006.003/.004; ecosystem glue parked in 003)
**Input**: Rebase `unet` onto the direct `amneziawg-go` codebase (located at `repos/unet`), embedding `gVisor netstack` (`tun/netstack`) directly into the core binary. This transforms `unet` into a self-contained, rootless AmneziaWG tunnel engine capable of running SOCKS5/HTTP local proxies and `yamux` multiplexed port forwarding without elevated host OS privileges or external CLI binary dependencies.

---

## Executive Context & Architecture

`unet` has been rebased directly on top of the native `amneziawg-go` engine (`repos/unet`), replacing the legacy wrapper project (`repos/unet-legacy`).

### Core Architecture Pillars

1. **Native AmneziaWG Transport Engine (`device/`, `conn/`)**:
   - Built-in DPI evasion using junk packet parameters (`Jc`, `Jmin`, `Jmax`), padding (`S1`, `S2`), and custom header magic bytes (`H1`, `H2`, `H3`, `H4`).
   - Pure Go implementation compiled directly into the binary.

2. **In-Process User-Space TUN (`tun/netstack`)**:
   - Uses Google's `gVisor netstack` to build a virtual TCP/IP stack entirely in user memory.
   - Eliminates all requirements for OS driver installations (`wintun`, `TAP-Windows`, `utun` root helpers).
   - Operates 100% rootless across Windows, macOS, and Linux.

3. **Dual Services Suite**:
   - **Local Proxying**: Embedded SOCKS5 and HTTP proxy listener (`127.0.0.1:1080`) routing traffic through the obfuscated netstack tunnel.
   - **Reverse Port Forwarding**: `yamux` multiplexer wrapping TCP connections from public Caddy endpoints down to local developer ports. The multiplexed control/data channel is carried **inside** the established AmneziaWG tunnel; the client's WireGuard public key is its sole identity to the gateway (no separate gateway secret).

---

## Clarifications

### Session 2026-07-18

- Q: How does `unet` authenticate/identify itself to the public gateway when registering a reverse port-forward tunnel? → A: The reverse-forward control channel rides inside the established AmneziaWG tunnel; tunnel identity = the client's WireGuard public key (no separate gateway secret, token, or mTLS).
- Q: Should the local SOCKS5/HTTP proxy on `127.0.0.1:1080` require authentication? → A: Optional — off by default (loopback is the trust boundary), enabled via a config flag (SOCKS5 username/password + HTTP Basic).
- Q: What happens when the AmneziaWG datagram stream drops mid-session? → A: Auto-reconnect with exponential backoff + jitter (capped interval); local listeners stay bound, in-flight streams fail cleanly, new dials retry until the tunnel is restored.
- Q: Where does `unet` obtain WireGuard key material and peer coordinates (keys, server endpoint, allowed-IPs)? → A: From a single local JSON config file (path via `--config`/env); the client keypair is auto-generated on first run if absent and the private key is overridable via an env var. **Paid product path (ECOSYSTEM 2026-07-23):** undrlla flagship **FR-043** issues/revokes peer material and device limits after subscription payment; undrlla (or installer) writes the config file. Remote URL pull remains optional later.
- Q: Gateway threat model? → A: Session TTL; drop reverse routes on disconnect; revoke list by WG public key; private keys encrypted at rest on client when OS keychain available.
- Q: What resource/performance envelope must v1 meet? → A: Router-friendly budget — idle memory ≤ ~64 MB RSS and sustained throughput ≥ ~50% of native WireGuard on the same host; no hard latency SLA in v1 (provisional budget, tuned after the first benchmark).
- Q: Which gateway session topology carries reverse port forwarding? → A: One long-lived outbound TCP session from `unet` to a fixed gateway address inside the established AmneziaWG tunnel; all reverse-forward channels are multiplexed over it with `yamux`.

---

## User Scenarios & Acceptance Criteria

### User Story 1 — Rootless Launch & Connection (Priority: P1)

As a user on a non-root environment, I want `unet` to launch and connect using embedded `amneziawg-go` netstack, so that I can establish a DPI-resistant tunnel without installing OS drivers or entering sudo/admin passwords.

**Acceptance Scenarios**:

1. **Given** a standard non-elevated terminal session, **When** executing `unet --config config.json`, **Then** the daemon initializes `amneziawg-go` with `gVisor netstack` and establishes the encrypted datagram stream to the remote endpoint without permission errors.
2. **Given** active stateful DPI filtering on the ISP connection, **When** `H1-H4` header obfuscation is enabled, **Then** the on-wire datagrams MUST NOT be classified as WireGuard by a reference DPI classifier. Measurable criterion: capture the handshake+data flow (e.g. `tcpdump`/`wireshark` with the WireGuard dissector, and a `nDPI` / `zeek` protocol-detection pass) and assert 0% of packets are labelled `wireguard`/`wg`. NOTE: real-world ISP/DPI-vendor evasion cannot be fully asserted in CI; that dimension is explicitly **manual/aspirational** and validated against the concrete classifier above rather than any specific ISP.

---

### User Story 2 — Port Forwarding & Local SOCKS5 Proxy (Priority: P2)

As a developer or router operator, I want `unet` to expose a local SOCKS5 proxy and a multiplexed reverse tunnel listener, so that both web browsing and dynamic port exposure work seamlessly.

**Acceptance Scenarios**:

1. **Given** an active `unet` process, **When** a client dials `SOCKS5 127.0.0.1:1080`, **Then** traffic is encapsulated through `netstack` and exits out of the remote VPS.
2. **Given** an incoming HTTP request at the public Caddy gateway, **When** mapped to a `yamux` channel, **Then** `unet` forwards the stream to the designated local port (e.g. `127.0.0.1:3000`).

---

## Requirements

- **FR-001**: System MUST integrate `amneziawg-go` directly into the `unet` repository root (`repos/unet`).
- **FR-002**: System MUST compile `tun/netstack` into the core binary for driverless user-space networking.
- **FR-003**: System MUST expose CLI flags and JSON config schemas for AmneziaWG obfuscation parameters (`Jc`, `Jmin`, `Jmax`, `S1`, `S2`, `H1-H4`).
- **FR-004**: System MUST embed an in-memory SOCKS5/HTTP server bound to `127.0.0.1:1080`. Proxy authentication (SOCKS5 username/password and HTTP Basic) MUST be supported but disabled by default, and enabled via a config flag; while auth is disabled the loopback bind (`127.0.0.1`) is the documented trust boundary. The HTTP proxy MUST support `CONNECT` for HTTPS and absolute-form HTTP requests, applying the same in-tunnel DNS/fail-closed rules as SOCKS5. NOTE (multi-user-host caveat): loopback is only a trust boundary on a single-user host. On shared/multi-user hosts, or containers that share a network namespace / publish localhost, any local user or co-located container can reach `127.0.0.1:1080`. When the host is shared, listener authentication MUST be enabled (and SHOULD be enforced by refusing to start unauthenticated if a `--shared-host` flag is set); operators are warned to prefer an abstract/Unix-socket bind where the platform allows.
- **FR-005**: System MUST integrate `hashicorp/yamux` for multiplexed reverse port forwarding.
- **FR-006**: System MUST carry the `yamux` reverse-forward control/data channel exclusively inside the established AmneziaWG tunnel. `unet` MUST maintain one long-lived outbound TCP session to the configured tunnel-internal gateway address; all reverse-forward channels MUST be multiplexed over that session with `yamux`. The client's WireGuard public key is the sole tunnel identity presented to the gateway. The system MUST NOT rely on a separate gateway-facing secret, bearer token, or independent TLS client certificate for reverse-tunnel authorization.
  - **FR-006.001 (port authorization / allow-list)**: Because the WireGuard public key is the sole identity, any peer that completes the WG handshake could otherwise open reverse-forward `yamux` streams to arbitrary local ports. The system MUST therefore constrain reverse-forward targets to an explicit, config-defined allow-list mapping `{ public_port → local_host:local_port }`. Any inbound `yamux` open-stream request whose requested target is not in the allow-list MUST be refused (stream reset + logged), with a default-deny posture (empty/absent list = no reverse-forward permitted). Wildcards MUST NOT be accepted for local host or port.
  - **FR-006.002 (capability token — RECOMMENDED, see research.md)**: To defend against a compromised/spoofed peer sharing the gateway, the system SHOULD additionally accept a lightweight per-tunnel capability token, issued at provisioning time and carried in the first `yamux` control frame; the gateway/authorizer validates it before honoring any allow-list entry. This is evaluated as the preferred hardening in `research.md` (public-key-only vs capability-token) and MAY ship behind a config flag in v1.
  - **FR-006.003 (gateway/Caddy counterpart contract)**: The public gateway side is an explicit integration dependency of v1. The feature MUST define the Caddy/gateway route shape, the active-session registry keyed by client public key, the authorization source for public-port ownership and optional capability token validation, and the behavior that maps an incoming public request to a `yamux` `open` frame sent to the correct client session. If the gateway implementation lives outside `repos/unet`, the contract and integration fixture still MUST live in this feature's `contracts/` and tests.
  - **FR-006.004 (revocation sync API)**: The gateway active-session registry MUST expose a secure administrative API endpoint (`POST /v1/gateway/revoke`) accepting `pubkey` + HMAC signature over a canonical input (interfaces §4.4) with a bounded replay window and durable revocation persistence (a gateway restart MUST NOT un-revoke a peer). In v1 the authorized caller is the local admin CLI (shared-secret stub); `undrlla` becomes the caller later per `003-ecosystem-integration` (FR-E-001) — the endpoint contract does not change. Upon a valid revocation, the gateway MUST immediately tear down all active `yamux` streams and reject future WireGuard handshakes for that public key.
- **FR-007**: System MUST automatically re-establish the AmneziaWG tunnel after an unexpected drop using exponential backoff with jitter and a capped maximum retry interval. Local SOCKS5/HTTP and `yamux` listeners MUST remain bound across the outage; in-flight streams MUST fail cleanly and new dials MUST retry until the tunnel is restored.
  - **FR-007.001 (two-level reconnect)**: Reconnect MUST be modeled at two independent levels: (a) **WG-level** — the AmneziaWG datagram stream drops; and (b) **yamux-level** — the long-lived TCP/`yamux` session dies while the WG tunnel is still up. A dead `yamux` session MUST be detected (see keepalive below) and re-dialed over the live WG tunnel *without* tearing down the WG tunnel; conversely a WG-level reconnect MUST re-establish the `yamux` session on top of the restored tunnel.
  - **FR-007.002 (roaming / endpoint change)**: The system MUST tolerate the peer endpoint DNS name resolving to a new IP, or the endpoint `host:port` changing (roaming). On repeated handshake failure the endpoint MUST be re-resolved (DNS re-lookup) before the next dial rather than caching a stale IP for the life of the process.
  - **FR-007.003 (runtime config mutation / rotation)**: Config mutation or key rotation at runtime MUST be handled deterministically: v1 MUST require an explicit reload signal (or restart) to adopt new key material or allow-list entries, MUST NOT hot-swap keys mid-handshake, and MUST re-apply file-permission checks (FR-008) on reload.
  - **FR-007.004 (terminal failure / give-up)**: The backoff cap bounds retry *interval* but not total duration. The system MUST define a terminal-failure condition (configurable max total retry duration and/or max attempts) after which it stops retrying, emits a clearly-logged **terminal-failure alert** (non-zero health signal / structured log event / optional exit code), and stops flapping. The default configuration MUST include a finite terminal condition. Infinite retry is allowed only through an explicit `retry_forever` opt-in that keeps health degraded and logs periodic warnings. This prevents an unbounded reconnect storm when the remote is permanently gone.
  - **FR-007.005 (timeout & liveness semantics — defines "fail cleanly")**: "Fail cleanly" is defined concretely as: (i) **WG session** — persistent keepalive is enabled and a handshake/keepalive that is not answered within a bounded window marks the tunnel down and triggers FR-007.001(a); (ii) **yamux session** — yamux keepalive pings are enabled with a bounded timeout; a missed keepalive marks the session dead and triggers FR-007.001(b); (iii) **SOCKS5 dial to an unreachable destination through a healthy tunnel** — the outbound dial MUST use a bounded connect timeout and return a proper SOCKS5 failure reply (e.g. `0x04 Host unreachable` / `0x03 Network unreachable`) rather than hanging; (iv) **half-open TCP** — idle/half-open streams MUST be reaped via read/write deadlines and keepalive so a silently-dead peer cannot pin resources; (v) a **slow-but-alive** gateway MUST be distinguished from a dead one by keepalive liveness (data starvation alone does not trigger reconnect while keepalives are answered).
- **FR-008**: System MUST read WireGuard key material and peer coordinates (client keypair, peer public key, endpoint `host:port`, allowed-IPs, optional preshared key), plus the tunnel-internal reverse-forward gateway TCP address, from a single local JSON config file — the same config referenced in FR-003 — whose path is set via a `--config` flag or env var. The system MUST auto-generate the client keypair on first run when absent, and MUST allow the private key to be supplied via an environment variable so it need not be persisted in plaintext. Remote/URL-based provisioning is out of scope for v1 and MAY later be added as an opt-in `--provision-url` mode.
  - **FR-008.001 (plaintext-at-rest risk)**: The DEFAULT auto-generate path persists the WireGuard **private key in plaintext** inside the local JSON config. This is a documented at-rest risk: anyone able to read the file recovers the tunnel identity. The env-var override (above) is the mitigation for hosts where plaintext-at-rest is unacceptable, and the risk MUST be surfaced in `quickstart.md` and in a first-run log notice.
  - **FR-008.002 (file permissions)**: When the system writes the config (first-run keygen or any rewrite) it MUST create/set it with owner-only permissions — `0600` on POSIX, and the Windows-ACL equivalent restricting access to the current user. Parent directory SHOULD be `0700`.
  - **FR-008.003 (permission enforcement on read)**: On load, if the config file contains any secret-bearing field (`private_key`, `preshared_key`, proxy password, gateway capability token, or future token fields) AND its permissions are broader than owner-only (group/other readable, or a permissive Windows ACL), the system MUST by default **refuse to start** with an actionable error telling the user to tighten permissions or move the secret to an environment reference. A `--insecure-permissions` (or equivalent) flag MAY downgrade this to a loud **warning** for constrained environments. If all secret-bearing values are supplied only via env vars or secret references and are not present in the file, the permission check MAY be relaxed to a warning.
  - **FR-008.004 (log & diagnostics secret hygiene, zcode F10)**: NO secret value MAY ever be written to logs, diagnostics dumps, `status` IPC output (interfaces §5), or error strings: `private_key`, `preshared_key`, `proxy.password` values, `gateway.capability_token` values, revoke `sig` values, env-derived secrets, and HTTP `Authorization`/`Proxy-Authorization` header values. The yamux `hello` frame is logged only in redacted form (key fingerprints, token-presence booleans). `IpcGet`-style diagnostics that expose key material MUST be gated behind an explicit local-admin debug command, never default log streams. A T7 automated test scans log output for seeded secrets (mirrors undevops SC-008 practice).
- **FR-009 (in-tunnel DNS — anti-leak)**: Because `unet` is a user-space netstack SOCKS5/HTTP proxy, DNS resolution is the primary deanonymization-leak vector: if name resolution uses the host resolver, the destination hostname leaks to the local/ISP resolver even though the TCP payload is tunneled. The system MUST resolve all proxied destination names **inside the tunnel** and MUST NOT fall back to the host OS resolver for proxied traffic. Concretely:
  - **FR-009.001**: For SOCKS5 the system MUST support `ATYP=0x03` (domain-name) requests and resolve them **via tunnel-side DNS only** — gVisor `LookupContextHost` against the configured `dns` resolver IPs (e.g. the gateway tailnet resolver from `ops/option0-baseline`). The system MUST NOT resolve proxied hostnames with `net.Resolver` against host `/etc/resolv.conf`/Windows resolver. NOTE (zcode F2): a `remote` hostname-forwarding mode is NOT implementable over the raw L3 tunnel without an exit-side protocol; it is deferred to `003-ecosystem-integration`.
  - **FR-009.002**: v1 ships `resolution_mode = tunnel_dns` ONLY (config `const`). At least one valid tunnel-side DNS resolver address MUST be configured (`dns` in config, see data-model; the gateway VPS runs the resolver, e.g. dnsmasq on its tailnet IP). When no tunnel-side resolver is reachable, a proxied name-based request MUST **fail closed** (return a SOCKS5/HTTP resolution failure) rather than silently leaking to the host resolver.
  - **FR-009.003**: The system MUST document, and by default avoid, any code path (health checks, endpoint re-resolution in FR-007.002, telemetry) that could emit out-of-tunnel DNS for *user* traffic; control-plane resolution of the WG endpoint hostname itself is exempt but MUST be limited to the endpoint and logged.
- **FR-010 (MTU / MSS budgeting)**: The data path is nested encapsulation (`yamux`-in-WG-in-netstack over UDP), so an incorrect effective MTU silently collapses throughput via fragmentation or blackholed large packets, directly undermining SC-004. The system MUST expose a configurable tunnel MTU (`mtu` in config, max 1420) with a safe default computed as `path_mtu − outer (20 IP + 8 UDP) − WG data header (32) − S4 transport padding`. NOTE (zcode F9): gVisor netstack **terminates** in-tunnel TCP and derives MSS from the NIC MTU — there is no SYN clamping to configure; the MTU value IS the MSS control. Handshake paddings `S1`/`S2` do NOT affect the data path; `S4` does (`device/uapi.go`, research R5). The benchmark harness (T6) MUST include large-transfer and fragmentation-boundary cases to validate the chosen MTU.
- **FR-011 / FR-012 (MOVED 2026-11-15)**: Compute & GPU node renting and Undrlla IdP capability entitlements were relocated out of this feature into [`specs/003-ecosystem-integration/spec.md`](../003-ecosystem-integration/spec.md). Reason: they arrived post-review (2026-07-23 ECOSYSTEM note), had zero coverage in plan/tasks/contracts, and are out of scope for the tunnel engine. This spec owns nothing beyond the tunnel engine, its local services, and the gateway contract (FR-001..FR-010).

---

## Assumptions (added 2026-11-15, zcode review)

- **Single gateway instance in v1**: hub-and-spoke through ONE VPS is the design center; the VPS being down takes the tailnet and all public exposure down with it. HA/multi-gateway is a non-goal for v1 (mirrors the undevops single-controller posture) and belongs to a future spec only if real usage demands it.
- **Performance budgets are hardware-relative**: SC-004's "router-class host" and "native WireGuard" baselines are defined by the T6 harness procedure (research R3), not by a specific machine; the numbers are budgets-to-tune and MUST be re-anchored when T6 runs.

---

## Success Criteria

- **SC-001**: `unet` builds into a single standalone Go binary with 0 external system CLI or DLL dependencies.
- **SC-002**: 100% rootless execution confirmed under restricted user accounts on Windows, macOS, and Linux.
- **SC-003**: AmneziaWG header obfuscation passes a **concrete** DPI-classifier check: capturing the handshake+data flow and running a reference protocol classifier (`nDPI` and/or the Wireshark WireGuard dissector, `zeek`) yields **0%** of packets labelled `wireguard`/`wg`. Pass criterion is against this classifier; evasion of any specific commercial/ISP DPI appliance is explicitly **manual/aspirational** (see US1 acceptance #2) and out of CI scope.
- **SC-004**: On a constrained router-class host, `unet` holds idle memory ≤ ~64 MB RSS and sustains data-plane throughput ≥ ~50% of native WireGuard on the same hardware, with no hard latency SLA for v1. These budgets are provisional and their basis (gVisor/netstack Windows compat, throughput/RSS baseline methodology) is documented in `research.md`; they MUST be re-tuned after the first benchmark (T6).
- **SC-005 (concurrency / per-stream memory)**: Under concurrent load the process MUST stay within a documented envelope: with **N = 256** concurrent SOCKS5 connections and **M = 256** active `yamux` streams, steady-state RSS MUST remain ≤ ~128 MB, and the marginal memory per active stream MUST be ≤ ~256 KB (per-stream ceiling). Exact N/M/ceiling values are provisional pending T6 benchmark (see `research.md`) but the harness MUST report them.
- **SC-006 (no DNS leak)**: Under an integration test that routes name-based requests through the SOCKS5 proxy while monitoring the host's out-of-tunnel network path, **zero** DNS queries for proxied destinations MUST appear on the host resolver / clear-net path; all such resolution MUST be observed inside the tunnel (or delegated remote-side). A misconfigured/absent in-tunnel resolver MUST fail closed (no host-resolver fallback), per FR-009.

---
*End of Specification.*
