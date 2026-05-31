# Feature Specification: Desktop Integration (System Tray, Autostart, Notifications, Auto-Reconnect)

**Feature Branch**: `004-desktop-integration`
**Created**: 2026-05-27
**Status**: Draft
**Input**: Desktop UX for unet — system tray, autostart, OS notifications, automatic reconnect on network change. Daemon currently runs in foreground from terminal; no tray, no autostart, no awareness of WiFi/4G transitions. Unusable as daily-driver for non-technical users.

## Terminology

| Term | Meaning |
|---|---|
| **Tray** | The `unet-tray.exe` process — the UI layer |
| **Tray icon** | The visual element in the system tray area |
| **Tray menu** | The right-click context menu (interaction surface) |
| **Daemon** | The `unet.exe` background process managing WireGuard tunnels |
| **VPS health** | VPS endpoint is reachable AND responding to health checks |

## Clarifications

### Session 2026-05-27

- Q: Tray library choice? → **Decision: `fyne.io/systray`** — Actively maintained fork of getlantern/systray; Win+macOS+Linux coverage with minimal deps.
- Q: Windows notification mechanism? → **Decision: `github.com/go-toast/toast` for P1** — Modern Toast notifications (Windows 10+). Minimum version baseline: Windows 10 1809+. AmneziaWG itself requires Win10+, so no legacy constraint conflict. PowerShell dependency accepted; fallback to Win32 balloon tips if PowerShell unavailable.
- Q: Tray process model? → **Decision: Separate executable communicating via daemon HTTP API** — Crash isolation, independent updates, clean separation of concerns; daemon stays headless-friendly.
- Q: Autostart mechanism on Windows? → **Decision: Registry HKCU Run** — No UAC prompt, user-scope, industry-standard for desktop tools (Discord, Slack); simple launch semantics suffice.
- Q: Network change detection scope? → **Decision: Monitor default-route reachability as P1** — The tunnel only cares about "can I reach the VPS endpoint." Per-interface events as diagnostic log enrichment (P2).
- Q: macOS/Linux parity timeline? → **Decision: Within this same spec (Win impl as P1, macOS/Linux as P2/P3)** — Designing abstraction now prevents costly retrofit; impl can be staged but the interface is defined once.

### Session 2026-05-27 (round 1)

| Topic | Decision |
|---|---|
| Tray library | `fyne.io/systray` |
| Tray process model | Separate executable communicating via daemon HTTP API |
| Windows autostart mechanism | Registry HKCU Run |
| macOS/Linux parity timeline | Within this same spec (Win P1, macOS/Linux P2/P3) |

See inline notes in each FR / section for full rationale.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Tray Shows Tunnel Status at Login (Priority: P1)

As a developer, I want unet to start automatically at login and show a system tray icon reflecting my tunnel + VPS health (green/yellow/red) so that I know my connectivity status at a glance without opening a terminal.

**Why this priority**: Without tray + autostart, unet requires manual terminal invocation every reboot. This is the single biggest UX gap between unet and commercial alternatives (ngrok, Tailscale). No other feature matters if the user has to `cd` to a directory and run a binary by hand.

**Independent Test**: Enable autostart. Reboot. Verify tray icon appears in system tray within 10s of desktop ready. Verify icon color matches tunnel state (green = connected, yellow = connecting, red = error/disconnected). Right-click tray → verify menu appears within 200ms.

**Acceptance Scenarios**:

1. **Given** autostart is enabled and daemon was connected before reboot, **When** user logs in, **Then** tray icon appears within 10s, icon is green, and tunnel auto-connects.
2. **Given** autostart is enabled and daemon was disconnected before reboot, **When** user logs in, **Then** tray icon appears within 10s, icon is red, no auto-connect attempted.
3. **Given** autostart is disabled, **When** user logs in, **Then** no tray icon appears, no daemon process running.

---

### User Story 2 - Tray Quick Actions Menu (Priority: P1)

As a developer, I want to right-click the tray icon and see a context menu with quick actions — Connect/Disconnect, Copy public URL, Open admin UI, About, Quit — so that I can control unet without opening the web UI or terminal.

