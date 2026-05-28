# Implementation Plan: Desktop Integration

## Technical Context
- **Target**: `unet-tray.exe` as a separate Go binary communicating with `unet.exe` daemon over `localhost:<PORT>/api/`. Port discovered from daemon config file (`%LOCALAPPDATA%\unet\config.json`) or `UNET_DAEMON_PORT` env var, defaulting to 8080.
- **Dependencies**: `fyne.io/systray` (tray icon/menu), `github.com/go-toast/toast` (notifications), `golang.org/x/sys/windows/registry` (autostart).
- **Process Model**: `unet-tray.exe` is the UI, `unet.exe` is the background daemon.

## Constitution Check
- **Principle III (Secrets)**: Tray does not handle keys. It calls daemon API. No secrets to leak.
- **Principle IV (Type Safety)**: Standard Go type safety, typed errors. No `as any` equivalents.

## Proposed Architecture

### 1. `cmd/unet-tray`
- Main entrypoint for the tray application.
- Uses `fyne.io/systray` to run the tray loop.
- Wires up the tray menu to the API client.

### 2. `internal/platform`
- `tray.go`: Defines `Tray` interface.
- `tray_windows.go`: Implementation using `fyne.io/systray`.
- `notifier_windows.go`: Implementation using `go-toast/toast`.
- `autostart_windows.go`: Registry `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`.
- `network_monitor.go`: 2s polling loop checking reachability to VPS.

### 3. `internal/trayapi`
- HTTP client wrappers to interact with the daemon's localhost API.
- Port discovery: read from daemon config → env var → default 8080.
- Client-side timeouts: 5s for status/health, 10s for connect/disconnect.
- Methods: `GetStatus()`, `Connect()`, `Disconnect()`, `GetPorts()`, `GetEvents(limit, cursor)`.

### 4. `internal/traylogic`
- State machine bridging the `NetworkMonitor`, `Tray` UI, and `trayapi`.
- Handles exponential backoff reconnect logic.
- Singleton guard: named mutex `Local\unet-tray-singleton` on startup — abort if already held.

### 5. Daemon-side extensions (daemon repo)
- `POST /api/v1/settings/autostart` — enable/disable autostart from admin UI (extends spec 002).
- `GET /api/v1/events?limit=N&after=<cursor>` — structured event log with pagination, ring buffer retention (1000 events).
- `.graceful_exit` sentinel: daemon writes `%LOCALAPPDATA%\unet\.graceful_exit` on clean shutdown, deleted on startup.

## Verification Plan
1. Compile `unet-tray.exe`.
2. Run `unet.exe` daemon.
3. Run `unet-tray.exe`, verify icon appears, menu is responsive.
4. Click Connect/Disconnect, verify tunnel state changes.
5. Disable WiFi, verify tray turns yellow, then reconnects when WiFi is back.
6. Verify Toast notification appears on disconnect.
7. Verify Autostart registry key is created/removed on toggle.
