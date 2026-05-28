# /speckit.analyze Execution Report (006)

**Spec**: `specs/006-peer-onboarding/spec.md`
**Reviewer**: analyze (self-consistency, same AI — Constitution Principle VI gate requires ≥2 external reviewers after this)
**Date**: 2026-05-28
**Artifacts reviewed**: spec.md, plan.md, tasks.md, data-model.md, contracts/wizard-api.md, contracts/invite-protocol.md, contracts/qr-deeplink.md, contracts/wizard-state-machine.md

---

## 1. Spec → Plan Coverage (Check 1)

**14/14 FRs addressed.**

| FR | Plan Section | Evidence |
|----|-------------|----------|
| FR-001 (wizard flow + steps) | Component 1 (WizardOrchestrator) + Component 10 (WizardUI) | `plan.md:§Component 1` — session lifecycle, step dispatch, commit. `plan.md:§Data Flow` — happy path covers all 8 steps. |
| FR-002 (state persistence + resume) | Component 1 + Migration section | `plan.md:§Wizard State Persistence` — `~/.unet/wizard-state.json`, atomic write, resume from last step. |
| FR-003 (SSH validation) | Component 2 (SSHValidator) | `plan.md:§Component 2` — TCP connect + SSH auth + `sudo docker ps`. Returns `ssh_connection_refused`, `ssh_auth_failed`, `ssh_no_sudo`, `ssh_no_docker`, `ssh_passphrase_protected`. |
| FR-004 (VPS preflight) | Component 3 (DistroPreflight) | `plan.md:§Component 3` — distro check, disk, sudo, docker, port availability. |
| FR-005 (domain validation) | Component 4 (DomainValidator) | `plan.md:§Component 4` — A-record lookup, Cloudflare NS detection, TLS feasibility. |
| FR-006 (QR code generation) | Component 7 (QRGenerator) | `plan.md:§Component 7` — `skip2/go-qrcode`, 256×256, level M, all AmneziaWG params. |
| FR-007 (QR + copyable + download) | Component 10 (WizardUI) + Component 7 | `plan.md:§Component 10` — QRDisplay.tsx renders QR + copyable text + download .conf. `contracts/qr-deeplink.md:§Fallback` covers all platforms. |
| FR-008 (one-click expose) | Component 9 (OneClickPublisher) | `plan.md:§Component 9` — atomic route+DNS creation, rollback on failure, `svc-<random-4>`. |
| FR-009 (nip.io auto-subdomain) | Component 6 (NipIoFallback) | `plan.md:§Component 6` — `<label>.<wg-ip-dashed>.nip.io`, skip DNS steps. |
| FR-010 (Cloudflare DNS-01) | Component 5 (CloudflareIntegrator) | `plan.md:§Component 5` — token validation, zone lookup, scope check, wildcard cert. |
| FR-011 (peer creation auto-keys) | Cross-Component Integration (002) | `plan.md:§002 Control Plane` — in-process call to peer handler, user provides only name. |
| FR-012 (invite links HMAC) | Component 8 (InviteLinkManager) | `plan.md:§Component 8` — HMAC-SHA256, AES-256-GCM encrypted blob, JSONL store. |
| FR-013 (invite landing page) | Component 8 | `plan.md:§Component 8` + `contracts/invite-protocol.md` — validate → consume → QR + download + OS detection. |
| FR-014 (wizard undo/back-nav) | Component 1 + 10 | `plan.md:§Component 1` — back-navigation before commit, disabled after. `wizard-state-machine.md:§Transitions` — explicit back transitions for all pre-commit steps. |

**Verdict**: PASS. All 14 FRs mapped to plan components with specific section references.

---

## 2. Spec → Tasks Coverage (Check 2)

**14/14 FRs traced to ≥1 task.**

Verified against `tasks.md:§Coverage Validation → FRs Covered: 14/14` table. Independent verification:

| FR | Tasks | Verified |
|----|-------|----------|
| FR-001 | TASK-1.1, 1.3, 1.4, 4.1, 4.2, 6.2 | ✅ 1.1=reducer, 1.3=HTTP routes, 1.4=step dispatch, 4.1=wizard shell, 4.2=step components, 6.2=commit |
| FR-002 | TASK-1.2, 4.1 | ✅ 1.2=persistence, 4.1=resume in frontend |
| FR-003 | TASK-1.4 | ✅ SSH validator in steps.go |
| FR-004 | TASK-2.1, 2.2 | ✅ 2.1=preflight package, 2.2=wire into handler |
| FR-005 | TASK-3.1, 3.2 | ✅ 3.1=DNS lookup, 3.2=CF token |
| FR-006 | TASK-5.1, 5.2, 4.3 | ✅ 5.1=QR gen, 5.2=HTTP handler, 4.3=display |
| FR-007 | TASK-4.3, 5.2 | ✅ 4.3=copyable+download UI, 5.2=config text in response |
| FR-008 | TASK-6.1 | ✅ one-click expose endpoint |
| FR-009 | TASK-3.3, 6.1 | ✅ 3.3=nip.io routing, 6.1=nip.io subdomain in expose |
| FR-010 | TASK-3.2 | ✅ CF token validation + scope check |
| FR-011 | TASK-6.2 | ✅ commit orchestrator calls 002 peer handler |
| FR-012 | TASK-5.3, 5.4 | ✅ 5.3=HMAC+AES-GCM, 5.4=handlers |
| FR-013 | TASK-5.4 | ✅ landing page + download handler |
| FR-014 | TASK-1.1, 4.1 | ✅ 1.1=reducer back-transitions, 4.1=back button |

**Verdict**: PASS.

---

## 3. Spec → SC Measurability (Check 3)

| SC | Measurable? | Notes |
|----|------------|-------|
| SC-001 (wizard <5min) | ✅ | Wall-clock time, automatable E2E test. |
| SC-002 (QR scan <30s) | ⚠️ | Manual measurement — requires physical device + QR scan. Not fully automatable. |
| SC-003 (one-click <3s) | ✅ | `POST /v1/routes/expose` latency, automatable. |
| SC-004 (95% completion without docs) | ⚠️ | **Aspirational only** — telemetry opt-in OFF per clarification round 1. Cannot be measured without opt-in data. Spec explicitly notes this. Acceptable. |
| SC-005 (invite consumed once, zero leaks) | ✅ | Automated test in TASK-7.2 — expired/consumed invite returns no config. |
| SC-006 (BYO-domain precheck catches DNS errors) | ✅ | TASK-3.1 validates A-record, TASK-7.2 tests mismatch cases. |
| SC-007 (wizard resume at any step) | ✅ | TASK-7.2 tests resume after interruption at each step. |
| SC-008 (zero terminal requirements) | ✅ | E2E test via Playwright — no CLI interaction needed. |

**Verdict**: PASS with NOTE. SC-002 and SC-004 are not fully automatable; spec acknowledges this. No blocking issue.

---

## 4. Plan → Tasks Coverage (Check 4)

**10/10 components have impl tasks.**

| Component | Task(s) | Verified |
|-----------|---------|----------|
| 1. WizardOrchestrator | TASK-1.1, 1.3, 1.4, 6.2 | ✅ |
| 2. SSHValidator | TASK-1.4 | ✅ |
| 3. DistroPreflight | TASK-2.1 | ✅ |
| 4. DomainValidator | TASK-3.1, 3.2 | ✅ |
| 5. CloudflareIntegrator | TASK-3.2 | ✅ |
| 6. NipIoFallback | TASK-3.3 | ✅ |
| 7. QRGenerator | TASK-5.1, 5.2 | ✅ |
| 8. InviteLinkManager | TASK-5.3, 5.4 | ✅ |
| 9. OneClickPublisher | TASK-6.1 | ✅ |
| 10. WizardUI | TASK-4.1, 4.2, 4.3, 4.4 | ✅ |

**Verdict**: PASS.

---

## 5. Contracts → Tasks Coverage (Check 5)

### 5a. wizard-api.md endpoints → tasks

wizard-api.md defines **11 endpoints**. Verified:

| Endpoint | Task | In wizard-api.md? |
|----------|------|--------------------|
| `POST /v1/wizard/sessions` | TASK-1.3 | ✅ (line 21-53) |
| `GET /v1/wizard/sessions/{id}` | TASK-1.3 | ✅ (line 56-94) |
| `DELETE /v1/wizard/sessions/{id}` | TASK-1.3 | ✅ (line 98-108) |
| `POST /v1/wizard/sessions/{id}/steps/{step}` | TASK-1.3, 1.4 | ✅ (line 112-207) |
| `POST /v1/wizard/sessions/{id}/preflight` | TASK-2.2 | ✅ (line 210-258) |
| `POST /v1/wizard/sessions/{id}/commit` | TASK-6.4 | ✅ (line 261-305) |
| `POST /v1/peers/{peerId}/qr` | TASK-5.2 | ✅ (line 309-343) |
| `POST /v1/peers/{peerId}/invite` | TASK-5.4 | ✅ (line 346-397) |
| `GET /invite/{peerId}` | TASK-5.4 | ✅ (line 400-466) |
| `GET /invite/{peerId}/download` | TASK-5.4 | ✅ (line 469-478) |
| `POST /v1/routes/expose` | TASK-6.1 | ✅ (line 482-532) |

tasks.md §Endpoints now correctly states **11/11** with note explaining the 3 discovered endpoints beyond the original 8 core wizard surface.

### 5b. invite-protocol.md → tasks

- HMAC URL generation (§Mode 1): TASK-5.3 ✅
- Short-code generation (§Mode 2): TASK-5.4 ✅
- AES-256-GCM config encryption (§Config Blob Encryption): TASK-5.3 ✅
- Invite store JSONL format (§Invite Store Format): TASK-5.3 ✅
- Rate limiting (§Rate Limiting): TASK-5.4 ✅
- Consumption semantics (§Consumption Semantics): TASK-5.4 ✅

### 5c. qr-deeplink.md → tasks

- QR generation params: TASK-5.1 ✅
- Deeplink URI `wireguard://import?config=`: TASK-5.1 ✅
- Fallback manual import instructions: TASK-4.3 ✅
- Landing page behavior (3 scenarios): TASK-5.4 ✅
- OS detection: TASK-5.4 ✅
- Download links per OS: TASK-5.4 ✅

### 5d. wizard-state-machine.md → tasks

- 11 states: TASK-1.1 ✅ (WizardStep enum)
- ~20 transitions: TASK-1.1 ✅ (table-driven tests for all)
- Back-navigation guards: TASK-1.1 ✅
- Resume logic: TASK-1.2 ✅
- React reducer mirror: TASK-4.1 ✅

**Verdict**: PASS.

---

## 6. Tech-Stack Consistency (Check 6)

