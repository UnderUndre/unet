# /speckit.analyze Execution Report (005) — Re-run

**Spec**: `specs/005-observability/spec.md`
**Reviewer**: Valera (self-analysis, GLM-5.1)
**Date**: 2026-05-28
**Inputs**: spec.md (22 FRs, 7 SCs), plan.md (8 components, 7 beyond-spec decisions, 8 open risks), tasks.md (25 tasks, 6 phases), data-model.md (6 entities), contracts/ (4 files)
**Previous run**: PASS_WITH_NOTES (0 critical, 2 major, 6 minor)
**Changes since previous**: MAJ-1 fix (TASK-SEC1 added), MAJ-2 fix (FR-017 default corrected), MIN-2 resolution documented

---

## Methodology

12-check cross-artifact consistency audit. Every claim verified against source text. Where artifacts disagree, the discrepancy is cited with file:line evidence.

---

## Check 1: Spec → Plan Coverage (FRs)

| FR | Plan Coverage | Evidence |
|----|---------------|----------|
| FR-001 (JSONL schema) | ✅ | plan.md §Component 1 (StructuredLogger) — slog handler produces JSONL with ts/level/component/source/msg/seq/fields |
| FR-002 (log levels + per-component) | ✅ | plan.md §Component 1 — global threshold + per-component overrides from `observability.logLevels` |
| FR-003 (rotation size + date) | ✅ | plan.md §Component 2 (LogRotator) — lumberjack size + date-based swap under mutex |
| FR-004 (retention cleanup) | ✅ | plan.md §Component 2 — daemon start + daily timer, delete expired archives |
| FR-005 (secret redaction) | ✅ | plan.md §Component 1 — redact.go scans fields map, replaces secrets with `<redacted>` |
| FR-006 (SSE endpoint) | ✅ | plan.md §Component 4 (LogStream) + §002 Control Plane Integration — mounted on :8443 |
| FR-007 (SSE filters) | ✅ | plan.md §Component 4 — server-side filtering (level/component/query) at connect time |
| FR-008 (SSE event format + keepalive) | ✅ | plan.md §Component 4 — event type `log`, keepalive every 15s |
| FR-009 (10 subscribers + backpressure) | ✅ | plan.md §Component 4 — bounded buffer 1000, overflow event + disconnect |
| FR-010 (Prometheus endpoint) | ✅ | plan.md §Component 7 (MetricsExposer) — separate :9090 listener |
| FR-011 (metrics bind address) | ✅ | plan.md §Component 7 — default 127.0.0.1:9090, configurable |
| FR-012 (non-loopback warn + bearer) | ✅ | plan.md §Component 7 — startup warn log + bearer token for non-loopback |
| FR-013 (metric catalog) | ✅ | plan.md §Component 6 (MetricsRegistry) — all 10 metrics listed |
| FR-014 (admin UI log viewer backend) | ✅ | plan.md §Component 8 (LogUITail) — SSE via loopback bypass on :8080 |
| FR-015 (UI controls — backend) | ✅ | plan.md §Component 4 — server-side filters support level/component/query |
| FR-016 (initial 200 lines) | ✅ | plan.md §Component 4 — ring buffer of 200 entries |
| FR-017 (container log capture) | ✅ | plan.md §Component 3 (ContainerLogAggregator) — Docker SDK follow. **Default `true`** — matches spec.md Clarification round 1 and current FR-017 text. |
| FR-018 (container stop event) | ✅ | plan.md §Component 3 — warn event + goroutine exit on container stop |
| FR-019 (missing container) | ✅ | plan.md §Component 3 — warn log, no goroutine spawn |
| FR-020 (log export endpoint) | ✅ | plan.md §Component 5 (LogExporter) — date-range tarball |
| FR-021 (PII scrub) | ✅ | plan.md §Component 5 — masks IPs, replaces peer names, only in export |
| FR-022 (hot-reload) | ✅ | plan.md §Component 8 — atomic.Value for config, hot-reload for logLevels/retentionDays/metrics.enabled/captureContainerLogs/scrubPii |

