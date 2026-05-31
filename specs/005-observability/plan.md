# Implementation Plan: Observability — Logging, Metrics & Log UI

**Spec**: `specs/005-observability/spec.md`
**Branch**: `specs/005-observability`
**Created**: 2026-05-28
**Status**: Draft

---

## Constitution Check

### Principle VI — Cross-AI Review Gate

This is `/speckit.plan` — NOT `/speckit.implement`. No code is being written. The review gate **does not apply** at the planning stage. When this plan proceeds to implementation via `/speckit.implement`, the gate WILL require:

1. `specs/005-observability/reviews/analyze.md` with `verdict: PASS` or `verdict: MEDIUM`.
2. ≥2 external reviewer PASS files from different AI providers.
3. No contradicting `_gate-override.md`.

**Verdict**: PASS (planning stage, gate not yet active).

### Principle VII — Artifact Versioning

The `snapshot-stage.{sh,ps1}` scripts do not exist in this repo (TODO_SNAPSHOT_SCRIPT per constitution §VII). This plan does NOT attempt to call missing scripts.

Per the constitution's graceful-degradation clause: "[if the script is missing] the stage command MUST log a `[snapshot-deferred]` warning but still complete."

Manual tag `plan/005-observability/v1` is encouraged after commit. Note that snapshot-stage tooling is aspirational and cannot be invoked until implemented.

**Verdict**: SKIPPED — tooling not yet available.

### Principle VIII — Knowledge Self-Maintenance

**Drift detected**: `specs/main/architecture.md` does NOT include an Observability Layer description. The architecture describes three layers (Data Plane, Control Plane, Admin Surface) but has no mention of structured logging, log streaming, metrics exposition, or container log aggregation.

Specifically, `architecture.md` should be updated to include:

1. An **Observability Layer** description covering structured logging (JSONL + rotation), SSE log streaming, Prometheus metrics exposition, and container log aggregation.
2. Reference to `specs/005-observability/` in the spec cross-reference table.
3. The observability stack's relationship to existing layers: log output from all layers flows through the structured logger; SSE stream endpoint is mounted on the control plane listener (spec 002); metrics endpoint is a separate listener (loopback by default).
4. The `~/.unet/logs/` directory and `~/.unet/audit.jsonl` (spec 002) / `~/.unet/lifecycle-audit.jsonl` (spec 003) relationship.

**Accumulated drift**: Specs 002 and 003 both flagged similar architecture.md drift (002: Control Plane layer path prefix, 003: missing Lifecycle Layer). Three specs flagging drift in `architecture.md` suggests this should be addressed before any spec proceeds to external review — the architecture document is falling behind active design work.

**Follow-up**: Update `architecture.md` to add Observability Layer description, fix 002's path prefix (`/api/v1/*` → `/v1/*`), add 003's Lifecycle Layer, and add all three specs to the Spec Registry table before external reviews.

**Verdict**: NOTE — architecture.md missing Observability Layer entirely. Non-blocking for plan, tracked as mandatory follow-up before implementation merge.

---

## Technical Approach Summary

### Language & framework

- **Go** (same as existing daemon). No new languages introduced.
- Structured logging via Go stdlib `log/slog` (Go 1.21+). **Rationale**: zero external dependency, native `slog.Handler` interface allows custom JSON handler, stdlib guarantees long-term stability. `zerolog` rejected — external dependency with no material perf advantage for unet's log volume (<1k logs/sec).
- Log rotation via `gopkg.in/natefin/lumberjack.v2` — de facto Go standard for size-based + date-based rotation. Battle-tested, handles atomic file swap under mutex.
- SSE: custom minimal implementation (~100-150 LOC). `r3labs/sse` rejected — adds external dep for trivial protocol (4 SSE event types: `log`, `overflow`, `keepalive` comment, error). Our impl has zero third-party maintenance risk.
- Container log aggregation: Docker Engine API via `github.com/docker/docker/client` SDK — official Go client, follows `/containers/{id}/logs?follow=true&stdout=true&stderr=true`.
- Prometheus exposition: `github.com/prometheus/client_golang` — standard library for Go Prometheus instrumentation. Text exposition format per round 1 clarification.
- Admin UI log viewer: extends existing React admin UI (embedded in Go binary via `go embed`). SSE consumed via native `EventSource` API. **Flagged for cross-spec coordination** — backend plan only; frontend implementation tracked separately.

