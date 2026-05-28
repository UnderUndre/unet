# Tasks: Desktop Integration

## Dependency Graph
T001 → T002, T003
T002 + T003 → T004
T004 → T005
T005 → T006
T006 → T007
T007 → T008
T008 → T009
T009 → T011
T011 → T010, T012

## Parallel Lanes
- **Lane 1**: UI & OS Hooks (T002, T004, T005, T008, T009)
- **Lane 2**: API & Network (T003, T006, T007)

## Agent Summary
- `[SETUP]`: 1
- `[FE]`: 5
- `[BE]`: 3
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
- [ ] T007 [BE] [US4] Implement `internal/traylogic/manager.go` integrating tray state, network monitor, API polling (daemon health), exponential backoff reconnects, and dispatching notifications.

## Phase 7: User Story 5 (Toggle Autostart)
- [ ] T008 [FE] [US5] Implement the "Start at login" toggle in the tray menu logic in `internal/platform/tray_windows.go`, reading/writing via `internal/platform/autostart_windows.go`.

## Phase 8: User Story 6 (Multi-VPS Switching)
- [ ] T009 [FE] [US6] Implement the UI abstraction for VPS switching in `internal/platform/tray.go` and `tray_windows.go`, with a minimal dummy stub for current state since daemon doesn't support multi-VPS yet.

## Phase 9: User Story 7 (Cross-Platform Abstraction Documented)
- [ ] T010 [DOC] [US7] Create `internal/platform/README.md` documenting the cross-platform abstraction interfaces (`Tray`, `Notifier`, `NetworkMonitor`, `AutoStart`) and stubs for macOS/Linux `tray_darwin.go` and `tray_linux.go`.

## Phase 10: Polish & Integration
- [ ] T011 [FE] Update `cmd/unet-tray/main.go` to wire all components together and start the tray loop.
- [ ] T012 [E2E] Create and execute manual verification test script in `tests/e2e/desktop_test.md` to verify autostart, network disconnect/reconnect, and notifications.
