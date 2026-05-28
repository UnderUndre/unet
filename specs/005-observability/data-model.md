# Data Model: Observability (005)

**Spec**: `specs/005-observability/spec.md`
**Created**: 2026-05-28

---

## Entities

### LogRecord

A single structured log line. Immutable once written to file. The fundamental unit of all observability output.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ts` | `string` (ISO-8601 UTC) | YES | Timestamp with millisecond precision. e.g., `"2026-05-27T14:32:01.123Z"` |
| `level` | `enum` | YES | `debug` \| `info` \| `warn` \| `error` |
| `component` | `string` | YES | Emitting subsystem. e.g., `"tunnel"`, `"container.unet-caddy"`, `"api"` |
| `source` | `enum` | YES | `daemon` \| `container` \| `lifecycle` \| `api-audit` |
| `msg` | `string` | YES | Human-readable message |
| `fields` | `map[string]any` | NO | Optional structured context (extensible) |
| `fields.peer_id` | `string` | NO | Peer UUID when relevant |
| `fields.route_id` | `string` | NO | Route UUID when relevant |
| `fields.request_id` | `string` | NO | API request correlation ID |
| `fields.error_class` | `string` | NO | Error category for metrics: `tunnel` \| `caddy` \| `dns` \| `ssh` \| `config` |
| `fields.container` | `string` | NO | Container name (when `source: "container"`) |
| `fields.container_ts` | `string` | NO | Original container timestamp (when parseable from Docker output) |
| `fields.exit_code` | `int` | NO | Container exit code (on container stop events) |
| `seq` | `int64` | YES | Monotonically increasing sequence number (per daemon session). Used for SSE `Last-Event-ID`. Starts at 1. |

**Validation rules**:
- `ts` MUST be valid ISO-8601 with timezone (UTC `Z` suffix required).
- `level` MUST be one of the four enum values. Case-sensitive lowercase.
- `component` MUST be non-empty string, matching `^[a-z][a-z0-9._-]{0,63}$`.
- `msg` MUST be non-empty.
- `seq` MUST be > 0 and monotonically increasing within a daemon session.

**Persistence**: Written to `~/.unet/logs/daemon-YYYY-MM-DD.jsonl` (one JSON object per line). Also held in in-memory ring buffer (last 200 entries).

---

### LogFile

Represents a log file on disk. Used internally for rotation tracking and export.

| Field | Type | Description |
|-------|------|-------------|
| `path` | `string` | Absolute path. e.g., `~/.unet/logs/daemon-2026-05-27.jsonl` |
| `opened_at` | `time.Time` | When this file was created (rotation or date change) |
| `current_size` | `int64` | Current file size in bytes |
| `rotation_threshold` | `int64` | Max size in bytes before rotation (from config, default 100MB) |
| `status` | `enum` | `active` \| `archived` \| `expired` |
| `date` | `string` | Date portion of filename (`YYYY-MM-DD`) |
| `rotation_index` | `int` | 0 for primary, N for rotated archive |

**Lifecycle**:
1. **active**: Currently being written to. `rotation_index = 0`.
2. **archived**: Rotated out (size or date). Renamed to `daemon-YYYY-MM-DD.N.jsonl`. Sealed, never modified.
3. **expired**: Older than `observability.retentionDays`. Deleted by retention cleanup.

**Persistence**: Files only — no database. State derived from filesystem listing.

---

### LogStreamSubscriber

An active SSE connection consuming the log stream.

| Field | Type | Description |
|-------|------|-------------|
| `id` | `string` (UUID) | Unique subscriber ID |
| `filter_level` | `string` | Minimum level threshold. e.g., `"warn"` (warn + error only) |
| `filter_component` | `string` | Exact component match, or empty for all |
| `filter_source` | `string` | Exact source match, or empty for all |
| `filter_query` | `string` | Case-insensitive substring match in `msg` |
| `created_at` | `time.Time` | Connection established time |
| `last_sent_seq` | `int64` | Sequence number of last successfully sent event |
| `backpressure_state` | `enum` | `healthy` \| `lagging` \| `disconnected` |
| `buffer_used` | `int` | Number of events currently in per-client buffer |
| `buffer_capacity` | `int` | Max buffer size (from config, default 1000) |

**Lifecycle**:
1. Client connects via `GET /v1/logs/stream` → subscriber created with filters.
2. Hub broadcasts log records → subscriber's goroutine filters → buffers → writes to SSE response.
3. Buffer exceeds capacity → `overflow` event sent → subscriber disconnected.
4. Client disconnects (close, cancel, timeout) → goroutine terminates within 5s → hub removes subscriber.

**Persistence**: In-memory only. Lost on daemon restart. Ring buffer provides initial load for reconnecting clients.

**Constraints**:
- Maximum 10 concurrent subscribers (configurable, per FR-009).
- Per-client buffer bounded (default 1000, per FR-009).
- Goroutine cleanup within 5 seconds of context cancellation.

---

### MetricSnapshot

A point-in-time collection of all metric values. Used internally for `/metrics` scrape response.

| Field | Type | Description |
|-------|------|-------------|
| `timestamp` | `time.Time` | When the snapshot was taken |
| `gauges` | `map[string]float64` | Current gauge values: `unet_peers_connected`, `unet_routes_active`, `unet_uptime_seconds`, `unet_tunnel_info` |
| `counters` | `map[string]uint64` | Cumulative counter values: `unet_api_requests_total`, `unet_errors_total`, `unet_peer_handshakes_total`, `unet_bandwidth_bytes_total` |
| `histograms` | `map[string]HistogramSnapshot` | Bucket counts + sum + count for each histogram |

**Persistence**: In-memory only via `prometheus.Registry`. No on-disk storage — metrics reset on daemon restart (standard Prometheus behavior).

**Note**: `MetricSnapshot` is a logical entity — the actual implementation uses `prometheus.Registry.Gather()` which returns `dto.MetricFamily` protos. This entity describes the conceptual model.

---

### MetricSeries

A named metric with its type and label schema.

| Name | Type | Labels | Description |
|------|------|--------|-------------|
| `unet_api_requests_total` | counter | `method`, `path`, `status` | Cumulative API request count |
| `unet_errors_total` | counter | `class` | Cumulative error count by category |
| `unet_peer_handshakes_total` | counter | — | Cumulative peer handshake events |
| `unet_bandwidth_bytes_total` | counter | `direction` (`in` \| `out`) | Cumulative bytes through tunnel |
| `unet_peers_connected` | gauge | — | Current connected peer count |
| `unet_routes_active` | gauge | — | Current active route count |
| `unet_uptime_seconds` | gauge | — | Daemon uptime in seconds |
| `unet_tunnel_info` | gauge | `status` | Value 1 for current status, 0 for others |
| `unet_api_request_duration_seconds` | histogram | `path` | API request latency distribution |
| `unet_log_write_duration_seconds` | histogram | — | Log write latency distribution |

**Validation rules**:
- Counter names MUST end with `_total` (Prometheus convention).
- Label values MUST be lowercase, no spaces.
- Gauge values MUST be non-negative.
- Histogram buckets: default Go client buckets `[.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10]` for duration histograms.

---

### LogExportBundle

A tarball generated by the export endpoint.

| Field | Type | Description |
|-------|------|-------------|
| `bundle_id` | `string` (UUID) | Unique export identifier |
| `date_range_start` | `time.Time` | `from` parameter |
| `date_range_end` | `time.Time` | `to` parameter |
| `file_count` | `int` | Number of JSONL files in tarball |
| `total_size` | `int64` | Uncompressed size of all JSONL content |
| `compressed_size` | `int64` | Tarball file size |
| `scrubbed` | `bool` | Whether PII scrubbing was applied |
| `includes_container_logs` | `bool` | Whether container-sourced records are included |
| `created_at` | `time.Time` | When the export was generated |
| `path` | `string` | Temporary tarball path (deleted after response sent) |

**Lifecycle**:
1. Request received → snapshot current file list → assemble tarball → stream response → delete temp file.
2. No persistent storage — exports are ephemeral. Generated on demand, streamed to client, discarded.

**Validation rules**:
- `date_range_start` MUST be before `date_range_end`.
- Date range MUST NOT exceed 30 days (prevents excessive resource consumption).
- `scrubbed` reflects the `observability.scrubPii` config value at export time.

---

## Entity Relationships

```
Daemon subsystems
    │
    │ emit LogRecord (via slog)
    ▼
