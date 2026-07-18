---
name: lint-and-validate
description: Automatic quality control, linting, and static analysis procedures. Use after every code modification to ensure syntax correctness and project standards. Triggers onKeywords: lint, format, check, validate, types, static analysis.
allowed-tools: Read, Glob, Grep, Bash
---

# Lint and Validate Skill

> **MANDATORY:** Run appropriate validation tools after EVERY code change. Do not finish a task until the code is error-free.

### Procedures by Ecosystem

#### Node.js / TypeScript
1. **Lint/Fix:** `npm run lint` or `npx eslint "path" --fix`
2. **Types:** `npx tsc --noEmit`
3. **Security:** `npm audit --audit-level=high`

#### Python
1. **Linter (Ruff):** `ruff check "path" --fix` (Fast & Modern)
2. **Security (Bandit):** `bandit -r "path" -ll`
3. **Types (MyPy):** `mypy "path"`

## The Quality Loop
1. **Write/Edit Code**
2. **Run Audit:** `npm run lint && npx tsc --noEmit`
3. **Analyze Report:** Check the "FINAL AUDIT REPORT" section.
4. **Fix & Repeat:** Submitting code with "FINAL AUDIT" failures is NOT allowed.

## Error Handling
- If `lint` fails: Fix the style or syntax issues immediately.
- If `tsc` fails: Correct type mismatches before proceeding.
- If no tool is configured: Check the project root for `.eslintrc`, `tsconfig.json`, `pyproject.toml` and suggest creating one.
## Automatable AI Engineering Coach Checks

Rules from [microsoft/AI-Engineering-Coach](https://github.com/microsoft/AI-Engineering-Coach) that can be caught by linting:

| Check | Detection Method | Rule |
|-------|-----------------|------|
| `as any` in TS files | `grep -r "as any" --include="*.ts"` | Proper type or `unknown` |
| `console.log()` left in code | `grep -r "console.log" --include="*.ts" --include="*.tsx"` | Use `logger.info()` |
| Empty catch blocks | `grep -rz "catch.*{ *}" --include="*.ts"` | Log + rethrow |
| `process.env.X \|\| "fallback"` | `grep -rE 'process\.env\.\w+\s*\|\|' --include="*.ts"` | Throw if missing |
| Instruction file >4 KB | `wc -c .github/copilot-instructions.md` | Trim to <4 KB |
| Oversized custom instructions | Check `.claude/` and `.github/instructions/` file sizes | Move examples to separate files |
| `dangerouslySetInnerHTML` | `grep -r "dangerouslySetInnerHTML" --include="*.tsx"` | Use `DOMPurify.sanitize()` |
| Magic numbers | `eslint no-magic-numbers` rule | Named constants |
| `any` type | `tsc --noEmit` + `eslint @typescript-eslint/no-explicit-any` | Proper types |
| Missing error class | `grep -rE "throw new Error\(" --include="*.ts"` | Typed error classes |

---

**Strict Rule:** No code should be committed or reported as "done" without passing these checks.

---

## Scripts

| Script | Purpose | Command |
|--------|---------|---------|
| `scripts/lint_runner.py` | Unified lint check | `python scripts/lint_runner.py <project_path>` |
| `scripts/type_coverage.py` | Type coverage analysis | `python scripts/type_coverage.py <project_path>` |

