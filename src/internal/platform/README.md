# Platform Abstractions

Cross-platform abstraction layer for unet desktop integration.

## Interfaces

### Tray (`platform.Tray`)

System tray icon and menu management.

```go
type Tray interface {
    Run(ctx context.Context) error
    SetIcon(state IconState)
    SetTooltip(text string)
    SetMenu(items []MenuItem)
    SetMenuItemLabel(id, label string)
    SetMenuItemDisabled(id string, disabled bool)
    SetMenuItemChecked(id string, checked bool)
}
```

**Implementations**: `tray_windows.go` (fyne.io/systray), `stubs_other.go` (macOS/Linux stubs)

### Notifier (`platform.Notifier`)

OS-native notifications.

```go
type Notifier interface {
    Send(title, body string, severity Severity) error
}
```

**Windows**: `go-toast/toast` (Toast notifications, requires PowerShell)
**macOS**: `UNUserNotificationCenter` (future, macOS 11+)
**Linux**: `libnotify` via D-Bus (future)

### NetworkMonitor (`platform.NetworkMonitor`)

Network connectivity change detection.

```go
type NetworkMonitor interface {
    Watch(ctx context.Context) <-chan NetworkEvent
}
```

**Strategy**: 2s TCP polling of `8.8.8.8:53`. Requires 2 consecutive failures before declaring reachability lost. Jitter ±500ms.

### AutoStart (`platform.AutoStart`)

Login autostart management.

```go
type AutoStart interface {
    Enable() error
    Disable() error
    IsEnabled() bool
}
```

**Windows**: Registry `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` (no UAC)
**macOS**: LaunchAgent plist (future)
**Linux**: XDG autostart `.desktop` file (future)

## Build Tags

- `*_windows.go` — Windows implementation
- `stubs_other.go` — macOS/Linux stubs (`!windows` build tag)
- `tray.go` — Interface definitions (no build tag, all platforms)

## Adding a New Platform

1. Create `tray_<platform>.go` with the appropriate build tag
2. Implement all four interfaces
3. Ensure `NewTray()`, `NewNotifier()`, `NewAutoStart()`, `NewNetworkMonitor()` are provided
4. Add icons to `icons/` directory
