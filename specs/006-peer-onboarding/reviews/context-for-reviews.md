# SpecKit Review Context: 006-peer-onboarding

This document contains the complete context for a critical review of the **Peer Onboarding Wizard** feature for the **unet** project.

**Feature Slug**: 006-peer-onboarding
**Context gathered at**: 2026-05-30
**Repository Constitution Version**: 1.4.0

---

## 1. PROJECT CONSTITUTION (.specify/memory/constitution.md)
Governs principles, quality gates, and cross-AI review rules.

```markdown
# UnderUndre AI Helpers Constitution (v1.4.0)

### VI. Cross-AI Review Gate (NON-NEGOTIABLE)
`/speckit.implement` MUST NOT proceed without explicit gate approval. The gate requires:
1. `/speckit.analyze` written `specs/<slug>/reviews/analyze.md` with verdict ∈ {PASS, OVERRIDDEN}.
2. At least **2 distinct external AI reviewers** wrote `specs/<slug>/reviews/<provider>.md` via `/speckit.review` with verdict ∈ {PASS, OVERRIDDEN}.
```

---

## 2. GLOBAL ARCHITECTURE (specs/main/architecture.md)
Shows how the Onboarding Wizard fits into the unet ecosystem (Admin Surface & Lifecycle Operations).

```markdown
# unet Architecture (v0.2.0)

## Layer Details: Admin Surface
The embedded React UI served from the Go binary on localhost. Provides a GUI for configuring VPS, managing tunnel connections, and exposing ports.

**Wizard/Onboarding**: First-run multi-step wizard orchestrates Lifecycle bootstrap and Control Plane peer/route creation to collapse the SSH+manual-config cliff.
```

---

## 3. FEATURE SPECIFICATION (specs/006-peer-onboarding/spec.md)
The "What" and "Why".

```markdown
# Feature Specification: Peer Onboarding Wizard

## Core Proposition
First-run VPS setup wizard, mobile peer onboarding via QR codes, one-click "share localhost" UX — collapsing the SSH+manual-config cliff.

## User Scenarios
1. Zero-to-First-URL via Wizard (P1) - < 5 min setup.
2. Add Mobile Peer via QR Code (P1) - < 30 sec connection.
3. One-Click Port Exposure (P1) - Atomic route+DNS creation.
4. Cloudflare Auto-DNS (P2) - API token integration.
5. Shareable Peer Invite Link (P2) - HMAC-signed URLs.

## Requirements (Highlights)
- **FR-001**: 8-step wizard state machine.
- **FR-004**: VPS Preflight (Distro, RAM, Ports, Docker).
- **FR-006**: QR code containing AmneziaWG obfuscation params.
- **FR-008**: "Expose Port" action with atomic rollback on DNS failure.
- **FR-012**: HMAC-signed one-time-use invite links.
```

---

## 4. DATA MODEL (specs/006-peer-onboarding/data-model.md)
Schema and persistence details.

```markdown
# Data Model

### WizardState
- Stored in `~/.unet/wizard-state.json` (0600).
- Tracks steps: welcome -> ssh -> preflight -> domain_mode -> domain_check -> commit -> success.

### InviteLink
- JSONL store with encrypted config blobs (AES-256-GCM).
- Mode: `hmac_url` or `short_code`.
```

---

## 5. IMPLEMENTATION PLAN (specs/006-peer-onboarding/plan.md)
The "How".

```markdown
# Implementation Plan

- **Language**: Go + React.
- **QR Lib**: `github.com/skip2/go-qrcode` (pure Go).
- **State Machine**: React `useReducer` mirroring backend enum states.
- **Integration**:
  - Calls 003 `Bootstrap()` for commit.
  - Calls 002 `peer/route` handlers in-process.
  - Emits 005 `OnboardingEvent` logs.
```

---

## 6. TASKS (specs/006-peer-onboarding/tasks.md)
The "When".

```markdown
# Tasks (7 Phases)
- Phase 1: Backend Foundation (Reducer, Persistence, Routes).
- Phase 2: Preflight + SSH Validation.
- Phase 3: Domain Validation (DNS, Cloudflare, nip.io).
- Phase 4: Frontend Wizard UI (React steps).
- Phase 5: QR + Invite (HMAC, short-codes, encryption).
- Phase 6: Integration (Commit orchestrator, Event emission).
- Phase 7: Testing & Security Audit.
```

---

## 7. CONTRACTS & PROTOCOLS (specs/006-peer-onboarding/contracts/)

### Wizard State Machine (wizard-state-machine.md)
Detailed transition table and React reducer types.

### Invite Protocol (invite-protocol.md)
HMAC-SHA256 signature logic and AES-256-GCM config encryption details.

### QR & Deeplink (qr-deeplink.md)
`wireguard://import?config=<base64url>` format and landing page OS detection logic.

### Wizard API (wizard-api.md)
11 endpoints covering sessions, steps, preflight, commit, QR, and invites.

---

## 8. CROSS-ARTIFACT ANALYSIS (specs/006-peer-onboarding/reviews/analyze.md)
Self-consistency check result.

```markdown
**VERDICT**: PASS
**Major Issues**: 0 (Resolved: SSH passphrase contradiction, Endpoint count sync).
**Minor Issues**: 5 (Notifier no-op task, Preflight duplication, Already-provisioned edge case).
```

---

**ACTION FOR REVIEWER**: 
Perform a critical adversarial review using the lenses defined in `.agent/workflows/speckit.review.md`:
- **Logical consistency**: (e.g. Does the wizard state correctly handle VPS reboot mid-bootstrap?)
- **Hidden assumptions**: (e.g. Does `nip.io` assume the VPS IP is publicly routable?)
- **Missing edge cases**: (e.g. Client browser losing WebSocket/SSE during the 5-min bootstrap.)
- **Failure modes**: (e.g. Cloudflare API rate limits during DNS setup.)
- **Security & privacy threats**: (e.g. Invite link enumeration, Short-code brute force.)
- **Performance & scale**: (e.g. QR generation CPU cost under load.)
- **Alternative approaches**: (e.g. Why separate QR generation instead of client-side JS?)
- **Constitution alignment**.

Write your report to `specs/006-peer-onboarding/reviews/<provider>.md`.