**Why this priority**: Tray menu is the primary interaction surface. Without it, the tray icon is passive decoration. This is what makes the tray functional vs. decorative.

**Independent Test**: Start tray. Right-click icon. Verify menu items appear. Click "Connect" → verify tunnel connects and icon turns green. Click "Copy public URL" → verify clipboard contains `https://*.domain.com`. Click "Quit" → verify daemon shuts down gracefully.

**Acceptance Scenarios**:

1. **Given** tunnel is disconnected, **When** user right-clicks tray and selects "Connect", **Then** tunnel initiates connection and icon transitions yellow → green on success.
2. **Given** tunnel is connected and at least one route is exposed, **When** user selects "Copy public URL", **Then** clipboard contains the first active route's public URL. If multiple routes, sub-menu lists all.
3. **Given** tray is running, **When** user selects "Open admin UI", **Then** default browser opens to `http://localhost:8080`.
4. **Given** tray is running, **When** user selects "Quit", **Then** daemon disconnects tunnel, removes WireGuard interface, and exits. No orphan processes.

---

### User Story 3 - Auto-Reconnect on Network Change (Priority: P1)

As a developer working on a laptop, I want the tunnel to automatically reconnect within 10 seconds when my network changes (WiFi → 4G, WiFi reconnect, Ethernet → WiFi) without any manual action, so that my exposed services stay accessible during mobile work.

**Why this priority**: Network transitions are the #1 reliability pain point for tunnel users. If unet can't handle WiFi→4G seamlessly, it's unusable for laptop users — the primary target demographic.

**Independent Test**: Connect tunnel over WiFi. Disable WiFi (or switch to 4G hotspot). Verify tray icon turns yellow within 5s. Verify tunnel reconnects and icon turns green within 10s total. Verify exposed routes resume working.

**Acceptance Scenarios**:

1. **Given** tunnel is connected over WiFi, **When** WiFi disconnects and 4G activates, **Then** tray icon turns yellow within 5s, tunnel reconnects within 10s, icon turns green.
2. **Given** tunnel is connected and network changes, **When** reconnect attempt fails (VPS unreachable on new network), **Then** exponential backoff begins (1s → 2s → 4s → 8s → ... → 60s cap) and OS notification fires "unet: reconnecting (attempt N)".
3. **Given** tunnel is in backoff and network becomes stable, **When** next reconnect attempt succeeds, **Then** tunnel connects and notification fires "unet: tunnel connected".

---

### User Story 4 - OS Notifications for Tunnel Events (Priority: P2)

As a developer, I want to receive OS-native notifications when significant tunnel events occur (disconnect, reconnect, error) so that I'm aware of connectivity changes even when the tray icon is in the overflow area.

**Why this priority**: Notifications complement the tray icon. The tray shows current state; notifications announce state transitions. P2 because the tray icon already provides visual feedback — notifications add awareness for background events.

**Independent Test**: Connect tunnel. Simulate disconnect (kill `awg-quick`). Verify OS notification appears within 2s with text "unet: tunnel disconnected, retrying". Verify no more than 1 notification per reconnect cycle (no spam during backoff).

**Acceptance Scenarios**:

1. **Given** tunnel disconnects unexpectedly, **When** disconnect is detected, **Then** OS notification fires within 2s: "unet: tunnel disconnected, retrying".
2. **Given** tunnel reconnects after disconnect, **When** connection is established, **Then** OS notification fires: "unet: tunnel connected".
3. **Given** tunnel is in exponential backoff, **When** multiple reconnect attempts fail, **Then** no notification spam — max 1 notification per reconnect cycle (disconnected → connected = 1 cycle; if never reconnects, max 1 notification per minute).

---

### User Story 5 - Toggle Autostart from Tray or Admin UI (Priority: P2)

As a developer, I want to enable or disable autostart from the tray menu or the admin web UI so that I don't have to edit the Windows Registry manually.

