# Phase 0 Research: 001-init-netstack-tunnel (unet)

**Branch**: `001-init-netstack-tunnel` | **Date**: 2026-07-18 | **Spec**: `spec.md` (same folder)
**Status**: Living document. Numeric budgets flagged **[ESTIMATE — pending benchmark]** are provisional until the T6 harness runs; do not treat them as validated.

This document records the Phase 0 decisions that the plan and success criteria depend on. Each section states the **Decision**, **Rationale**, and **Alternatives considered**.

---

## R1 — gVisor `netstack` on Windows compatibility

**Decision**: Target `gvisor.dev/gvisor/pkg/tcpip` via `golang.zx2c4.com/wireguard/tun/netstack` compiled into the binary; support Windows/macOS/Linux via pure-Go cross-compile, no cgo.

**Rationale**:
- `tun/netstack` is pure Go and already used by `wireguard-go` user-space builds, so it cross-compiles to `windows/amd64` and `windows/arm64` without a kernel driver — this is the whole point of the rootless requirement (SC-002).
- No `wintun.dll` / TAP-Windows / `utun` helper is needed; the virtual TCP/IP stack lives in process memory.

**Risks / open items**:
- Windows scheduler + UDP socket behavior under load can differ from Linux; validate with the T6 harness on a Windows runner in CI (plan Risk: "gVisor Windows compatibility").
- gVisor pins specific Go versions; keep toolchain at Go 1.20+ and pin the `gvisor`/`wireguard` module versions in `go.mod`.

**Alternatives considered**: kernel WireGuard + `wintun` (rejected — requires driver install / admin, violates rootless); `wireguard-go` with OS TUN (rejected — still needs elevated TUN creation on most platforms).

---

## R2 — AmneziaWG obfuscation parameters

**Decision**: Expose the full AmneziaWG parameter set — junk packet count/size (`Jc`, `Jmin`, `Jmax`), init/response padding (`S1`, `S2`), and header magic bytes (`H1`, `H2`, `H3`, `H4`) — via config/CLI (FR-003). Ship conservative non-zero defaults chosen so the on-wire handshake is not classifiable as WireGuard (see R6/SC-003).

**Rationale**:
- These parameters are what make the handshake fail nDPI/dissector fingerprinting; they must be tunable per deployment because DPI environments differ.
- `S1`/`S2` padding and `H1-H4` change packet sizes and the first bytes, which interacts with MTU budgeting (see R5).

**Open item**: Default values are **[ESTIMATE]** until the DPI-classifier check (SC-003) and throughput impact (larger junk = more overhead) are measured together in T6. Larger `Jmax`/`S2` improves evasion but costs bandwidth — the default is a balance to be re-tuned.

**Alternatives considered**: plain WireGuard (rejected — trivially DPI-classified); external obfuscation proxy (rejected — extra process/binary, violates single-binary SC-001).

---

## R3 — Throughput / RSS baseline methodology (basis for SC-004)

**Decision**: Establish the ≤64MB idle RSS and ≥50%-of-native-WireGuard throughput budgets from a documented baseline procedure, not a guess. The T6 harness MUST:

1. **Native baseline**: measure kernel/`wireguard-go` throughput host-to-host with `iperf3` (TCP, multiple parallel streams) on the target hardware; record MB/s.
2. **unet baseline**: run the same `iperf3` transfer through the `unet` netstack SOCKS5 path and through a `yamux` reverse-forward channel; record MB/s. Report as a percentage of (1).
3. **Idle RSS**: start `unet`, establish the tunnel, no traffic, sample RSS after GC settle (force `runtime.GC()`, read `runtime.MemStats.HeapInuse` + OS RSS) at 30s/60s; report peak.
4. **MTU sweep**: repeat (2) across MTU values (R5) to find the fragmentation cliff.

