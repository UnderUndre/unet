# SpecKit Review Context: 004-desktop-integration

This document contains the complete context for a critical review of the **Desktop Integration** feature for the **unet** project.

**Feature Slug**: 004-desktop-integration
**Context gathered at**: 2026-05-30
**Repository Constitution Version**: 1.4.0

---

## 1. PROJECT CONSTITUTION (.specify/memory/constitution.md)
Governs principles, quality gates, and cross-AI review rules. (See Principles I, VI, VII).

---

## 2. GLOBAL ARCHITECTURE (specs/main/architecture.md)
Shows how Desktop Integration fits into the unet ecosystem (as a tray client interacting with the daemon API).

---

## 3. FEATURE SPECIFICATION (specs/004-desktop-integration/spec.md)
The "What" and "Why".

```markdown
# Feature Specification: Desktop Integration

## Resolved Decisions
- **Tray library**: `fyne.io/systray` (Win/macOS/Linux).
- **Process model**: Separate `unet-tray.exe` communicating via daemon HTTP API.
- **Windows Notification**: `go-toast/toast` (Toast API) with balloon-tip fallback.
- **Autostart**: Registry `HKCU\...Run` (no UAC, user-scope).
- **Network Change Detection**: Default-route polling every 2s (10s reconnect SLA).

## User Scenarios
1. Tray Status at Login (P1) - Green/Yellow/Red visual feedback.
2. Quick Actions Menu (P1) - Connect/Disconnect, Copy URL, Open UI, Quit.
3. Auto-Reconnect on Network Change (P1) - Seamless WiFi/4G transitions.
4. OS Notifications (P2) - Awareness for background events.
5. Toggle Autostart (P2) - User control via Tray or Admin UI.
6. Multi-VPS Switching (P3) - Future-proofed abstraction.

## Requirements (Highlights)
- **FR-001**: Tray icon visual states (Connected, Transition, Error).
- **FR-003**: Context menu actions and state-awareness.
- **FR-005**: OS-native notification dispatch with throttling.
- **FR-007**: Autostart management via Registry HKCU.
- **FR-009**: Network change detection (2 reachability failures threshold).
- **FR-010**: Exponential backoff reconnect (1s -> ... -> 60s).
- **FR-014**: Daemon health monitoring and auto-restart prompt on crash.
```

---

## 4. IMPLEMENTATION PLAN (specs/004-desktop-integration/plan.md)
The "How".

```markdown
# Implementation Plan

- **Target**: `unet-tray.exe` separate binary.
- **IPC**: localhost HTTP API (`localhost:8080/api/`).
- **Components**:
  - `cmd/unet-tray`: Entrypoint.
  - `internal/platform`: OS abstractions (tray, notifier, autostart, netmon).
  - `internal/trayapi`: HTTP client wrappers.
  - `internal/traylogic`: State machine, backoff logic, notifier orchestration.
- **Daemon Extensions**:
  - `POST /api/v1/settings/autostart`.
  - `GET /api/v1/events` (structured event log ring buffer).
  - `.graceful_exit` sentinel for crash detection.
```

---

## 5. TASKS (specs/004-desktop-integration/tasks.md)
The "When".

```markdown
# Tasks (10 Phases)
- Phase 1: Setup (`cmd/unet-tray`, deps).
- Phase 2: Platform Abstractions (Registry, Toast, API Client).
- Phase 3-4: Tray UI & Menu.
- Phase 5: Network Monitor (Polling).
- Phase 6: State Machine & Reconnect Logic.
- Phase 6b: Daemon-side Extensions (Events API, Sentinel).
- Phase 7-10: Autostart toggle, Multi-VPS stub, Docs, E2E.
```

---

## 6. RESEARCH & DECISIONS (specs/004-desktop-integration/research.md)
Rationale for key technical choices.

```markdown
# Research Summary
- Polling (2s) chosen over NLM COM events for simplicity/reliability.
- `.graceful_exit` sentinel allows distinguishing crash vs manual kill.
- Separate process model ensures daemon stability and headless operation.
- WebSocket/SSE deferred; polling (3s) sufficient for v0.1.
```

---

## 7. CROSS-ARTIFACT ANALYSIS
(To be performed by /speckit.analyze)

---

**ACTION FOR REVIEWER**: 
Perform a critical adversarial review using the lenses defined in `.agent/workflows/speckit.review.md`:
- **Logical consistency**: (e.g. Does the tray accurately reflect daemon-side autostart changes?)
- **Hidden assumptions**: (e.g. Does `go-toast` PowerShell dependency create security or latency risks?)
- **Missing edge cases**: (e.g. Tray behavior during Windows "Fast User Switching" or RDP.)
- **Failure modes**: (e.g. Network change detection false positives/negatives.)
- **Security & privacy threats**: (e.g. Registry path injection, Toast notification content leakage.)
- **Performance & scale**: (e.g. Impact of 2s polling on battery/CPU.)
- **Alternative approaches**: (e.g. Single-binary with GUI vs current split-process model.)
- **Constitution alignment**.

Write your report to `specs/004-desktop-integration/reviews/<provider>.md`.