**Why this priority**: Autostart management is essential for user control. P2 because the initial setup (installer or first-run wizard) handles the first enable; tray/UI toggle handles subsequent changes.

**Independent Test**: Start tray. Right-click → "Settings" → toggle "Start at login" ON. Verify Registry Run key contains unet entry. Toggle OFF. Verify Registry key removed. Reboot to confirm behavior.

**Acceptance Scenarios**:

1. **Given** autostart is disabled, **When** user enables it via tray menu, **Then** Registry `HKCU\...\Run` key is set with correct binary path (no UAC prompt).
2. **Given** autostart is enabled, **When** user disables it via tray menu or admin UI, **Then** Registry key is removed, next login does not start unet.
3. **Given** autostart is enabled, **When** user changes the unet binary location, **Then** autostart entry updates to reflect new path on next daemon start.

---

### User Story 6 - Tray Shows Active VPS and Switching for Multi-VPS Users (Priority: P3)

As a developer managing multiple VPS endpoints, I want the tray to show which VPS is currently active and offer a sub-menu to switch between VPS instances so that I can manage multiple tunnels without opening the admin UI.

**Why this priority**: Multi-VPS is a power-user scenario. The current architecture is 1:1 (one daemon = one VPS), but the tray should be designed to handle the transition when multi-VPS support lands. P3 — design the abstraction now, implement minimal version.

**Independent Test**: Configure two VPS entries. Verify tray shows active VPS name. Right-click → "Switch VPS" → select second VPS. Verify tunnel reconnects to second VPS.

**Acceptance Scenarios**:

1. **Given** only one VPS configured, **When** tray menu opens, **Then** "Switch VPS" sub-menu is absent or disabled (single VPS — nothing to switch to).
2. **Given** multiple VPS configured, **When** user switches VPS via sub-menu, **Then** current tunnel disconnects gracefully, new tunnel connects to selected VPS, icon reflects new connection state.

---

### User Story 7 - Cross-Platform Tray Abstraction Documented (Priority: P3)

As a future contributor, I want the tray implementation to use a well-defined platform abstraction layer so that adding macOS menu bar or Linux AppIndicator support requires implementing the interface, not refactoring the core.

**Why this priority**: This is a design investment, not a deliverable. The abstraction must exist from day one to avoid Windows-specific coupling. P3 because the value materializes in future specs.

**Independent Test**: Review `platform.Tray` interface. Verify it defines: `SetIcon(state)`, `SetMenu(items)`, `OnReady()`, `OnExit()`. Verify Windows implementation satisfies the interface. Verify macOS/Linux stubs exist with TODO markers.

**Acceptance Scenarios**:

1. **Given** the `platform.Tray` interface, **When** a new platform implementation is written, **Then** it can be swapped via build tags without changing any tray logic code.
2. **Given** macOS documentation in this spec, **When** implementation begins, **Then** the abstraction covers macOS menu bar requirements (NSStatusItem, menu bar position, click behavior differences).

### Edge Cases

