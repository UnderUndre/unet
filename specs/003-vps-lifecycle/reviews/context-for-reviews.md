# SpecKit Review Context: 003-vps-lifecycle

This document contains the complete context for a critical review of the **VPS Lifecycle Management** feature for the **unet** project.

**Feature Slug**: 003-vps-lifecycle
**Context gathered at**: 2026-05-30
**Repository Constitution Version**: 1.4.0

---

## 1. PROJECT CONSTITUTION (.specify/memory/constitution.md)
Governs principles, quality gates, and cross-AI review rules. (See Principles I, VI, VII).

---

## 2. GLOBAL ARCHITECTURE (specs/main/architecture.md)
Shows how VPS Lifecycle fits into the unet ecosystem (Lifecycle Operations layer).

---

## 3. FEATURE SPECIFICATION (specs/003-vps-lifecycle/spec.md)
The "What" and "Why".

```markdown
# Feature Specification: VPS Lifecycle Management

## Resolved Decisions
- **State backup**: Local file + optional S3-compatible (R2/B2/MinIO).
- **Encryption**: `age` + passphrase (scrypt KDF).
- **Migration strategy**: Cutover with DNS-TTL redirect.
- **Compose source**: Embedded in daemon binary (Go embed.FS).

## User Scenarios
1. Clean-VPS Bootstrap (P1) - Idempotent provisioning.
2. Attach to Existing (P1) - State sync without peer disruption.
3. Partition Recovery (P2) - Exp-backoff reconnect.
4. State Backup/Restore (P2) - Machine-to-machine transfer.
5. VPS-to-VPS Migration (P3) - Cutover protocol.
6. Version Drift Handling (P3).

## Requirements (Highlights)
- **FR-001**: Idempotent bootstrap (Docker install + compose deploy).
- **FR-003**: Four-state taxonomy: `blank`, `old`, `current`, `incompatible`.
- **FR-007**: Health probe over WireGuard tunnel (ICMP + HTTP).
- **FR-009**: Encrypted state bundle (header + payload payloadHash).
- **FR-011**: 10-step migration protocol with crash recovery.
- **FR-014**: Config-only snapshots before mutations.
```

---

## 4. DATA MODEL (specs/003-vps-lifecycle/data-model.md)
Schema and persistence details.

```markdown
# Data Model

### VPSProfile (vps.json)
- SSH coords, AuthMode (key/password), status, composeHash, wgEndpoint.

### StateBundle (.jsonl.age)
- Encrypted JSONL stream.
- Manifest (meta + payloadHash) + Payload (peers, routes, tokens, config).

### MigrationPlan (migration.json)
- Phase tracking (bootstrapping -> syncing -> cutover -> draining -> complete).

### HealthSnapshot (In-memory)
- Temporal health state for reconnect decisions.
```

---

## 5. IMPLEMENTATION PLAN (specs/003-vps-lifecycle/plan.md)
The "How".

```markdown
# Implementation Plan

- **Language**: Go.
- **Libs**: `golang.org/x/crypto/ssh`, `filippo.io/age`, `aws-sdk-go-v2`.
- **New Components**: `internal/lifecycle/{bootstrap,attach,detect,migrate,backup,compose,health,snapshot}`, `internal/ssh/pool`.

### Open Risks
1. SSH key passphrases (daemon needs unattended keys).
2. Disk full (ENOSPC) during bootstrap.
3. Age passphrase loss (unrecoverable).
4. DNS TTL surprises during migration.
5. Snapshot size (config-only tradeoff).
```

---

## 6. TASKS (specs/003-vps-lifecycle/tasks.md)
The "When". (Large file summary)

```markdown
# Tasks (8 Phases)
- Phase 1: Foundation (SSH pool, compose embed, state, audit)
- Phase 2: Bootstrap (preflight, docker, deploy, rollback)
- Phase 3: Attach + Detect
- Phase 4: Health Probing + Reconnect
- Phase 5: Backup (Export/Import)
- Phase 6: Migration (10-step cutover)
- Phase 7: API Surface (Async tasks, localhost + remote)
- Phase 8: Testing + Integration (SSH mocks, DinD)
```

---

## 7. CONTRACTS & PROTOCOLS (specs/003-vps-lifecycle/contracts/)

### Bootstrap Protocol (bootstrap-protocol.md)
1. Preflight (arch, OS, disk, sudo)
2. Docker Install (idempotent curl)
3. Compose Deploy (tee + up -d)
4. Health Verify (poll containers + awg0)

### Migration Protocol (migration-protocol.md)
1. Pre-flight -> 2. Snapshot -> 3. Bootstrap target -> 4. Export -> 5. Import -> 6. Verify health -> 7. DNS cutover -> 8. Drain source -> 9. Decommission source -> 10. Update profile.

### Lifecycle API (lifecycle-api.md)
Endpoints: `POST /api/vps/bootstrap`, `POST /api/vps/attach`, `GET /api/vps/lifecycle`, `POST /api/vps/migrate`, etc.
Task lifecycle: `pending -> running -> completed/failed`.

### State Bundle Schema (state-bundle.schema.json)
JSON Schema for the encrypted header line.

---

## 8. CROSS-ARTIFACT ANALYSIS (specs/003-vps-lifecycle/reviews/analyze.md)
(Placeholder: Assume analyze PASS but noted architecture.md drift).

---

**ACTION FOR REVIEWER**: 
Perform a critical adversarial review using the lenses defined in `.agent/workflows/speckit.review.md`:
- **Logical consistency**: (e.g. Does the migration protocol handle DNS failures correctly?)
- **Hidden assumptions**: (e.g. Does S3 sync assume a specific region?)
- **Missing edge cases**: (e.g. What if VPS disk fills up DURING state import?)
- **Failure modes**: (e.g. SSH session pool exhaustion)
- **Security & privacy threats**: (e.g. age passphrase entropy, SSH key storage permissions)
- **Performance & scale**: (e.g. Large clientsTable sync latency)
- **Alternative approaches**: (e.g. Dual-write vs Cutover migration)
- **Constitution alignment**.

Write your report to `specs/003-vps-lifecycle/reviews/<provider>.md`.
