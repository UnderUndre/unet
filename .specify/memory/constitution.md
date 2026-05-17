<!--
Sync Impact Report — Constitution Genesis
=========================================
Version change: (no prior file) → 1.4.0
Rationale for initial version: CLAUDE.md, command files, and review tooling already
reference Principle VI and Principle VII at v1.4.0 from the upstream template seed.
This file ratifies the de-facto state rather than starting at v1.0.0, to avoid mass
rewrites of cross-references in `.claude/`, `.gemini/`, `.github/`.

Added principles (all new — no prior content to compare):
  I.   Spec-First Development
  II.  Atomicity (WRAP)
  III. Secrets Discipline
  IV.  Type Safety & Error Discipline
  V.   Source-of-Truth: `.claude/`
  VI.  Cross-AI Review Gate (NON-NEGOTIABLE)
  VII. Artifact Versioning

Added sections:
  - Governance (amendment procedure, versioning, compliance review)

Templates requiring updates:
  ✅ N/A — `.specify/templates/{plan,spec,tasks}-template.md` do not exist yet (will be
        created on first `/speckit.*` template-generation invocation). No drift to fix.
  ✅ `CLAUDE.md` Standing Orders + Stop Conditions already align with Principles I-V.
  ⚠ `snapshot-stage.{sh,ps1}` referenced by Principle VII does not exist yet.
     Deferred (TODO_SNAPSHOT_SCRIPT) — Principle VII is aspirational until tooling lands.
  ⚠ Specs that pre-date this constitution (`specs/001-init/`) must pass
     `/speckit.analyze` + ≥2 external reviewer PASS before `/speckit.implement` (Principle VI).
-->

# clai-helpers Constitution

**Version**: 1.4.0
**Ratified**: 2026-05-16
**Last Amended**: 2026-05-16

This constitution governs the `clai-helpers` repository: the CLI package, the curated
`.claude/` source-of-truth template, and all artifacts derived from it via `helpers regen`
(`.github/`, `.gemini/`, agent-specific mirrors). Principles below are NON-NEGOTIABLE
unless an override path is explicitly defined.

---

## Principle I — Spec-First Development

Every change that touches **more than 3 files** OR introduces a new feature domain MUST
flow through the SpecKit pipeline:

```
/speckit.start → /speckit.specify → /speckit.clarify → /speckit.plan
              → /speckit.tasks → /speckit.analyze → /speckit.review (≥2 AIs)
              → /speckit.implement
```

Inline implementation is permitted only for in-domain, ≤3-file changes (bugfix, typo,
single-call refactor). Any task meeting the >3-file threshold MUST stop and route to
`/speckit.start` before code is touched.

**Rationale**: undisciplined "just one more file" expansions are the dominant source of
incoherent commits in this repo's history. The pipeline forces commitment to a written
contract before code.

**Override**: none. The pipeline IS the override — start over from `/speckit.specify`.

---

## Principle II — Atomicity (WRAP)

A change is **atomic** if it is one of:
- A single refactor preserving behaviour (zero new tests, zero new features).
- A single feature increment (≤500 LOC of net new logic + tests for it).

A commit MUST NOT contain BOTH a refactor AND a feature increment. They MUST be split
into separate commits, in that order: **refactor first, feature second**.

Tracer-bullet skeletons (interface stubs, end-to-end smoke wiring) are permitted as
their own atomic commit, before the feature flesh-out commit lands.

**Rationale**: mixed-purpose commits are unreviewable; rollback becomes binary
("revert the whole thing") instead of surgical. Chain-of-Verification breaks down when
"what changed?" has two answers.

**Override**: none. Split the commit.

---

## Principle III — Secrets Discipline

Code MUST NEVER:
- Embed credentials, API tokens, private keys, or passwords as literals.
- Use `process.env.X || "fallback"` for security-sensitive values. Missing env vars
  MUST raise a typed error at startup (`if (!env.X) throw new ConfigError(...)`).
- Log secret values, even on error paths. Errors involving credentials MUST be
  redacted to a structural placeholder (`<redacted-token>`) before reaching any logger
  or telemetry sink.