- **Active VPN interference**: User has another VPN (e.g., Mullvad, corporate VPN) active when network changes. Our network change detection MUST distinguish between "my network changed" and "a VPN adapter appeared/disappeared". Filter events by adapter type or monitor only the default route's upstream interface. Silently handle — if default route is reachable, attempt connection regardless of other VPNs.
- **Multiple simultaneous adapter changes**: WiFi disconnects AND Ethernet connects in the same event batch. Network monitor must handle batched events gracefully — not trigger multiple reconnect sequences. Debounce: coalesce events within 500ms window.
- **Tray crashes while daemon healthy**: Tray process exits unexpectedly (user killed it, OOM, bug). Daemon MUST continue running unaffected. Tray restart path: user re-launches `unet-tray.exe` manually. Daemon does NOT auto-respawn tray (deferred/out of scope).
- **Daemon crashes while tray healthy**: Daemon process exits (panic, killed, updated). Tray MUST detect daemon absence (health check ping to localhost API fails) and display red icon + "Daemon stopped" message. Tray offers "Restart daemon" menu item. If user consents, tray re-launches daemon binary.
- **User manually killed daemon**: Tray detects daemon death. Tray MUST show "Daemon stopped" but MUST NOT auto-restart without explicit user consent (menu click). This prevents unwanted resource consumption if the user intentionally stopped the daemon.
- **Hibernate/sleep wake-up**: Laptop wakes from sleep. Network adapter reports "connected" but default route is unreachable for 5-10s (DHCP renewal, ARP resolution). Network monitor MUST wait for actual reachability (e.g., ping VPS endpoint or gateway) before declaring "network up". Premature reconnect attempt on stale route wastes time and generates spurious error notifications.
- **Fast user switching**: User A has unet running. User B switches in via Windows fast user switch. Both desktops share the same network stack. Per-user tray instance is acceptable — each user session gets its own tray, daemon is user-scoped.
- **Daemon binary updated while tray running**: Auto-update or manual replace. Tray detects binary change (path or hash mismatch). Must not launch stale binary on restart. Tray detects version mismatch and prompts "daemon updated, restart now?" Auto-update itself is out of scope for this spec.
- **Tray launched without daemon running**: Standalone tray start. Tray shows red icon, "Daemon not running" status, offers "Start daemon" menu action. No error dialogs.
- **RDP/remote desktop session**: Tray runs in RDP session. Network transitions inside RDP sessions might not fire standard events. Best effort support. Documented limitation — not a guaranteed scenario.


## Requirements *(mandatory)*

### Functional Requirements

**Tray Icon (P1)**:

- **FR-001**: System tray icon MUST display one of three visual states reflecting tunnel + VPS health:
  - **Green** (connected): tunnel established, VPS reachable, at least one handshake within last 75s (3 × PersistentKeepalive).
  - **Yellow** (connecting/transient): tunnel connection in progress, reconnect in backoff, or network change detected but reconnect not yet attempted.
  - **Red** (disconnected/error): tunnel down, VPS unreachable, or daemon not running.
  - State transitions MUST update the icon within 1s of the underlying state change. Icons embedded as PNG in binary via Go `embed` — 16×16 and 32×32 variants for DPI scaling.
- **FR-002**: Tray icon MUST display a tooltip on hover showing: tunnel status, active VPS name/host, number of exposed routes, and daemon uptime. Tooltip MUST update within 2s of state change.

**Tray Context Menu (P1)**:

- **FR-003**: Right-click on tray icon MUST display a context menu within 200ms of click event. Menu items:
  - **Connect / Disconnect** (toggle): connects or disconnects the WireGuard tunnel. Label changes based on current state.
  - **Copy public URL**: copies the first (or selected) active route's public URL to clipboard. If multiple routes, expand to sub-menu listing each route's subdomain. Disabled when no routes are exposed.
  - **Open admin UI**: opens `http://localhost:<PORT>` in default browser. Port discovered from daemon config or API.
  - **Separator**.
  - **Start at login** (checkbox): toggles autostart. Checked state reflects current Registry Run key presence.
  - **Separator**.
  - **About**: shows version, build hash, and link to project repository.
  - **Quit**: initiates graceful shutdown (FR-008).
- **FR-004**: Menu item state MUST reflect current daemon state. "Connect" is disabled when already connected; "Disconnect" disabled when disconnected. "Copy public URL" disabled when no active routes. Menu MUST refresh state on every open (no stale cached state).

**OS Notifications (P2)**:

- **FR-005**: The tray MUST dispatch OS-native notifications for significant tunnel lifecycle events:
  - Tunnel disconnected (unexpected): "unet: tunnel disconnected, retrying…"
  - Tunnel reconnected: "unet: tunnel connected"
  - Tunnel error (persistent, >3 failed reconnects): "unet: cannot reach VPS — check network"
  - Daemon crashed (detected by tray): "unet: daemon stopped"
  - Notification throttling: max 1 notification per event type per 60s. During exponential backoff, only the first disconnect and final outcome (connected or persistent error) generate notifications — no per-attempt spam.
