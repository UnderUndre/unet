# SpecKit Analyze: 001-init-netstack-tunnel (post-surgery)

**Reviewer**: analyze (zcode in-session)
**Reviewed at**: 2026-11-15
**Commit**: uncommitted working-tree changes over `d0a5da78cb04fd3f1dfea146a7ef186ddcd3bfde` (upstream remote state)
**Artifacts reviewed**: spec.md, plan.md, tasks.md, data-model.md, research.md, quickstart.md, contracts/config.schema.json, contracts/interfaces.md; cross-checked 003-ecosystem-integration/spec.md and ops/option0-baseline/

## Context

Post-implementation-readiness surgery (2026-11-15): FR-011 (compute renting) and FR-012 (IdP entitlements) relocated to `003-ecosystem-integration`; reference gateway task (T3a) added; gateway-side operational contract added (`contracts/interfaces.md` §4); data-model §2.3.1 extended with TTL/revocation; T1 gained a mandatory gVisor bump step; Status → `Ready for implement`.

## Requirements → Tasks Coverage (in-scope: FR-001..FR-010 incl. sub-FRs, SC-001..SC-006)

| Requirement | Task(s) | Status |
|---|---|---|
| FR-001, FR-002 (embed amneziawg-go + netstack) | T1 | COVERED |
| FR-003 (obfuscation param schema) | T1 | COVERED |
| FR-004 (SOCKS5/HTTP loopback proxy, auth, multi-user caveat) | T2 (+T7 tests) | COVERED |
| FR-005 (yamux multiplexing) | T3 | COVERED |
| FR-006 (tunnel-internal control channel) | T3 | COVERED |
| FR-006.001 (port allow-list, default-deny) | T3 | COVERED |
| FR-006.002 (capability token) | T3 | COVERED |
| FR-006.003 (gateway contract — client side) | T3, T7 | COVERED |
| FR-006.003 (gateway-side: ingress, registry, TTL) | **T3a**, T7 | COVERED (was GAP → fixed) |
| FR-006.004 (revocation sync endpoint) | **T3a**, T7 | COVERED (was GAP → fixed) |
| FR-007 + .001–.005 (two-level reconnect, liveness, give-up) | T5 (+T7) | COVERED |
| FR-008 + .001–.003 (config, keys, perms) | T4 (+T1 read enforcement) | COVERED |
| FR-009 + .001–.003 (in-tunnel DNS anti-leak) | T2 (+T7 leak test) | COVERED |
| FR-010 (MTU/MSS clamp) | T1 (config), T6 (validation) | COVERED |
| SC-001, SC-002 (single binary, rootless) | T7 | COVERED |
| SC-003 (DPI classifier, falsifiable) | T7 | COVERED |
| SC-004, SC-005 (throughput/RSS, concurrency) | T6 | COVERED |
| SC-006 (no DNS leak) | T7 | COVERED |
| FR-011, FR-012 (compute, IdP) | — | **RELOCATED** to 003-ecosystem-integration (intentional) |

Coverage: 100% of in-scope requirements have ≥1 task. No orphan tasks.

## Consistency Checks

| Check | Result |
|---|---|
| spec FR-007.004 (finite terminal condition) ↔ schema retry `oneOf {max_total_duration_ms≥1 \| max_attempts≥1 \| retry_forever}` | PASS (`0 => never give up` hole closed, copilot-F4 resolved) |
| spec FR-009.002 (explicit resolution mode) ↔ schema `dns` `oneOf` (minItems 1 when tunnel_dns) | PASS (copilot-F5 resolved) |
| endpoint / gateway.address validation ↔ `hostPort` $defs | PASS (copilot-F6 resolved) |
| spec §US2 acceptance (public request → yamux open → local port) ↔ tasks | PASS — now implementable end-to-end: client T3 + gateway T3a + fixture T7 (was testable-only-with-mock, fixed) |
| spec ↔ interfaces §2 (session handshake, stream-open default-deny) | PASS |
| spec ↔ interfaces §4 (gateway-side operational contract, NEW) ↔ data-model §2.3.1 | PASS — TTL, route drop, revoke endpoint aligned across all three |
| FR-011/FR-012 residue in 001 artifacts | CLEAN — only intentional MOVED pointer (spec.md) + task-count note (tasks.md); reviews never referenced them (0 hits) |
| 003 spec ↔ 001 | PASS — Park Gate requires 001 shipped + paying client; no circular ownership |

## External Review Resolution (verified this session, not taken on trust)

