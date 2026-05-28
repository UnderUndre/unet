# Tasks: Desktop Integration

## Dependency Graph
T001 → T002, T003
T002 + T003 → T004
T004 → T005
T005 → T006
T006 → T007a
T007a → T007b
T007b → T007c
T007c → T008
T008 → T009
T009 → T011
T011 → T010, T012
T003 → T013, T014, T015 (parallel with tray work)

## Parallel Lanes
- **Lane 1**: UI & OS Hooks (T002, T004, T005, T008, T009)
- **Lane 2**: API & Network (T003, T006, T007a, T007b, T007c)
- **Lane 3**: Daemon Extensions (T013, T014, T015)

## Agent Summary
- `[SETUP]`: 1
- `[FE]`: 5
- `[BE]`: 6
- `[DOC]`: 1
- `[E2E]`: 1

## Phase 1: Setup
- [ ] T001 [SETUP] Initialize `cmd/unet-tray/main.go`, update `Makefile` to build `unet-tray`, and add `fyne.io/systray`, `github.com/go-toast/toast` dependencies.

## Phase 2: Foundational (Platform Abstractions)
- [ ] T002 [FE] Implement `internal/platform/autostart_windows.go` (Registry logic) and `notifier_windows.go` (Toast logic).
- [ ] T003 [BE] Implement `internal/trayapi/client.go` to wrap the daemon's localhost API endpoints (`status`, `connect`, `disconnect`, `ports`).

## Phase 3: User Story 1 (Tray Shows Tunnel Status)
- [ ] T004 [FE] [US1] Implement `internal/platform/tray_windows.go` using `fyne.io/systray` with embedded icons (green, yellow, red).

## Phase 4: User Story 2 (Tray Quick Actions)
- [ ] T005 [FE] [US2] Implement the tray menu structure and click handlers bridging to `trayapi.Client` in `internal/platform/tray_windows.go`.

## Phase 5: User Story 3 (Auto-Reconnect on Network Change)
- [ ] T006 [BE] [US3] Implement `internal/platform/network_monitor.go` with 2s polling for default route reachability.

## Phase 6: User Story 4 (OS Notifications)
- [ ] T007a [BE] [US4] Implement `internal/traylogic/state.go` — tray state machine, daemon health polling (5s interval, 5s timeout, 3-failure threshold), and icon/tooltip state transitions.
- [ ] T007b [BE] [US4] Implement `internal/traylogic/reconnect.go` — exponential backoff reconnect orchestrator integrating with `NetworkMonitor`, calling `trayapi.Connect()` with full `awg-quick down/up` cycle. Backoff: min(prev×2, 60), starting at 1s.
- [ ] T007c [BE] [US4] Implement `internal/traylogic/notifier.go` — notification dispatch with per-event-type throttling (max 1 per 60s). Integrates with `platform.Notifier`. Tracks notification state in-memory.

## Phase 6b: Daemon-side Extensions
- [ ] T013 [BE] Add `POST /api/v1/settings/autostart` endpoint to daemon (extends spec 002 control plane). Accepts `{enabled: bool}`. Writes/removes Registry `HKCU\...\Run` entry with quoted tray binary path.
- [ ] T014 [BE] Add `GET /api/v1/events` endpoint to daemon. Supports `?limit=N&after=<cursor>` pagination. Events stored in ring buffer (1000 entries). Returns structured event log (network changes, reconnect attempts, tunnel state transitions).
- [ ] T015 [BE] Add `.graceful_exit` sentinel file logic to daemon: write `%LOCALAPPDATA%\unet\.graceful_exit` on clean shutdown, delete on startup.

## Phase 7: User Story 5 (Toggle Autostart)
- [ ] T008 [FE] [US5] Implement the "Start at login" toggle in the tray menu logic in `internal/platform/tray_windows.go`, reading/writing via `internal/platform/autostart_windows.go`.

## Phase 8: User Story 6 (Multi-VPS Switching)
- [ ] T009 [FE] [US6] Implement the UI abstraction for VPS switching in `internal/platform/tray.go` and `tray_windows.go`, with a minimal dummy stub for current state since daemon doesn't support multi-VPS yet.

## Phase 9: User Story 7 (Cross-Platform Abstraction Documented)
- [ ] T010 [DOC] [US7] Create `internal/platform/README.md` documenting the cross-platform abstraction interfaces (`Tray`, `Notifier`, `NetworkMonitor`, `AutoStart`) and stubs for macOS/Linux `tray_darwin.go` and `tray_linux.go`.

## Phase 10: Polish & Integration
- [ ] T011 [FE] Update `cmd/unet-tray/main.go` to wire all components together and start the tray loop.
- [ ] T012 [E2E] Create and execute manual verification test script in `tests/e2e/desktop_test.md` to verify autostart, network disconnect/reconnect, and notifications.