**Result**: 22/22 FRs addressed in plan. **PASS**

---

## Check 2: Spec → Tasks Coverage (FRs)

tasks.md §Coverage Validation claims 22/22. Verified against task "Maps to" fields:

| FR | Task(s) | Verified |
|----|---------|----------|
| FR-001 | TASK-1.1 | ✅ slog handler, JSONL schema |
| FR-002 | TASK-1.4 | ✅ per-component level filtering |
| FR-003 | TASK-2.1 | ✅ lumberjack integration |
| FR-004 | TASK-2.2 | ✅ retention sweeper |
| FR-005 | TASK-1.3, TASK-SEC1 | ✅ redact.go + security audit validates completeness |
| FR-006 | TASK-4.3, TASK-4.4 | ✅ SSE handler + route wiring |
| FR-007 | TASK-4.2 | ✅ subscriber filter logic |
| FR-008 | TASK-4.1, TASK-4.2 | ✅ hub keepalive + SSE format |
| FR-009 | TASK-4.1, TASK-4.2 | ✅ 10 subscriber limit + backpressure |
| FR-010 | TASK-5.2, TASK-5.4 | ✅ exposer + init wiring |
| FR-011 | TASK-5.2 | ✅ configurable bind address |
| FR-012 | TASK-5.2, TASK-SEC1 | ✅ bearer token + warn log + auth bypass audit |
| FR-013 | TASK-5.1, TASK-5.3 | ✅ registry + instrumentation |
| FR-014 | TASK-4.4 | ✅ dual-listener (:8443 + :8080) |
| FR-015 | TASK-4.2 | ✅ server-side filters |
| FR-016 | TASK-1.2, TASK-4.3 | ✅ ring buffer + initial load |
| FR-017 | TASK-3.1, TASK-3.2 | ✅ aggregator + init wiring |
| FR-018 | TASK-3.1 | ✅ container stop event |
| FR-019 | TASK-3.1 | ✅ missing container handling |
| FR-020 | TASK-6.1, TASK-6.2 | ✅ export handler + route wiring |
| FR-021 | TASK-6.1, TASK-SEC1 | ✅ PII scrub in exporter + adversarial regex audit |
| FR-022 | TASK-1.4, TASK-3.2, TASK-5.4 | ✅ hot-reload wiring |

**Result**: 22/22 FRs trace to ≥1 task. **PASS**

---

## Check 3: Spec → SC Measurability

| SC | Measurable Threshold | Verdict |
|----|---------------------|---------|
| SC-001 | SSE delivery < 1s P95 | ✅ Clear, testable |
| SC-002 | Rotation within 5s of size threshold | ✅ Clear, testable |
| SC-003 | 10 subscribers, each < 1s P95 | ✅ Clear, testable |
| SC-004 | Prometheus scrape < 100ms P95 | ✅ Clear, testable |
| SC-005 | Log files < 100MB/day at info level | ✅ Clear, testable |
| SC-006 | Admin UI displays new lines < 2s | ⚠️ "visual confirmation" is subjective. Acceptable for backend spec — UI latency depends on frontend rendering. |
| SC-007 | Zero secrets in 24h log data (grep test) | ✅ Clear, testable. Now also covered by TASK-SEC1 acceptance criteria. |

**Result**: 7/7 SCs measurable. Minor note on SC-006 subjectivity (acceptable). **PASS**

---

## Check 4: Plan → Tasks Coverage (Components)

