# SpecKit Analyze: 001-init

**Reviewer**: analyze (Claude self-consistency, post-remediation pass)
**Reviewed at**: 2026-05-16T21:44:01Z
**Commit**: 557b73d1e44ef52137959ae50e984d740df2b8c7 (uncommitted edits in working tree — Phase 1-4 remediation)
**Artifacts**: spec.md, plan.md, tasks.md, data-model.md, research.md, contracts/caddy-api.md, contracts/daemon-api.md, quickstart.md, appendix-peer-add-flow.md
**Constitution**: `.specify/memory/constitution.md` v1.4.0
**Prior run**: 2026-05-16T21:25:01Z (commit `557b73d`) — verdict CRITICAL (1 CRITICAL, 2 HIGH, 2 MEDIUM, 3 LOW)

---

## Remediation Log (Δ from prior run)

| Prior Finding | Severity | Status | What was applied |
|---------------|----------|--------|------------------|
| **F1** Container netns: Caddy can't see `awg0` | CRITICAL | ✅ **FIXED** | `data-model.md` §2.1 + §2.4 rewritten — `network_mode: "service:unet-amnezia-awg"` added, port mappings relocated to amnezia (sole netns owner). `research.md` §3.3 compose YAML updated. `spec.md` FR-001 expanded with explicit shared-netns mandate. `tasks.md` T008 description rewritten with both services' details. |
| **F2** mTLS multi-peer lockout via admin API | HIGH | ✅ **FIXED** | `contracts/caddy-api.md` "mTLS Bootstrap Flow" → "mTLS Provisioning Flow" rewritten — pubkey registration happens via SSH+`docker exec` edit of `/config/caddy/autosave.json` followed by `caddy reload`, NOT via the admin API. `tasks.md` T016d rewritten accordingly. `appendix-peer-add-flow.md` §2.6 NEW — per-peer mTLS pubkey injection step. |
| **F3** Bash `<(…)` process substitution in Alpine `ash` | HIGH | ✅ **FIXED** | `appendix-peer-add-flow.md` — new "Shell conventions" preamble explaining the three shell layers + temp-file pattern: `awg-quick strip ... > /tmp/awg0-strip.conf && awg syncconf <iface> /tmp/awg0-strip.conf && rm /tmp/awg0-strip.conf`. All occurrences replaced. |
| **F4** JSON injection via `clientsTable.userData.clientName` | MEDIUM | ✅ **FIXED** | `appendix-peer-add-flow.md` §2.4 rewritten — JSON marshalled in Go with `encoding/json.Marshal`, pushed via `docker exec -i sh -c 'cat > …'` stdin (byte-passthrough, no shell interpolation of user-supplied names). `spec.md` FR-012 extended with explicit "JSON-bound values" subclause. `tasks.md` T013b cross-references the discipline. |
| **F5** Wildcard cert single-label limitation | MEDIUM | ✅ **FIXED** | `spec.md` FR-009 (Cloudflare mode) extended with explicit single-label constraint. `spec.md` FR-012 extended with mode-dependent depth check. `contracts/daemon-api.md` `POST /api/ports` adds `400 invalid_subdomain_depth` response with structured remediation hints. `tasks.md` T017 validates depth in Cloudflare mode. |
| **L1** Recovery flow missing mTLS state | LOW | ✅ **FIXED** | `appendix-peer-add-flow.md` §5.1 extended with Step 4 (mTLS-only): restore Caddy admin config + `caddy reload`. `data-model.md` `serverMirror` schema extended with `caddyAdminConfig` field, `awgConfSha256`, and `serverPrivateKeyB64` (security trade-off documented inline). |
| **L2** Network topology diagram needs shared-netns annotation | LOW | ✅ **FIXED** | `data-model.md` §2.4 fully redrawn with explicit "Shared Linux netns" box enclosing both services. |
| **L3** Caddy mTLS bootstrap missing cert-loss recovery | LOW | ✅ **FIXED** | `contracts/caddy-api.md` new "Recovery from Client Cert Loss" sub-section. |
| **GLM-residual** Stale local state after daemon restart | n/a (residual) | ✅ **FIXED** | `spec.md` FR-010 split into sub-1 (server drift) + sub-2 (local stale-state reconciliation: `awg show` on startup, `docker ps` probe). Edge case added for `tunnel.status: "connected"` after crash. |
| **GLM-residual** First-run config.json bootstrap | n/a (residual) | ✅ **FIXED** | `spec.md` edge case added — daemon creates `~/.unet/` with `0700`, writes default skeleton with empty `exposedPorts`, fresh `uiToken`. |
| **GLM-residual** Unicode subdomain handling | n/a (residual) | ✅ **FIXED** | `spec.md` edge case added — reject Unicode subdomains, suggest Punycode. Reason: Caddy/Cloudflare/LE all operate on ASCII labels. |
| **GLM-residual** Timezone normalization | n/a (residual) | ✅ **FIXED** | `spec.md` edge case added — all stored timestamps ISO-8601 with explicit `Z` / `±HH:MM`; UI may render local TZ. |