### What's reused

- **Daemon core**: Same Go process. Logger replaces scattered `log.Printf` calls globally — `slog` handler set as default at daemon init.
- **Control plane listener (spec 002)**: SSE endpoint `GET /v1/logs/stream` and export endpoint `GET /v1/logs/export` mount on the existing `:8443` listener. No new listener for SSE — it's an HTTP handler upgrade.
- **Auth middleware (spec 002)**: SSE and export endpoints reuse the same PAT/JWT Bearer auth middleware + scope enforcement (`read` scope required).
- **Audit log infrastructure (spec 002)**: Audit entries are written as structured log records via the same pipeline. Spec 002's `~/.unet/audit.jsonl` remains separate (API audit) — spec 005's unified log stream includes audit events tagged `source: "api-audit"`.
- **Config persistence**: New `observability` key in `~/.unet/config.json`. Same atomic-write pattern.

### What's new

| Package | Purpose |
|---------|---------|
| `src/internal/logger/` | `slog` handler wrapper: JSON output, component tagging, level filtering, secret redaction, dual-write (file + ring buffer) |
| `src/internal/logstream/` | In-memory ring buffer + SSE fan-out with backpressure handling |
| `src/internal/metrics/` | Prometheus collectors registry: counters, gauges, histograms |
| `src/internal/loguicapi/` | (Backend API portion) Container log aggregator, log export handler |
| `src/internal/observability/` | Top-level config + init: wires logger, logstream, metrics, container capture |

### Key decisions locked by spec

1. **Prometheus text exposition** format (Clarification round 1).
2. **Container logs default ON**, toggleable off in config (Clarification round 1).
3. **PII scrubbing default OFF** — full logs preserved locally (Clarification round 1).
4. **Metrics endpoint loopback-only** by default; external bind requires bearer token (Clarification round 1).
5. **SSE-only** for log streaming. WebSocket explicitly out of scope for v0.1 (Clarification pending but recommended SSE-only).
6. **Single global retention** with configurable days (default 30d). Per-component retention is over-engineering for v0.1.
7. **In-memory ring buffer** of last 200 entries for admin UI initial load. Not persisted — rebuilt from session output.

---

## Project Structure

