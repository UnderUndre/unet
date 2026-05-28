# SpecKit Review: 004-desktop-integration

**Reviewer**: claude
**Reviewed at**: 2026-05-28T12:30:00Z
**Commit**: 5b92bfa5512d06df60c4ea41d11c0031b4730cf5
**Artifacts reviewed**: spec.md, plan.md, tasks.md, research.md, reviews/analyze.md

## Summary

Spec is thorough — 312 lines, 15 FRs, 7 user stories, well-prioritized. The headline weakness: **plan.md is skeletal (41 lines) and doesn't map critical spec requirements to tasks**. Two daemon-side API endpoints (autostart settings, event log) have zero implementation coverage. Port discovery mechanism contradicts between spec and plan. Tasks are all tray-side, leaving daemon extensions implicit.

## Findings

| ID | Severity | Area | Finding | Recommendation |
|---|---|---|---|---|
| F1 | HIGH | Consistency | plan.md hardcodes `localhost:8080/api/` (plan.md:4) but FR-003 requires "Port discovered from daemon config or API" (spec.md:172). No discovery mechanism in plan or tasks. | Add port discovery to plan: read from daemon config file, env var, or well-known path. Add task for discovery logic in `trayapi/client.go`. |
| F2 | HIGH | Coverage Gap | FR-007 extends spec 002 with `POST /api/settings/autostart` (spec.md:196) and FR-011 adds `GET /api/v1/events` (spec.md:212). Both require daemon-side endpoint implementation. No task covers daemon work — all 12 tasks are tray-side. Either these endpoints already exist (not verified) or tasks are missing. | Verify spec 002 provides these endpoints. If not, add daemon-side tasks (T013, T014) or explicitly mark as blocked-by/spec-002 with acceptance criteria. |
| F3 | MEDIUM | Atomicity | T007 (tasks.md:42) combines 5 concerns: state machine, daemon health polling, network monitor integration, exponential backoff reconnects, and notification dispatch. Likely exceeds 500 LOC per Principle II. | Split T007 → T007a (state machine + health polling), T007b (backoff + reconnect orchestrator), T007c (notification dispatch + throttling). |
| F4 | MEDIUM | Spec Hygiene | research.md resolves 7 `NEEDS CLARIFICATION` items (research.md:1-31) but spec.md still contains `[NEEDS CLARIFICATION]` markers at lines 13, 16, 143, 145, 149, 150, 152, 197, 202, 242, 302-311. Stale markers mislead implementers. | Update spec.md: replace resolved markers with decisions from research.md. Keep only genuinely unresolved items. |
| F5 | MEDIUM | Failure Mode | No HTTP client timeout specified for tray→daemon API calls. If daemon hangs (not crashes), tray goroutines leak indefinitely. Spec mentions 5s health check interval (FR-014) but not the client-side timeout. | Specify timeouts in trayapi design: 5s for status/health, 10s for connect/disconnect operations. |
| F6 | MEDIUM | Edge Case | No singleton guard for tray process. Multiple `unet-tray.exe` launches → duplicate tray icons, conflicting daemon API calls, confusing UX. | Add named mutex (`Local\unet-tray-singleton`) on tray startup. |
| F7 | MEDIUM | Security | `.graceful_exit` sentinel file location unspecified (research.md:14). Predictable temp path allows tampering — malicious process could create sentinel to suppress crash detection. | Specify location (e.g., `%LOCALAPPDATA%\unet\.graceful_exit`) and consider file permissions/ACL. |
| F8 | MEDIUM | Dependency | `go-toast/toast` requires PowerShell runtime (research.md:7 acknowledges ~500ms latency). PowerShell can be disabled via Group Policy in corporate environments — notifications silently fail with no fallback. | Document PowerShell as system requirement OR add balloon-tip fallback via Win32 `Shell_NotifyIcon` API. |
| F9 | MEDIUM | Edge Case | Network monitor polls every 2s (spec.md:202) with no confirmation logic. Single transient packet loss triggers full `awg-quick down/up` cycle — destructive for sub-second network blips. | Require 2 consecutive reachability failures before triggering reconnect. Add jitter to poll interval. |
| F10 | MEDIUM | Edge Case | Registry Run value with spaces in binary path (e.g., `C:\Program Files\unet\unet-tray.exe`) must be quoted. Spec doesn't specify quoting requirement. Unquoted paths with spaces break autostart silently. | Add to FR-007: Registry value MUST be `"C:\full\path\to\unet-tray.exe"` (quoted). Verify in autostart_windows.go. |
| F11 | LOW | Meta | analyze.md reported 0 findings across all dimensions for a 312-line spec with 15 FRs and 11 open questions. Self-consistency check wasn't adversarial enough. | Flag for future analyze runs — consider seeding with known-weak areas. |
| F12 | LOW | Clarity | FR-010 backoff sequence shows `1s → 2s → 4s → 8s → 16s → 32s → 60s cap` (spec.md:207-209). The 32→60 jump isn't 2x. Actual 2x sequence would be 64, capped at 60. Display is misleading about when cap applies. | Correct to: `1→2→4→8→16→32→60(cap)`. Clarify: each step = min(prev*2, 60). |
| F13 | LOW | Terminology | "Tray" used interchangeably for: system tray area, tray icon, tray process (`unet-tray.exe`), tray UI (menu). Non-technical readers may be confused. | Add terminology box at spec top: Tray = process, Icon = visual element, Menu = interaction surface. |
| F14 | LOW | API Design | `GET /api/v1/events` (FR-011, spec.md:212) has no pagination, retention policy, or cleanup. Unbounded growth in long-running daemon. | Add `?limit=N&after=<cursor>` pagination. Define retention (e.g., last 1000 events in memory, rotated). |

## Alternative Approaches Considered

1. **Named pipes instead of HTTP for tray↔daemon IPC**: On Windows, named pipes offer ACL-based security (only current user can connect) and no port-conflict risk. HTTP is simpler and cross-platform but requires port management. Worth weighing for v2 — HTTP is acceptable for v1 given the localhost-only constraint.

2. **Windows Service for daemon instead of Registry Run**: Windows Services provide auto-restart on crash, delayed start, dependency ordering. More robust than Registry Run for the daemon lifecycle. Tray stays as user-process. Consider for future spec — current approach is acceptable for P1.

3. **Windows Task Scheduler for autostart**: More flexible than Registry Run (trigger conditions, retry on failure, delayed start until network ready). Addresses F9 (network not ready at login) and F10 (quoting) automatically. More complex to configure programmatically but more reliable. Worth considering as P2 enhancement.

## VERDICT

```yaml
verdict: HIGH
reviewer: claude
reviewed_at: 2026-05-28T12:30:00Z
commit: 5b92bfa5512d06df60c4ea41d11c0031b4730cf5
critical_count: 0
high_count: 2
medium_count: 8
low_count: 4
```