- **FR-006**: On Windows, notifications MUST use the modern Toast notification API (Windows 10+) via `github.com/go-toast/toast`. **System requirement**: PowerShell must be available (go-toast invokes PowerShell under the hood). If PowerShell is unavailable (e.g., disabled via Group Policy), fall back to Win32 `Shell_NotifyIcon` balloon tips. On future platforms: macOS `UNUserNotificationCenter` (macOS 11+), Linux `libnotify` via D-Bus. Platform abstraction via `platform.Notifier` interface.

**Autostart (P2)**:

- **FR-007**: The tray MUST provide autostart management:
  - **Enable**: Write entry to Windows Registry `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` with key name `unet` and value = quoted absolute path to tray binary (e.g., `"C:\Users\user\AppData\Local\unet\unet-tray.exe"`). User-scope only — no UAC elevation required. Path MUST be quoted to handle spaces.
  - **Disable**: Delete the Registry entry.
  - **Sync on start**: On tray launch, verify Registry entry matches current binary path. If binary moved (common during updates), update the entry silently.
  - **Admin UI parity**: Autostart toggle MUST also be accessible from the admin web UI via `POST /api/settings/autostart` (depends on spec 002 control plane API, extend with settings endpoint).
  - Autostart launches tray binary → tray discovers daemon (localhost API health check) → starts daemon if not running.

**Network Change Detection and Auto-Reconnect (P1)**:

- **FR-009**: The tray MUST detect network connectivity changes that affect the default route and trigger tunnel reconnect:
  - **Windows P1**: Poll default-route reachability every 2s. Simple, acceptable latency for 10s reconnect SLA. NLM event-driven as P2 optimization.
  - **Detection scope**: Monitor default-route reachability (can we reach the VPS endpoint or a known-good gateway?). Per-interface events are logged for diagnostics but do not drive reconnect logic independently.
  - **Debounce**: Coalesce network events within 500ms window into a single reconnect trigger.
  - **Confirmation**: Require **2 consecutive reachability failures** before triggering reconnect. Single transient packet loss (<2s) MUST NOT trigger a full `awg-quick down/up` cycle. Jitter poll interval by ±500ms to avoid thundering herd.
- **FR-010**: On network change detection, the system MUST attempt automatic tunnel reconnect with exponential backoff:
  - Initial delay: 1s after network change confirmed (2 consecutive failures).
  - Backoff multiplier: 2×, each step = min(prev×2, 60): `1 → 2 → 4 → 8 → 16 → 32 → 60(cap)`.
  - Cap: 60s between attempts.
  - Reset: backoff resets to 1s on successful connection.
  - Max attempts: unlimited (user must explicitly disconnect or quit to stop).
  - Reconnect MUST be a full `awg-quick down && awg-quick up` cycle, not a hot-reload attempt.
- **FR-011**: The system MUST log every network change event (type, timestamp, affected interfaces, reconnect outcome) in a structured format for diagnostics. Log entries available via `GET /api/v1/events?limit=N&after=<cursor>` (extends spec 002 control plane API) and written to daemon log file. Pagination: default limit=100, max=1000. Retention: last 1000 events kept in ring buffer, older events discarded. Events endpoint is localhost-only — no authentication required.

**Graceful Shutdown (P1)**:

- **FR-012**: "Quit" from tray menu MUST initiate graceful shutdown:
  - Disconnect tunnel (`awg-quick down`).
  - Remove any exposed routes from Caddy (API call to VPS).
  - Stop localhost HTTP listeners (daemon API, control plane API).
  - Wait for in-flight requests to complete (max 5s drain timeout).
  - Remove pidfile/named mutex.
  - Exit with code 0.
  - No orphan WireGuard interfaces, Docker operations, or background goroutines after exit.
  - If shutdown takes >10s, force-exit with code 1 and log a warning.

**Tray ↔ Admin UI State Sync (P2)**:

