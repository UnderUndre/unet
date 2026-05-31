# Cross-Artifact Analysis — 002-api-control-plane

**Date**: 2026-05-27
**Analyst**: Hermes (delegated by Claude)
**Spec slug**: 002-api-control-plane
**Pipeline state at analysis**: spec.md ✓, plan.md ✓, tasks.md ✓, data-model.md ✓, contracts/ ✓
**Revision**: 2 (post-fix re-analysis)

## Summary

All 3 major issues from v1 resolved. Network partition behavior aligned across spec/plan/tasks (synchronous 503, no queue). Both `[NEEDS CLARIFICATION]` markers resolved inline with formal round-2 clarification table. Loopback admin-scope security trade-off explicitly documented in dedicated spec section + FR-001 cross-reference. 17 FRs, 14 endpoints, 30 tasks — coverage matrices intact. 6 minor issues persist unchanged (non-blocking). Pipe layout is solid — ready for implement gate.

## Coverage Matrix

### Spec → Plan

| FR-ID | Covered in plan.md | Where |
|-------|---|---|
| FR-001 | ✅ | §Component 2 (auth middleware, PAT validation) |
| FR-002 | ✅ | §Component 3 (bcrypt hash, store), §Decisions table |
| FR-003 | ✅ | §Component 5 (token handlers), §Component 3 (store) |
| FR-004 | ✅ | §Component 5 (peers.go), §Endpoint mapping table |
| FR-005 | ✅ | §Component 5 (peers.go), §Open Risk 5 (concurrent creation) |
| FR-006 | ✅ | §Component 5 (peers.go) |
| FR-007 | ✅ | §Component 5 (peers.go) |
| FR-008 | ✅ | §Component 5 (routes.go), §Endpoint mapping table |
| FR-009 | ✅ | §Component 5 (routes.go) |
| FR-010 | ✅ | §Component 5 (routes.go) |
| FR-011 | ✅ | §Component 5 (tunnel.go), §Endpoint mapping table |
| FR-012 | ✅ | §Component 1 (TLS listener, self-signed cert) |
| FR-013 | ✅ | §Component 1 (configurable address) |
| FR-014 | ✅ | §Component 5 (error response helpers) |
| FR-015 | ✅ | §Component 2 (rate limiter), §Decisions table |
| FR-016 | ✅ | §Component 4 (audit logger), §Component 2 (audit middleware) |
| FR-017 | ✅ | §Component 4 (audit reader) |

**Result**: 17/17 FRs covered. Zero orphans.

### Spec → Tasks

| FR-ID | Covered by tasks | Task IDs |
|-------|---|---|
| FR-001 | ✅ | TASK-2.1, TASK-2.3 |
| FR-002 | ✅ | TASK-1.2, TASK-1.3, TASK-2.1 |
| FR-003 | ✅ | TASK-3.5, TASK-2.8 |
| FR-004 | ✅ | TASK-2.4 |
| FR-005 | ✅ | TASK-3.1 |
| FR-006 | ✅ | TASK-2.4 |
| FR-007 | ✅ | TASK-3.2 |
| FR-008 | ✅ | TASK-2.5 |
| FR-009 | ✅ | TASK-3.3 |
| FR-010 | ✅ | TASK-3.4 |
| FR-011 | ✅ | TASK-2.6 |
| FR-012 | ✅ | TASK-1.5 |
| FR-013 | ✅ | TASK-1.5 |
| FR-014 | ✅ | TASK-1.4 |
| FR-015 | ✅ | TASK-5.4, TASK-6.5 |
| FR-016 | ✅ | TASK-5.1, TASK-5.5 |
| FR-017 | ✅ | TASK-5.2, TASK-5.3 |

**Result**: 17/17 FRs covered. Matches tasks.md self-report. Independent verification confirms.

### Contracts → Tasks (Endpoints)