- Read `.env`, `.env.*`, `~/.ssh/`, `~/.unet/`, `~/.aws/credentials`, OS keychain,
  or equivalents unless the user explicitly asked.

**Rationale**: credential leaks via `console.log(config)` and "temporary debug" lines
are the most common security incident class in Node/Python CLIs. A blanket prohibition
is cheaper than per-call review.

**Override**: explicit user request for one specific path, scoped to one session.
Never persist the override.

---

## Principle IV — Type Safety & Error Discipline

TypeScript code MUST NOT:
- Use `as any`, `as unknown as T`, or `@ts-expect-error` without a paired comment
  citing the upstream bug / planned-fix ticket.
- Throw bare `new Error("...")`. All thrown errors MUST be instances of a domain
  error class (`AppError.badRequest(...)`, `ConfigError`, etc.) with a stable `.code`.
- Swallow `catch (e) {}` blocks. Either re-throw, classify-then-handle, or log with
  `logger.error({ err: e }, "...")` (using the project's `consola` logger, never
  `console.log`).
- Classify errors via `err.message.includes("timeout")`. Use structural signals:
  `err.name`, `err.code`, `instanceof DomainError`.

Python code MUST use type hints on every public function signature and pass `mypy`
in strict-optional mode.

**Rationale**: weak typing and string-matched errors create silent contract breakage
when libraries upgrade. Structural contracts survive refactors; string matching
doesn't.

**Override**: none. If a typed wrapper is missing, write it first.

---

## Principle V — Source-of-Truth: `.claude/`

The directory `.claude/` (and `CLAUDE.md` at repo root) is the **canonical** source for
all agent prompts, skills, commands, and persona definitions. Generated mirrors live at:

- `.github/copilot-instructions.md`, `.github/prompts/`, `.github/instructions/`
- `.gemini/commands/`, `.gemini/agents/`, root `GEMINI.md`
- `.agent/` (cross-tool shared mirror)
- `.agents/commands/` (Codex Desktop mirror)

Direct edits to ANY generated mirror MUST NOT be committed. Workflow:

```
1. Edit `.claude/commands/<x>.md`  (or skills/agents/CLAUDE.md)
2. Run `npx clai-helpers sync` (alias: `helpers regen`)
3. Verify with `npx clai-helpers status --strict`  (CI-friendly; exit 2 on drift)
4. Commit `.claude/` + regenerated mirrors together
```

**Rationale**: divergence between mirrors causes silent regressions when an AI tool
picks the stale copy. A single source of truth + deterministic transpile is the only
sustainable model.

**Override**: temporary patch to a mirror IS permitted only when blocked by a
generator bug, and only with a `// MIRROR-PATCH(issue#NNN): ...` comment and a paired
issue. The patch MUST be removed in the same PR that fixes the generator.

---

## Principle VI — Cross-AI Review Gate (NON-NEGOTIABLE)

`/speckit.implement` MUST refuse to start until ALL of the following hold:

1. `specs/<slug>/reviews/analyze.md` exists with `verdict: PASS` OR `verdict: MEDIUM`
   (LOW/HIGH/CRITICAL block).
2. At least **two** files matching `specs/<slug>/reviews/<provider>.md` exist with
   `verdict: PASS`, where `<provider>` ∈ {codex, antigravity, gemini, copilot, glm,
   grok, deepseek}. The reviewers MUST be DIFFERENT AI providers — two reviews from
   the same provider do not count.
3. No file `specs/<slug>/reviews/_gate-override.md` exists in a state that contradicts
   the current commit.

**Override path**: `/speckit.implement --override-gate "<rationale>"`. The override:
- MUST log a non-empty rationale (minimum 100 characters of justification).
- MUST write to `specs/<slug>/reviews/_gate-override.md` with author, commit, rationale.
- MUST be re-applied per implementation session (overrides do not survive a new
  `/speckit.implement` invocation).
- Triggers a `[GATE-OVERRIDE]` prefix on every commit in that session for audit.