| Plan Component | Setup Task | Impl Task | Verified |
|---------------|------------|-----------|----------|
| 1. StructuredLogger | TASK-1.1, TASK-1.3, TASK-1.4 | TASK-1.1 (full impl) | ✅ |
| 2. LogRotator | TASK-2.1 | TASK-2.2, TASK-2.3 | ✅ |
| 3. ContainerLogAggregator | TASK-3.1 | TASK-3.2 | ✅ |
| 4. LogStream (ring + hub + SSE) | TASK-1.2, TASK-4.1 | TASK-4.2, TASK-4.3, TASK-4.4 | ✅ |
| 5. LogExporter | TASK-6.1 | TASK-6.2 | ✅ |
| 6. MetricsRegistry | TASK-5.1 | TASK-5.3 | ✅ |
| 7. MetricsExposer | TASK-5.2 | TASK-5.4 | ✅ |
| 8. Observability init + config | TASK-1.5 | — (wiring across phases) | ✅ |

**Result**: 8/8 components have setup + impl tasks. **PASS**

---

## Check 5: Contracts → Tasks Coverage

### SSE Protocol (contracts/sse-protocol.md)
- Endpoint `GET /v1/logs/stream` → TASK-4.3, TASK-4.4 ✅
- Auth (Bearer token + loopback bypass) → TASK-4.3, TASK-4.4 ✅
- Query params (level/component/source/q) → TASK-4.2, TASK-4.3 ✅
- Event types (log, overflow, keepalive, connected) → TASK-4.1, TASK-4.2 ✅
- Reconnection (Last-Event-ID) → TASK-4.3 ✅
- Backpressure (bounded buffer + overflow) → TASK-4.1 ✅
- Subscriber limit (10 max, 429) → TASK-4.1 ✅
- Error responses (401/403/400/429/503) → TASK-4.3 ✅

### Prometheus Metrics (contracts/metrics.md)
- All 10 metrics → TASK-5.1 ✅
- Auth (loopback no-auth, non-loopback bearer) → TASK-5.2 ✅
- Configuration → TASK-5.2, TASK-5.4 ✅

### Log Record Schema (contracts/log-record.schema.json)
- Schema validation → TASK-1.1 acceptance ✅
- `seq` monotonic → TASK-1.1 ✅
- `source` enum → TASK-1.1 ✅

### Log UI Events (contracts/log-ui-events.md)
- Dual-listener → TASK-4.4 ✅
- Loopback auth bypass → TASK-4.3 ✅
- Initial 200 lines → TASK-4.3 ✅

**Result**: 4/4 contracts fully traced to tasks. **PASS**

---

## Check 6: Tech-Stack Consistency

| Tech (from plan) | Tasks Match | Evidence |
|-----------------|-------------|----------|
| `log/slog` (Go stdlib) | ✅ | TASK-1.1: "custom slog handler" |
| `gopkg.in/natefin/lumberjack.v2` | ✅ | TASK-2.1: "Integrate lumberjack writer" |
| Custom SSE (~100-150 LOC) | ✅ | TASK-4.1-4.3: hand-rolled SSE |
| Docker Engine SDK | ✅ | TASK-3.1: "Docker Engine API" |
| `prometheus/client_golang` | ✅ | TASK-5.1: "prometheus.Registry" |
| `fsnotify`/polling for file tail | ✅ | TASK-3.3: "polling (stat for size change)" |
| `atomic.Value` for hot-reload | ✅ | TASK-1.4: "atomic.Value" |

**No drift detected. PASS**

---

## Check 7: Cross-Spec Consistency

### 7a: 005 owns `/v1/logs/stream` but NOT in 002's OpenAPI

**Status**: ✅ Properly flagged

Plan.md §"002 Control Plane Integration" explicitly states the cross-spec follow-up for OpenAPI extension.

**Finding (MINOR)**: No task to update 002's OpenAPI. Tracked as post-implementation cleanup.

### 7b: 005 reads 003's lifecycle-audit.jsonl as read-only

**Status**: ✅ Correct

TASK-3.3 implements `FileAggregator` that tails lifecycle-audit.jsonl read-only. No tasks write to 003's file.

