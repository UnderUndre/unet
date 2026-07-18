---
name: ai-engineering-hygiene
description: Catalog of AI-generated-code anti-patterns (TypeScript traps) and prompt/workflow hygiene rules. Consult BEFORE writing or reviewing code, and when shaping prompts or agent sessions. Triggers on code review, anti-pattern, codegen, prompt hygiene, token economy, refactor, lint, vibe-coding.
allowed-tools: Read, Grep, Glob
---

# AI Engineering Hygiene

Reference catalog, loaded on demand. Two tables: (1) AI-generated TypeScript code traps, (2) prompt/workflow anti-patterns. The root `CLAUDE.md` keeps only a pointer here so the always-loaded core stays lean — consult this skill before producing or reviewing code, or when tuning a prompt/session.

## AI-Generated Code Guardrails

Универсальные TS-грабли. Webapp-specific помечены [web].

| Anti-Pattern                                             | Correct Pattern                                                     |
| -------------------------------------------------------- | ------------------------------------------------------------------- |
| `process.env.X \|\| "fallback"`                          | `if (!env.X) throw new Error()`                                     |
| `as any`                                                 | Proper type or `unknown`                                            |
| `throw new Error()` (no class)                           | Typed error (`AppError.badRequest()`, domain enum)                  |
| `console.log()`                                          | `logger.info({ ctx }, 'msg')` (consola in this repo)                |
| `catch (e) { }` (swallow)                                | `catch (e) { logger.error({ err: e }); throw; }`                    |
| `if (x === y) return true` (unconditional bypass)        | Add a qualifying condition                                          |
| [web] `dangerouslySetInnerHTML`                          | `DOMPurify.sanitize()`                                              |
| [web] `req.body.field` without Zod                       | `schema.parse(req.body)`                                            |
| File/class named after LLM model (`haiku-compressor.ts`) | Name by **purpose** (`compressor.ts`); model = config               |
| `err.message.includes("timeout")` classification         | Structural signals: `err.name`, `err.code`, `instanceof`            |
| `Number(formValue)` without guard                        | `v === "" \|\| !Number.isFinite(Number(v)) ? undefined : Number(v)` |
| Caller ignoring `{ committed: boolean }` flag            | `if (result.committed) localState = newValue`                       |

Full catalog with production-incident backstories: [`.github/instructions/coding/copilot-instructions.md`](../../../.github/instructions/coding/copilot-instructions.md) §14.

## AI Engineering Coach Rules (adapted from [microsoft/AI-Engineering-Coach](https://github.com/microsoft/AI-Engineering-Coach))

Prompt/workflow anti-patterns. Marked ⚡ = adapted (existing guardrail takes precedence).