| Review | Verdict | Findings resolved |
|---|---|---|
| claude (4H/4M) | HIGH | 8/8 — F1→FR-009/SC-006, F2→FR-008.001-.003, F3→FR-006.001/.002, F4→artifacts exist, F5→FR-007.001-.005, F6→FR-010, F7→SC-005, F8→FR-007.005 |
| copilot (4H/3M) | HIGH | 7/7 — F1→FR-006.003 + interfaces §2/§4 + T3a, F2→T2 HTTP contract, F3→FR-008.003 secret-class check, F4→schema oneOf, F5→schema dns, F6→hostPort, F7→T2 ownership |
| grok (3H/2M/1L) | HIGH | F1→relocated to 003 (FR-E-001), F2→FR-008.001-.003 + FR-006.004, F3→TTL/route-drop (data-model §2.3.1 + interfaces §4.2), F4→kept manual per R6, F5→003 FR-E-001(d), F6→README positioning |

All three reviews: 0 CRITICAL. No unresolved HIGH/MEDIUM remains.

## Residual Low Findings

- **L1**: T3a listed at list-position 9 (ID stability preserved); implementers must read the Lane D / Critical Path notes for true sequencing.
- **L2**: gVisor bump (T1 first step) may shift SC-004/SC-005 budgets; research.md R1/R3 protocol mandates re-tuning rather than silently keeping stale numbers.
- **L3**: `ops/option0-baseline/` kit carries `[VERIFY]` markers (awg-tools binary names, `WG_QUICK_USERSPACE_IMPLEMENTATION` support, `awg syncconf`) — ops-layer unknowns outside the spec gate; resolve on first VPS run and demote markers.

## Constitution Alignment

| Principle | Status |
|---|---|
| VI — Cross-AI Review | PASS — three external reviews exist (claude/copilot/grok, 0 critical each) and all findings are resolved or consciously relocated; the one post-review scope addition (FR-011/012) is now relocated to 003 instead of shipping unreviewed |
| VII — Artifact Versioning | PASS — history via git; surgery dated in-artifact (2026-11-15) |
| I — Source of Truth | PASS — spec/plan/tasks/contracts/data-model mutually consistent as of this pass |

## Metrics

- In-scope requirements: 10 FRs (+10 sub-FRs) + 6 SCs
- Tasks: 9 (T1–T8 + T3a)
- Coverage: 100% of in-scope requirements
- Ambiguity count: 0 · Duplication count: 0
- CRITICAL: 0 · HIGH: 0 · MEDIUM: 0 · LOW: 3

## VERDICT

```yaml
verdict: PASS
reviewer: analyze
reviewed_at: 2026-11-15
critical_count: 0
high_count: 0
medium_count: 0
low_count: 3
```

**Rationale**: every external HIGH/MEDIUM finding is resolved in-artifact (verified line-by-line this session), the two structural gaps (gateway implementation task; unreviewed ecosystem FRs) are closed by T3a and the 003 relocation respectively, and coverage of the v1 scope is complete. Remaining LOWs are execution-order notes, not correctness defects.

**Next**: `/speckit.implement` may start at T1 (gVisor bump first), with the Option 0 baseline kit (`ops/option0-baseline/`) deployed on the author's VPS in parallel as the UX acceptance baseline.

---

## Addendum 2026-11-15: zcode external review (CRITICAL) — verified and resolved

An independent zcode review (`reviews/zcode.md`, v0.1 verdict CRITICAL: 3C/5H/5M/2L) landed after this analyze pass. Every claim was re-verified against the actual repo before acceptance — **all confirmed**, including two places where THIS analyze pass itself was wrong:

- v0.1 of this analyze claimed the retry policy used `oneOf` — the schema actually had `anyOf`, which allowed `{max_attempts, retry_forever:true}` simultaneously (zcode F7). Corrected.
- v0.1 of this analyze passed the gateway contract while it was internally contradictory ("binds loopback only" vs tunnel-internal dialing) and lacked wire framing (zcode F3). The consistency check was too shallow; fixed.

Resolution: same-day fix pass across spec.md (FR-009 tunnel_dns-only, FR-010 MTU/MSS+S4 erratum, FR-008.004 log hygiene, FR-006.004 generic admin API, Assumptions SPOF), contracts (schema rewrite: `local_addresses`, `schema_version`, AWG 1.5 param alignment, oneOf+defaults, mtu ≤1420; interfaces: §2.0 framing, §4.1 three listeners, §4.3 authoritative routing, §4.4 canonical HMAC+replay+persistence, §5 control/health/exit codes), data-model §1, tasks (T1/T2/T3a/T7), plan (Go 1.25 + layout disposition), quickstart, research R5 erratum, `ops/option0-baseline` (dnsmasq on the tailnet IP — the client `DNS=10.66.0.1` pointer now has a live resolver), and the business plan license (Apache → MIT). Full F1–F15 resolution table: `reviews/zcode.md` §v2.

Updated verdict for the package: **PASS** (0 critical, 0 high, 0 medium; residual follow-ups tracked in zcode.md §v2: constitution adaptation, `outline/` role, T6 budget re-anchoring).