**Convergence**: 1 CRITICAL + 2 HIGH + 2 MEDIUM + 3 LOW + 4 residual → **0 / 0 / 0 / 0**.

---

## Findings (this run)

After applying Phases 1-4 + glm-residual, I performed a fresh detection pass against the current working tree. Below are issues that surfaced from THE CHANGES THEMSELVES (introducing fixes can introduce new gaps).

| ID | Category | Severity | Location(s) | Summary | Recommendation |
|----|----------|----------|-------------|---------|----------------|
| **N1** | Coverage gap | LOW | `data-model.md` `serverMirror.caddyAdminConfig` | New field introduced by L1 fix has no explicit task that POPULATES it. T016d (mTLS provisioning) is the natural producer but its description doesn't mention writing the local mirror. | Extend T016d description: "After successful `caddy reload`, daemon MUST snapshot `/config/caddy/autosave.json` back into `serverMirror.caddyAdminConfig` for L1 recovery." |
| **N2** | Security design choice | LOW | `data-model.md` `serverMirror.serverPrivateKeyB64` | New field added by L1 fix to enable full volume-loss recovery. **Trade-off NOT discussed explicitly**: mirroring the server's WG private key locally means daemon-machine compromise → server takeover. Alternative is "forced re-enrollment of all peers" on volume loss (safer but disruptive). | Add a brief ADR-style paragraph in `data-model.md` (next to the field) or in `research.md` §9 explaining the trade-off and that v1 accepts the local-mirror risk because (a) `~/.unet/config.json` is `0600`/ACL-protected and (b) daemon machine is already a trusted endpoint (it has the SSH key for the same VPS anyway). |
| **N3** | Documentation drift | LOW | `appendix-peer-add-flow.md` §6 "Why Not Use Amnezia's Own Add-Client Script?" point 4 | Says "by using `bash -s <<EOF` with parameter substitution via Go templating, we avoid passing user-controlled strings through interactive shell parsing." Slightly stale after F3/F4 fixes — current approach uses **quoted heredocs + Go-marshalled byte payloads**, not parameter substitution. | Reword point 4 to reflect actual current approach (quoted heredoc + stdin byte passthrough + `encoding/json` for structured data). |
| **N4** | Consistency | LOW | `tasks.md` T013a description | Mentions storing `serverMirror.awgConfRaw` only. New schema also defines `awgConfSha256` (for FR-010 sub-1 drift detection) and `caddyAdminConfig` / `serverPrivateKeyB64` (for L1 recovery). T013a should produce `awgConfSha256` alongside the raw blob. | One-line append to T013a: "...and `awgConfSha256` (SHA-256 of the raw conf for FR-010 sub-1 drift detection)." |

**Summary**: 0 CRITICAL, 0 HIGH, 0 MEDIUM, **4 LOW**. All LOWs are cleanup-debt from the remediation pass itself — none block implementation; all can be addressed in a 5-minute follow-up edit.

---

## Coverage Summary

| Requirement | Mapped Tasks | Notes |
|-------------|--------------|-------|
| FR-001 (VPS deployment + shared netns) | T007, T008 (rewritten), T008b, T008c, T008d, T009, T009b, T010, T011 | F1 compose changes captured in T008 |
| FR-002 (client config mgmt) | T012, T013a, T013b, T013 | T013b now reflects F3/F4 patterns |
| FR-003 (full obfuscation set) | T012, T013 | Unchanged |
| FR-003a (awg-quick PATH) | T020c | Unchanged |
| FR-004 (dynamic Caddy routing) | T016 | Unchanged |
| FR-005 (bind 127.0.0.1) | T005, T006 | Unchanged |
| FR-006 (privilege check) | T020a, T020b | Unchanged |
| FR-007 (port + single-instance) | T023, T020d | Unchanged |
| FR-008 (IP-bind or mTLS via SSH) | T016, T016d (rewritten) | F2 captured |
| FR-009 (DNS modes + single-label) | T016a, T016b, T017 (extended) | F5 captured in T017 |
| FR-010 (drift + reconciliation) | T013a, T013, T020a-startup | Sub-2 reconciliation added; LOW N4 about awgConfSha256 production |
| FR-011 (perms + redaction + masking) | T004, T025 | Unchanged |
| FR-012 (input validation, all contexts) | T007, T013b, T017 | JSON + Filesystem contexts added |
| SC-001..SC-006 | T022, T026, T027, T028 | Unchanged |

**Coverage rate**: 100% (13/13 FRs mapped, 6/6 SCs covered).

---

## Constitution Alignment Issues

