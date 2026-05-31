# SpecKit Review Context: 002-api-control-plane

This document contains the complete context for a critical review of the **Remote Control Plane API** feature for the **unet** project.

**Feature Slug**: 002-api-control-plane
**Context gathered at**: 2026-05-30
**Repository Constitution Version**: 1.4.0

---

## 1. PROJECT CONSTITUTION (.specify/memory/constitution.md)
Governs principles, quality gates, and cross-AI review rules.

```markdown
# UnderUndre AI Helpers Constitution

Binding principles for `clai-helpers` CLI + the curated `.claude/` template it ships. Every `/speckit.*` command checks plans and tasks against this file. Violations halt work until resolved or the constitution is explicitly amended.

## Core Principles

### I. Source of Truth Discipline
`.claude/` is **the** authoritative AI configuration. All downstream formats (`.github/prompts/`, `.github/instructions/*.instructions.md`, `.gemini/`, `GEMINI.md`, `.github/copilot-instructions.md`) are **generated**, never hand-edited.

### VI. Cross-AI Review Gate (NON-NEGOTIABLE)
`/speckit.implement` MUST NOT proceed without explicit gate approval. The gate requires:
1. `/speckit.analyze` written `specs/<slug>/reviews/analyze.md` with verdict ∈ {PASS, OVERRIDDEN}.
2. At least **2 distinct external AI reviewers** wrote `specs/<slug>/reviews/<provider>.md` via `/speckit.review` with verdict ∈ {PASS, OVERRIDDEN}.

Rationale: the model that wrote the spec is the worst auditor of the spec. Independent eyes find what the author already rationalized away.
```

*(Full constitution omitted for brevity here, but implied as the project's "law")*

---

## 2. GLOBAL ARCHITECTURE (specs/main/architecture.md)
Shows how 002 fits into the unet ecosystem.

```markdown
# unet Architecture (v0.2.0)

## Layer Details: Control Plane
The control plane exposes a network-accessible, authenticated HTTP API for programmatic management of unet resources.

**Purpose**: Enable external consumers (undevops dashboard plugin, third-party tools, future multi-user enterprise tier) to manage peers, routes, and tunnel status without SSH + docker exec.

**Components**:
- **Remote HTTP API**: Separate HTTP listener in the same Go process as the local daemon. Bound to configurable address (default `0.0.0.0:8443`). TLS required. Authenticated via API tokens with scoped permissions (`read`/`write`/`admin`). Path prefix: `/v1/`.
- **Token store**: API tokens stored hashed in `~/.unet/config.json`.
- **Audit log**: Append-only record of all state-changing API actions. Stored locally as JSONL (`~/.unet/audit.jsonl`).

**Relationship to daemon API**: The control plane reuses the daemon's VPS connection and state. It does NOT replace the localhost daemon API — both run simultaneously on different listeners with different auth requirements.
```

---

## 3. FEATURE SPECIFICATION (specs/002-api-control-plane/spec.md)
The "What" and "Why".

```markdown
# Feature Specification: Remote Control Plane API

## Resolved Decisions
- **Auth method**: Opaque PAT-style tokens for CLI/external + JWT for admin UI sessions.
- **Path prefix**: `/v1/` (no `/api/` outer prefix).
- **Backward compat**: Keep both localhost (unauth) and remote (auth) surfaces.
- **Process placement**: Same Go process as daemon, separate port (:8443).
- **Multi-host**: Single-host MVP; multi-host deferred.

## User Scenarios
1. External Tool Lists Peers (P1)
2. External Tool Creates Peer (P1) - Automating SSH + awg0.conf editing.
3. External Tool Queries Status (P1)
4. External Tool Creates Ingress Route (P2)
5. Scoped API Tokens (P2)
6. Token Identity and Audit Log (P3)

## Requirements (Highlights)
- **FR-001**: Auth via Bearer tokens. Loopback skips auth (admin scope).
- **FR-002**: Tokens stored hashed (bcrypt/argon2).
- **FR-005**: `POST /v1/peers` returns full WireGuard client config.
- **FR-012**: Listener MUST require TLS (auto-gen self-signed on first start).
- **FR-014**: Consistent JSON error structure.
- **FR-016**: Append-only audit log for mutations.

## Accepted Security Trade-offs
- **Loopback :8443 → unconditional admin scope**: Single-admin model assumes shell access = full access.
```

---

## 4. DATA MODEL (specs/002-api-control-plane/data-model.md)
Schema and persistence details.

```markdown
# Data Model

### APIToken
- Stored in `~/.unet/config.json`.
- `tokenHash`: bcrypt cost 12.
- `scope`: read | write | admin.

### Session (JWT)
- HS256 signed. Key derived from daemon secret.
- 15 min TTL. References PAT id.

### AuditEntry
- JSONL file at `~/.unet/audit.jsonl`.
- Immutable, append-only.
```

---

## 5. IMPLEMENTATION PLAN (specs/002-api-control-plane/plan.md)
The "How".

```markdown
# Implementation Plan

- **Language**: Go (std lib `net/http` + `crypto/tls`).
- **Reuse**: Daemon core logic (SSH to VPS, awg, Caddy admin API).
- **New Components**: `internal/api/remote/`, `internal/api/middleware/`, `internal/auth/`, `internal/audit/`, `internal/api/v1/`.

### Open Risks
1. bcrypt latency → In-memory LRU cache for tokens (5-min TTL).
2. Config contention → Reuse existing serialization/mutex.
3. Concurrent peer creation → implementación MUST hold mutex across entire alloc+write+sync sequence.
```

---

## 6. TASKS (specs/002-api-control-plane/tasks.md)
The "When".

```markdown
# Tasks

- **Phase 1**: Foundation (Auth + Token Store + TLS Server)
- **Phase 2**: Read-Only Endpoints
- **Phase 3**: Mutation Endpoints (Peer/Route CRUD)
- **Phase 4**: JWT Session Flow
- **Phase 5**: Audit + Rate Limit + Polish
- **Phase 6**: Testing & Integration

**Critical Path**: TASK-1.2 (hash) → TASK-1.3 (store) → TASK-2.1 (PAT mid) → TASK-2.3 (auth dispatcher) → TASK-2.4 (read peers) → TASK-3.1 (POST peers) → TASK-6.2 (peer tests).
```

---

## 7. API CONTRACT (specs/002-api-control-plane/contracts/api.openapi.yaml)
OpenAPI 3.1 definitions.

*(Full YAML follows — see spec files for details)*
Endpoints: `/status`, `/tunnel/status`, `/peers`, `/peers/{id}`, `/routes`, `/routes/{id}`, `/tokens`, `/audit`, `/auth/session`.

---

## 8. AUTH FLOWS (specs/002-api-control-plane/contracts/auth-flows.md)
Sequence diagrams and logic.

```markdown
### Auth-by-Bind-Address Logic
- loopback? → skip auth, admin identity.
- network? → Bearer check (unet_* = PAT, eyJ* = JWT).
```

---

## 9. CROSS-ARTIFACT ANALYSIS (specs/002-api-control-plane/reviews/analyze.md)
Internal consistency check result.

```markdown
**VERDICT**: PASS
**Status**: All behavioral mismatches (network partition queuing, clarified rate limits) resolved.
**Minor Issues**: architecture.md path prefix drift; name validation mismatch; no task for `rotate_cert` (reserved).
```

---

**ACTION FOR REVIEWER**: 
Perform a critical adversarial review using the lenses defined in `.agent/workflows/speckit.review.md`:
- Logical consistency
- Hidden assumptions
- Missing edge cases
- Failure modes
- Security & privacy threats
- Performance & scale
- Alternative approaches
- Stakeholder clarity
- Constitution alignment

Write your report to `specs/002-api-control-plane/reviews/<provider>.md`.