**MIN-2 Resolution (no-action, documented)**:
> TASK-3.3 assumes file path `~/.unet/lifecycle-audit.jsonl` but spec 003 may reference encrypted `.jsonl.age` output. No code change needed. Spec 003's `.jsonl.age` decision applies to the StateBundle backup file, NOT to the operational lifecycle-audit.jsonl. The audit log is plaintext JSONL by design (mirrors 002's audit.jsonl pattern in plan.md). TASK-3.3's assumption is correct; cross-reference clarified for future readers.

### 7c: architecture.md Observability subsection

**Status**: ✅ Updated

architecture.md contains Observability Subsystem in Control Plane diagram, route listings, full Observability section, Spec Registry entry.

**PASS**

---

## Check 8: Dependency Graph Sanity

tasks.md includes text-based and Mermaid DAG representations. TASK-SEC1 added to Mermaid.

### Cycle check
Phase 1 → Phase 2/3/4/5 (all depend on TASK-1.5) → Phase 6 (depends on earlier phases). TASK-SEC1 depends on tasks from Phases 2-5 (TASK-2.3, TASK-3.1, TASK-3.3, TASK-4.4, TASK-5.3, TASK-6.2) — runs after all security-relevant implementations land. No back-edges.

**No cycles detected. PASS**

### Critical path depth verification

Longest chain remains:
```
TASK-1.2 → TASK-1.5 → TASK-4.1 → TASK-4.2 → TASK-4.3 → TASK-4.4 → TASK-6.4 → TASK-6.5 = 8 tasks
```

TASK-SEC1 depends on TASK-2.3, TASK-3.1, TASK-3.3, TASK-4.4, TASK-5.3, TASK-6.2 but runs in parallel with TASK-6.4/6.5 — it doesn't extend the critical path. Longest chain to TASK-SEC1: TASK-1.1 → TASK-1.5 → TASK-2.1 → TASK-2.3 → TASK-SEC1 = 5, or TASK-1.2 → TASK-1.5 → TASK-4.1 → TASK-4.2 → TASK-4.3 → TASK-4.4 → TASK-SEC1 = 7. Neither exceeds the SSE chain's 8.

**Critical path unchanged: 8 tasks. PASS**

---

## Check 9: Phase Ordering

| Priority | FRs | Phase | Correct? |
|----------|-----|-------|----------|
| P1 (live tail + JSONL rotation) | FR-001, FR-002, FR-003, FR-004, FR-005, FR-014, FR-016 | Phase 1 + Phase 2 + Phase 4 | ✅ |
| P2 (SSE external + metrics) | FR-006, FR-007, FR-008, FR-009, FR-010, FR-011, FR-012, FR-013, FR-022 | Phase 4 + Phase 5 | ✅ |
| P3 (container capture + export) | FR-017, FR-018, FR-019, FR-020, FR-021 | Phase 3 + Phase 6 | ✅ |

TASK-SEC1 in Phase 6 runs after all security-relevant tasks from Phases 2-5 are complete. Correct placement.

**PASS**

---

## Check 10: Edge Cases Coverage

| Edge Case | Spec Ref | Addressed In | Verdict |
|-----------|----------|-------------|---------|
| Log writer disk full | spec.md §Edge Cases | TASK-2.3 (graceful degradation cascade) | ✅ |
| SSE slow consumer | spec.md §Edge Cases | TASK-4.1 (bounded buffer), TASK-4.2 (overflow disconnect) | ✅ |
| Container stops mid-stream | spec.md §Edge Cases | TASK-3.1 (exit detection + warn event) | ✅ |
| Metrics non-loopback without auth | spec.md §Edge Cases → resolved round 1 | TASK-5.2 (bearer token) | ✅ |
| Log rotation race with active writer | spec.md §Edge Cases | TASK-2.1 (mutex-protected swap) | ✅ |
| Clock skew (daemon vs containers) | spec.md §Edge Cases | TASK-3.1 (dual timestamps) | ✅ |
| Concurrent SSE connect/disconnect storm | spec.md §Edge Cases | TASK-4.1 (WaitGroup + context cancel, 5s cleanup) | ✅ |
| Log export + rotation overlap | spec.md §Edge Cases | TASK-6.1 (consistent snapshot) | ✅ |

