# Research: Desktop Integration

## Decisions for NEEDS CLARIFICATION

1. **Windows Notification Mechanism**
   - **Decision**: Use `github.com/go-toast/toast` for P1.
   - **Rationale**: Simplest path to Toast notifications on Windows 10+. Although it uses PowerShell under the hood (~500ms latency), it avoids the complexity of raw COM bindings (`go-ole`) or CGO. Can be optimized in P2.

2. **Network Change Detection**
   - **Decision**: Poll default route reachability every 2 seconds.
   - **Rationale**: Polling avoids complex Windows Network List Manager (NLM) COM event bindings. 2-second polling easily meets the 10-second reconnect SLA without heavy CPU overhead.

3. **Daemon Crash vs User Kill Distinction**
   - **Decision**: Daemon writes a `.graceful_exit` sentinel file on clean shutdown.
   - **Rationale**: Allows the tray to distinguish between a crash (sentinel missing) and intentional quit (sentinel present). Tray will only auto-offer restart on crash.

4. **Tray Auto-Respawn by Daemon**
   - **Decision**: Deferred/Out of scope.
   - **Rationale**: Tray will be launched by OS autostart or user. Daemon does not need to manage the tray's lifecycle.

5. **RDP Session Support**
   - **Decision**: Documented limitation.
   - **Rationale**: Network transitions inside RDP sessions might not fire standard events. Best effort support.

6. **VPN Interference**
   - **Decision**: Silently handle by monitoring default route reachability.
   - **Rationale**: If the default route is reachable, we attempt connection. If another VPN takes over and routes correctly, we ride on top of it.

7. **WebSocket/SSE Push for Real-Time Sync**
   - **Decision**: Deferred to a future spec (spec 005 observability / control plane updates).
   - **Rationale**: Polling daemon API every 3s is sufficient for P1 tray state sync.