- **FR-013**: Tray state MUST stay synchronized with daemon state:
  - Tray polls daemon API (`GET /api/status`) every 3s when connected, every 10s when disconnected. Client-side timeout: 5s per request.
  - Daemon pushes state changes via WebSocket/SSE — deferred to spec 002 enhancement (P3). Polling is sufficient for v0.1.
  - Changes made in admin UI (expose port, disconnect tunnel) MUST reflect in tray within one poll cycle.
  - Tray actions (connect, disconnect, quit) MUST be reflected in admin UI within one poll cycle.

**Daemon Health Monitoring (P1)**:

- **FR-014**: Tray MUST detect daemon process health:
  - Health check: HTTP GET to `http://localhost:<PORT>/api/status` every 5s. Client-side timeout: 5s per request.
  - Consecutive failures: after 3 failures (15s), declare daemon dead.
  - Crash vs clean shutdown: daemon writes `.graceful_exit` sentinel file to `%LOCALAPPDATA%\unet\.graceful_exit` on clean shutdown. Tray checks sentinel: if present → clean quit (no auto-restart prompt); if absent → crash (offer restart). Sentinel is deleted on tray read.
  - On daemon death: tray icon → red, tooltip → "Daemon stopped", menu → "Restart daemon" item appears (replaces "Connect").
  - "Restart daemon" launches daemon binary and resumes health checking.
  - If user manually killed daemon (detected via `graceful_exit` sentinel present): tray MUST NOT auto-restart. Only restart on explicit user click of "Restart daemon".

**Cross-Platform Abstraction (P3)**:

- **FR-015**: All platform-specific functionality MUST be abstracted behind Go interfaces in a `platform` package:
  - `platform.Tray`: `SetIcon(state IconState)`, `SetMenu(items []MenuItem)`, `SetTooltip(text string)`, `OnClick()`, `Run(ctx context.Context) error`
  - `platform.Notifier`: `Send(title, body string, severity Severity) error`
  - `platform.NetworkMonitor`: `Watch(ctx context.Context) <-chan NetworkEvent`
  - `platform.AutoStart`: `Enable() error`, `Disable() error`, `IsEnabled() bool`
  - Build-tagged implementations: `tray_windows.go`, `tray_darwin.go` (stub), `tray_linux.go` (stub).
  - Interface stability: these are internal APIs (not exported from module), but MUST be documented for contributors.

### Key Entities

