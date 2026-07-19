# SpecKit Review: 001-init-netstack-tunnel

**Reviewer**: claude
**Reviewed at**: 2026-07-19T00:00:00Z
**Commit**: e269c15bb10c1cf0c6c62ad12e80cb634915d538 (repo root `UnderUndre`; `repos/unet` is not a standalone git repo)
**Artifacts reviewed**: spec.md, plan.md, tasks.md, constitution.md (`.specify/memory/`). Absent: research.md, data-model.md, contracts/, quickstart.md.

## Summary

The spec is coherent at the narrative level and the clarifications session resolved several key ambiguities (auth, identity, provisioning, reconnect). However Phase 1 design artifacts promised by the plan (`research.md`, `data-model.md`, `contracts/`, `quickstart.md`) do not exist, leaving critical security decisions (key-at-rest handling, DNS resolution path, gateway trust) undocumented. For a rootless VPN/tunnel feature the security model is under-specified in several high-impact places, most notably DNS leakage, plaintext key persistence, and the single-public-key gateway authorization model.

## Findings

| ID | Severity | Area | Finding | Recommendation |
|---|---|---|---|---|
| F1 | HIGH | E Security / DNS | Nothing in spec (FR-001..008, SC-001..004) addresses DNS resolution. A user-space netstack SOCKS5 proxy that does not force DNS through the tunnel is the single most common cause of deanonymization leaks for this exact architecture. Silent gap. | Add an FR mandating in-tunnel DNS (proxy resolves remote-side, or forwards to a tunnel DNS), and an SC asserting no DNS leaks under test. |
| F2 | HIGH | E Security / key-at-rest | FR-008 (spec.md:77) auto-generates the private key and writes it to a local JSON config; the env-var override is offered "so it need not be persisted in plaintext" but the default path DOES persist plaintext private keys with no stated file-permission requirement (0600) or warning. | Mandate 0600 perms on the config file, document the plaintext-at-rest risk, and specify behavior when perms are too open. |
| F3 | HIGH | E Security / authZ | FR-006 (spec.md:75) makes the client's WG public key the *sole* gateway identity with "no separate secret." Any peer that completes the WG handshake can open reverse-forward yamux channels to arbitrary local ports. There is no authorization of *which* public ports map to *which* local ports, nor rate/port allow-listing. | Specify a gateway-side port-authorization/allow-list model, or explicitly document the threat that a compromised/spoofed gateway can reach any local port. |
| F4 | HIGH | A Traceability / I Process | Plan (plan.md:71) and constitution Principle VI/VII require `research.md`, `data-model.md`, `contracts/`, `quickstart.md` as Phase 1 deliverables; none exist. Phase 0 research (gVisor Windows compat, throughput baseline) is the stated basis for the ≤64MB / ≥50% budgets (SC-004) yet is unwritten, so those numbers are unfounded. | Produce research.md before implement; either generate the design artifacts or amend the plan to descope them explicitly. |
| F5 | MEDIUM | C Edge cases | Reconnect (FR-007, spec.md:76) covers AmneziaWG datagram drop, but not: yamux session death while WG is up, config file mutation/rotation at runtime, endpoint DNS/IP change (roaming), or reconnect storm when the remote is permanently gone (backoff is capped but no give-up/alert). | Enumerate yamux-level reconnect distinct from WG-level; define terminal-failure signaling. |
| F6 | MEDIUM | F Performance / MTU | No mention of MTU, fragmentation, or MSS clamping. WG-in-netstack plus yamux-in-WG is nested encapsulation; wrong MTU silently tanks throughput or blackholes large packets — directly undermines SC-004. | Add an MTU/MSS-clamp requirement and include large-transfer + fragmentation cases in the benchmark harness (T6). |
| F7 | MEDIUM | C Concurrency / scale | "Idle ≤64MB RSS" (SC-004) specifies only the idle case. No bound or target for N concurrent SOCKS5 conns or M yamux streams, though plan.md:52 flags per-stream memory as a Phase 0 concern. Load behavior is untestable as written. | Add a concurrency target (e.g. RSS at X streams) and a per-stream memory ceiling. |
| F8 | MEDIUM | D Failure modes | No timeout/failure semantics for a slow-but-alive gateway, half-open TCP, or SOCKS5 dial to an unreachable destination through a healthy tunnel. "Fail cleanly" (FR-007) is asserted, not defined. | Define dial timeouts, keepalive, and half-open detection for both the WG session and yamux. |
| F9 | LOW | H Clarity | US1 acceptance #2 (spec.md:53) "datagrams successfully bypass protocol handshakes filters" and SC-003 "passes DPI packet inspection validation tests" are unfalsifiable — no test method, tooling, or pass threshold. | Reference a concrete DPI test method/tool and a measurable pass criterion, or mark as manual/aspirational. |
| F10 | LOW | H Clarity | Tasks reference agents/est only; no acceptance-criteria-to-task mapping. FR-003 obfuscation-param schema, FR-008 env override, and DNS aren't traceable to a specific task deliverable. | Add a requirements→tasks traceability note or per-task "satisfies FR-xxx". |
| F11 | LOW | B Assumptions | Implicit assumption that loopback (`127.0.0.1:1080`) is a safe trust boundary (FR-004) ignores multi-user hosts and container/localhost-sharing scenarios where other local users can reach loopback. | Note the multi-user-host caveat; recommend auth-on when host is shared. |

## Alternative approaches considered

- **Gateway authorization (F3)**: a lightweight per-tunnel capability token issued at provisioning time, carried in the yamux control frame, would preserve the "no separate long-lived secret" goal while still authorizing port mappings — worth weighing against the pure-public-key model before locking FR-006.
- **DNS (F1)**: options are (a) proxy-side remote resolution (SOCKS5 `DOMAIN` address type resolved at exit), (b) an in-tunnel DNS forwarder, or (c) DoH pinned through the tunnel. The spec picks none; (a) is simplest and should be the documented default.

## VERDICT
```yaml
verdict: HIGH
reviewer: claude
reviewed_at: 2026-07-19T00:00:00Z
commit: e269c15bb10c1cf0c6c62ad12e80cb634915d538
critical_count: 0
high_count: 4
medium_count: 4
low_count: 3
```
