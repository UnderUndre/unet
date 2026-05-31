     1|     1|# Tasks: Observability — Logging, Metrics & Log UI
     2|     2|
     3|     3|**Input**: Design documents from `specs/005-observability/` (spec.md, plan.md, data-model.md, contracts/)
     4|     4|**Prerequisites**: plan.md (required), spec.md (required), data-model.md (required), contracts/log-record.schema.json (required), contracts/sse-protocol.md (required), contracts/metrics.md (required), contracts/log-ui-events.md (required)
     5|     5|**Version**: 1
     6|     6|**Last Updated**: 2026-05-28
     7|     7|
     8|     8|**Organization**: Tasks grouped by execution phase. Each task is assigned to a specialist agent. Phases unlock sequentially; tasks within a phase marked `[P]` can run in parallel.
     9|     9|
    10|    10|**Dependency Summary**: Foundation (slog handler + ring buffer + redaction + config) → File Output + Rotation → Container Aggregation → SSE Log Stream → Metrics → Export + Testing.
    11|    11|
    12|    12|---
    13|    13|
    14|    14|## Phase 1: Foundation (Structured Logger + Ring Buffer + Config)
    15|    15|
    16|    16|**Purpose**: Core logging primitives. Custom `slog.Handler` producing JSONL, lock-free ring buffer for in-memory capture, secret redaction pipeline, per-component level filtering, observability config struct. No file rotation, no SSE, no metrics yet. These primitives are the pipe foundation — every downstream phase flows through them.
    17|    17|
    18|    18|- [ ] TASK-1.1 [backend-specialist] [P] Implement custom slog handler with JSON output + dual-write in `internal/logger/handler.go`
    19|    19|  - `jsonHandler` implementing `slog.Handler`. Outputs one JSON object per `Handle()` call conforming to `contracts/log-record.schema.json`. Fields: `ts` (ISO-8601 UTC, ms precision), `level` (debug|info|warn|error), `component` (from `slog.Attr`), `source` (enum: daemon|container|lifecycle|api-audit, default "daemon"), `msg`, `seq` (atomic int64, starts at 1), `fields` (extensible map). Unknown attrs go into `fields`.
    20|    20|  - Dual-write: every record goes to (a) an `io.Writer` (file, set by Phase 2) and (b) a ring buffer (TASK-1.2). Writer and ring buffer injected at init. If writer is nil, skip file write (test mode). If ring buffer is nil, skip buffer write.
    21|    21|  - Mutex protects the writer swap (rotation will use this in Phase 2). Ring buffer write is lock-free (atomic store).
    22|    22|  - `slog.SetDefault()` bridge: existing `log.Printf` calls captured via Go's built-in log→slog bridge, tagged `component: "legacy"`.
    23|    23|  - **Deps**: none
    24|    24|  - **Acceptance**:
    25|    25|    - JSON output matches `log-record.schema.json` — validate with `encoding/json` unmarshal + field assertions
    26|    26|    - `seq` increments monotonically across concurrent goroutines (`go test -race`)
    27|    27|    - Dual-write: both writer and ring buffer receive identical records
    28|    28|    - `log.Printf` bridge produces records with `component: "legacy"`
    29|    29|    - Empty `msg` → record still written (no panic)
    30|    30|    - Golden file tests for JSON output format
    31|    31|  - **LOC**: ~300
    32|    32|  - **Maps to**: FR-001, plan.md §Component 1 (StructuredLogger), data-model.md LogRecord
    33|    33|
    34|    34|- [ ] TASK-1.2 [backend-specialist] [P] Implement lock-free ring buffer in `internal/logstream/ring.go`
    35|    35|  - Bounded ring buffer of `LogRecord` structs. Default capacity 200. Single-writer (handler mutex) + multi-reader (SSE subscribers). Write: atomic store at current index, wrap around. Read: snapshot entire buffer as slice (copy-on-read). No allocations on write path.
    36|    36|  - `NewRing(capacity int) *Ring`, `Write(record LogRecord)`, `Snapshot() []LogRecord`, `Since(seq int64) []LogRecord` (returns entries with seq > given value, for SSE replay).
    37|    37|  - `atomic.Uint64` for write index. `atomic.Int64` for total written count (used to compute oldest entry position).
    38|    38|  - **Deps**: none
    39|    39|  - **Acceptance**:
    40|    40|    - Capacity 5: writing 10 entries → Snapshot returns last 5
    41|    41|    - Since(seq): returns entries after given seq, empty if seq is current
    42|    42|    - Concurrent write + read: no data race (`go test -race`)
    43|    43|    - Zero allocations on Write path (bench with `go test -bench=Ring -benchmem`)
    44|    44|    - Snapshot is a copy — mutations to returned slice don't affect ring
    45|    45|  - **LOC**: ~120
    46|    46|  - **Maps to**: FR-016 (initial 200 lines), plan.md §Component 4 (LogStream), data-model.md LogRecord persistence "in-memory ring buffer"
    47|    47|
    48|    48|- [ ] TASK-1.3 [backend-specialist] [P] Implement secret redaction in `internal/logger/redact.go`
    49|    49|  - `RedactFields(fields map[string]any) map[string]any` — scans map values for known secret patterns. Redacts: private keys (PEM blocks), API tokens (`unet_` prefix + known Cloudflare/JWT patterns), SSH passwords, mTLS client keys, `uiToken`. Replaces with `<redacted>`. Recursive for nested maps. Also redacts string values containing `-----BEGIN PRIVATE KEY-----` or `-----BEGIN RSA PRIVATE KEY-----`.
    50|    50|  - Secret key list: `password`, `secret`, `private_key`, `privateKey`, `token`, `apiKey`, `api_key`, `bearerToken`, `uiToken`, `mtls_key`, `cf_token`. Case-insensitive key match.
    51|    51|  - Extends FR-011 (spec 001-init) — centralized in slog handler so ALL output benefits.
    52|    52|  - **Deps**: none
    53|    53|  - **Acceptance**:
    54|    54|    - Table-driven: `{password: "hunter2"}` → `{password: "<redacted>"}`
    55|    55|    - PEM block in value → `<redacted>`
    56|    56|    - Nested map redaction works recursively
    57|    57|    - Non-secret fields pass through unchanged
    58|    58|    - `nil` fields map → `nil` (no panic)
    59|    59|    - Benchmark: < 1μs per redaction for typical log record (10 fields)
    60|    60|  - **LOC**: ~100
    61|    61|  - **Maps to**: FR-005, FR-011 (spec 001), plan.md §Component 1 (secret redaction)
    62|    62|
    63|    63|- [ ] TASK-1.4 [backend-specialist] [P] Implement per-component level filtering in `internal/logger/levels.go`
    64|    64|  - `LevelConfig` struct: `Global slog.Level` + `Overrides map[string]slog.Level`. `Enabled(component string, level slog.Level) bool` — check overrides first, fall back to global. `ReloadFromConfig(cfg ObservabilityConfig)` swaps atomically via `atomic.Value`.
    65|    65|  - Hot-reload: `observability.logLevels` changes take effect immediately (FR-022). Writer swaps `atomic.Value` — readers never block.
    66|    66|  - **Deps**: none
    67|    67|  - **Acceptance**:
    68|    68|    - Global=info, override tunnel=debug → tunnel debug logs pass, api debug logs drop
    69|    69|    - No override → falls back to global
    70|    70|    - Hot-reload: update config → new level takes effect on next log call, no restart
    71|    71|    - Concurrent read during reload: no data race
    72|    72|  - **LOC**: ~80
    73|    73|  - **Maps to**: FR-002, FR-022 (hot-reload), plan.md §Component 1 (per-component thresholds)
    74|    74|
    75|    75|- [ ] TASK-1.5 [backend-specialist] Implement ObservabilityConfig struct + init wiring in `internal/observability/config.go` and `internal/observability/init.go`
    76|    76|  - `ObservabilityConfig` struct matching plan.md config key structure: `enabled`, `logLevels`, `maxFileSizeMB` (default 100), `retentionDays` (default 30), `captureContainerLogs` (default true per round 1), `scrubPii` (default false), `sseClientBuffer` (default 1000), `logToStdout` (default true during migration), `metrics: {enabled, listenAddr, bearerToken}`. Loads from `observability` key in `~/.unet/config.json`.
    77|    77|  - `Init(cfg ObservabilityConfig)`: creates `~/.unet/logs/` dir (mode 0700), creates ring buffer, creates slog handler, calls `slog.SetDefault()`. Returns `*ObservabilityBus` (holds ring + config ref) for downstream wiring.
    78|    78|  - **Deps**: TASK-1.1 (handler), TASK-1.2 (ring), TASK-1.3 (redact), TASK-1.4 (levels)
    79|    79|  - **Acceptance**:
    80|    80|    - Config loads from JSON with correct defaults for missing fields
    81|    81|    - `~/.unet/logs/` created with mode 0700
    82|    82|    - `slog.Default()` is the custom handler after Init
    83|    83|    - Missing config → defaults applied, no error
    84|    84|    - Init can be called in tests with temp dir
    85|    85|  - **LOC**: ~200
    86|    86|  - **Maps to**: FR-022, plan.md §Component 8 (config), plan.md config key structure
    87|    87|
    88|    88|**Checkpoint**: Custom slog handler produces valid JSONL. Ring buffer captures last 200 entries. Secret redaction works. Per-component levels configurable. Config loads + applies defaults. All existing `log.Printf` calls bridge through slog. No files written yet (no rotation), no SSE, no metrics.
    89|    89|
    90|    90|---
    91|    91|
    92|    92|## Phase 2: File Output + Rotation
    93|    93|
    94|    94|**Purpose**: Persistent JSONL file writing with size-based + date-based rotation, retention cleanup, disk-full graceful degradation. Lumberjack handles rotation mechanics; our handler wraps it.
    95|    95|
    96|    96|- [ ] TASK-2.1 [backend-specialist] Integrate lumberjack writer + daily rotation in `internal/logger/handler.go` (extend TASK-1.1)
    97|    97|  - Wire lumberjack `Logger` as the `io.Writer` for the slog handler. Config: `Filename: ~/.unet/logs/daemon-YYYY-MM-DD.jsonl`, `MaxSize: observability.maxFileSizeMB`, `MaxBackups: 0` (managed by our retention, not lumberjack's), `LocalTime: false` (UTC), `Compress: false`.
    98|    98|  - Date-based rotation: handler checks current date on each write. If date changed, atomically swap lumberjack Logger to new filename (under handler mutex). Old file sealed.
    99|    99|  - File path pattern: `daemon-2026-05-27.jsonl` (active), `daemon-2026-05-27.1.jsonl` (first rotation of that day).
   100|   100|  - **Deps**: TASK-1.1, TASK-1.5 (config with maxFileSizeMB)
   101|   101|  - **Acceptance**:
   102|   102|    - Log lines appear in `~/.unet/logs/daemon-YYYY-MM-DD.jsonl`
   103|   103|    - File rotation at size threshold: new file created, old renamed with `.N` suffix
   104|   104|    - Date rotation at midnight UTC: new file created, old file sealed
   105|   105|    - No log lines lost during rotation (mutex-protected swap)
   106|   106|    - Rotation test: write >100MB (or configurable lower threshold), verify new file created
   107|   107|  - **LOC**: ~200 (additions to handler.go)
   108|   108|  - **Maps to**: FR-003, plan.md §Component 2 (LogRotator), data-model.md LogFile lifecycle
   109|   109|
   110|   110|- [ ] TASK-2.2 [backend-specialist] Implement retention cleanup sweeper in `internal/logger/retention.go`
   111|   111|  - `RetentionSweeper`: on daemon start + daily timer, scan `~/.unet/logs/` for archives older than `observability.retentionDays` (default 30). Delete expired files. Active files (current day, regardless of age) never deleted.
   112|   112|  - Configurable retention period. Hot-reload: if `retentionDays` changes, next sweep uses new value.
   113|   113|  - **Deps**: TASK-1.5 (config)
   114|   114|  - **Acceptance**:
   115|   115|    - Files older than retention period → deleted
   116|   116|    - Current day's file → never deleted even if old
   117|   117|    - Empty log dir → no error
   118|   118|    - Hot-reload: new retention period applied on next sweep
   119|   119|    - Sweep logs its own action: "deleted N expired log archives"
   120|   120|  - **LOC**: ~100
   121|   121|  - **Maps to**: FR-004, plan.md §Component 2 (retention), data-model.md LogFile lifecycle (expired)
   122|   122|
   123|   123|- [ ] TASK-2.3 [backend-specialist] Implement disk-full graceful degradation in `internal/logger/handler.go` (extend TASK-2.1)
   124|   124|  - On write error from lumberjack: (1) attempt to delete oldest archive files beyond retention, (2) retry write, (3) if still failing, drop `debug`-level lines silently, (4) if still failing, drop `info`-level lines, (5) if still failing, stop writing to file and emit a single `error`-level alert to SSE subscribers only (not to file — chicken-and-egg). Resume writing when space becomes available (check on next write attempt).
   125|   125|  - 500MB headroom check on logger init: if `~/.unet/logs/` filesystem has < 500MB free, emit warn log but continue (don't refuse start — tunnel > logging per plan risk #2).
   126|   126|  - **Deps**: TASK-2.1 (writer), TASK-2.2 (retention)
   127|   127|  - **Acceptance**:
   128|   128|    - Simulate full disk (mock writer returning error): debug dropped → info dropped → SSE-only alert
   129|   129|    - Recovery: when mock writer succeeds again, normal logging resumes
   130|   130|    - 500MB headroom check: warn emitted, logging continues
   131|   131|    - No panic on I/O errors — daemon stays up
   132|   132|  - **LOC**: ~150
   133|   133|  - **Maps to**: spec.md Edge Case "Log writer disk full", plan.md Risk #2
   134|   134|
   135|   135|**Checkpoint**: Structured JSONL files written to disk. Size + date rotation working. Retention cleanup running. Disk-full handled gracefully. Ring buffer + SSE-ready dual-write pipeline complete. No SSE subscribers yet, no metrics.
   136|   136|
   137|   137|---
   138|   138|
   139|   139|## Phase 3: Container Log Aggregation
   140|   140|
   141|   141|**Purpose**: Docker container log capture. Spawns goroutines per managed container, streams stdout/stderr via Docker Engine API, re-emits as structured log records through the slog pipeline.
   142|   142|
   143|   143|- [ ] TASK-3.1 [backend-specialist] Implement Docker Engine API container log aggregator in `internal/loguicapi/container_aggregator.go`
   144|   144|  - `ContainerAggregator` struct. `Start(ctx context.Context, containers []string)` — for each container name, resolve to container ID via Docker API, spawn goroutine running `client.ContainerLogs(ctx, id, types.ContainerLogsOptions{Follow: true, ShowStdout: true, ShowStderr: true, Tail: "0"})`.
   145|   145|  - Each output line re-emitted as `slog.Info` with attrs: `source: "container"`, `container: "<name>"`, `component: "container.<name>"`, `container_ts: <parsed-timestamp-or-omitted>`. Raw container output in `msg`.
   146|   146|  - Docker API stdout/stderr demux: `stdcopy.StdCopy` from Docker SDK splits streams. Both go to same slog pipeline.
   147|   147|  - Container exit detection: when stream closes (container stopped), emit `{level: warn, msg: "container log capture stopped", container: "<name>", exit_code: <code>}`. Goroutine exits.
   148|   148|  - `Stop(ctx context.Context)` cancels all goroutines via context, waits via `sync.WaitGroup` (timeout 5s).
   149|   149|  - **Deps**: TASK-1.1 (handler), TASK-1.5 (init)
   150|   150|  - **Acceptance**:
   151|   151|    - Container running: goroutine spawned, log lines re-emitted as structured records
   152|   152|    - Container not found: warn log emitted, goroutine NOT spawned (per FR-019)
   153|   153|    - Container stops mid-stream: warn log emitted, goroutine exits cleanly
   154|   154|    - Stop() cancels all goroutines within 5s
   155|   155|    - Docker client mocked via interface for unit tests
   156|   156|    - No goroutine leaks (runtime.NumGoroutine before/after check)
   157|   157|  - **LOC**: ~300
   158|   158|  - **Maps to**: FR-017, FR-018, FR-019, plan.md §Component 3 (ContainerLogAggregator), data-model.md LogRecord source=container
   159|   159|
   160|   160|- [ ] TASK-3.2 [backend-specialist] Wire container aggregator into observability init in `internal/observability/init.go`
   161|   161|  - If `observability.captureContainerLogs` is true (default per round 1), create `ContainerAggregator` and `Start()` with managed containers: `["unet-amnezia-awg", "unet-caddy"]`. On daemon shutdown, call `Stop()`.
   162|   162|  - Hot-reload: if `captureContainerLogs` toggled from false→true, start aggregator. If true→false, stop aggregator. Re-read container list from config (future: configurable container list, for now hardcoded).
   163|   163|  - **Deps**: TASK-3.1, TASK-1.5
   164|   164|  - **Acceptance**:
   165|   165|    - Config true: aggregator starts at daemon init
   166|   166|    - Config false: aggregator not started, no Docker client created
   167|   167|    - Hot-reload true→false: running aggregator stops cleanly
   168|   168|    - Hot-reload false→true: aggregator starts
   169|   169|    - Missing Docker daemon: warn log, aggregator skipped (no retry loop)
   170|   170|  - **LOC**: ~80
   171|   171|  - **Maps to**: FR-017, FR-022 (hot-reload), plan.md §Component 3
   172|   172|
   173|   173|- [ ] TASK-3.3 [backend-specialist] Implement lifecycle-audit read-only tail aggregator in `internal/loguicapi/file_aggregator.go`
   174|   174|  - `FileAggregator`: tails `~/.unet/lifecycle-audit.jsonl` (spec 003) using polling (stat for size change, read new bytes). Each new line parsed and re-emitted as structured log via slog with `source: "lifecycle"`, `component: "lifecycle"`. No double-write — 003 owns its file, 005 reads it.
   175|   175|  - Similarly tails `~/.unet/audit.jsonl` (spec 002) as `source: "api-audit"`.
   176|   176|  - Graceful handling: file doesn't exist → skip (warn once). File appears later → start tailing. File truncated → reset offset, warn.
   177|   177|  - **Deps**: TASK-1.1, TASK-1.5
   178|   178|  - **Acceptance**:
   179|   179|    - New line appended to lifecycle-audit.jsonl → appears in unified log stream
   180|   180|    - New line appended to audit.jsonl → appears in unified log stream
   181|   181|    - File doesn't exist → warn log, no goroutine spawned
   182|   182|    - File truncated → offset reset, warn emitted
   183|   183|    - Stop() terminates polling goroutine within 2s
   184|   184|  - **LOC**: ~200
   185|   185|  - **Maps to**: plan.md §"003 Lifecycle Event Integration", data-model.md LogRecord source=lifecycle|api-audit
   186|   186|
   187|   187|**Checkpoint**: Container logs flowing into unified slog pipeline. Lifecycle audit + API audit tailed into unified stream. All log sources (daemon, container, lifecycle, api-audit) writing to same JSONL files + ring buffer. SSE endpoint not yet wired — that's Phase 4.
   188|   188|
   189|
   190|---
   191|
   192|## Phase 4: SSE Log Stream
   193|
   194|**Purpose**: HTTP handler for live log streaming via Server-Sent Events. Fan-out hub, per-subscriber filters, backpressure with bounded buffers, reconnection with Last-Event-ID replay, keepalive heartbeat. Custom SSE implementation (~100-150 LOC) per plan decision — no third-party SSE library.
   195|
   196|- [ ] TASK-4.1 [backend-specialist] Implement SSE fan-out hub in `internal/logstream/hub.go`
   197|  - `Hub` struct: manages up to 10 concurrent subscribers (per FR-009). `Register(sub *Subscriber) error` — returns error if limit reached. `Unregister(sub *Subscriber)`. `Broadcast(record LogRecord)` — sends to all registered subscribers' filter+buffer goroutines.
   198|  - Each subscriber has its own goroutine that reads from a buffered channel (capacity = `observability.sseClientBuffer`, default 1000). Goroutine applies filters (level, component, source, query) before writing to SSE response.
   199|  - Broadcast is non-blocking: if subscriber's channel is full, increment dropped counter. After broadcast, check overflow: if subscriber's channel has been full for > N consecutive broadcasts, send overflow event + disconnect.
   200|  - Keepalive: goroutine sends `: keepalive\n\n` every 15 seconds of inactivity (no log events sent). Reset timer on each event sent.
   201|  - Cleanup: `sync.WaitGroup` tracks all subscriber goroutines. `Shutdown()` cancels all contexts, waits up to 5s for goroutines to exit.
   202|  - **Deps**: TASK-1.2 (ring buffer)
   203|  - **Acceptance**:
   204|    - Register 10 subscribers → all receive broadcasts
   205|    - 11th subscriber → error (limit reached)
   206|    - Subscriber filter: only matching records delivered
   207|    - Backpressure: slow consumer's channel fills → overflow event sent → subscriber disconnected
   208|    - Keepalive sent every 15s during inactivity
   209|    - Shutdown: all goroutines terminate within 5s
   210|    - No goroutine leaks (runtime.NumGoroutine check)
   211|  - **LOC**: ~250
   212|  - **Maps to**: FR-006, FR-007, FR-008, FR-009, plan.md §Component 4 (LogStream), contracts/sse-protocol.md
   213|
   214|- [ ] TASK-4.2 [backend-specialist] Implement SSE subscriber + filter logic in `internal/logstream/subscriber.go`
   215|  - `Subscriber` struct: ID (UUID), filters (`filterLevel`, `filterComponent`, `filterSource`, `filterQuery`), per-client buffered channel, SSE response writer, context + cancel func.
   216|  - `Matches(record LogRecord) bool`: level ≥ filter, component exact match (or empty = all), source exact match (or empty = all), query case-insensitive substring in `msg` (or empty = all). AND-combined per FR-007.
   217|  - `WriteSSE(record LogRecord)`: format SSE event with `id: <seq>\nevent: log\ndata: <json>\n\n`. Flush after write.
   218|  - `WriteOverflow(missed int)`: format `event: overflow\ndata: {"missed": N}\n\n` then signal disconnect.
   219|  - **Deps**: TASK-4.1 (hub)
   220|  - **Acceptance**:
   221|    - Filter level=warn: info records dropped, warn+error delivered
   222|    - Filter component=tunnel: only tunnel records delivered
   223|    - Filter q="timeout": only records with "timeout" in msg delivered
   224|    - Multiple filters: AND-combined
   225|    - Empty filters: all records delivered
   226|    - SSE format matches contracts/sse-protocol.md
   227|    - Overflow event format correct
   228|  - **LOC**: ~150
   229|  - **Maps to**: FR-007, FR-008, FR-009, contracts/sse-protocol.md (event types), data-model.md LogStreamSubscriber
   230|
   231|- [ ] TASK-4.3 [backend-specialist] Implement SSE HTTP handler in `internal/logstream/sse_handler.go`
   232|  - `SSEHandler(hub *Hub, ring *Ring)` returns `http.HandlerFunc`. Mounted on `GET /v1/logs/stream`.
   233|  - Validates query params (level, component, source, q). Invalid values → 400 with structured error (per sse-protocol.md "Error Responses").
   234|  - Checks subscriber limit → 429 if full. Checks auth (Bearer token with `read` scope) via middleware from spec 002. Loopback bypass per FR-014.
   235|  - On success: sets SSE headers (`Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`, `X-Accel-Buffering: no`), sends `: connected\ndata: {"subscriber_id":"...", "filters":{...}}\n\n`.
   236|  - Initial load: reads ring buffer snapshot, filters, sends matching records as SSE events.
   237|  - Reconnection: checks `Last-Event-ID` header → uses `ring.Since(seq)` to replay missed events. If oldest available > requested, sends `: replay_truncated\ndata: {"available_from":N,"requested_from":M}\n\n`.
   238|  - Registers subscriber with hub, blocks on subscriber's context (client disconnect = context cancel).
   239|  - On disconnect: unregisters from hub, goroutine cleanup.
   240|  - **Deps**: TASK-4.1 (hub), TASK-4.2 (subscriber), TASK-1.5 (config for subscriber limit)
   241|  - **Acceptance**:
   242|    - Valid request → SSE connection established, initial 200 (or fewer) records sent
   243|    - Invalid level param → 400 with error JSON
   244|    - No auth (non-loopback) → 401
   245|    - Insufficient scope → 403
   246|    - Subscriber limit reached → 429 with Retry-After
   247|    - Last-Event-ID reconnect: missed records replayed
   248|    - Last-Event-ID older than ring → replay_truncated sent
   249|    - Client disconnect → subscriber unregistered, no goroutine leak
   250|  - **LOC**: ~200
   251|  - **Maps to**: FR-006, FR-007, FR-008, FR-009, FR-014, contracts/sse-protocol.md
   252|
   253|- [ ] TASK-4.4 [backend-specialist] Wire SSE handler into control plane + localhost listeners in `internal/observability/init.go` and `internal/api/remote/routes.go`
   254|  - Register `GET /v1/logs/stream` on spec 002's `:8443` mux (auth middleware applied). Also register on `:8080` localhost mux (loopback auth bypass per log-ui-events.md recommendation).
   255|  - Wire hub.Broadcast into slog handler: after each dual-write (file + ring), also broadcast to hub.
   256|  - Hub lifecycle: start in observability.Init, stop on daemon shutdown.
   257|  - **Deps**: TASK-4.3, TASK-1.5
   258|  - **Acceptance**:
   259|    - SSE endpoint accessible on both :8443 and :8080
   260|    - :8443 requires Bearer auth with read scope
   261|    - :8080 loopback bypass works
   262|    - Log events broadcast to all connected subscribers
   263|    - Hub starts with daemon, stops cleanly on shutdown
   264|  - **LOC**: ~80
   265|  - **Maps to**: FR-006, FR-014, contracts/log-ui-events.md (dual-listener), plan.md §"002 Control Plane Integration"
   266|
   267|**Checkpoint**: SSE endpoint live. Up to 10 concurrent subscribers. Per-subscriber filters. Backpressure with overflow disconnect. Reconnection with Last-Event-ID replay. Keepalive heartbeat. Dual-listener (:8443 + :8080). Admin UI can connect from localhost without auth. No metrics yet.
   268|
   269|---
   270|
   271|## Phase 5: Prometheus Metrics
   272|
   273|**Purpose**: Prometheus-compatible metrics exposition. Counter/gauge/histogram registration, separate `:9090` listener, loopback-only default with optional non-loopback + bearer token auth.
   274|
   275|- [ ] TASK-5.1 [backend-specialist] Implement Prometheus collector registry in `internal/metrics/registry.go`, `counters.go`, `gauges.go`, `histograms.go`
   276|  - `Registry` struct wrapping `prometheus.Registry`. Registers all metrics from `contracts/metrics.md`:
   277|    - Counters: `unet_api_requests_total{method,path,status}`, `unet_errors_total{class}`, `unet_peer_handshakes_total`, `unet_bandwidth_bytes_total{direction}`
   278|    - Gauges: `unet_peers_connected`, `unet_routes_active`, `unet_uptime_seconds`, `unet_tunnel_info{status}`
   279|    - Histograms: `unet_api_request_duration_seconds{path}`, `unet_log_write_duration_seconds`
   280|  - Public methods: `IncAPIRequest(method, path, status)`, `IncError(class)`, `IncPeerHandshake()`, `SetBandwidth(direction, bytes)`, `SetPeers(n)`, `SetRoutes(n)`, `SetUptime(seconds)`, `SetTunnelStatus(status)`, `ObserveAPIDuration(path, duration)`, `ObserveLogWriteDuration(duration)`.
   281|  - Error-level auto-increment: slog handler calls `IncError(class)` when `level=error` and `fields.error_class` is set.
   282|  - **Deps**: none (pure registry, no HTTP)
   283|  - **Acceptance**:
   284|    - All metrics registered with correct types and labels
   285|    - Inc/Set/Observe methods update values correctly
   286|    - `Registry.Gather()` returns all metric families
   287|    - Counter names end with `_total`
   288|    - Gauge values non-negative
   289|    - Histogram buckets match contracts/metrics.md defaults
   290|  - **LOC**: ~350
   291|  - **Maps to**: FR-013, plan.md §Component 6 (MetricsRegistry), data-model.md MetricSeries, contracts/metrics.md
   292|
   293|- [ ] TASK-5.2 [backend-specialist] Implement metrics exposer HTTP handler + separate listener in `internal/metrics/exposer.go`
   294|  - `Exposer` struct. Creates a separate `net/http.Server` for `/metrics` endpoint. Default bind: `127.0.0.1:9090` (loopback-only per round 1 clarification).
   295|  - When `observability.metrics.enabled` is false → handler returns 404 for all requests.
   296|  - Auth: loopback bind → no auth. Non-loopback bind → `Authorization: Bearer *** required, constant-time compare against `observability.metrics.bearerToken`. Missing/invalid → 401.
   297|  - Non-loopback without bearer token configured → emit warn log at startup (per FR-012): `"Metrics endpoint bound to non-loopback address without bearer token..."`.
   298|  - Response format: Prometheus text exposition (`text/plain; version=0.0.4; charset=utf-8`).
   299|  - Graceful shutdown: `Shutdown(ctx)` with 5s timeout.
   300|  - **Deps**: TASK-5.1 (registry)
   301|  - **Acceptance**:
   302|    - Enabled + loopback: metrics served without auth
   303|    - Disabled: 404 on any request
   304|    - Non-loopback + valid bearer: metrics served
   305|    - Non-loopback + invalid bearer: 401
   306|    - Non-loopback + no bearer token configured: warn log at startup
   307|    - Response Content-Type correct
   308|    - Shutdown completes within 5s
   309|  - **LOC**: ~200
   310|  - **Maps to**: FR-010, FR-011, FR-012, plan.md §Component 7 (MetricsExposer), contracts/metrics.md (configuration)
   311|
   312|- [ ] TASK-5.3 [backend-specialist] Instrument daemon core code paths with metrics calls
   313|  - Wire `metrics.Registry` into daemon subsystems: tunnel manager (SetPeers, SetTunnelStatus, SetBandwidth, IncPeerHandshake), caddy-client (SetRoutes), API handlers (IncAPIRequest, ObserveAPIDuration via middleware), slog handler (IncError on error-level records, ObserveLogWriteDuration).
   314|  - Middleware for API requests: `metricsMiddleware` wraps each handler — records method, path, status, duration. Registered in spec 002's middleware chain.
   315|  - Bandwidth polling: reuse existing `awg show` polling interval from spec 001 — extract transfer counters → SetBandwidth.
   316|  - Uptime: `time.Since(startTime)` gauge updated on each `/metrics` scrape (via custom `prometheus.GaugeFunc`).
   317|  - **Deps**: TASK-5.1, TASK-5.2
   318|  - **Acceptance**:
   319|    - API request → `unet_api_requests_total` incremented
   320|    - API request → `unet_api_request_duration_seconds` observed
   321|    - Tunnel connect → `unet_peers_connected` updated
   322|    - Error log with error_class → `unet_errors_total` incremented
   323|    - Log write → `unet_log_write_duration_seconds` observed
   324|    - `curl http://127.0.0.1:9090/metrics` returns all expected metrics with non-zero values after activity
   325|  - **LOC**: ~200
   326|  - **Maps to**: FR-013, plan.md §Component 6, contracts/metrics.md
   327|
   328|- [ ] TASK-5.4 [backend-specialist] Wire metrics exposer into observability init
   329|  - In `observability.Init()`: if `metrics.enabled`, create `Exposer`, start listener. On daemon shutdown, call `exposer.Shutdown()`.
   330|  - Hot-reload: if `metrics.enabled` toggled from false→true, start exposer. If true→false, stop exposer (FR-022).
   331|  - **Deps**: TASK-5.2, TASK-5.3, TASK-1.5
   332|  - **Acceptance**:
   333|    - Config enabled: exposer starts at init
   334|    - Config disabled: no listener started
   335|    - Hot-reload enabled→disabled: exposer stops
   336|    - Hot-reload disabled→enabled: exposer starts
   337|    - Port conflict → clear error log, exposer skipped (daemon continues)
   338|  - **LOC**: ~60
   339|  - **Maps to**: FR-010, FR-022, plan.md §Component 8
   340|
   341|**Checkpoint**: Prometheus `/metrics` endpoint live on separate `:9090` listener. All counters/gauges/histograms registered and instrumented. Loopback-only default, bearer token for non-loopback. Metrics auto-incremented from slog handler + API middleware. Hot-reload for enable/disable.

---

## Phase 6: Export + Testing

**Purpose**: Log export endpoint (tarball + PII scrub), retention sweeper integration tests, comprehensive unit + integration + perf tests. This is the validation phase — everything gets exercised.

- [ ] TASK-6.1 [backend-specialist] Implement log export handler + tarball assembly in `internal/loguicapi/export_handler.go` and `internal/loguicapi/exporter.go`
  - `ExportHandler` returns `http.HandlerFunc` for `GET /v1/logs/export`. Params: `from`, `to` (ISO-8601). Validates date range (start < end, max 30 days). Requires `read` scope auth (spec 002 middleware).
  - `Exporter.AssembleTarball(from, to time.Time, scrubPii bool) (*os.File, error)`: (1) snapshot current file list, (2) read sealed archives + active file up to snapshot offset, (3) filter JSONL lines by date range, (4) if scrubPii: mask IPs to `***.***.***.<last-octet>`, replace peer names with `peer-<id>`, (5) write `.tar.gz` to temp file, (6) return file handle.
  - Response: stream tarball as `application/gzip` with header `X-Unet-PII-Scrubbed: true|false`. Empty result → 404 with `error: "no_logs_in_range"`.
  - Temp file cleanup: delete after response sent (defer in handler).
  - **Deps**: TASK-2.1 (log files), TASK-1.5 (config for scrubPii)
  - **Acceptance**:
    - Valid date range → .tar.gz response with correct JSONL files
    - PII scrub enabled: IPs masked, peer names anonymized
    - PII scrub disabled: full content in tarball, `X-Unet-PII-Scrubbed: false`
    - Empty range → 404 with `no_logs_in_range`
    - Date range > 30 days → 400
    - Rotation during export → consistent snapshot (no partial files)
    - Temp file cleaned up after response
  - **LOC**: ~300
  - **Maps to**: FR-020, FR-021, plan.md §Component 5 (LogExporter), data-model.md LogExportBundle, spec.md Edge Case "rotation overlap"

- [ ] TASK-6.2 [backend-specialist] Wire export handler into control plane routes in `internal/api/remote/routes.go`
  - Register `GET /v1/logs/export` on spec 002's `:8443` mux with auth middleware (read scope).
  - **Deps**: TASK-6.1
  - **Acceptance**:
    - Endpoint accessible on :8443
    - Auth required: no token → 401, wrong scope → 403
    - Valid request returns tarball
  - **LOC**: ~30
  - **Maps to**: FR-020, plan.md §"002 Control Plane Integration"

- [ ] TASK-6.3 [test-engineer] Write unit tests for slog handler, ring buffer, redaction, and level filtering
  - `internal/logger/handler_test.go`: golden file comparison for JSON output. Concurrent write test. Dual-write verification. `log.Printf` bridge test.
  - `internal/logstream/ring_test.go`: capacity overflow, Since() replay, concurrent read/write (`-race`), zero-alloc benchmark.
  - `internal/logger/redact_test.go`: table-driven tests for all secret key patterns, PEM blocks, nested maps, nil input.
  - `internal/logger/levels_test.go`: table-driven for global/override, hot-reload atomic swap, concurrent read during reload.
  - **Deps**: TASK-1.1, TASK-1.2, TASK-1.3, TASK-1.4
  - **Acceptance**:
    - All tests pass with `go test -race ./internal/logger/ ./internal/logstream/`
    - Coverage > 85% for logger/ and logstream/ packages
    - Benchmarks: ring write < 100ns/op, redact < 1μs/op, handler Handle < 5μs/op
    - Golden files committed for JSON output regression
  - **LOC**: ~400
  - **Maps to**: plan.md §Testing Strategy (Unit tests table)

- [ ] TASK-6.4 [test-engineer] Write integration tests for SSE end-to-end, rotation, and container capture
  - SSE e2e: `httptest.Server` + SSE client (`EventSource`-like via `net/http` with streaming body). Test: connect → receive initial buffer → receive live events → filter → reconnect with Last-Event-ID → overflow disconnect. Measure P95 latency (target < 1s per SC-001).
  - Rotation: write to temp dir, exceed size threshold, verify new file created + old sealed. Date rotation: mock clock, verify midnight swap.
  - Container capture: mock Docker API server via `httptest`, stream container output, verify structured records in ring buffer. Container stop → warn event. Missing container → no goroutine.
  - Export: create log files in temp dir, export with/without PII scrub, verify tarball contents. Rotation during export → consistent snapshot.
  - **Deps**: TASK-4.3, TASK-2.1, TASK-3.1, TASK-6.1
  - **Acceptance**:
    - SSE e2e: events delivered < 1s (P95)
    - SSE reconnect: missed events replayed from ring
    - Rotation: no log lines lost during size/date rotation
    - Container capture: structured records correct, goroutine cleanup verified
    - Export: tarball valid, PII scrub correct, empty range → 404
    - All tests use temp dirs, no real `~/.unet`
  - **LOC**: ~500
  - **Maps to**: SC-001, SC-002, SC-003, SC-004, plan.md §Testing Strategy (Integration tests table)

- [ ] TASK-6.5 [test-engineer] Write perf benchmarks for sustained logging + SSE fan-out
  - Benchmark: 1k logs/sec sustained for 10 seconds. Measure: P99 write latency (target < 5ms), GC pause count, memory allocation rate. Test with `go test -bench=. -benchtime=10s -benchmem`.
  - SSE fan-out: 10 concurrent subscribers, measure P95 delivery latency from log emit to SSE event received. Target < 1s (SC-003).
  - Prometheus scrape: measure Gather() + serialization time. Target < 100ms P95 (SC-004).
  - Ring buffer concurrent: `go test -race -bench=Ring`, verify no lock contention > 1ms.
  - **Deps**: TASK-6.3, TASK-6.4
  - **Acceptance**:
    - 1k logs/sec: P99 write < 5ms, no GC stalls
    - 10 SSE subscribers: P95 delivery < 1s
    - Prometheus scrape: P95 < 100ms
    - Results committed as benchmark baseline file
  - **LOC**: ~200
  - **Maps to**: SC-001, SC-002, SC-003, SC-004, plan.md §Performance benchmarks table

- [ ] TASK-SEC1 [security-auditor] Holistic security audit of observability subsystem
  - Comprehensive security review covering PII scrub correctness (regex completeness, false-negative cases), metrics endpoint auth bypass attempts, SSE token validation timing-attack resistance, log file permission audit (0600 enforced), secret redaction completeness (peer keys, tokens never logged in plaintext). Produces a security findings document at `specs/005-observability/reviews/security-audit.md` with severity classification.
  - **Deps**: TASK-1.1, TASK-1.2, TASK-2.3, TASK-3.1, TASK-3.3, TASK-4.4, TASK-5.3, TASK-6.2 (after all security-relevant implementations land)
  - **Acceptance**:
    - All PII scrub regex tested against IPv4/IPv6/peer-name fixtures with adversarial inputs
    - Metrics endpoint auth bypass attempts documented (non-loopback origin tests with no token, wrong token, expired token)
    - SSE token validation uses constant-time comparison (verified by code review)
    - All log file paths verified to use 0600 perms via syscall stat in test
    - Secret-leak grep against current daemon source returns zero hits (peer.PrivateKey, token.Plaintext, etc)
    - Findings written to `specs/005-observability/reviews/security-audit.md`
  - **LOC**: ~150
  - **Maps to**: FR-005, FR-012, FR-021, plan.md security-relevant decisions, contracts/sse-protocol.md auth section

---

## Dependency Graph

```
Phase 1 (Foundation)
  TASK-1.1 ─────┐
  TASK-1.2 ─────┤
  TASK-1.3 ─────┼──→ TASK-1.5 ──┬──→ Phase 2 ──→ TASK-2.1 ──→ TASK-2.3
  TASK-1.4 ─────┘               │                TASK-2.2 ──┘
                                │
                                ├──→ Phase 3 ──→ TASK-3.1 ──→ TASK-3.2
                                │                TASK-3.3
                                │
                                ├──→ Phase 4 ──→ TASK-4.1 ──→ TASK-4.2 ──→ TASK-4.3 ──→ TASK-4.4
                                │
                                ├──→ Phase 5 ──→ TASK-5.1 ──→ TASK-5.2 ──→ TASK-5.3 ──→ TASK-5.4
                                │
                                └──→ Phase 6 ──→ TASK-6.1 ──→ TASK-6.2
                                                  TASK-6.3 (after P1)
                                                  TASK-6.4 (after P2-P4)
                                                  TASK-6.5 (after P6.3-P6.4)
```

### Mermaid DAG

```mermaid
graph TD
    subgraph P1["Phase 1: Foundation"]
        T11[1.1 slog handler] --- T12[1.2 ring buffer]
        T13[1.3 redaction] --- T14[1.4 level filtering]
        T11 & T12 & T13 & T14 --> T15[1.5 config + init]
    end

    subgraph P2["Phase 2: File Output"]
        T15 --> T21[2.1 lumberjack]
        T21 --> T22[2.2 retention]
        T21 & T22 --> T23[2.3 disk-full]
    end

    subgraph P3["Phase 3: Container Aggregation"]
        T15 --> T31[3.1 container aggregator]
        T31 --> T32[3.2 init wiring]
        T15 --> T33[3.3 file aggregator]
    end

    subgraph P4["Phase 4: SSE Stream"]
        T12 --> T41[4.1 hub]
        T41 --> T42[4.2 subscriber]
        T42 --> T43[4.3 SSE handler]
        T43 & T15 --> T44[4.4 route wiring]
    end

    subgraph P5["Phase 5: Metrics"]
        T51[5.1 registry] --> T52[5.2 exposer]
        T52 --> T53[5.3 instrumentation]
        T53 & T15 --> T54[5.4 init wiring]
    end

    subgraph P6["Phase 6: Export + Testing"]
        T21 --> T61[6.1 export handler]
        T61 --> T62[6.2 route wiring]
        T11 & T12 & T13 & T14 --> T63[6.3 unit tests]
        T43 & T21 & T31 & T61 --> T64[6.4 integration tests]
        T63 & T64 --> T65[6.5 perf benchmarks]
        T23 & T33 & T44 & T53 & T62 --> TSEC1[SEC1 security audit]
    end```
```

### Critical Path

**Length**: 8 tasks

```
TASK-1.1 → TASK-1.5 → TASK-2.1 → TASK-6.1 → TASK-6.2 (export chain, 5 tasks)
```

Actually longest chain:
```
TASK-1.1 → TASK-1.5 → TASK-2.1 → TASK-2.3 (4 tasks, disk-full)
TASK-1.2 → TASK-4.1 → TASK-4.2 → TASK-4.3 → TASK-4.4 → TASK-6.4 → TASK-6.5 (7 tasks, SSE chain)
TASK-5.1 → TASK-5.2 → TASK-5.3 → TASK-5.4 (4 tasks, metrics chain)
```

**Longest chain (critical path)**: TASK-1.2 → TASK-1.5 → TASK-4.1 → TASK-4.2 → TASK-4.3 → TASK-4.4 → TASK-6.4 → TASK-6.5 = **8 tasks**. SSE pipeline is the critical path because it has the most sequential steps: ring buffer → hub → subscriber → handler → wiring → integration test → perf bench.

### Parallel Lanes

| Lane | Tasks | Can run parallel with |
|------|-------|-----------------------|
| P1 foundation | TASK-1.1, 1.2, 1.3, 1.4 (all parallel) | Each other |
| P2 rotation | TASK-2.1, 2.2 | P3, P4 (after P1) |
| P3 containers | TASK-3.1, 3.3 (parallel) | P2, P4 |
| P4 SSE | TASK-4.1→4.4 (sequential) | P2, P3, P5 |
| P5 metrics | TASK-5.1→5.4 (sequential) | P2, P3, P4 |
| P6 export | TASK-6.1→6.2 | P6.3 (unit tests can run early) |

**Max parallelism**: After P1, Phases 2/3/4/5 can all proceed in parallel. P5 (metrics) has no dependency on P2/P3/P4 — can start immediately after P1. P6.3 (unit tests) can start as soon as P1 is done.

---

## Coverage Validation

### FRs Covered: 22/22

| FR | Task(s) |
|----|---------|
| FR-001 (JSONL schema) | TASK-1.1 |
| FR-002 (log levels + per-component) | TASK-1.4 |
| FR-003 (rotation size + date) | TASK-2.1 |
| FR-004 (retention cleanup) | TASK-2.2 |
| FR-005 (secret redaction) | TASK-1.3 |
| FR-006 (SSE endpoint) | TASK-4.3, TASK-4.4 |
| FR-007 (SSE filters) | TASK-4.2 |
| FR-008 (SSE event format + keepalive) | TASK-4.1, TASK-4.2 |
| FR-009 (10 subscribers + backpressure) | TASK-4.1, TASK-4.2 |
| FR-010 (Prometheus endpoint) | TASK-5.2, TASK-5.4 |
| FR-011 (metrics bind address) | TASK-5.2 |
| FR-012 (non-loopback warn + bearer) | TASK-5.2 |
| FR-013 (metric catalog) | TASK-5.1, TASK-5.3 |
| FR-014 (admin UI log viewer backend) | TASK-4.4 (dual-listener), TASK-4.3 (initial load) |
| FR-015 (UI controls — backend support) | TASK-4.2 (server-side filters) |
| FR-016 (initial 200 lines) | TASK-1.2, TASK-4.3 |
| FR-017 (container log capture) | TASK-3.1, TASK-3.2 |
| FR-018 (container stop event) | TASK-3.1 |
| FR-019 (missing container) | TASK-3.1 |
| FR-020 (log export endpoint) | TASK-6.1, TASK-6.2 |
| FR-021 (PII scrub) | TASK-6.1 |
| FR-022 (hot-reload) | TASK-1.4, TASK-3.2, TASK-5.4 |

### Components from Plan: 8/8

| Component | Task(s) |
|-----------|---------|
| 1. StructuredLogger | TASK-1.1, 1.3, 1.4 |
| 2. LogRotator (lumberjack) | TASK-2.1, 2.2, 2.3 |
| 3. ContainerLogAggregator | TASK-3.1, 3.2 |
| 4. LogStream (ring + hub) | TASK-1.2, 4.1, 4.2, 4.3, 4.4 |
| 5. LogExporter | TASK-6.1, 6.2 |
| 6. MetricsRegistry | TASK-5.1, 5.3 |
| 7. MetricsExposer | TASK-5.2, 5.4 |
| 8. Observability init + config | TASK-1.5 |

### Entities: 6/6

| Entity | Task(s) |
|--------|---------|
| LogRecord | TASK-1.1 (schema), TASK-1.2 (ring storage) |
| LogFile | TASK-2.1 (rotation lifecycle) |
| LogStreamSubscriber | TASK-4.2, TASK-4.3 |
| MetricSnapshot | TASK-5.1 (registry gather) |
| MetricSeries | TASK-5.1 (all 10 metrics) |
| LogExportBundle | TASK-6.1 |

### Endpoints: 3/3

| Endpoint | Task(s) |
|----------|---------|
| SSE: `GET /v1/logs/stream` | TASK-4.3, TASK-4.4 |
| Metrics: `GET /metrics` | TASK-5.2 |
| Export: `GET /v1/logs/export` | TASK-6.1, TASK-6.2 |

### Beyond-Spec Decisions: 7/7

| Decision | Reflected In |
|----------|-------------|
| slog over zerolog | TASK-1.1 (stdlib slog handler) |
| Custom SSE over r3labs/sse | TASK-4.1, TASK-4.2 (hand-rolled SSE) |
| Docker SDK over `docker logs -f` CLI | TASK-3.1 (Docker Engine API) |
| Lifecycle audit read-only tail | TASK-3.3 (FileAggregator) |
| Separate metrics listener | TASK-5.2 (own net/http.Server) |
| Parallel stdout during migration | TASK-1.5 (logToStdout config) |
| Lock-free ring via atomic index | TASK-1.2 (atomic store, zero alloc) |

### Open Risks Mitigated: 8/8

| Risk | Mitigation Task |
|------|----------------|
| 1. SSE backpressure — slow consumers | TASK-4.1 (bounded buffer + overflow disconnect) |
| 2. Log file disk-full | TASK-2.3 (graceful degradation cascade) |
| 3. PII leak via misconfigured scrub | TASK-6.1 (X-Unet-PII-Scrubbed header) |
| 4. Container log capture lag | TASK-3.1 (daemon ingestion timestamp, opaque parsing) |
| 5. slog handler mutex contention | TASK-1.2 (lock-free ring), TASK-6.5 (perf benchmark) |
| 6. Prometheus binary size | TASK-5.4 (conditional start, can skip if disabled) |
| 7. Hot-reload race conditions | TASK-1.4 (atomic.Value for level config) |
| 8. Docker SDK socket access | TASK-3.2 (graceful skip if Docker unavailable) |

---

## Task Summary

### By Phase

| Phase | Tasks | Est LOC |
|-------|-------|---------|
| 1. Foundation | 5 (TASK-1.1 → 1.5) | ~800 |
| 2. File Output + Rotation | 3 (TASK-2.1 → 2.3) | ~450 |
| 3. Container Aggregation | 3 (TASK-3.1 → 3.3) | ~580 |
| 4. SSE Log Stream | 4 (TASK-4.1 → 4.4) | ~680 |
| 5. Prometheus Metrics | 4 (TASK-5.1 → 5.4) | ~810 |
| 6. Export + Testing | 6 (TASK-6.1 → 6.5, TASK-SEC1) | ~1580 |
| **Total** | **25** | **~4900** |

### By Agent

| Agent | Tasks |
|-------|-------|
| backend-specialist | 20 (TASK-1.1→1.5, 2.1→2.3, 3.1→3.3, 4.1→4.4, 5.1→5.4, 6.1→6.2) |
| test-engineer | 3 (TASK-6.3, 6.4, 6.5) |
| security-auditor | 1 (TASK-SEC1: holistic security review post-implementation) |

### Parallel-Eligible

- Phase 1: TASK-1.1, 1.2, 1.3, 1.4 (4 parallel)
- Phase 2 vs 3 vs 4 vs 5: all phases can run in parallel after Phase 1
- Phase 3: TASK-3.1, 3.3 (2 parallel)
- Phase 6: TASK-6.3 can start after Phase 1; TASK-6.1 can start after Phase 2

### Cycles: no
### Deepest chain: 8 levels (SSE pipeline: 1.2 → 1.5 → 4.1 → 4.2 → 4.3 → 4.4 → 6.4 → 6.5)
