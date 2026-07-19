# Tasks: 001-init-netstack-tunnel (unet)

## Overview

Generated from `plan.md`. Tasks are ordered with dependencies and include a suggested agent tag for assignment.

## Tasks (high level)

1. T1 — Integration skeleton: embed `amneziawg-go` + `tun/netstack`, CLI flags, config loader (incl. `0600`/ACL perm write + permission enforcement on read)
   - Owner: `backend-specialist`
   - Est: 2d
   - Depends on: none
   - Agent: backend
   - Satisfies: FR-001, FR-002, FR-003, FR-010 (MTU/MSS config), FR-008.002/.003

2. T2 — Local SOCKS5/HTTP proxy implementation (loopback), optional auth flag, in-tunnel DNS resolution (remote-side / tunnel resolver, fail-closed, no host-resolver fallback)
   - Owner: `backend-specialist`
   - Est: 1.5d
   - Depends on: T1
   - Agent: backend
   - Satisfies: FR-004, FR-009 (in-tunnel DNS anti-leak), FR-004 multi-user caveat; includes SOCKS5 `ATYP=0x03`, HTTP `CONNECT`, absolute-form HTTP requests, Basic auth, and fail-closed HTTP status mapping.

3. T3 — Yamux reverse-forward implementation and gateway contract fixture (single long-lived TCP session + multiplexing), port allow-list enforcement (default-deny), optional capability-token control frame
   - Owner: `backend-specialist`
   - Est: 2d
   - Depends on: T1
   - Agent: backend
   - Satisfies: FR-005, FR-006, FR-006.001 (allow-list), FR-006.002 (capability token), FR-006.003 (gateway/Caddy counterpart contract). Deliverables include the client yamux session, stream-open authorization, and a minimal gateway integration fixture or external-gateway contract test that maps public request metadata to a yamux `open` frame.

4. T4 — Key provisioning & local JSON config writer (auto-generate keypair, env override, `0600`/ACL perms, secret-bearing-field checks, plaintext-at-rest first-run notice)
   - Owner: `backend-specialist`
   - Est: 1d
   - Depends on: T1
   - Agent: backend
   - Satisfies: FR-008, FR-008.001 (at-rest risk notice), FR-008.002 (perms), FR-008.003 (permissions enforced for private key, PSK, proxy password, capability token), env overrides/references.

5. T5 — Reconnect strategy & graceful failure handling (WG-level + yamux-level reconnect, keepalive/half-open, roaming re-resolution, finite terminal-failure give-up + alert, explicit retry-forever opt-in, dial timeouts)
   - Owner: `backend-specialist`
   - Est: 1d
   - Depends on: T3, T4
   - Agent: backend
   - Satisfies: FR-007, FR-007.001–.005

6. T6 — Benchmark harness and memory tuning (integration & perf); MUST include large-transfer + fragmentation-boundary/MTU-MSS cases and concurrency runs (N SOCKS5 conns / M yamux streams) to establish SC-004/SC-005 basis
   - Owner: `performance-optimizer`
   - Est: 1.5d
   - Depends on: T2, T3
   - Agent: performance
   - Satisfies: SC-004 (basis), SC-005 (concurrency/per-stream), FR-010 (MTU validation)

7. T7 — Integration tests & CI (cross-compile, smoke tests for Windows/macOS/Linux); MUST include DPI-classifier check (SC-003), no-DNS-leak test (SC-006), HTTP proxy tests, and gateway reverse-forward integration fixture
   - Owner: `test-engineer`
   - Est: 2d
   - Depends on: T1, T2, T3, T4, T5, T6
   - Agent: test
   - Satisfies: SC-001, SC-002, SC-003 (DPI classifier), SC-006 (no DNS leak). Tests cover SOCKS5 + HTTP proxy DNS behavior, invalid DNS config fail-closed, public gateway request -> yamux open -> local port response, no active session, token mismatch, unlisted public port, and finite terminal retry behavior.

8. T8 — Quickstart and release packaging (README, examples, build scripts)
   - Owner: `documentation-writer` / `backend-specialist`
   - Est: 0.5d
   - Depends on: T7
   - Agent: documentation

## Parallel lanes

- Lane A: T1 (skeleton) -> T2 (proxy) and T3 (yamux) can be developed in parallel after T1 completes.
- Lane B: T4 (key provisioning) runs alongside T2/T3 but must be ready before T5.
- Lane C: T6 (perf) begins once T2+T3 provide measurable throughput.

## Critical Path

T1 -> T3 -> T5 -> T7
(This path includes the reverse-forward core and the resilience required to validate integration tests.)

## Agent Dispatch Plan

- `backend-specialist`: implement T1, T2, T3, T4, T5
- `performance-optimizer`: implement T6 and run benchmark tuning
- `test-engineer`: implement T7 and smoke CI
- `documentation-writer`: create T8 quickstart and examples

## Suggested MVP Scope

Deliver minimal v1 binary that: embeds netstack, exposes SOCKS5/HTTP loopback (no auth by default on single-user hosts), supports yamux reverse-forward inside AmneziaWG with a gateway/Caddy integration contract, auto-generates keypair with secret-aware config permissions, and includes CI smoke/leak tests plus quickstart.

## Requirements → Tasks Traceability

| Requirement | Task(s) |
| ----------- | ------- |
| FR-001, FR-002 | T1 |
| FR-003 (obfuscation-param schema) | T1 (config schema) |
| FR-004 (SOCKS5/HTTP, optional auth, multi-user caveat) | T2 |
| FR-005, FR-006 (yamux inside WG) | T3 |
| FR-006.001 (port allow-list) | T3 |
| FR-006.002 (capability token) | T3 |
| FR-006.003 (gateway/Caddy counterpart contract) | T3, T7 |
| FR-007 + .001–.005 (two-level reconnect, timeouts, give-up) | T5 |
| FR-008 (config/keys), FR-008.001 (at-rest notice) | T4 |
| FR-008.002/.003 (perms write + enforce on read) | T4 (write), T1 (enforce on read) |
| FR-008 env override | T4 |
| FR-009 + .001–.003 (in-tunnel DNS anti-leak) | T2 (impl), T7 (leak test) |
| FR-010 (MTU/MSS clamp) | T1 (config/clamp), T6 (validation) |
| SC-001, SC-002 | T7 |
| SC-003 (DPI classifier) | T7 |
| SC-004 (RSS/throughput basis) | T6, research.md |
| SC-005 (concurrency/per-stream) | T6 |
| SC-006 (no DNS leak) | T7 |

## Total task count: 8