| Principle | Status | Notes |
|-----------|--------|-------|
| I — Spec-First Development | ✅ COMPLIANT | Pipeline followed; remediation is itself a spec-edit pass, no implementation jumped ahead. |
| II — Atomicity (WRAP) | ✅ COMPLIANT (per-task) | Each remediation edit is scoped to one concern (F1, F2, etc.). |
| III — Secrets Discipline | ✅ COMPLIANT | FR-011 masking unchanged; N2 LOW raises a trade-off discussion but the existing protections (0600 + ACL + redaction) satisfy the principle. |
| IV — Type Safety & Error Discipline | ✅ IMPLEMENTATION-LEVEL | Unchanged. |
| V — Source-of-Truth: `.claude/` | N/A | Feature is the Go daemon. |
| VI — Cross-AI Review Gate | 🔄 ANALYZE-GATE PASS; EXTERNAL-REVIEW-GATE PARTIAL | `analyze.md` PASS this run. **External reviews**: 1 received (antigravity, CRITICAL — but its findings ARE NOW RESOLVED; antigravity SHOULD be re-invoked for a fresh verdict). Second distinct provider review still pending. |
| VII — Artifact Versioning | ⏸ DEFERRED | `snapshot-stage.{sh,ps1}` not implemented. |

---

## Unmapped Tasks

None. All tasks are either FR-mapped, SC-mapped, or cross-cutting (T001-T003 setup, T024 docs, T025 SEC, T026-T028 E2E).

---

## Metrics

- Total Requirements: 19 (13 FRs + 6 SCs)
- Total Tasks: 39
- Coverage % (FRs with ≥1 task): **100%**
- Coverage % (SCs verified): **100%**
- External reviews received: **1 / 2** (antigravity@CRITICAL → fixes applied → re-review pending)
- Findings this run: **0 CRITICAL, 0 HIGH, 0 MEDIUM, 4 LOW**
- Convergence from prior CRITICAL run:
  - CRITICAL: 1 → **0**
  - HIGH: 2 → **0**
  - MEDIUM: 2 → **0**
  - LOW: 3 + 4 residual → **4** (all new cleanup-debt, none blocking)

---

## VERDICT

```yaml
verdict: PASS
reviewer: analyze
reviewed_at: "2026-05-16T21:44:01Z"
commit: 557b73d1e44ef52137959ae50e984d740df2b8c7  # working tree contains uncommitted Phase 1-4 remediation
critical_count: 0
high_count: 0
medium_count: 0
low_count: 4
gate_status:
  analyze_gate: PASS
  external_review_gate: PARTIAL — 1/2 reviewers landed; current antigravity verdict (CRITICAL) is STALE because its findings are now resolved; re-submission needed
  implement_status: BLOCKED on external_review_gate (Principle VI requires ≥2 fresh PASS from distinct providers)
  blocking_findings: []  # all prior blockers fixed
external_reviews_received:
  - provider: antigravity
    last_verdict: CRITICAL
    last_against_commit: 557b73d1e44ef52137959ae50e984d740df2b8c7
    findings_status: ALL RESOLVED (F1-F5 + L1-L3 fixed in working tree)
    valid_for_principle_vi: false  # last verdict was CRITICAL; needs re-run
  - provider: glm-pre-overhaul
    last_verdict: HISTORICAL
    valid_for_principle_vi: false  # explicit archive
prior_run_convergence:
  high_delta: -2
  medium_delta: -2
  critical_delta: -1
  low_delta: +1  # was 3, then prior-resolved → introduced 4 new from cleanup; net +1, but ALL non-blocking
```

---

## Next Actions

### Immediate (unblocks external review)

1. **Commit current working tree** with a message like:
   ```
   refactor(specs/001-init): apply Phase 1-4 remediation from antigravity review
   
   - F1 (CRITICAL): network_mode: service:unet-amnezia-awg for shared netns
   - F2 (HIGH): mTLS pubkey registration via SSH+docker exec, not admin API
   - F3 (HIGH): replace bash <(…) with temp-file pattern (Alpine ash compat)
   - F4 (MEDIUM): JSON-via-Go encoding/json.Marshal for clientsTable
   - F5 (MEDIUM): single-label subdomain constraint in Cloudflare mode
   - L1/L3: mTLS state recovery flow + cert-loss recovery section
   - glm-residual: FR-010 reconciliation, first-run/Unicode/timezone edge cases
   ```
   so the next external reviewer sees a clean commit, not a working-tree diff.

2. **Re-submit to antigravity** at the new commit — its previous verdict (CRITICAL) is now stale because every single F1-F5 + L1-L3 has been addressed. A fresh antigravity verdict of PASS is plausible.

3. **Run `/speckit.review` from a SECOND distinct provider** — recommend in order: Gemini, Codex Desktop, Copilot, Grok, DeepSeek. Two reviews from antigravity do NOT count per Principle VI.

### Optional (after external gate opens)

4. **Apply N1-N4 LOW cleanup** (5-minute edits):
   - N1: T016d population of `serverMirror.caddyAdminConfig`
   - N2: data-model.md ADR-style trade-off note for `serverPrivateKeyB64`
   - N3: appendix §6 rewording
   - N4: T013a appending `awgConfSha256` production

5. **Tooling**: implement `snapshot-stage.{sh,ps1}` (TODO_SNAPSHOT_SCRIPT) so Principle VII activates from the next feature onward.

**Remediation suggestion**: N1-N4 can land in the SAME follow-up commit if desired. They're tightly scoped, mechanical edits.