| # | Anti-Pattern | Why It Bites | Correct Pattern |
|---|-------------|-------------|-----------------|
| 1 | Single-message sessions (abandon after 1 prompt) | No refinement → garbage output | Iterate with follow-up messages; one-shot rarely works |
| 2 | Agent mode for simple questions ("what is JWT?") | Pays agent-loop tax for zero tool use | Use chat/ask mode for quick Qs; agent mode for multi-step work |
| 3 | Agentic requests with no tools enabled | Agent mode sans tools = expensive chat | Enable file search, terminal, web search in agent mode |
| 4 | Auto-approved terminal commands | Blind execution of AI-generated `rm`, `DROP TABLE`, etc. | Review before run; session-scoped approval only for trusted tools |
| 5 | Pinning one premium model for every request | Overpays on simple work; no auto-routing savings | Default to `auto`; reserve premium for hard reasoning/planning |
| 6 | Fragmented coding flow (constant context switches) | Long pauses + scattered blocks = never deep in flow | Block 2+ hr uninterrupted slots; batch meetings |
| 7 | Prompt cache starvation (large prompts, 0% cache hits) | Every request pays full price for same prefixes | Stabilize prompt front: short stable instructions, avoid frequent compaction |
| 8 | Caps-lock rage prompts | Signals frustration → worse communication → worse output | Step away, breathe, return with structured prompt |
| 9 | Context engineering gaps (no agents/skills/MCP/instructions) | AI lacks project context → generic answers | Set up AGENTS.md, SKILL.md, MCP tools, #file refs, .instructions.md |
| 10 | Copy-paste blindness (large AI code, zero refinement) | Accepting unreviewed code = bugs + tech debt | Always review; ask follow-up Qs to refine, test, understand |
| 11 | Attaching 30+ files to a single prompt | Most files never read; paying for dead context | Be selective: 3-5 relevant files; use `#codebase` for on-demand search |
| 12 | Frustration signals (excessive !!!/???) | Approach isn't working → escalating makes it worse | New session, rephrase, break into smaller pieces |
| 13 | Excessive request cancellations | Wastes premium quota; indicates unclear prompting | Write clearer prompts; wait for responses |
| 14 | Oversized instruction files (>4 KB) | Bloats every request's input tokens | Trim to essentials; move examples to separate files; <4 KB |
| 15 | Late-night coding (midnight-5am) | Fatigue → more bugs, lower quality | Establish healthier hours; quality drops when tired |
| 16 | Lazy prompting (<30 chars, no context) | Garbage in → garbage out | Describe intent, constraints, expected output format |
| 17 | No constraints in prompts ("do not", "must", "avoid") | Unconstrained → boilerplate/hallucinated output | Add explicit constraints: "do not use X", "limit to N lines" |
| 18 | Zero markdown output (no specs/plans/docs) | Skipping specs → more iteration cycles | Spec-first: brief spec or plan before coding; even 3 bullets help |
| 19 | Tool/MCP bloat (>40 tools per session) | Every registered tool adds tokens to every prompt | Trim toolset; scope per workspace; group by relevance |
| 20 | Mega sessions (50+ messages) | Context degrades → accuracy drops | New sessions every 15-25 messages; break large tasks up |
| 21 | Single model for everything (no diversity) | Lighter models suffice for routine work | Use lighter models for simple tasks; premium for complex reasoning |
| 22 | No custom instructions file | Missing persistent project context | Create `.github/copilot-instructions.md` or `.instructions.md` |
| 23 | ⚡ Unsandboxed terminal execution (no devcontainer) | Agent commands modify host OS | Set up `.devcontainer/devcontainer.json` or use Codespaces |
| 24 | No file context in prompts | AI can't see relevant code → generic answers | Use `#file` refs; open files in editor for context |
| 25 | No language/framework exploration | Missing learning opportunities | Try new languages via AI; lowers barrier dramatically |
| 26 | Heavy agent usage, never plan mode | Jumping to implementation → wrong approaches | Use `/plan` or plan mode before complex tasks |
| 27 | No skills usage | Missing specialized domain knowledge | Explore IDE skills for frameworks, cloud, workflows |
| 28 | No slash commands | Missing targeted response patterns | `/fix` for bugs, `/explain` for understanding, `/tests` for coverage |
| 29 | No spec-driven development (no specs/plans before code) | Vibe-coding → more iterations, worse quality | Start with spec/plan/requirements; even brief ones beat nothing |
| 30 | Unstructured task starts (vague first prompts) | No bullets, no requirements → meandering output | Use bullet points, numbered requirements, acceptance criteria |
| 31 | Premium model for lookup questions ("what is X?") | Factual Qs don't need premium reasoning | Default to `auto`; reserve premium for actual reasoning tasks |
| 32 | Premium model for simple requests | Short prompt, no code output → wasted premium | Lighter models for quick Qs; premium for complex generation |
| 33 | Profanity/hostile language in prompts | Deep frustration → approach isn't working | Break, fresh session, different approach |
| 34 | High/max reasoning effort for all requests | 2-4× more output tokens; same answer for routine tasks | Default `medium`; escalate only for complex algorithms/ambiguous specs |
| 35 | Near-duplicate prompts repeated | Wastes quota without new results | Rephrase or add more context instead of retrying same message |
| 36 | Runaway agent loops (15+ tools per request) | Agent spinning on failing approaches | Break into smaller requests; cancel + rephrase with constraints |
| 37 | Session drift (4+ task types in one session) | Mixed-purpose confuses context | New session per task type (bug fix ≠ feature ≠ docs) |
| 38 | Slow responses (>30s) | Overly broad/complex prompts | Break into smaller, focused requests; lighter models for simple Qs |
| 39 | Speed-accept (<15s gap after 20+ AI LOC) | No time to review = bugs shipped | Read AI code before moving on; a glance is not a review |
| 40 | Single-workspace tunnel vision | Missing AI benefits across projects | Use AI in other workspaces too: docs, testing, DevOps |
| 41 | Verbose model output (>5K tokens from short prompt) | Burns completion budget without proportional value | Specify "concise", "one-line summary", "no commentary" |
| 42 | Verbose prompts with filler words | Paying token tax for pleasantries | Be terse: "write add(a,b)" not "please kindly write a function..." |
| 43 | Vibe-coding (high AI LOC, minimal prompts, no specs) | Velocity without understanding = knowledge debt | Slow down; spec first; review line by line; understand before moving on |
| 44 | Weekend overwork | Burnout → decreased productivity | Maintain work-life boundaries |
| 45 | YOLO mode (>90% auto-approve rate) | Agent runs virtually unsupervised | Review file edits, terminal cmds, web searches individually |