**Rationale for the current numbers**:
- **≤64MB idle RSS [ESTIMATE]**: netstack pre-allocates packet buffer pools and the AmneziaWG device keeps per-peer state; 64MB is the router-class envelope the product targets, chosen as a *target* to tune GC/buffer knobs toward, **not** a measured result. If the first benchmark exceeds it, either the knobs are tuned or the budget is formally revised in this file.
- **≥50% native throughput [ESTIMATE]**: user-space netstack + nested encapsulation (R5) is expected to roughly halve kernel throughput based on published `wireguard-go`/netstack figures; 50% is a floor to design toward, confirmed/revised by (2).

**Consequence**: Until T6 runs, SC-004 numbers are explicitly documented estimates whose basis is this methodology. Reviewers should read them as budgets-with-a-plan, not validated facts.

---

## R4 — Concurrency envelope & per-stream memory (basis for SC-005)

**Decision**: Characterize memory as `RSS(streams) ≈ base + k * active_streams`. T6 measures `base` and `k` by opening a sweep of concurrent SOCKS5 connections and `yamux` streams (e.g. 1, 16, 64, 256) and fitting the marginal cost.

**Provisional targets [ESTIMATE]**: at N=256 SOCKS5 conns / M=256 yamux streams, RSS ≤ ~128MB and per-stream marginal ≤ ~256KB. These come from: each `yamux` stream carries send/receive window buffers (default 256KB window) plus netstack per-connection TCB. The 256KB/stream ceiling is essentially the yamux window; lowering the yamux `MaxStreamWindowSize` is the primary tuning knob if the ceiling is exceeded.

**Rationale**: plan.md flags per-stream memory as a scale risk; without a bound, M large transfers can blow the router-class envelope. The window size is the dominant, directly-tunable term.

---

## R5 — MTU / MSS in nested encapsulation (basis for FR-010)

**Decision**: Compute a safe default tunnel MTU that subtracts, from the path MTU, the UDP+IP outer header (20 IP + 8 UDP), the WireGuard data header (32), and the **S4 transport padding** (zcode F9 erratum: `S1`/`S2` are handshake-only paddings and do NOT affect the data path; `H1-H4` replace header values, not add bytes). gVisor netstack TERMINATES in-tunnel TCP and derives MSS from the NIC MTU — there is no SYN clamping to configure; the MTU value IS the MSS control (schema caps `mtu` at 1420).

**Rationale**:
- Standard WireGuard uses MTU 1420 (1500 − 80). AmneziaWG padding/junk adds variable overhead, so the safe MTU is **lower** and depends on obfuscation params — a wrong value silently blackholes or fragments large packets and tanks throughput, directly undermining SC-004. This is why FR-010 mandates a configurable MTU + MSS clamp and why T6 must include large-transfer/fragmentation cases.
- yamux-in-WG means the effective payload for a reverse-forward stream is further reduced by the yamux framing; MSS clamping at the netstack edge is the robust fix rather than trusting PMTUD across a user-space stack.

**Open item**: exact default MTU is **[ESTIMATE]** until measured against representative `S4/Jmax` defaults in T6.

**Alternatives considered**: rely on PMTUD (rejected — unreliable across user-space netstack + obfuscated UDP, ICMP often filtered); fixed 1280 (safe but leaves throughput on the table — use as the conservative fallback only).

---

## R6 — DPI validation method (basis for SC-003 / US1 #2)

**Decision**: Make the DPI claim falsifiable by pinning a concrete classifier: capture the flow (`tcpdump`/`pcap`) and run (a) the Wireshark WireGuard dissector and (b) `nDPI` (optionally `zeek`) protocol detection; pass = 0% of packets labelled `wireguard`/`wg`. Real ISP/commercial-appliance evasion is explicitly **manual/aspirational** and not a CI gate.

**Rationale**: the original "bypasses protocol handshake filters" / "passes DPI validation" wording was unfalsifiable. A named classifier + a numeric threshold makes it testable in T7 while honestly scoping out what CI cannot prove.

---

## R7 — Gateway authorization model (basis for FR-006.001/.002)