| OpenAPI endpoint | Implementation task | Status |
|---|---|---|
| `GET /v1/status` | TASK-2.7 | ✅ |
| `GET /v1/tunnel/status` | TASK-2.6 | ✅ |
| `GET /v1/peers` | TASK-2.4 | ✅ |
| `POST /v1/peers` | TASK-3.1 | ✅ |
| `GET /v1/peers/{peerId}` | TASK-2.4 | ✅ |
| `DELETE /v1/peers/{peerId}` | TASK-3.2 | ✅ |
| `GET /v1/routes` | TASK-2.5 | ✅ |
| `POST /v1/routes` | TASK-3.3 | ✅ |
| `DELETE /v1/routes/{routeId}` | TASK-3.4 | ✅ |
| `GET /v1/tokens` | TASK-2.8 | ✅ |
| `POST /v1/tokens` | TASK-3.5 | ✅ |
| `DELETE /v1/tokens/{tokenId}` | TASK-3.5 | ✅ |
| `GET /v1/audit` | TASK-5.3 | ✅ |
| `POST /v1/auth/session` | TASK-4.3 | ✅ |

**Result**: 14/14 endpoints covered. Zero gaps.

### Auth Flows → Tasks

| Auth flow | Implementation tasks | Status |
|---|---|---|
| §1 PAT creation (incl. bootstrap) | TASK-3.5 | ✅ |
| §2 PAT usage (validation + scope + rate limit) | TASK-2.1, TASK-2.2, TASK-2.3, TASK-5.4 | ✅ |
| §3 JWT session establishment | TASK-4.1, TASK-4.2, TASK-4.3 | ✅ |
| §4 Token revocation | TASK-3.5 | ✅ |
| §5 Auth-by-bind-address logic | TASK-2.3 | ✅ |

**Result**: 5/5 auth flows covered.

### Edge Cases → Coverage

| Edge case | Addressed where | Status |
|---|---|---|
| VPS unreachable (stale reads) | spec.md SC-005, TASK-2.6 (stale data) | ✅ stale: true with 5s threshold, formally resolved in round 2 |
| VPS unreachable (write rejection) | spec.md Edge Cases, TASK-3.1 (503) | ✅ synchronous 503 with structured error body — aligned across spec/plan/tasks |
| Token expired/revoked mid-session | TASK-2.1 (401), TASK-4.2 (JWT checks PAT), auth-flows.md §4 | ✅ |
| Peer config conflicts (IP exhaustion) | TASK-3.1 (507 ip_pool_exhausted) | ✅ |
| Concurrent peer creation | TASK-3.1 (mutex), plan.md §Open Risk 5, SC-009 | ✅ |
| Network partition (daemon↔VPS) | spec.md Edge Cases, TASK-3.1 (503), plan.md §Decisions table | ✅ synchronous 503 — behavioral mismatch resolved |
| Control plane TLS cert expiry | TASK-2.7 (certExpiryWarning < 30 days) | ✅ |
| Rate limiting | TASK-5.4, TASK-6.5 | ✅ hardcoded 60 req/min + burst 10 — formally resolved in round 2 |

**Result**: 7/7 edge cases fully addressed. Zero partials. Network partition queuing mismatch from v1 resolved.

## Issues Found

### Critical (blocks PASS)

None.

### Major (should fix before implement)

None. All 3 majors from v1 resolved:
- I-101 (network partition queuing vs 503): spec.md edge case rewritten to synchronous 503; plan.md decision table updated.
- I-102 (unresolved `[NEEDS CLARIFICATION]`): both markers replaced with inline Decision text; round 2 clarification table added; FR-015 marker resolved.
- I-103 (loopback admin scope unacknowledged): spec.md "Accepted Security Trade-offs" section added; FR-001 has `[security-note]` cross-reference; plan.md decision table updated.

### Minor (nice-to-fix)

- **I-201**: **architecture.md path prefix drift not yet fixed.** plan.md §VIII correctly identifies `:8443/api/v1/*` in architecture.md:82 should be `:8443/v1/*`. Still present. Non-blocking but should be tracked as a follow-up task or included in Phase 1 implementation.