- **TrayState**: Current visual + logical state of the tray. Attributes: iconState (green/yellow/red), tunnelStatus, vpsName, exposedRoutes (count + URLs), daemonAlive (bool), tooltip text.
- **NetworkEvent**: A detected network change. Attributes: eventType (default_route_changed | interface_up | interface_down | reachability_lost | reachability_restored), timestamp, affectedInterfaces ([]string), previousState, newState.
- **ReconnectAttempt**: A single reconnect cycle entry. Attributes: attemptNumber, startedAt, delayBeforeAttempt, outcome (success | fail), errorMessage, resolvedAt. Used for diagnostics and backoff state tracking.
- **AutostartConfig**: Autostart state. Attributes: enabled (bool), binaryPath (absolute), registryKeyPresent (bool), lastUpdated. Platform-specific storage (Windows: Registry Run key; macOS: LaunchAgent plist; Linux: XDG autostart `.desktop` file).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Tray context menu appears within 200ms of right-click (measured from click event to menu render on Windows 10+ with standard hardware).
- **SC-002**: Tunnel reconnects within 10s after network change detected (measured from network change event to successful `awg show` handshake confirmation). Covers WiFi→4G, WiFi reconnect, and sleep wake-up scenarios.
- **SC-003**: OS notification dispatched within 2s of triggering event (disconnect, reconnect, daemon crash). Measured from event detection to `IToastNotification.Show()` call completion.
- **SC-004**: Autostart toggle (enable/disable) takes effect at next login without daemon restart and without UAC prompt. Verified by: toggle on → check Registry key present → reboot → verify daemon/tray started; toggle off → check Registry key absent → reboot → verify no daemon/tray.
- **SC-005**: Windows install + autostart enable requires zero UAC prompts. All Registry writes target `HKCU` (current user), not `HKLM`. Binary installed to user-writable location (e.g., `%LOCALAPPDATA%\unet\`).
- **SC-006**: Tray binary adds < 5MB to daemon distribution (measured as `.exe` size delta: `unet-tray.exe` vs nothing). Icons and assets embedded via `go:embed` — no external resource files.
- **SC-007**: Graceful shutdown via "Quit" completes within 10s with zero orphan processes (verified by: check no `unet` or `awg` processes remain after exit; check `awg show` returns no interface).
- **SC-008**: Tray polls daemon health every 5s and detects daemon death within 15s (3 consecutive failures). "Restart daemon" appears in menu within 1s of death detection.

## Assumptions

- **Windows 10+ is the P1 platform**: Windows 10 1809+ as minimum version. Toast notifications require Windows 10+. AmneziaWG client also requires Windows 10+. No need to support Windows 7/8.
- **Tray is a separate executable**: The tray binary (`unet-tray.exe`) communicates with the daemon via the existing localhost HTTP API. This decouples tray and daemon lifecycles.
- **Daemon localhost API is the IPC mechanism**: Tray ↔ daemon communication uses HTTP to `localhost:<PORT>/api/*`. No additional IPC layer (named pipes, D-Bus, etc.) is needed. The API already exists (spec 001) and will be extended (spec 002).
- **Single-user, single-session**: One user, one desktop session. Multi-user fast-switching is not a P1 concern.
- **No installer in this spec**: Binary distribution is out of scope. The user (or a future installer spec) places the binary in the correct location. Autostart Registry entry uses whatever path the binary is at.

## Out of Scope (for this spec)

- Full native GUI / admin UI redesign (admin UI stays web-based, served by daemon)
- macOS and Linux tray implementation (abstraction designed here, implementation in future specs)
- Code-signing, notarization, or Windows Authenticode (separate ops/packaging concern)
- Auto-update mechanism (binary self-update is a separate spec)
- Installer / MSI package (separate spec)
- WebSocket/SSE push for real-time tray↔daemon sync (polling is sufficient for v0.1)
- Multi-VPS switching (abstraction designed here, full implementation deferred to multi-VPS spec)

## Cross-References

- **Depends on**: `specs/002-api-control-plane/` — tray actions (connect, disconnect, status) call daemon API endpoints. Tray is a first-class API consumer.
- **Depends on**: `specs/001-init/` — FR-010 (stale state reconciliation) is used during reconnect. FR-003 (`awg-quick` via `os/exec`) is the tunnel management primitive.
- **Mirrored by**: `specs/006-peer-onboarding/` (future) — notifications during peer onboarding flow use the same `platform.Notifier` abstraction.
- **Future**: `specs/005-observability/` (future) — network change events and reconnect attempt logs feed into the observability pipeline.
- **Extends**: `specs/002-api-control-plane/` — new endpoint `GET /api/v1/events` for structured event log; new endpoint `POST /api/v1/settings/autostart` for admin UI autostart toggle.

## Open Questions

All questions resolved. See research.md for detailed rationale.

| # | Question | Decision | Source |
|---|---|---|---|
| 1 | Tray library | `fyne.io/systray` | Clarification round 1 |
| 2 | Windows notification | `go-toast/toast` (Toast API) with balloon-tip fallback | research.md |
| 3 | Process model | Separate executable via daemon HTTP API | Clarification round 1 |
| 4 | Autostart mechanism | Registry HKCU Run — no UAC, user-scope | Clarification round 1 |
| 5 | Network change detection | Default-route polling every 2s | research.md |
| 6 | macOS/Linux parity | Within this spec (P1=Win, P2/P3=macOS/Linux) | Clarification round 1 |
| 7 | Daemon crash vs user-kill | `.graceful_exit` sentinel file in `%LOCALAPPDATA%\unet\` | research.md |
| 8 | Tray auto-respawn by daemon | Deferred/out of scope | research.md |
| 9 | RDP session support | Documented limitation (best effort) | research.md |
| 10 | VPN interference | Silently handle via default-route monitoring | research.md |
| 11 | WebSocket/SSE push | Deferred to spec 002 enhancement (P3) | research.md |