**Context**: FR-006 makes the client WG public key the **sole** gateway identity. Any peer that completes the WG handshake could otherwise open reverse-forward `yamux` streams to arbitrary local ports — a privilege-escalation/lateral-movement risk on a shared gateway.

**Options weighed**:

| Option | Pros | Cons |
|---|---|---|
| **A. Public-key-only (status quo)** | Simplest; no extra secret to provision | No per-target authorization; a valid peer reaches *any* local port; no revocation short of removing the WG peer |
| **B. Public-key + port allow-list (CHOSEN, FR-006.001)** | Default-deny; explicit `{public_port → local_host:port}` map; bounds blast radius with no new secret | Allow-list must be provisioned/maintained; still trusts any handshaking peer within the allowed set |
| **C. Per-tunnel capability token (RECOMMENDED add-on, FR-006.002)** | Token issued at provisioning (by e.g. `undrlla`), carried in first yamux control frame, validated before honoring allow-list; enables revocation and per-tunnel scoping without a full mTLS/PKI | Requires a token issuance/validation path; slightly more provisioning complexity |

**Decision**: Adopt **B as a MUST** (default-deny allow-list) and **C as a SHOULD** hardening that may ship behind a flag. Rationale: B removes the "arbitrary local port" hole cheaply and immediately; C addresses the residual "any handshaking peer" trust on shared gateways and is lightweight because it is a single control-frame field validated against a provisioning-issued token — far cheaper than mTLS while giving revocation. Full mTLS/PKI is rejected for v1 as disproportionate and contrary to the single-binary/simple-provisioning goal.

---

## R8 — In-tunnel DNS strategy (basis for FR-009)

**Context**: A user-space netstack SOCKS5 proxy that resolves names with the host resolver leaks every destination hostname to the local/ISP resolver — the **top deanonymization vector** for this architecture even though payloads are tunneled.

**Decision**: Resolve proxied names **inside the tunnel**, never via the host resolver, with fail-closed behavior:
- Prefer **remote-side resolution**: forward SOCKS5 `ATYP=0x03` (domain) requests so the exit resolves them; the hostname never leaves via the host path.
- Alternatively resolve via a **tunnel-side DNS** server (the WG-assigned resolver in the `dns` config field), sending queries *through* the netstack tunnel.
- If neither is available, **fail closed** (SOCKS5 resolution-failure reply) rather than falling back to `net.Resolver`/`/etc/resolv.conf`/Windows resolver.
- The only permitted out-of-tunnel resolution is the control-plane lookup of the WG **endpoint hostname** itself (needed to bring the tunnel up / roaming re-resolution FR-007.002); it is limited to the endpoint and logged.

**Rationale**: remote-side resolution is the strongest (no in-tunnel resolver round-trip, exit-local DNS) and simplest for SOCKS5; tunnel-side resolver covers cases where the exit cannot resolve. Fail-closed is mandatory — a silent host-resolver fallback would defeat the whole design. SC-006 asserts zero out-of-tunnel DNS for proxied destinations under test.

**Alternatives considered**: host resolver with tunneled transport (rejected — still leaks query to host stack config/cache); split-DNS heuristics (rejected for v1 — complexity, leak-prone).

---

## Summary of provisional numbers (to validate in T6)

| Item | Value | Status |
|---|---|---|
| Idle RSS budget (SC-004) | ≤ ~64 MB | [ESTIMATE — pending benchmark] |
| Throughput floor (SC-004) | ≥ ~50% native WG | [ESTIMATE — pending benchmark] |
| Concurrency RSS (SC-005) | ≤ ~128 MB @ N=M=256 | [ESTIMATE — pending benchmark] |
| Per-stream ceiling (SC-005) | ≤ ~256 KB / stream | [ESTIMATE — ~ yamux window] |
| Default tunnel MTU (FR-010) | TBD (< 1420, param-dependent) | [ESTIMATE — pending benchmark] |
| Obfuscation defaults (FR-003) | conservative non-zero | [ESTIMATE — pending SC-003 tune] |

*End of Phase 0 Research.*
