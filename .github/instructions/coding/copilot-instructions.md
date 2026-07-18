# AI_CODING_prompt.md

## Metadata

| Field         | Value |
| ------------- | ----- |
| **Version**   | 2.1.0 |
| **Updated**   | 2026-07-06 |
| **Scope**     | Lean Coding Foundation (Standing Orders, Stop Conditions, Principles Gist) |
| **Companion** | `.github/instructions/coding/copilot-instructions-ref.md` (Heavy reference) |

---

## Coding Standards Foundation

All agents MUST adhere to these distilled core rules. For the full normative text, worked details, and incident playbooks, see [Coding Reference](.github/instructions/coding/copilot-instructions-ref.md).

1. **Standing Orders (MUST)**
   - Never execute migrations directly (generate SQL for review).
   - Never use `--force`, `--yes`, `-y` or bypass flags (ask when prompted).
   - Never put secrets, API keys, or passwords in code, commits, or logs.
   - Never install packages without approval.
   - Never run destructive commands (`rm -rf`, `DROP TABLE`, `git push --force`) without triple confirmation.
   - Never commit, push, or deploy without request.
   - Never read `.env`, `.env.*`, ssh or secrets unless asked.

2. **Stop Conditions (MUST)**
   - Stop and plan first if: change touches >3 files, ≥2 valid approaches exist, unsure about library API (check context7), task is ambiguous (Interview Mode), deleting/renaming public API, or confidence <0.85.

3. **Universal Principles (SHOULD)**
   - Readable, DRY, KISS, YAGNI, Crash Early, Deliberate, No Broken Windows, Boring is Good, Negative Lines, Fail Sanely, Small Batches, Shift Left, No Alert Without Runbook, Untested Code is Legacy.

4. **Plumber's Loop Core Workflow**
   - Follow: Classify → Analyze → Spec → Plan → Execute → Verify → Reflect.
   - WRAP Atomicity: <500 LOC/change, refactor XOR feature (never mixed).
   - Chain of Verification: Plan → Verify schema/routes → Tracer bullet (end-to-end skeleton) → Flesh out.
   - *See Coding Reference §5 for detailed workflow.*

5. **Anti-Patterns Gist (MUST avoid)**
   - No file/class/type names matching specific LLM names (e.g. *haiku*).
   - Delete security theater (client-only guards with no server verification).
   - Resolve user/operator identity ONLY from verified JWT, never request body.
   - Classify errors via structural properties (name, code, status), never message substrings.
   - Guard numeric inputs with `Number.isFinite()` at boundaries (HTML accepts "e").
   - Callers MUST guard state updates behind `{ committed }` / success flag to block stale memory.
   - *See Coding Reference §14 for details and production incident logs.*