| Tech choice | Plan says | Tasks say | Consistent? |
|-------------|----------|----------|-------------|
| QR library | `skip2/go-qrcode` | TASK-5.1: `github.com/skip2/go-qrcode` | ✅ |
| State machine | `useReducer` (React) | TASK-4.1: `useReducer` | ✅ |
| Peer creation | In-process call | TASK-6.2: in-process handler call | ✅ |
| Invite config encryption | AES-256-GCM | TASK-5.3: AES-256-GCM with 12-byte nonce | ✅ |
| 004 Notifier | No-op interface | TASK-6.2: (not imported — commit orchestrator doesn't mention notifier) | ⚠️ See MINOR-1 |
| Cloudflare client | `cloudflare-go` | TASK-3.2: `github.com/cloudflare/cloudflare-go` | ✅ |
| Logging | `slog` (via 005) | TASK-6.3: `slog.Info` | ✅ |
| SSH pool | spec 003 `internal/ssh/` | TASK-1.4: spec 003 SSH pool (interface) | ✅ |

**MINOR-1**: Plan §Component 9 mentions 004 Notifier no-op interface, but no task explicitly creates this interface. TASK-6.2 (commit orchestrator) doesn't mention notification emission at all. The plan says "commit success should trigger a system tray notification" but this is a SHOULD, not a MUST. Plan correctly notes spec 004 is not yet planned and the notifier is a future injection. No task is needed for the no-op — it's implementation detail within TASK-6.2. Low risk.

**Verdict**: PASS.

---

## 7. Cross-Spec Consistency (Check 7) — HIGH PRIORITY

### 7a. 003 bootstrap reuse

**Claim**: Wizard calls `bootstrap.Bootstrap(ctx, sshCoords, opts)` from `internal/lifecycle/bootstrap/`. Zero reimplementation.

**Evidence**:
- `plan.md:§Cross-Component Integration → 003` — "Wizard does NOT reimplement any SSH+Docker+compose logic."
- `plan.md:§What's Reused` — "003 Bootstrap: Wizard's 'commit' step calls `internal/lifecycle/bootstrap.Bootstrap(ctx, sshCoords, opts)` directly."
- `tasks.md:TASK-6.2` — "call `bootstrap.Bootstrap(ctx, sshCoords, opts)` from spec 003's `internal/lifecycle/bootstrap/`"

**Verified against 003**: `specs/003-vps-lifecycle/contracts/bootstrap-protocol.md` confirms the bootstrapper runs SSH commands (arch check, OS check, Docker install, compose deploy, health verify). 006's TASK-6.2 delegates to this — no task reimplements SSH+Docker commands. ✅

**006 preflight vs 003 preflight**: 006's TASK-2.1 runs its OWN preflight checks (distro, disk, sudo, docker, ports) over SSH BEFORE calling bootstrap. This overlaps with 003's bootstrap-protocol Phase 1 (arch verify, OS verify, disk check, sudo check). **MINOR-2**: 006's preflight duplicates some 003 bootstrap preflight checks. Acceptable because 006 preflight runs earlier (user-facing, showing results in wizard UI) while 003 bootstrap runs at commit time. The duplication is intentional UX — show compatibility BEFORE committing. The 003 bootstrapper remains the authoritative executor. Not a real issue, but worth noting.

### 7b. 002 peer creation

**Claim**: Wizard calls peer handler in-process, not HTTP loopback.

**Evidence**: `plan.md:§002 Control Plane` — "in-process: `wizard/commit.go` calls the handler function directly." `tasks.md:TASK-6.2` — "create first peer via in-process call to 002's peer handler."

**Verified against 002**: `api.openapi.yaml` has `POST /peers` (createPeer). 006 calls the same handler function. ✅

### 7c. 005 event emission

**Claim**: Events via `slog` flowing into 005's pipeline.

**Evidence**: `plan.md:§005 Observability` — "Wizard emits structured log records via `slog` at each step transition." `tasks.md:TASK-6.3` — "`slog.Info(\"wizard.step_complete\", \"event\", OnboardingEvent{...})`". Events tagged `component: "wizard"`, `source: "onboarding"`.

**Verified against 005**: `specs/005-observability/plan.md` confirms unified log pipeline via `slog` handler → ring buffer → SSE stream. `GET /v1/logs/stream?component=wizard` filtering is consistent with 005's design. ✅

### 7d. 004 Notifier stub

**Claim**: Wizard defines `Notifier` interface with no-op default.

**Evidence**: `plan.md:§004 Desktop Integration` — "Define a `Notifier` interface in `internal/wizard/` with a no-op default. Wizard code never imports `internal/desktop/` directly."

**Verified against 004**: `specs/004-desktop-integration/spec.md` confirms separate tray process model. 006 correctly avoids importing 004's package. ✅

**MINOR-3**: No task explicitly creates the `Notifier` interface. If 004 lands before 006, the interface definition needs to happen somewhere. Currently a future detail within TASK-6.2 scope. Low risk.

### 7e. 002 OpenAPI extension

**Claim**: TASK-6.4 updates 002's OpenAPI contract.

**Evidence**: `tasks.md:TASK-6.4` — "Update spec 002's OpenAPI contract (`contracts/api.openapi.yaml`) to document new endpoints."

**Verified**: 002's `api.openapi.yaml` currently has `/peers`, `/peers/{peerId}`, `/routes`, `/routes/{routeId}`. 006 adds `/v1/wizard/*`, `/v1/peers/{id}/qr`, `/v1/peers/{id}/invite`, `/v1/routes/expose`. TASK-6.4 handles this documentation update. ✅

**Cross-spec alignment: 5/5 verified.**

**Verdict**: PASS with minor notes.

---

## 8. Dependency Graph Sanity (Check 8)

**26 tasks, 7 phases. DAG verified:**

- Phase 1 (4 tasks): TASK-1.1 → 1.2 → 1.3 → 1.4 (sequential within). No cycles.
- Phase 2 (2 tasks): TASK-2.1 → 2.2. Depends on 1.4.
- Phase 3 (3 tasks): TASK-3.1 → 3.2, TASK-3.3 parallel. Both depend on 1.4 + 2.2 (via 3.1).
- Phase 4 (4 tasks): TASK-4.1 → 4.2 → 4.3, TASK-4.4 after 4.1 + 2.2. No cycles.
- Phase 5 (4 tasks): Two parallel chains: 5.1→5.2, 5.3→5.4. No deps on P2/P3/P4.
- Phase 6 (4 tasks): TASK-6.1 → 6.2 → 6.3 → 6.4 (sequential). 6.1 depends on 3.2, 3.3, 5.2. 6.2 depends on 5.1, 6.1, 5.3.
- Phase 7 (5 tasks): Depends on P1-P6 as appropriate. No cycles.

**Critical path verified**: `1.1 → 1.3 → 1.4 → 2.1 → 2.2 → 3.1 → 6.1 → 6.2 → 6.4` = 9 tasks. ✅

**Note**: Mermaid DAG in tasks.md shows TASK-5.1 (QR generation) as having no incoming deps from P1. This is correct — `skip2/go-qrcode` is a standalone library with no dependency on wizard infrastructure. TASK-5.1 can start immediately in parallel with P2/P3/P4.

**Verdict**: PASS.

---

## 9. Phase Ordering (Check 9)

**P1 user stories mapped to phases:**

| User Story (P1) | Phases | Verified |
|-----------------|--------|----------|
| US-1 (zero-to-URL <5 min) | P1-P6 (all backend + frontend + integration) | ✅ Full wizard requires all phases |
| US-2 (QR scan <30s) | P5 (QR generation) + P4 (frontend QR display) | ✅ |
| US-3 (one-click expose <3s) | P6 TASK-6.1 (expose endpoint) | ✅ |

**P2 user stories:**
| US-4 (Cloudflare DNS) | P3 TASK-3.2 | ✅ |
| US-5 (invite link) | P5 TASK-5.3-5.4 | ✅ |

**P3 user stories:**
| US-6 (BYO vs nip.io trade-off) | P3 + P4 (domain step + UI) | ✅ |
| US-7 (wizard resume) | P1 TASK-1.2 + P4 TASK-4.1 | ✅ |

**Testing in Phase 7**: All P1 stories are implemented in Phases 1-6. Testing phase verifies all. ✅

**Verdict**: PASS.

---

## 10. Edge Cases Coverage (Check 10)

| Edge Case | Spec Reference | Task Coverage | Verified |
|-----------|---------------|---------------|----------|
| SSH succeeds but lacks sudo/docker | `spec.md:§Edge Cases` line 147 | TASK-1.4 (ssh_no_sudo, ssh_no_docker) + TASK-2.1 (preflight) | ✅ |
| DNS A-record wrong IP | `spec.md:§Edge Cases` line 148 | TASK-3.1 (A-record mismatch → warning) | ✅ |
| Cloudflare token insufficient scopes | `spec.md:§Edge Cases` line 149 | TASK-3.2 (scope validation, specific errors) | ✅ |
| Mobile WG app not installed | `spec.md:§Edge Cases` line 150 | TASK-5.4 (landing page: QR + .conf + copyable) + `contracts/qr-deeplink.md:§Fallback` | ✅ |
| nip.io subdomain conflict | `spec.md:§Edge Cases` line 151 | TASK-3.3 (nip.io fallback + Caddy conflict check) | ✅ |
| Wizard interrupted between validate and commit | `spec.md:§Edge Cases` line 153 | TASK-1.2 (resume logic) + `wizard-state-machine.md:§Resume logic` | ✅ |
| Port exposure conflict | `spec.md:§Edge Cases` line 154 | TASK-6.1 (409 route_conflict + suggest alternative) | ✅ |
| Unsupported distro | `spec.md:§Edge Cases` line 155 | TASK-2.1 (distro check, reject with message) | ✅ |
| Already-provisioned VPS | `spec.md:§Edge Cases` line 156 | ⚠️ **See MINOR-4** |
| SSH key vs password auth | `spec.md:§Edge Cases` line 157-159 (amended) | TASK-1.4 (auth_type: key/password + `ssh_passphrase_protected`) + TASK-4.2 (SSHCredentialsStep with key/password toggle) | ✅ |
| **Passphrase-protected SSH key detection** | `spec.md:§Edge Cases` line 158-159 (new edge case) | TASK-1.4 (ssh_passphrase_protected error + CLI redirect to quickstart §SSH Keys & ssh-agent) | ✅ |
| nip.io DNS unreachable | `spec.md:§Edge Cases` line 160 | TASK-3.3 (resolution check + warning + BYO fallback) | ✅ |
| Invite link recipient incompatible OS | `spec.md:§Edge Cases` line 152 | TASK-5.4 (OS detection via User-Agent + WG client links) | ✅ |

**MINOR-4**: Edge case "Wizard started on already-provisioned VPS" (spec line 156) says wizard should detect existing unet deployment and offer "Re-provision" or "Connect to existing". No task explicitly addresses this detection logic. The plan mentions first-run detection via `GET /v1/status → vps: null` check, but this only prevents wizard launch when `vps.isProvisioned == true`. The edge case where someone manually triggers the wizard on an already-provisioned VPS (bypassing the status check) is not covered by any task. Low risk since the UI redirect prevents it, but the spec explicitly requires detection + options.

**Edge cases: 12/13 covered.**

**Verdict**: PASS with MINOR note.

---

## 11. Security Invariants (Check 11)

### 11a. PII file permissions

| File | Required perms | Task | Verified |
|------|---------------|------|----------|
| `~/.unet/wizard-state.json` | 0600 | TASK-1.2: "File mode 0600" | ✅ |
| `~/.unet/invites.jsonl` | 0600 | TASK-5.3: "file mode 0600" | ✅ |
| `~/.unet/config.json` (CF token) | 0600 | TASK-3.2: "stored in state file with 0600 perms" | ✅ |

### 11b. HMAC constant-time comparison

**Claim**: `crypto/subtle.ConstantTimeCompare` used.
**Evidence**: TASK-5.3 — "constant-time comparison (`crypto/subtle.ConstantTimeCompare`)", TASK-SEC1 — "verify `crypto/subtle.ConstantTimeCompare` used everywhere signatures are compared". ✅

### 11c. AES-GCM key derivation + IV uniqueness

**Claim**: Key = `sha256(daemon_secret)[:32]`, nonce = `crypto/rand` 12 bytes, nonce prepended to ciphertext.
**Evidence**: `contracts/invite-protocol.md:§Config Blob Encryption` — specifies exact key derivation and nonce handling. TASK-5.3 — "AES-256-GCM with 12-byte nonce (crypto/rand), key = `sha256(daemon_secret)[:32]`". TASK-SEC1 — "verify nonce uniqueness, key derivation from daemon secret". ✅

### 11d. Rate limiting

**Claim**: 5 attempts/IP/60s, 20 total failed → invalidate.
**Evidence**: TASK-5.4 — "Rate limiting: 5 attempts/IP/60s (in-memory sliding window), 20 total failed attempts per code → invalidate." TASK-SEC1 — "Short-code brute-force resistance — verify rate limiting". ✅

### 11e. SSH key passphrase

**Claim**: Not supported in wizard. Clear error message with CLI redirect.
**Evidence**: `spec.md:§Edge Cases` — amended from MUST to SHOULD with explicit passphrase detection + error + CLI redirect. `plan.md:§Open Risks → Risk 1` — "Design decision: passphrase-protected keys are NOT supported in the wizard UI. Spec amended to SHOULD with clear error detection." `tasks.md:TASK-1.4` — acceptance criteria includes `ssh_passphrase_protected` error code with CLI redirect to quickstart §SSH Keys & ssh-agent. `quickstart.md:§SSH Keys & ssh-agent` — documents passwordless key generation + CLI ssh-agent alternative.

**Previously MAJOR-2 (SSH passphrase contradiction) — RESOLVED.** Spec now consistently states passphrase NOT supported in wizard with clear error detection and CLI alternative. Plan risk section aligned. Task acceptance criteria include passphrase-detection. Quickstart provides user-facing docs.

### 11f. TASK-SEC1 placement

**Claim**: Security audit task after HMAC, AES-GCM, file perms implemented.
**Evidence**: TASK-SEC1 deps: "TASK-5.3 (HMAC + AES-GCM), TASK-5.4 (invite handlers), TASK-6.4 (full integration)". Correctly placed in Phase 7 after all security-critical code is implemented. ✅

**Security invariants: 6/6 clean.**

**Verdict**: PASS.

---

## 12. Constitution Alignment (Check 12)

### Principle VI — Cross-AI Review Gate

Plan explicitly acknowledges: `plan.md:§Principle VI` — "This is `/speckit.plan` — NOT `/speckit.implement`. No code is being written. The review gate does not apply at the planning stage." Correct. This analyze.md is the first gate check. ✅

### Principle VII — Artifact Versioning

Plan acknowledges: `plan.md:§Principle VII` — "snapshot-stage scripts do not exist." Defers with manual tag. Constitution's graceful-degradation clause applied correctly. ✅

### Principle VIII — Knowledge Self-Maintenance

Plan identifies 5 gaps in `architecture.md`:
1. Admin Surface missing wizard/onboarding description
2. Control Plane missing wizard endpoints
3. Operations layer: no gap
4. Spec Registry missing 006
5. Technology Stack missing QR library

Plan notes follow-up required before implementation merge. ✅

**Constitution alignment: 3/3 principles checked.**

**Verdict**: PASS.

---

## Summary of Findings

### Critical: 0

### Major: 0

Previous MAJOR-1 (endpoint count) and MAJOR-2 (SSH passphrase contradiction) both **resolved**:

- **MAJOR-1 resolved**: `tasks.md` §Endpoints updated from 8/8 → 11/11 with explanatory note about 3 discovered endpoints. `plan.md` §Component 9 includes discovered-endpoints paragraph.
- **MAJOR-2 resolved**: `spec.md` §Edge Cases line 157 rewritten from MUST → SHOULD with passphrase-protected key detection + CLI redirect. New edge case added. `plan.md` §Open Risks Risk 1 aligned. `tasks.md` TASK-1.4 acceptance criteria includes `ssh_passphrase_protected`. `quickstart.md` §SSH Keys & ssh-agent section added.

### Minor: 5

**MINOR-1**: Plan mentions 004 Notifier no-op interface but no task explicitly creates it. Low risk — implementation detail within TASK-6.2 scope.

**MINOR-2**: 006 preflight (TASK-2.1) duplicates some 003 bootstrap preflight checks. Acceptable UX trade-off but worth documenting as intentional duplication.

**MINOR-3**: No task explicitly creates the `Notifier` interface for 004 integration. Future detail.

**MINOR-4**: Edge case "already-provisioned VPS detection" (spec line 156) not explicitly covered by a task. UI redirect via status check prevents the common case, but spec requires explicit detection + options.

**MINOR-5**: *(Absorbed into MAJOR-2 resolution)* TASK-1.4 now explicitly includes passphrase-protected-key acceptance criterion. Previously a minor noting the gap — now closed.

### Info: 2

**INFO-1**: SC-002 (QR scan <30s) and SC-004 (95% completion) are not fully automatable. Spec acknowledges this. Acceptable.

**INFO-2**: Plan §Open Risk 5 (nip.io LE rate limits) is a production concern but correctly documented as a BYO-domain upsell incentive.

---

## Coverage Summary

| Dimension | Result |
|-----------|--------|
| Spec → Plan FRs | 14/14 |
| Spec → Tasks FRs | 14/14 |
| Spec → SC measurability | 8/8 (2 aspirational) |
| Plan → Tasks components | 10/10 |
| Contracts → Tasks (endpoints) | 11/11 |
| Contracts → Tasks (invite protocol) | 6/6 |
| Contracts → Tasks (QR/deeplink) | 6/6 |
| Contracts → Tasks (state machine) | 5/5 |
| Tech-stack consistency | 8/8 |
| Cross-spec alignment | 5/5 |
| Dependency graph sanity | DAG verified, critical path = 9 |
| Phase ordering | P1 stories in P1-P6, testing in P7 |
| Edge cases | 12/13 |
| Security invariants | 6/6 |
| Constitution alignment | 3/3 |

---

## Top 3 Issues

1. ~~**MAJOR-2**: SSH passphrase support contradiction between spec and plan~~ — **RESOLVED**. Spec amended to SHOULD + passphrase detection + CLI redirect. Plan aligned. Task criteria updated. Quickstart section added.
2. ~~**MAJOR-1**: Endpoint count claim in tasks.md inaccurate (8/8 vs 11/11)~~ — **RESOLVED**. tasks.md updated to 11/11 with discovered-endpoints note. plan.md §Component 9 updated.
3. **MINOR-4**: Already-provisioned VPS edge case detection not explicitly tasked. Low risk — UI redirect prevents common case.

---

## Recommendation

1. Proceed to external review. Zero majors remain.
2. MINOR-4 (already-provisioned VPS) can be addressed during implementation if desired — add a guard check to TASK-6.2 commit orchestrator.
3. Cross-AI review per Constitution Principle VI: ≥2 external reviewers required before `/speckit.implement`.

---

```yaml
verdict: PASS
reviewer: analyze
reviewed_at: "2026-05-28T14:00:00Z"
commit: pending
major: 0
minor: 5
critical: 0
notes:
  - "MAJOR-2 RESOLVED: SSH passphrase spec/plan contradiction fixed — spec amended to SHOULD + detection, plan aligned, task criteria updated, quickstart section added"
  - "MAJOR-1 RESOLVED: Endpoint count updated from 8/8 to 11/11 in tasks.md + plan.md"
  - "Coverage is excellent (14/14 FRs, 11/11 endpoints, 10/10 components)"
  - "Cross-spec integration correctly delegates to 003/002/005/004"
  - "5 minors remain (acceptable) — most notable: MINOR-4 already-provisioned VPS edge case"
```

---

## Fix + Re-analyze Report (006)

### Fixes applied
- MAJOR-2: spec.md (passphrase clause rewritten from MUST to SHOULD + detection + CLI redirect + new edge case), quickstart.md (SSH Keys & ssh-agent section added), plan.md (open risk Risk 1 wording aligned), tasks.md (TASK-1.4 acceptance criteria: `ssh_passphrase_protected` + CLI redirect)
- MAJOR-1: tasks.md (endpoint count 8/8 → 11/11 + discovered-endpoints note), plan.md §Component 9 (3 discovered endpoints paragraph)

### Re-analyze verdict
- Status: PASS
- Critical: 0
- Major: 0
- Minor: 5

### Verification per fix
- MAJOR-2 resolved: yes
  - spec.md §Edge Cases: line 157 rewritten to SHOULD + NOT supported + passphrase detection error + CLI redirect to quickstart. New edge case "Passphrase-protected SSH key detection" added at line 158-159.
  - plan.md §Open Risks Risk 1: wording updated to "Design decision: passphrase-protected keys are NOT supported in wizard UI. Spec amended to SHOULD with clear error detection."
  - tasks.md TASK-1.4 acceptance: includes `ssh_passphrase_protected` error code with CLI redirect to quickstart §SSH Keys & ssh-agent.
  - tasks.md §Open Risks Mitigated: Risk 1 updated to reference passphrase detection + CLI redirect.
  - quickstart.md: new §SSH Keys & ssh-agent section with passwordless key generation, CLI ssh-agent workflow, and explanation of wizard limitation.
  - Security Check 11e: 6/6 clean, no contradiction remains.
- MAJOR-1 resolved: yes
  - tasks.md §Coverage Validation → Endpoints: header changed to "11/11", blockquote note explains 8 core + 3 discovered, table includes Discovered? column with descriptions.
  - plan.md §Component 9 (OneClickPublisher): discovered-endpoints paragraph added documenting 3 additional endpoints.

DONE.