LogRecord ──────────────────────────────────┐
    │                                        │
    ├──▶ LogFile (JSONL persistence)         │
    │    └── rotation → archived LogFile     │
    │    └── retention → expired → deleted   │
    │                                        │
    ├──▶ Ring Buffer (last 200 in-memory)    │
    │    └──▶ LogStreamSubscriber (SSE)      │
    │         ├── filter: level/component/source/query
    │         └── buffer: bounded, overflow → disconnect
    │                                        │
    ├──▶ MetricSeries (counters/gauges)      │
    │    └──▶ MetricSnapshot (on scrape)     │
    │                                        │
    └──▶ LogExportBundle (on demand)         │
         └── date range from LogFile(s)      │
         └── optional PII scrub             │
```

---

## Persistence Summary

| Entity | Storage | Location | Persistence |
|--------|---------|----------|-------------|
| LogRecord | JSONL file | `~/.unet/logs/daemon-YYYY-MM-DD.jsonl` | Durable, rotated |
| LogFile | Filesystem state | `~/.unet/logs/` | Derived from disk |
| LogStreamSubscriber | In-memory | Process memory | Ephemeral, lost on restart |
| MetricSeries | In-memory (Prometheus registry) | Process memory | Ephemeral, reset on restart |
| MetricSnapshot | In-memory | Process memory | Generated on demand |
| LogExportBundle | Temporary file | OS temp dir | Ephemeral, deleted after response |