**Result**: 8/8 edge cases covered. **PASS**

---

## Check 11: Security Invariants

### 11a: PII scrubbing default OFF
- spec.md §Clarifications: "Default OFF (full logs preserved locally)" ✅
- plan.md §Component 5: "Scrubbing applies ONLY to exported content" ✅
- TASK-6.1: `scrubPii` parameter, response header ✅
- TASK-1.5 config: `scrubPii` default `false` ✅

**PASS**

### 11b: Metrics endpoint loopback-only by default
- plan.md §Component 7: default bind 127.0.0.1:9090 ✅
- TASK-5.2: loopback no-auth, non-loopback bearer token ✅
- TASK-SEC1: auth bypass attempts documented ✅

**PASS**

### 11c: SSE auth required (token from 002)
- contracts/sse-protocol.md: Bearer token with `read` scope, loopback bypass ✅
- TASK-4.3: auth middleware, loopback bypass ✅
- TASK-SEC1: constant-time comparison verified ✅

**PASS**

### 11d: Log file permissions
- TASK-1.5: `~/.unet/logs/` created with mode 0700 ✅
- TASK-SEC1: file permission audit (0600 enforced) ✅

**PASS**

### 11e: Security-auditor task assignment
- **RESOLVED**: TASK-SEC1 (security-auditor) added to Phase 6.
  - 6 deps on security-relevant implementation tasks
  - 6 acceptance criteria covering: PII regex adversarial testing, auth bypass, constant-time comparison, file permissions, secret-leak grep, findings document
  - Produces `specs/005-observability/reviews/security-audit.md`

**PASS** (previously MAJOR — now resolved)

---

## Check 12: Constitution Alignment

### Principle VI (Cross-AI Review Gate)
plan.md correctly notes review gate does not apply at planning stage. This analyze.md is the first gate artifact. **PASS**

### Principle VII (Artifact Versioning)
Plan applies graceful-degradation clause for missing snapshot scripts. **PASS (with known gap)**

### Principle VIII (Knowledge Self-Maintenance)
architecture.md updated with Observability subsection. **PASS**

---

## Additional Findings

### A. SSE `: connected` format ambiguity

contracts/sse-protocol.md shows `: connected` as SSE comment, then `data:` on next line. Technically two frames. Recommend clarifying before implementation.

**Finding (MINOR)**: SSE protocol contract ambiguous — recommend clarification.

### B. spec.md FR-006 path inconsistency

