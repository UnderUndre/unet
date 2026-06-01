# E2E Manual Verification: Desktop Integration

## Prerequisites
- unet.exe daemon built and available
- unet-tray.exe built and available
- Windows 10+ with PowerShell available

## Test Cases

### TC-1: Tray Shows Tunnel Status (US1)
1. Start `unet.exe` daemon
2. Start `unet-tray.exe`
3. Verify: tray icon appears in system tray within 10s
4. Verify: icon is RED (disconnected — no tunnel yet)
5. Connect tunnel via tray → verify icon turns GREEN
6. Hover icon → verify tooltip shows "Connected" + VPS name + route count

### TC-2: Tray Quick Actions (US2)
1. Right-click tray icon → verify menu appears within 200ms
2. Verify menu items: Connect/Disconnect, Copy public URL, Open admin UI, Start at login, About, Quit
3. Click "Connect" → verify tunnel connects, icon turns green
4. Click "Copy public URL" → verify clipboard contains `https://*.domain.com`
5. Click "Open admin UI" → verify browser opens to `http://localhost:8080`
6. Click "Disconnect" → verify tunnel disconnects, icon turns red
7. Click "Quit" → verify tray exits cleanly, daemon still running

### TC-3: Auto-Reconnect on Network Change (US3)
1. Connect tunnel over WiFi
2. Disable WiFi (or switch to different network)
3. Verify: tray icon turns YELLOW within 5s
4. Verify: tunnel reconnects within 10s of network restoration
5. Verify: icon turns GREEN on successful reconnect

### TC-4: OS Notifications (US4)
1. Connect tunnel
2. Force disconnect (kill awg interface)
3. Verify: OS notification "unet: tunnel disconnected, retrying" within 2s
4. Wait for reconnect → verify notification "unet: tunnel connected"
5. Verify: no notification spam during backoff (max 1 per 60s)

### TC-5: Autostart Toggle (US5)
1. Right-click tray → "Start at login" → toggle ON
2. Verify: Registry `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` contains "unet" entry
3. Toggle OFF → verify Registry entry removed
4. Reboot (optional) → verify behavior matches setting

### TC-6: Daemon Crash Detection (FR-014)
1. Kill daemon process (Task Manager or `taskkill /F`)
2. Verify: tray icon turns RED within 15s (3 health checks × 5s)
3. Verify: "Restart daemon" menu item appears
4. Verify: notification "unet: daemon stopped" fires
5. Click "Restart daemon" → verify daemon starts, icon recovers

### TC-7: Singleton Guard (F6)
1. Start `unet-tray.exe`
2. Start another `unet-tray.exe`
3. Verify: second instance exits immediately with error

### TC-8: Graceful Shutdown (FR-012)
1. Start tray + daemon
2. Click "Quit" from tray menu
3. Verify: tunnel disconnects
4. Verify: no orphan `unet` or `awg` processes
5. Verify: `.graceful_exit` sentinel file exists in `%LOCALAPPDATA%\unet\`

## Build & Run
```powershell
cd src
go build -o unet.exe ./cmd/unet
go build -o unet-tray.exe ./cmd/unet-tray
./unet.exe -port 8080
./unet-tray.exe
```
