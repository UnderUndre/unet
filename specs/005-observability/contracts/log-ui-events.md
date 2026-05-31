# Admin UI Log Tail Protocol

**Spec**: `specs/005-observability/spec.md`
**Created**: 2026-05-28

---

## Overview

This document defines the protocol contract between the unet admin UI (React, embedded in Go binary) and the backend log stream. The admin UI consumes the **same SSE endpoint** as external tools (`GET /v1/logs/stream`), with loopback auth bypass.

**Scope**: This document covers the **backend expectations** for the admin UI client. Frontend implementation details (React component structure, state management) are out of scope for this backend plan — tracked for cross-spec frontend coordination.

## Connection

The admin UI connects to:

```
GET http://localhost:8080/v1/logs/stream
```

Note: The admin UI is served on `localhost:8080` (spec 001-init). The SSE endpoint lives on `localhost:8443` (spec 002). To avoid CORS issues, the SSE endpoint MUST also be accessible on `localhost:8080` via a proxy pass or route registration on the localhost server.

**Alternative**: Admin UI connects directly to `http://localhost:8443/v1/logs/stream` with `EventSource`. Since both are localhost, CORS is not a concern if the SSE response includes `Access-Control-Allow-Origin: *` (or specific localhost origin).

**Recommendation**: Register the SSE handler on BOTH listeners (`:8080` and `:8443`). On `:8080`, loopback auth bypass applies. On `:8443`, normal PAT/JWT auth applies. This avoids any cross-origin issue.

## Authentication (Admin UI)

Loopback requests bypass Bearer auth (per spec FR-014). The admin UI connects without a token when accessing from `localhost`.

No separate authentication needed — the UI's existing session (uiToken from spec 001-init) is not used for SSE. Loopback detection is sufficient.

## Initial Load

On connection, the server sends the last 200 entries from the ring buffer (filtered by query params) before switching to live events.

Implementation: The SSE handler reads the current ring buffer snapshot, filters each record, and sends matching records as `log` events with their `id` (seq) fields. Then switches to live broadcast mode.

The admin UI SHOULD display these initial 200 lines immediately on connect, providing context before live events arrive.

## Client-Side Controls

The following controls are implemented ENTIRELY client-side. The backend provides no UI-specific state tracking.

### Pause / Resume

- **Pause**: Client stops rendering new SSE events. Events continue arriving from the server and are buffered client-side (up to 500 lines per FR-015).
- **Resume**: Client renders all buffered events and resumes live display.
- **Backend impact**: None. SSE stream continues regardless of client-side pause state.

### Level Filter

- Client-side filtering on top of the SSE stream.
- **Option A** (preferred): Use server-side `?level=` query param on connect. Changing the level filter requires reconnecting the SSE connection.
- **Option B**: Client receives all events and filters locally. More flexible but higher bandwidth.
- **Recommendation**: Use `?level=` for the initial connection. If the user changes the level filter, reconnect with new params.

### Component Filter

- Same approach as level filter: `?component=tunnel` on connect, reconnect to change.
- Dynamic component dropdown populated from observed `component` values in the received stream.

### Text Search

- Client-side only. The `?q=` parameter is available for server-side search, but real-time interactive search is better implemented client-side over the buffered events.
- Case-insensitive substring match on `msg` field.

### Auto-scroll

- Client-side behavior: if the log container is scrolled to the bottom, auto-scroll on new events. If the user has scrolled up (reading history), disable auto-scroll until they scroll back to bottom.
- **Backend impact**: None.

## Display Format

Each log line renders:

| Field | Render |
|-------|--------|
| `ts` | User's local timezone (via `new Date(ts).toLocaleString()`) |
| `level` | Color-coded badge: debug=gray, info=default, warn=yellow, error=red |
| `component` | Badge with component name |
| `source` | Small label (daemon/container/lifecycle) |
| `msg` | Full message text |
| `fields` | Expandable JSON tree (collapsed by default) |

## Reconnection Behavior

The admin UI uses `EventSource`'s built-in reconnection:
1. Connection drops → `EventSource` auto-reconnects with `Last-Event-ID`.
2. Server replays missed events from ring buffer.
3. UI shows a brief "Reconnecting..." indicator.
4. On reconnect, resume from last received `seq`.

If reconnect fails after 30 seconds, show a persistent "Log stream disconnected" banner with a manual "Reconnect" button.

## Bandwidth Considerations

At default `info` level with moderate activity (~10 events/minute), the SSE stream consumes negligible bandwidth (< 1 KB/sec). During log storms (container crash loop, high API traffic), event rate may spike to 100+ events/second. The client-side buffer (500 lines) prevents memory issues. Server-side backpressure (1000 events per subscriber) prevents server-side memory issues.

## Cross-Spec Coordination Notes

- **Spec 001-init**: Admin UI is defined here. Log viewer is a new view added to the existing React app.
- **Spec 002**: SSE endpoint is defined here. Route registration on both `:8080` and `:8443` listeners.
- **Frontend implementation**: Not in scope for spec 005 backend plan. Recommended to track as a separate frontend task after backend SSE endpoint is implemented and tested.
