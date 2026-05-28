# SSE Log Streaming Protocol

**Spec**: `specs/005-observability/spec.md`
**Created**: 2026-05-28

---

## Endpoint

```
GET /v1/logs/stream
```

Mounted on the control plane API listener (spec 002, `:8443`). Requires TLS on non-loopback.

## Authentication

Same Bearer token mechanism as the control plane API (spec 002):
- **PAT**: `Authorization: Bearer unet_<opaque>` — long-lived, scope `read` required.
- **JWT**: `Authorization: Bearer eyJ...` — short-lived session token, scope `read` required.
- **Loopback bypass**: Requests from loopback addresses skip auth (admin UI access from localhost).

## Query Parameters (Server-Side Filtering)

All parameters are optional. Multiple parameters are AND-combined.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `level` | `string` | (all levels) | Minimum level threshold. Values: `debug`, `info`, `warn`, `error`. e.g., `?level=warn` → only warn + error. |
| `component` | `string` | (all components) | Exact component match. e.g., `?component=tunnel`. |
| `source` | `string` | (all sources) | Exact source match. e.g., `?source=container`. |
| `q` | `string` | (no filter) | Case-insensitive substring match in `msg` field. |

**Multiple values**: Not supported for `level`, `component`, `source`. For multiple components, open multiple SSE connections.

**Invalid parameter values**: Return `400 Bad Request` with structured error BEFORE SSE upgrade:
```json
{
  "error": "invalid_filter",
  "message": "Invalid level value 'verbose'. Must be one of: debug, info, warn, error",
  "context": { "parameter": "level", "value": "verbose" }
}
```

## Connection Upgrade

On successful auth + filter validation:

```
HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
```

## Event Types

### `log` event (default)

Delivers a single `LogRecord` as JSON. Conforms to `contracts/log-record.schema.json`.

```
event: log
data: {"ts":"2026-05-27T14:32:01.123Z","level":"info","component":"tunnel","source":"daemon","msg":"handshake completed","seq":42,"fields":{"peer_id":"abc-123"}}

```

The event type `log` is explicit (not the SSE default unnamed event). This allows clients to distinguish log events from control events.

### `overflow` event

Sent when a subscriber's per-client buffer overflows. This is the FINAL event before disconnect.

```
event: overflow
data: {"missed": 47}

```

After sending this event, the server closes the connection. The client MUST reconnect with adjusted filters to reduce event volume.

### `: keepalive` comment (heartbeat)

Sent every 15 seconds of inactivity (no log events to send). SSE comment — not delivered to `EventSource.onmessage`.

```
: keepalive

```

### `: connected` comment (initial)

Sent immediately after successful connection upgrade. Confirms subscription is active.

```
: connected
data: {"subscriber_id": "uuid-here", "filters": {"level": "warn"}}

```

## Reconnection

Clients SHOULD use SSE's built-in reconnection with `Last-Event-ID`.

- Every `log` event includes an `id` field (SSE standard) set to the record's `seq` value:
  ```
  id: 42
  event: log
  data: {...}
  ```
- On reconnect, client sends `Last-Event-ID: 42` header.
- Server replays missed events from the ring buffer (if still available — ring holds last 200).
- If `Last-Event-ID` is older than the ring buffer's oldest entry, server starts from the oldest available entry + sends a `: replay_truncated` comment:
  ```
  : replay_truncated
  data: {"available_from": 38, "requested_from": 20}
  ```

## Backpressure

Each subscriber has a bounded per-client buffer:
- **Default**: 1000 events
- **Configurable**: `observability.sseClientBuffer` in daemon config
- **On overflow**: Send `overflow` event → disconnect client
- **Buffer behavior**: When buffer is full, new events are dropped (newest wins — ring buffer semantics). The `missed` count in the overflow event reports how many events were dropped.

## Subscriber Limits

- **Maximum concurrent subscribers**: 10 (per FR-009)
- **When limit reached**: Return `429 Too Many Requests` (before SSE upgrade):
  ```json
  {
    "error": "subscriber_limit_reached",
    "message": "Maximum 10 concurrent log stream subscribers",
    "context": { "limit": 10, "retry_after": 30 }
  }
  ```
  With `Retry-After: 30` header.

## Error Responses (Before SSE Upgrade)

| Status | Error Code | When |
|--------|-----------|------|
| 401 | `unauthorized` | Missing or invalid Bearer token |
| 403 | `insufficient_scope` | Token lacks `read` scope |
| 400 | `invalid_filter` | Invalid filter parameter value |
| 429 | `subscriber_limit_reached` | 10 subscribers already connected |
| 503 | `service_unavailable` | Log stream subsystem not initialized |

## Example Session

```
→ GET /v1/logs/stream?level=warn HTTP/1.1
→ Host: localhost:8443
→ Authorization: Bearer unet_abc123def456
→ Accept: text/event-stream
→
← HTTP/1.1 200 OK
← Content-Type: text/event-stream
← Cache-Control: no-cache
←
← : connected
← data: {"subscriber_id":"550e8400-e29b-41d4-a716-446655440000","filters":{"level":"warn"}}
←
← id: 156
← event: log
← data: {"ts":"2026-05-27T14:35:22.456Z","level":"error","component":"tunnel","source":"daemon","msg":"handshake timeout","seq":156,"fields":{"peer_id":"peer-7","error_class":"tunnel"}}
←
← : keepalive
←
← id: 203
← event: log
← data: {"ts":"2026-05-27T14:35:37.789Z","level":"warn","component":"container-capture","source":"daemon","msg":"container log capture stopped","seq":203,"fields":{"container":"unet-caddy","exit_code":0}}
←
```