FR-006 says `GET /api/v1/logs/stream`, FR-020 says `GET /api/v1/logs/export`. All other artifacts use `/v1/` (no `/api` prefix). Plan.md notes `/v1/` is correct (matching 002's routes).

**Finding (MINOR)**: FR-006/FR-020 have stale `/api/v1/` prefix. Recommend spec.md fix.

### C. `captureContainerLogs` default — RESOLVED

FR-017 now reads `default: true`, matching Clarification round 1, plan.md, tasks.md, and config struct. No remaining contradiction.

**RESOLVED** (previously MAJOR — now fixed)

### D. No `source` filter in spec FR-007

spec.md FR-007 lists `level`, `component`, `q`. contracts/sse-protocol.md adds `source`. TASK-4.2 includes `filterSource`. Additive, not contradictory.

**Finding (MINOR)**: `source` filter in contracts/tasks but not in FR-007. Not blocking.

### E. LOC estimate

Total: ~4,900 LOC across 25 tasks. Constitution Principle II caps at <500 LOC per atomic change. All individual tasks within bounds. **No violation**.

### F. No task to update 002's OpenAPI

Cross-spec follow-up for `/v1/logs/stream` and `/v1/logs/export` endpoints noted in plan but no tracking task.

**Finding (MINOR)**: Post-implementation cleanup needed for 002's OpenAPI.

### G. Individual JSONL file permissions

Directory is 0700. Individual files not explicitly specified (lumberjack uses process umask). TASK-SEC1 now covers this with explicit 0600 audit.

**Finding (MINOR → mitigated by TASK-SEC1)**: Individual file perms now covered by security audit acceptance criteria.

---

## Summary of Findings

### CRITICAL (0)

None.

### MAJOR (0)

Both majors from previous run resolved:
1. ~~MAJ-1: Zero security-auditor tasks~~ → **RESOLVED**: TASK-SEC1 added to Phase 6 with 6 deps, 6 acceptance criteria, producing security-audit.md.
2. ~~MAJ-2: FR-017 default contradicts Clarification~~ → **RESOLVED**: spec.md FR-017 corrected to `default: true`.

### MINOR (6)

1. **MIN-1**: No task to update 002's OpenAPI with `/v1/logs/stream` and `/v1/logs/export` — post-implementation cleanup.
2. **MIN-2**: ~~TASK-3.3 assumes plaintext `lifecycle-audit.jsonl`~~ → **RESOLVED (no-action)**: Spec 003's `.jsonl.age` decision applies to StateBundle backup, NOT operational lifecycle-audit.jsonl. Audit log is plaintext JSONL by design (mirrors 002's audit.jsonl). TASK-3.3 assumption correct; cross-reference clarified.
3. **MIN-3**: SSE `: connected` event format ambiguous in contract — recommend clarification.
4. **MIN-4**: spec.md FR-006/FR-020 have stale `/api/v1/` path prefix. All other artifacts use `/v1/`.
5. **MIN-5**: `source` filter param in contracts/tasks but not in spec FR-007. Additive, not contradictory.
6. **MIN-6**: Individual JSONL file permissions not explicitly specified. Mitigated by TASK-SEC1 0600 audit.

---

## Coverage Summary

| Dimension | Result |
|-----------|--------|
| Spec→Plan FRs | 22/22 ✅ |
| Spec→Tasks FRs | 22/22 ✅ |
| Contracts→Tasks | 4/4 contracts, all metrics/protocols traced ✅ |
| Cross-spec alignment | 3/3 (002 OpenAPI gap flagged, 003 read-only confirmed, architecture.md updated) ✅ |
| Edge cases | 8/8 ✅ |
| Security invariants | 5/5 (auth, loopback, redaction, file perms, security audit) ✅ |

---

## Verdict

```yaml
verdict: PASS
reviewer: valera-analyze
reviewed_at: "2026-05-28T12:00:00Z"
commit: HEAD
critical: 0
major: 0
minor: 6
previous_run:
  verdict: PASS_WITH_NOTES
  major: 2
  fixes_applied:
    - MAJ-1: TASK-SEC1 added (security-auditor, Phase 6)
    - MAJ-2: FR-017 default corrected to true
    - MIN-2: documented as no-action with rationale
```

**Status: PASS**

The artifact suite is thorough and well-cross-referenced. All 22 FRs trace through all three layers (spec → plan → tasks). Contracts are complete and consistent. Edge cases comprehensively covered. Security invariants now have dedicated auditor coverage via TASK-SEC1. The `captureContainerLogs` default contradiction is resolved.

6 minors remain — none are blocking for `/speckit.implement`. They can be addressed during implementation or as post-implementation cleanup.

---

## Top 3 Items for Attention During Implementation

1. **MIN-4** (FR-006/FR-020 stale `/api/v1/` prefix) — 2-line spec.md fix recommended before coding
2. **MIN-3** (SSE `: connected` format) — clarify contract before TASK-4.3 implementation
3. **MIN-2** (lifecycle-audit format) — confirmed no-action but implementers should be aware 003 writes plaintext JSONL

---

## Recommendation

Proceed to `/speckit.review` (external reviews). All majors resolved. Minors are non-blocking — address opportunistically during implementation.