**Rationale**: single-AI review has a 30-40% miss rate on architectural issues per
this project's retrospectives. Two-AI cross-check catches the systematic blind spots
of any one model (training-data bias, hallucinated APIs, missed concurrency cases).
The gate is non-negotiable because the alternative — trusting one AI's review — has
already burned us in 001-init's draft (Bearer-token Caddy auth, missing AmneziaWG
parameters, hardcoded `wg0`/10.8.0.x).

---

## Principle VII — Artifact Versioning

Every SpecKit stage transition MUST tag the repo via `snapshot-stage.{sh,ps1}`:

```
<stage>/<slug>/v<N>
  ─────  ────  ────
   │      │     │
   │      │     └─ monotonically incrementing per (stage, slug) pair
   │      └─ feature slug (e.g. "001-init")
   └─ pipeline stage (specify | clarify | plan | tasks | analyze | review | implement)
```

`/speckit.diff <slug> [from] [to]` operates over these tags. No `.history/`, no
`*.bak`, no `<file>.v2.md` files — **git is the history**.

The tooling enforces this: `snapshot-stage.sh` MUST be invoked from each
`/speckit.<stage>` command's exit path; if the script is missing, the stage command
MUST log a `[snapshot-deferred]` warning but still complete (graceful degradation
during bootstrap).

**Status**: TODO_SNAPSHOT_SCRIPT — `snapshot-stage.{sh,ps1}` does not yet exist in
this repo. Principle VII is aspirational until tooling lands; meanwhile, manual
`git tag <stage>/<slug>/v<N>` after each stage is encouraged but not enforced.

**Rationale**: stage-tagged snapshots make `/speckit.retrospective` and bisecting
across pipeline stages trivial. Without them, `git log -- specs/<slug>/` is the
only fallback — workable but lossy for non-spec changes that happen between stages.

---

## Governance

### Amendment Procedure

1. **Propose** — open a PR that modifies this file. Include a `Sync Impact Report`
   HTML comment block at the top (this file's existing report shows the format).
2. **Justify** — the PR description MUST explain which Principle is being changed,
   why, and what alternative was rejected.
3. **Review** — Principle VI applies recursively: amendments to this constitution
   MUST themselves pass `/speckit.analyze` + ≥2 cross-AI reviews. The reviewers MUST
   include at least one AI provider that was NOT involved in drafting the amendment.
4. **Ratify** — merge to `main` updates `LAST_AMENDED_DATE` and `Version`.

### Versioning Policy

Semantic versioning applies to this document:

- **MAJOR** (X.0.0): Backward-incompatible governance changes — removing a Principle,
  redefining the override path of a NON-NEGOTIABLE Principle, changing the amendment
  procedure.
- **MINOR** (1.X.0): Adding a new Principle, expanding an existing Principle with
  materially new MUST/SHOULD requirements, adding a section.
- **PATCH** (1.4.X): Clarifications, typo fixes, rewording without semantic change.

Version bumps that are ambiguous between MINOR and MAJOR MUST default to MAJOR.

### Compliance Review

- **Every PR** — CI runs `npx clai-helpers status --strict` (Principle V) and lint
  rules covering Principles III + IV.
- **Every SpecKit feature** — `/speckit.analyze` validates against Principles I, II,
  VI, VII before `/speckit.implement` is unblocked.
- **Quarterly** — manual audit of `specs/_overlap.md` (multi-feature scope
  coordination) and a sample of past `_gate-override.md` files to assess whether
  override-justifications were retrospectively warranted.

### Conflict Resolution

If a Principle here conflicts with a directive in `CLAUDE.md`, `GEMINI.md`,
`AGENTS.md`, or a skill file, **this constitution wins**. The dependent file MUST be
updated to align (or to explicitly carve out a documented exception under that
Principle's "Override" clause). Until alignment, the conflicting directive is
considered ADVISORY only.

If a Principle here conflicts with an **explicit user instruction** in-session, the
user wins for that session, but the override MUST be re-justified each session — no
implicit grandfathering.