- **I-202**: **APIToken `name` validation mismatch between data-model.md and OpenAPI.** data-model.md says `name`: "1–128 chars, `[a-zA-Z0-9_-]` + spaces." OpenAPI `CreateTokenRequest.name` has `minLength: 1, maxLength: 128` but no `pattern` constraint. TASK-3.5 doesn't specify the exact regex. Recommendation: align during implementation.

- **I-203**: **No explicit task for cert rotation (`rotate_cert` audit action).** The audit action enum includes `rotate_cert` but no task implements cert rotation. Fine for MVP (manual replacement). Recommendation: note as reserved for future use.

- **I-204**: **`GET /v1/routes` in OpenAPI missing `GET /v1/routes/{routeId}` endpoint.** Intentional asymmetry with peers (routes have no per-route stats to query). Worth documenting explicitly.

- **I-205**: **TASK-5.5 excludes audit for loopback requests.** Consistent with design but means local admin actions are unauditable. Accepted for single-admin MVP.

- **I-206**: **Token `createdBy` field population not explicit in tasks.** TASK-1.3 describes struct, TASK-3.5 handles creation, but neither specifies `createdBy` = caller token ID or "system" for bootstrap. Minor implementation gap.

## Check-by-Check Summary

| # | Check | Result | Notes |
|---|---|---|---|
| 1 | Spec → Plan coverage | ✅ 17/17 | Zero orphans |
| 2 | Spec → Tasks coverage | ✅ 17/17 | Matches self-report |
| 3 | Spec → SC measurability | ✅ 10/10 | All have numeric thresholds or clear pass/fail |
| 4 | Plan → Tasks coverage | ✅ 6/6 components | Every component has setup + impl tasks |
| 5 | Contracts → Tasks coverage | ✅ 14/14 endpoints, 5/5 flows | Zero gaps |
| 6 | Tech-stack consistency | ✅ | bcrypt, HS256, ECDSA P-256, JSONL, config.json — all consistent across plan/tasks/contracts |
| 7 | Cross-spec consistency | ✅ | 001-init localhost API (`/api/*`) no clash with 002 (`/v1/*`); undevops plugin-sdk.md referenced; architecture.md drift noted and tracked |
| 8 | Dependency graph sanity | ✅ DAG | No cycles. Mermaid graph verified. Critical path = 10 tasks. |
| 9 | Phase ordering | ✅ | P1 stories: auth in P1, read endpoints in P2, mutations in P3. Correct ordering. |
| 10 | Edge cases coverage | ✅ 7/7 | All edge cases fully addressed. Network partition now aligned (synchronous 503, no queue mismatch). |
| 11 | Security invariants | ✅ | Loopback admin scope explicitly acknowledged in "Accepted Security Trade-offs" section; PAT scopes enforced via middleware; JWT TTL bounded; TLS required; audit captures mutations. No silent escalations. |
| 12 | Constitution alignment | ✅ | Plan §Constitution Check addresses VI (not yet applicable), VII (TODO_SNAPSHOT_SCRIPT, graceful degradation), VIII (architecture.md drift noted). Spec now has round 2 clarifications resolving all open questions. |

## VERDICT

**Status**: PASS

**Reason**: All 17 FRs, 14 endpoints, 5 auth flows, 7 edge cases fully traced through spec→plan→tasks with zero behavioral mismatches. All 3 major issues from v1 resolved. Zero `[NEEDS CLARIFICATION]` markers remain. Security trade-offs explicitly documented. 6 minor issues persist — all non-blocking, addressable during implementation.

**Critical issues**: 0
**Major issues**: 0
**Minor issues**: 6

**Recommendation**: Proceed to `/speckit.review` (external AI review). Minor issues can be tracked as implementation follow-ups or resolved inline during coding.

**Reviewer tag**: claude (via Hermes delegation)