New code goes under `src/internal/` (daemon's internal package root):

```
src/
├── internal/
│   ├── logger/                        # NEW: structured logging core
│   │   ├── logger.go                  # Init(), SetDefault(), component-scoped loggers
│   │   ├── handler.go                 # Custom slog.Handler: JSON → file + ring buffer
│   │   ├── redact.go                  # Secret field redaction (extends 001-init FR-011)
│   │   ├── levels.go                  # Per-component level thresholds from config
│   │   ├── handler_test.go
│   │   ├── redact_test.go
│   │   └── levels_test.go
│   ├── logstream/                     # NEW: in-memory ring buffer + SSE fan-out
│   │   ├── ring.go                    # Bounded ring buffer (200 entries default)
│   │   ├── subscriber.go              # SSE subscriber: filter, backpressure buffer, context cancel
│   │   ├── hub.go                     # Fan-out: register/unregister subscribers, broadcast
│   │   ├── sse_handler.go             # HTTP handler for GET /v1/logs/stream
│   │   ├── ring_test.go
│   │   ├── subscriber_test.go
│   │   └── hub_test.go
│   ├── metrics/                       # NEW: Prometheus metrics
│   │   ├── registry.go                # Registry init, collector registration
│   │   ├── counters.go                # unet_api_requests_total, unet_errors_total, etc.
│   │   ├── gauges.go                  # unet_peers_connected, unet_routes_active, etc.
│   │   ├── histograms.go              # unet_api_request_duration_seconds, etc.
│   │   ├── exposer.go                 # HTTP handler for GET /metrics, bind config
│   │   ├── registry_test.go
│   │   └── exposer_test.go
│   ├── loguicapi/                     # NEW: container log capture + log export
│   │   ├── container_aggregator.go    # Docker SDK follow per container, re-emit as structured
│   │   ├── container_aggregator_test.go
│   │   ├── export_handler.go          # GET /v1/logs/export handler
│   │   ├── exporter.go                # Tarball assembly, date-range filtering, PII scrub
│   │   └── exporter_test.go
│   ├── observability/                 # NEW: top-level init + config wiring
│   │   ├── config.go                  # ObservabilityConfig struct, defaults, hot-reload
│   │   ├── init.go                    # Init(): wire logger → logstream → SSE → metrics → container capture
│   │   └── config_test.go
│   └── daemon/                        # EXISTING — modified
│       └── main.go                    # Add observability.Init() call at startup
```

**Files touched**: `internal/daemon/main.go` (add observability init + metrics exposer startup). `internal/api/remote/routes.go` (add SSE + export route registration under 002's mux). All other files are new.

**Dependencies added**:
- `gopkg.in/natefin/lumberjack.v2` — log rotation
- `github.com/prometheus/client_golang` — Prometheus exposition
- `github.com/docker/docker/client` — container log streaming (Docker Engine API)
- `log/slog` — Go 1.21+ stdlib (no external dep)

---

## Component Breakdown

### 1. StructuredLogger (`internal/logger/`)

`slog.Handler` implementation that replaces all daemon logging. Dual-write: every log record goes to (a) JSONL file via lumberjack writer, (b) in-memory ring buffer for SSE/admin UI. Component tagging: each subsystem gets a scoped logger via `logger.With("component", "tunnel")`. Level filtering: global threshold (default: `info`) + per-component overrides from `observability.logLevels` config. Secret redaction: wraps FR-011 (spec 001-init) — scans `fields` map for known secret keys (private keys, tokens, passwords) and replaces values with `<redacted>` before writing. Redaction applies to file output, ring buffer, AND SSE stream — all three outputs share the same handler pipeline.

### 2. LogRotator (via lumberjack)

Size-based rotation: when `daemon-YYYY-MM-DD.jsonl` exceeds `observability.maxFileSizeMB` (default 100MB), lumberjack rotates atomically — renames current to `daemon-YYYY-MM-DD.N.jsonl`, creates new file. Date-based rotation: at midnight UTC, the handler detects date change in the filename component and creates a new file. Retention cleanup: on daemon start + daily timer, delete archives older than `observability.retentionDays` (default 30). Active files (current day) are never deleted. Disk-full graceful degradation (per spec edge case): on write error → delete oldest archives → retry → drop debug → drop info → emit SSE-only error alert → continue operating (tunnel > logging).

### 3. ContainerLogAggregator (`internal/loguicapi/container_aggregator.go`)

Spawns a goroutine per managed container (`unet-amnezia-awg`, `unet-caddy`) when `observability.captureContainerLogs` is `true` (default per round 1 clarification: ON). Uses Docker Engine API `client.ContainerLogs(ctx, containerID, types.ContainerLogsOptions{Follow: true, ShowStdout: true, ShowStderr: true})` to stream container output. Each line is re-emitted as a structured log record via the slog pipeline with `source: "container"`, `container: "<name>"`, `component: "container.<name>"`. Container exit detection: when the Docker API stream closes (container stopped), emits `{ level: "warn", msg: "container log capture stopped", container: "<name>", exitCode: <code> }`. No retry — container lifecycle is managed externally (spec 003). Missing container at start: emit warn log, skip goroutine spawn (per FR-019).

### 4. LogStream (`internal/logstream/`)

In-memory ring buffer (default 200 entries, configurable) + SSE fan-out hub. Ring buffer stores the last N `LogRecord` structs in a lock-free ring (atomic index, no allocations on read). Hub manages up to 10 concurrent SSE subscribers (per FR-009). Each subscriber has a per-client bounded buffer (default 1000 events, configurable via `observability.sseClientBuffer`). Server-side filtering: subscribers specify level/component/query filters at connect time (query params). Backpressure: when a subscriber's buffer overflows, hub sends `event: overflow` with `{missed: N}` and disconnects. Subscriber goroutine cleanup: `sync.WaitGroup` + context cancellation, MUST terminate within 5 seconds (per spec edge case). Heartbeat: hub sends `: keepalive\n\n` every 15 seconds to all subscribers.

### 5. LogExporter (`internal/loguicapi/exporter.go`)

Date-range tarball assembly. Handler: `GET /v1/logs/export?from=<ISO>&to=<ISO>`. Takes a snapshot of the current file list at export start (consistent snapshot — no partial files from mid-export rotation, per spec edge case). Reads sealed archives + active file up to snapshot offset. Assembles `.tar.gz`. PII scrubbing (when `observability.scrubPii` is `true`): masks client IPs to `***.***.***.<last-octet>`, replaces peer names with `peer-<id>`. Scrubbing applies ONLY to exported content, NOT to on-disk files or live stream. Empty result → 404 with `error: "no_logs_in_range"`.

### 6. MetricsRegistry (`internal/metrics/`)

Registers Prometheus collectors on a `prometheus.Registry`:
- Counters: `unet_api_requests_total{method,path,status}`, `unet_errors_total{class}`, `unet_peer_handshakes_total`
- Gauges: `unet_peers_connected`, `unet_routes_active`, `unet_uptime_seconds`, `unet_bandwidth_bytes_total{direction}`, `unet_tunnel_info{status}`
- Histograms: `unet_api_request_duration_seconds{path}`, `unet_log_write_duration_seconds`

Metric values are updated by daemon core code calling registry methods (e.g., `metrics.IncAPIRequest(method, path, status)`, `metrics.SetPeersConnected(n)`). No new instrumentation points for bandwidth — reuses `awg show` transfer counters already consumed by spec 001.

### 7. MetricsExposer (`internal/metrics/exposer.go`)

Separate `net/http.Server` for `/metrics` endpoint. Default bind: `127.0.0.1:9090` (loopback-only per round 1 clarification). Configurable via `observability.metrics.listenAddr`. When bound to non-loopback: requires `observability.metrics.bearerToken` (static Bearer token) — validated via constant-time compare. Emits startup warn log when non-loopback without bearer token. Response format: Prometheus text exposition (`text/plain; version=0.0.4`). Disabled by default — `observability.metrics.enabled: false`. Returns 404 when disabled.

### 8. LogUITail (cross-spec flag)

Admin UI client component that connects to the same SSE endpoint (`GET /v1/logs/stream`). For localhost admin UI, auth is bypassed (loopback detection, per spec FR-014). UI-specific controls (pause/resume, text search, component filter) are client-side over the SSE stream — backend doesn't track UI state. **This component is out of scope for the backend plan** but flagged for cross-spec frontend coordination with spec 001-init's embedded React UI.

---

## Data Flow

```
Daemon subsystems (tunnel, api, lifecycle, caddy-client, dns, ssh, config)
    │
    │ slog.Info("msg", "key", "value", ...)   [all daemon code paths]
    ▼
StructuredLogger (slog.Handler)
    │
    ├─── Secret redaction (FR-005 / FR-011)
    │    └─── Replace secret values → <redacted>
    │
    ├─── Per-component level check (FR-002)
    │    └─── Drop if below threshold
    │
    ├─── Dual-write
    │    ├──▶ JSONL file (via lumberjack)
    │    │    └──▶ Rotation (size ≥ 100MB or date change)
    │    │         └──▶ Archive: daemon-YYYY-MM-DD.N.jsonl
    │    │              └──▶ Retention cleanup (30d default)
    │    │
    │    └──▶ Ring buffer (last 200 entries)
    │         └──▶ LogStream Hub
    │              ├──▶ SSE subscriber 1 (filtered by level/component/q)
    │              ├──▶ SSE subscriber 2
    │              └──▶ ... up to 10 subscribers
    │
    └─── Metrics increment (async)
         └──▶ unet_errors_total{class} on error-level logs

Container output (Docker Engine API follow)
    │
    │ Raw stdout/stderr lines
    ▼
ContainerLogAggregator
    │ Re-emit as structured log via slog
    │ (source: "container", component: "container.<name>")
    ▼
StructuredLogger → (same dual-write path above)

Export flow:
    GET /v1/logs/export?from=X&to=Y
        │
        ├─── Snapshot current file list
        ├─── Read sealed archives + active file (up to offset)
        ├─── IF scrubPii: mask IPs, anonymize peer names
        └──▶ .tar.gz response

Metrics flow:
    Prometheus scraper ──▶ GET /metrics (separate listener)
        │
        └──▶ prometheus.Registry.Gather()
             └──▶ Text exposition format response
```

---

## Cross-Component Integration

### 002 Control Plane Integration

- **`GET /v1/logs/stream`**: Owned by spec 005, mounted on 002's `:8443` listener. Route registration in `internal/api/remote/routes.go` — the SSE handler is provided by 005's `logstream` package, but the mux and auth middleware come from 002.
- **`GET /v1/logs/export`**: Same pattern — handler from 005, mux + auth from 002.
- **`GET /metrics`**: Separate listener (default `:9090`), NOT on 002's `:8443` mux. Metrics listener has its own auth (bearer token for non-loopback) — independent of 002's PAT/JWT auth.
- **Follow-up**: 002's OpenAPI contract (`contracts/api.openapi.yaml`) needs extension to include `/v1/logs/stream` and `/v1/logs/export` endpoints. This is a documentation-only change to 002 — tracked as cross-spec follow-up.

### 003 Lifecycle Event Integration

- **Lifecycle audit log**: Spec 003 defines `~/.unet/audit.jsonl` (API audit, spec 002) and `lifecycle-audit.jsonl` (lifecycle audit, spec 003) as separate files. Spec 005's unified log stream reads from BOTH files as `source: "lifecycle"` events.
- **Aggregation approach**: The ContainerLogAggregator pattern extends to file-based sources. A `FileLogAggregator` goroutine tails `lifecycle-audit.jsonl` using a `fsnotify` watcher or polling (file append detection), parses each line, and re-emits as structured log via slog with `source: "lifecycle"`. This avoids double-write — 003 writes to its own file, 005 reads it into the unified stream.
- **No double-write**: Spec 005 does NOT intercept spec 003's audit logger. Each spec writes to its own file. Aggregation is read-only from 005's perspective.

### 001-Init Integration

- **Secret redaction**: Spec 005's `logger/redact.go` extends FR-011 (spec 001) with the same secret key list. Centralized in the slog handler — all log output (including 001's existing `log.Printf` calls once migrated to slog) benefits from redaction.
- **Log directory**: `~/.unet/logs/` is adjacent to existing `~/.unet/config.json`. Created by logger init with mode `0700`.
- **Existing `log.Printf` migration**: During transition, the slog handler is set as Go's default logger via `slog.SetDefault()`. Existing `log.Printf` calls are captured by slog's `log.Default()` bridge (Go 1.21+ feature). These are tagged `component: "legacy"` until migrated. Parallel running until all call sites are updated.

---

## Migration Strategy

### Phase 1: Logger installation (zero disruption)

1. Add `internal/logger/` package with custom slog handler.
2. Call `logger.Init()` in `daemon/main.go` startup, BEFORE any other subsystem.
3. `slog.SetDefault()` bridges existing `log.Printf` calls.
4. All existing code continues working — no call-site changes needed yet.
5. JSONL files start appearing in `~/.unet/logs/`.
6. Existing stdout output (Docker logs) continues in parallel.

### Phase 2: Ring buffer + SSE (incremental)

1. Add `internal/logstream/` package.
2. Wire logger handler's ring buffer write to hub broadcast.
3. Register SSE handler on 002's mux.
4. Test with `curl` SSE client.
5. Admin UI log viewer (frontend) in separate phase.

### Phase 3: Metrics + Container capture + Export (additive)

1. Add `internal/metrics/` — register collectors, start exposer.
2. Add `internal/loguicapi/container_aggregator.go` — wire Docker SDK follow.
3. Add `internal/loguicapi/exporter.go` — wire export handler.
4. Instrument daemon core code paths with metrics calls.

### Phase 4: Migration cleanup

1. Replace remaining `log.Printf` call sites with explicit `slog.Info/Warn/Error` calls.
2. Remove `component: "legacy"` bridge tags.
3. Disable parallel stdout logging (config flag `observability.logToStdout: false` by default after migration).

---

## Testing Strategy

### Unit tests

| Component | What's mocked | Tool |
|-----------|--------------|------|
| slog handler (JSON output) | `bytes.Buffer` as write target | `testing` + golden file comparison |
| Secret redaction | N/A (pure function over map) | Table-driven: secret keys → `<redacted>` |
| Per-component levels | Config override map | Table-driven: global=info, tunnel=debug → expected filter |
| Ring buffer (bounded) | N/A (in-memory) | Concurrent write/read, overflow behavior |
| SSE subscriber filter | N/A (in-memory) | Table-driven: level/component/q filter combinations |
| SSE backpressure | Buffered channel | Verify overflow event emission, subscriber disconnect |
| Prometheus collectors | N/A (registry in-memory) | Verify metric values after Inc/Set/Observe |
| Metrics exposer auth | HTTP request with/without Bearer | `httptest` + constant-time compare |
| Container aggregator | Docker client (interface) | Mock container log stream, verify structured output |
| Log exporter | Filesystem (temp dir) | `t.TempDir()` + golden tarball comparison |
| PII scrubbing | N/A (pure function) | Table-driven: IP masking, peer name anonymization |
| Config hot-reload | Filesystem (temp dir) | Modify config → verify level change takes effect |

### Integration tests

| Test | What runs real | What's mocked |
|------|---------------|---------------|
| Full logger pipeline | slog handler + lumberjack + ring buffer | N/A |
| SSE end-to-end | httptest server + SSE client | Docker containers, VPS SSH |
| Log rotation | Real file rotation in temp dir | N/A |
| Export with PII scrub | Real tarball assembly | Docker containers |
| Metrics scrape | Real Prometheus text exposition | Upstream metric sources (awg show) |
| Container log capture | Docker SDK client (mock server) | Real Docker API mock via `httptest` |

### Performance benchmarks

| Benchmark | Target | Method |
|-----------|--------|--------|
| 1k logs/sec sustained | No GC stalls, <5ms P99 write latency | `go test -bench=. -benchtime=10s` with continuous writes |
| Ring buffer concurrent read/write | No lock contention >1ms | `go test -race -bench=Ring` |
| SSE fan-out 10 subscribers | <1s P95 delivery latency | Mock subscribers, measure time from log emit to SSE event |
| Prometheus scrape | <100ms P95 under normal load | `httptest` server, measure Gather() + serialization |

---

## Open Risks

1. **SSE backpressure — slow consumers**: Per-client buffer of 1000 events may overflow during log storms (e.g., container crash loop). Mitigation: overflow event + disconnect. Client reconnects with adjusted filters. Risk: if all clients are slow simultaneously, hub goroutines accumulate. Mitigation: hard limit 10 subscribers, 5s goroutine cleanup on cancel.

2. **Log file disk-full handling**: JSONL files on `~/.unet/logs/` can fill disk. Spec edge case defines graceful degradation (delete archives → drop levels → SSE-only alert). Risk: deletion itself may fail if truly full. Mitigation: 500MB headroom check on logger init, refuse to start if < 500MB free.

3. **PII leak via misconfigured scrub flag**: `observability.scrubPii` defaults to OFF. Export without scrub includes full IPs and peer names. This is intentional (self-hosted, user owns logs) but risky if user accidentally shares an unscrubbed export. Mitigation: warn in export response header `X-Unet-PII-Scrubbed: false`, and in UI export button tooltip.

4. **Container log capture lag**: Docker Engine API follow stream has inherent latency (container runtime buffering). Under heavy container output (e.g., Caddy access logs for high-traffic site), capture may fall behind. Mitigation: since container output is opaque (no structured parsing per spec out-of-scope), lag is acceptable — timestamps are daemon ingestion time.

5. **slog handler mutex contention**: The custom handler holds a mutex during dual-write (file + ring buffer). Under high log volume, this becomes a serialization point. Mitigation: ring buffer write is O(1) atomic, lumberjack write is the bottleneck. If contention surfaces, batch ring buffer writes in a separate goroutine.

6. **Prometheus client_golang binary size impact**: `prometheus/client_golang` adds ~2MB to the binary. For a 30MB budget (SC-003 in spec 001), this is significant. Mitigation: if metrics are disabled at compile time, use build tag to exclude the package. Or accept the size increase as the cost of observability.

7. **Hot-reload race conditions**: Changing `observability.logLevels` via config file watcher while daemon is actively logging could race with in-flight log records. Mitigation: use `atomic.Value` for the level config map — readers never block, writer swaps atomically.

8. **Docker SDK connection to Docker daemon**: `docker/docker/client` requires access to the Docker daemon socket. If the daemon runs in a containerized environment itself (unlikely for unet's architecture), Docker socket mounting is needed. Risk: nil for typical unet deployment (daemon runs on host).

---

## Decisions Made in Plan Beyond Spec

| Topic | Decision | Why |
|-------|----------|-----|
| Logging library | `slog` (Go stdlib) over `zerolog` | Zero external dependency, stdlib stability guarantee, `slog.Handler` interface is sufficient for custom JSON handler. unet's log volume (<1k/sec) doesn't need zerolog's zero-alloc optimizations. |
| SSE implementation | Custom ~100-150 LOC over `r3labs/sse` | SSE protocol is trivial (4 event types). External dep adds maintenance risk with no material benefit. Custom impl gives full control over backpressure and keepalive timing. |
| Container log capture method | Docker Engine API SDK over `docker logs -f` CLI | SDK provides structured stream with proper demux of stdout/stderr, no shell-injection surface, native context cancellation. `docker logs -f` would require `os/exec` + pipe parsing. |
| Lifecycle audit aggregation | Read-only tail via `fsnotify`/polling | No double-write. Spec 003 owns its lifecycle-audit.jsonl; 005 reads it into unified stream. Clean separation of concerns. |
| Metrics endpoint listener | Separate `net/http.Server` from control plane | Metrics scraper expects simple `/metrics` endpoint, not the full auth middleware chain of 002. Separate listener allows independent bind address and auth model. |
| Parallel stdout during migration | `observability.logToStdout` config flag (default true during migration, false after) | Ensures no log loss during transition. Users who rely on `docker logs` for daemon output continue seeing it until migration is verified. |
| Ring buffer implementation | Lock-free ring via atomic index | Zero allocation on read, no GC pressure. Write side uses atomic store; read side is snapshot-based. Suitable for single-writer (handler mutex) + multi-reader (SSE subscribers). |
| Config key structure | `observability: { enabled, logLevels, maxFileSizeMB, retentionDays, captureContainerLogs, scrubPii, sseClientBuffer, logToStdout, metrics: { enabled, listenAddr, bearerToken } }` | Grouped under `observability` for clarity. Metrics sub-config for isolation. Matches the pattern established by 002's `remoteApi` key. |
