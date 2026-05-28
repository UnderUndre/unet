# Feature Specification: Observability — Logging, Metrics & Log UI

**Feature Branch**: `specs/005-observability`
**Created**: 2026-05-27
**Status**: Draft
**Input**: Structured logging + log streaming API + optional metrics endpoint + admin-UI log viewer. Current state: Go default logger + container stdout — not unified, not queryable, no live tail, no metrics. Production debugging is blind.

## Clarifications

### Session 2026-05-27

- Q: Metrics exposition format? → **Decision: Prometheus text exposition** — De facto standard; covers Prometheus/VictoriaMetrics/Grafana Agent/OTel scrape; minimal code.
- Q: Log retention policy granularity? → A: [NEEDS CLARIFICATION: single global retention (default 30d), or per-component (e.g., keep tunnel logs 90d but container logs 7d)? Recommendation: single global default, configurable via `~/.unet/config.json` `observability.retentionDays`. Per-component is over-engineering for v0.1.]
- Q: Container log aggregation — default on or opt-in? → **Decision: Default ON, toggleable off in config** — Debug-experience materially better out-of-box; disk usage manageable with rotation defaults.
- Q: PII scrubbing default? → **Decision: Default OFF (full logs preserved locally)** — Self-hosted OSS means user owns their logs — no third-party processor; masking can be enabled per-export or per-stream.
- Q: SSE-only or also WebSocket? → A: [NEEDS CLARIFICATION: SSE-only for log streaming, or add WebSocket as alternative? Recommendation: SSE-only for v0.1. SSE is simpler, works over standard HTTP, native browser support via EventSource. WebSocket adds complexity (connection upgrade, frame parsing, ping/pong) with no benefit for one-way log streaming. If future needs arise (bidirectional control), WebSocket can be added in a later spec.]
- Q: Metrics endpoint auth? → **Decision: Loopback-only bind by default; external bind requires API token with metrics scope** — Secure by default; matches Prometheus scrape conventions when externally exposed.

### Session 2026-05-27 (round 1)

| Topic | Decision |
|---|---|
| Metrics exposition format | Prometheus text exposition |
| Container logs (Caddy, AmneziaWG) inclusion default | Default ON, toggleable off in config |
| PII scrubbing default | Default OFF (full logs preserved locally) |
| Metrics endpoint auth | Loopback-only bind by default; external bind requires API token with metrics scope |

See inline notes in each FR / section for full rationale.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Live Log Tail in Admin UI (Priority: P1)

As an operator, I open the admin UI and see a live log tail that streams daemon events in real time, filterable by level and component, with pause/resume control, so that I can debug issues without SSH or log file inspection.

**Why this priority**: The admin UI already exists (spec 001-init). Adding a log viewer is the highest-impact observability improvement — most operators will never touch log files or API endpoints directly. This is the primary human-facing interface for observability.

**Independent Test**: Start the daemon. Open the admin UI. Navigate to the Logs view. Perform an action (e.g., connect tunnel). Verify the log line appears within 2 seconds. Filter by level=error. Pause. Perform another action. Verify no new lines appear. Resume. Verify buffered lines appear.

**Acceptance Scenarios**:

1. **Given** the daemon is running, **When** the operator opens the admin UI Logs view, **Then** a live-updating log stream is displayed showing the most recent 200 lines, with new lines appended at the bottom as they occur
2. **Given** the live log view is active, **When** the operator selects a level filter (e.g., "error"), **Then** only log lines with level ≥ error are shown
3. **Given** the live log view is active, **When** the operator selects a component filter (e.g., "tunnel"), **Then** only log lines with `component: "tunnel"` are shown
4. **Given** the live log view is active, **When** the operator types a free-text search query, **Then** only log lines containing the query string (case-insensitive) are shown
5. **Given** the live log view is active, **When** the operator clicks "Pause", **Then** new log lines are buffered client-side but not rendered; clicking "Resume" renders all buffered lines

---

### User Story 2 - Structured Log Files with Rotation (Priority: P1)

As the daemon, I write structured JSONL log files to `~/.unet/logs/` with automatic rotation by size and date, so that logs are persistently stored, queryable with standard tools (`jq`, `grep`), and don't consume unbounded disk space.

**Why this priority**: Without structured persistent logs, all observability is ephemeral. File-based logging is the foundation — the SSE endpoint, the admin UI, and future log shipping all consume from the same structured stream. Must be solid first.

**Independent Test**: Start the daemon. Perform various actions. Check `~/.unet/logs/daemon-YYYY-MM-DD.jsonl`. Verify each line is valid JSONL with required fields (`ts`, `level`, `component`, `msg`). Generate >100MB of logs (or lower threshold for test). Verify rotation creates a new file.

**Acceptance Scenarios**:

1. **Given** the daemon starts, **When** any event is logged, **Then** a line is appended to `~/.unet/logs/daemon-YYYY-MM-DD.jsonl` with fields: `ts` (ISO-8601 UTC), `level` (debug|info|warn|error), `component` (string), `msg` (string), `fields` (optional object)
2. **Given** the active log file exceeds 100MB, **When** the next log line is written, **Then** the current file is rotated (renamed to `daemon-YYYY-MM-DD.N.jsonl`), and a new file is created atomically
3. **Given** log files older than the configured retention period (default 30 days), **When** rotation runs, **Then** expired archive files are deleted
4. **Given** the daemon is logging at default verbosity (info), **When** a secret field (private key, API token, SSH password) is included in a log context, **Then** the value is replaced with `<redacted>` in the log output

---

### User Story 3 - External Tool Subscribes to Log Stream (Priority: P2)

As an external monitoring tool, I authenticate with an API token and subscribe to the SSE log-streaming endpoint, so that I can integrate unet logs into my existing monitoring stack in real time.

**Why this priority**: Enables programmatic observability without the admin UI. Required for integration with monitoring dashboards, alerting systems, and future log shipping. P2 because the admin UI (P1) provides the immediate human-facing value.

**Independent Test**: Generate an API token with `read` scope. Connect to `GET /api/v1/logs/stream` with `Authorization: Bearer *** Trigger a daemon event. Verify the SSE event arrives within 1 second.

**Acceptance Scenarios**:

1. **Given** a valid API token with `read` scope, **When** the client connects to `GET /api/v1/logs/stream`, **Then** an SSE connection is established with `Content-Type: text/event-stream`
2. **Given** an active SSE connection, **When** the daemon logs an event, **Then** an SSE event is sent to the subscriber within 1 second with the event data as a JSONL-formatted string
3. **Given** a valid API token with `read` scope, **When** the client connects to `GET /api/v1/logs/stream?level=error`, **Then** only events with level ≥ error are sent
4. **Given** a valid API token with `read` scope, **When** the client connects to `GET /api/v1/logs/stream?component=tunnel`, **Then** only events with `component: "tunnel"` are sent
5. **Given** an invalid or missing API token, **When** the client attempts to connect to the SSE endpoint, **Then** the connection is rejected with 401

---

### User Story 4 - Prometheus Metrics Endpoint (Priority: P2)

As an operator, I enable the optional Prometheus metrics endpoint and configure my Prometheus scraper to collect unet metrics, so that I can visualize bandwidth, peer count, error rate, and route count in Grafana or similar dashboards.

**Why this priority**: Metrics complement logs — they answer "how much" and "how fast" where logs answer "what happened." Optional because not all self-hosted setups run Prometheus, but for those that do, it's essential. P2 because logs are the higher-priority foundation.

**Independent Test**: Enable metrics in config. Start the daemon with an active tunnel. `curl http://127.0.0.1:<metricsPort>/metrics`. Verify response is valid Prometheus text format with at least: `unet_bandwidth_bytes_total`, `unet_peers_connected`, `unet_routes_active`, `unet_errors_total`, `unet_uptime_seconds`.

**Acceptance Scenarios**:

1. **Given** the metrics endpoint is enabled in config, **When** a Prometheus scraper hits `GET /metrics`, **Then** the response is valid Prometheus text exposition format (`text/plain; version=0.0.4`) with standard unet metrics
2. **Given** the metrics endpoint is NOT enabled (default), **When** any request hits `GET /metrics`, **Then** the response is 404
3. **Given** an active tunnel with 3 peers and 2 routes, **When** the metrics endpoint is scraped, **Then** `unet_peers_connected` reports 3 and `unet_routes_active` reports 2
4. **Given** the metrics endpoint is bound to `0.0.0.0:9090` (non-loopback), **When** an unauthenticated request arrives, **Then** metrics are still served (no auth on metrics endpoint) — [NEEDS CLARIFICATION: confirm this is acceptable security posture for non-loopback bind]

---

### User Story 5 - Log Export for Support (Priority: P3)

As a user, I export the last 24 hours of logs as a tarball via the API, so that I can attach it to a bug report or support request without manually finding and zipping log files.

**Why this priority**: Convenience feature. Low complexity (read files, tar, serve). P3 because operators can always manually zip the log directory. But having a clean API endpoint is nice-to-have for support workflows.

**Independent Test**: Authenticate. Call `GET /api/v1/logs/export?from=2026-05-26T00:00:00Z&to=2026-05-27T00:00:00Z`. Verify the response is a tarball containing the relevant JSONL files.

**Acceptance Scenarios**:

1. **Given** log files exist for the requested date range, **When** the client calls `GET /api/v1/logs/export?from=<ISO>&to=<ISO>`, **Then** the response is a `.tar.gz` file containing all relevant JSONL files with status 200
2. **Given** `observability.scrubPii` is enabled in config, **When** the export is generated, **Then** client IPs are masked to `***.***.***.<last-octet>` and peer names are replaced with `peer-<id>` in the exported JSONL content
3. **Given** no log files exist for the requested range, **When** the client calls the export endpoint, **Then** the response is 404 with `error: "no_logs_in_range"`

---

### User Story 6 - Container Log Aggregation (Priority: P3)

As an operator, I enable container log capture and see Caddy and AmneziaWG container logs in the unified admin UI log stream, distinguished by `source: "container"` and `container: "<name>"` fields, so that I can debug reverse-proxy and tunnel issues without running `docker logs` separately.

**Why this priority**: Aggregating container logs into the unified stream is valuable for correlation (e.g., Caddy 502 + AmneziaWG handshake failure in one view). P3 because it adds complexity (goroutine per container, log parsing, container lifecycle tracking) and operators can already run `docker logs` manually.

**Independent Test**: Enable container log capture in config. Restart daemon. Open admin UI log view. Trigger a Caddy route change. Verify a log line appears with `source: "container"`, `container: "unet-caddy"`.

**Acceptance Scenarios**:

1. **Given** container log capture is enabled, **When** the daemon starts, **Then** it spawns a `docker logs -f <container>` goroutine for each managed container (unet-amnezia-awg, unet-caddy)
2. **Given** an active container log capture goroutine, **When** the container outputs a log line, **Then** the line is re-emitted as a structured log event with `source: "container"`, `container: "<name>"`, and the raw container output in `msg`
3. **Given** an active container log capture, **When** the container stops (crash or deliberate), **Then** a log event is emitted with `level: "warn"`, `msg: "container log capture stopped"`, `container: "<name>"`, `exitCode: <code>` — and the goroutine exits cleanly
4. **Given** container log capture is enabled for a container that does not exist, **When** the daemon starts, **Then** a warn-level log is emitted and the capture goroutine is NOT spawned (no retry loop)

---

### Edge Cases

- **Log writer disk full**: When the log directory's filesystem reaches capacity, the logger MUST degrade gracefully: (1) delete the oldest archive files beyond retention, (2) if still full, drop `debug`-level lines silently, (3) if still full, drop `info`-level lines, (4) if still full, stop writing to file and emit a single `error`-level alert to SSE subscribers (not to file — chicken-and-egg). Resume writing when space becomes available. [NEEDS CLARIFICATION: should the daemon exit on total log failure, or continue without logging? Recommendation: continue operating — tunnel and routing are more important than logging.]
- **SSE subscriber slow consumer (backpressure)**: Each SSE subscriber has a bounded per-client buffer (default 1000 events). When the buffer is full, the server disconnects the subscriber with a final SSE event `event: overflow` containing `{ "reason": "client_buffer_exceeded" }`. The client must reconnect. This prevents a slow consumer from consuming unbounded server memory.
- **Container stops mid-stream**: The `docker logs -f` goroutine detects container exit (command exit code), emits a structured log event with `source: "container"`, `event: "container_stopped"`, `exitCode: <code>`, and terminates the goroutine. No retry — the container is a managed resource, not an external dependency. If the container restarts (Docker restart policy), a new goroutine is spawned on next container-appears detection cycle.
- **Metrics endpoint enabled on non-loopback without auth**: Security implication documented clearly in config comments and startup warning log. The metrics endpoint exposes no secrets (no keys, no IPs in plain — only aggregate counters). **Resolved**: Exposing aggregate counters without auth is acceptable for loopback bind. Non-loopback bind requires `observability.metrics.bearerToken` (resolved 2026-05-27 round 1).
- **Log rotation race with active writer**: Use atomic rename pattern: create new file, redirect writes via file handle swap under mutex. The writer holds a mutex during the swap. The rotated file is sealed and never written to again. No log line is lost — the line that triggers rotation is written to the new file.
- **Clock skew between daemon and containers**: Container log lines carry the container's timestamp in `msg` (raw Docker log output). The daemon wraps them with its own `ts` field (daemon clock, UTC). Both timestamps are preserved: `ts` = daemon ingestion time, `container_ts` = original container timestamp if parseable from the log line. Daemon clock is source of truth for all structured fields.
- **Concurrent SSE connect/disconnect storm**: If >10 subscribers connect/disconnect rapidly, the server MUST handle connection cleanup without goroutine leaks. Use `sync.WaitGroup` + context cancellation per subscriber. Subscriber goroutine MUST terminate within 5s of context cancellation.
- **Log export with PII scrubbing and rotation overlap**: If rotation occurs during export generation, the export MUST be a consistent snapshot (no partial files). Implement by taking a snapshot of the current file list at export start, reading sealed archives + the active file up to the snapshot offset. New lines after snapshot start are excluded from the export.

## Requirements *(mandatory)*

### Functional Requirements

**Structured Logging Core (P1)**:

- **FR-001**: The daemon MUST emit all log output as structured JSONL to `~/.unet/logs/daemon-YYYY-MM-DD.jsonl`. Each line MUST conform to the following schema:
  ```
  {
    "ts": "2026-05-27T14:32:01.123Z",     // ISO-8601 UTC, millisecond precision
    "level": "info",                       // debug | info | warn | error
    "component": "tunnel",                 // emitting subsystem
    "msg": "handshake completed",          // human-readable message
    "fields": {                            // optional structured context
      "peer_id": "abc123",
      "route_id": "route-7",
      "request_id": "req-xyz",
      "source": "daemon"                   // "daemon" | "container"
    }
  }
  ```
  Fields map is extensible. `peer_id`, `route_id`, `request_id`, `source` are standardized keys used where applicable. Unknown keys are allowed for component-specific context.

- **FR-002**: The daemon MUST support log levels `debug`, `info`, `warn`, `error` with a configurable global threshold (default: `info`). Additionally, per-component threshold overrides MUST be supported via config: `observability.logLevels: { "tunnel": "debug", "caddy-client": "warn" }`. A component not listed falls back to the global threshold.

- **FR-003**: The daemon MUST perform log rotation based on two triggers: (a) file size exceeding 100MB (configurable via `observability.maxFileSizeMB`), and (b) calendar date change (new file at midnight UTC). Rotation MUST use atomic rename: active file is renamed to `daemon-YYYY-MM-DD.N.jsonl` (N = sequential integer starting at 1), and a new file is created. The rotation operation MUST hold a mutex so no log line is lost during the swap.

- **FR-004**: The daemon MUST enforce log retention by deleting archive files older than the configured retention period (default 30 days, configurable via `observability.retentionDays`). Retention cleanup MUST run once at daemon start and once daily thereafter. Active files (current day, regardless of age) are never deleted.

- **FR-005**: The daemon MUST redact all secret values (SSH passwords, private keys, API tokens, Cloudflare tokens, `uiToken`, mTLS client keys) to `<redacted>` in all log output — file, SSE stream, and admin UI. Redaction MUST apply to the `fields` map and any structured context. This extends FR-011 (spec 001-init) secret-masking to the structured logging pipeline.

**SSE Log Streaming (P2)**:

- **FR-006**: The daemon MUST expose an SSE endpoint at `GET /api/v1/logs/stream` (under the control plane API from spec 002). The endpoint MUST require authentication via the same Bearer token mechanism as the control plane API (spec 002, FR-001). Required scope: `read`.

- **FR-007**: The SSE endpoint MUST support the following query parameters for server-side filtering:
  - `level`: minimum log level threshold (e.g., `?level=warn` → only warn + error)
  - `component`: exact component match (e.g., `?component=tunnel`)
  - `q`: free-text search in `msg` field (case-insensitive substring match)
  Multiple parameters are AND-combined. Invalid parameter values return 400 before SSE upgrade.

- **FR-008**: Each SSE event MUST use the event type `log` with the data payload being a single JSONL line matching the FR-001 schema. Keep-alive comments (`: keep-alive\n\n`) MUST be sent every 15 seconds of inactivity to prevent proxy/connection timeouts.

- **FR-009**: The SSE endpoint MUST support up to 10 concurrent subscribers. Each subscriber has a per-client buffer of 1000 events (configurable via `observability.sseClientBuffer`). When a subscriber's buffer overflows, the server MUST send a final `event: overflow` SSE event and disconnect the client. The client is expected to reconnect with adjusted filters.

**Prometheus Metrics (P2)**:

- **FR-010**: The daemon MUST expose an optional Prometheus-compatible metrics endpoint at `GET /metrics` (off by default, enabled via `observability.metrics.enabled: true`). The endpoint MUST serve the Prometheus text exposition format (`text/plain; version=0.0.4`).

- **FR-011**: The metrics endpoint MUST be bound to a configurable address (default: `127.0.0.1:9090`). The admin MAY change the bind address to `0.0.0.0` for remote scraping, accepting the security implications documented in FR-012.

- **FR-012**: When the metrics endpoint is enabled and bound to a non-loopback address, the daemon MUST emit a `warn`-level log at startup: `"Metrics endpoint bound to non-loopback address — metrics are accessible without authentication. Consider binding to 127.0.0.1 or setting observability.metrics.bearerToken."` **Decision: implement `observability.metrics.bearerToken`** — static Bearer token required for non-loopback bind (resolved Clarifications 2026-05-27 round 1).**FR-013**: The following core metrics MUST be exposed when the metrics endpoint is enabled:
  - `unet_bandwidth_bytes_total{direction="in|out"}` — cumulative bytes transferred through the tunnel (from `awg show` transfer stats)
  - `unet_peers_connected` — number of peers with recent handshake (< 3 × PersistentKeepalive)
  - `unet_routes_active` — number of active Caddy ingress routes
  - `unet_errors_total{class="<error_class>"}` — cumulative error count by category (tunnel, caddy, dns, ssh, config)
  - `unet_uptime_seconds` — daemon uptime in seconds
  - `unet_tunnel_info{status="connected|disconnected|connecting|error"}` — gauge with value 1 for current status, 0 for others

**Admin UI Log Viewer (P1)**:

- **FR-014**: The admin UI MUST include a Logs view accessible from the main navigation. The view MUST display a live-updating log tail using the SSE endpoint (FR-006, but authenticated via the local daemon session — no separate token needed for localhost access).

- **FR-015**: The admin UI log viewer MUST support the following controls:
  - Level filter dropdown (all / debug / info / warn / error)
  - Component filter dropdown (populated dynamically from observed components)
  - Free-text search input (substring match, case-insensitive)
  - Pause/Resume button (pauses rendering, buffers up to 500 lines client-side)
  - Auto-scroll toggle (default: on — new lines scroll into view; off — user is reading history)
  - Clear button (clears displayed lines, not the file)

- **FR-016**: The admin UI log viewer MUST display the most recent 200 lines on initial load (fetched from the daemon's in-memory ring buffer, not from file). Lines MUST be rendered with: timestamp (rendered in user's local timezone), level (color-coded: debug=gray, info=default, warn=yellow, error=red), component (badge), message, and expandable fields map.

**Container Log Capture (P3)**:

- **FR-017**: When `observability.captureContainerLogs` is `true` (default: `true`), the daemon MUST spawn a goroutine per managed container (unet-amnezia-awg, unet-caddy) that runs `docker logs -f <container>` and re-emits each line as a structured log event with fields: `source: "container"`, `container: "<name>"`, and the raw output in `msg`. The `component` field MUST be set to `container.<name>`.

- **FR-018**: When a container being captured stops (exit code from the `docker logs` command), the daemon MUST emit a structured log event: `{ level: "warn", component: "container-capture", msg: "container log capture stopped", fields: { container: "<name>", exitCode: <code>, source: "container" } }`. The goroutine MUST exit cleanly. No automatic retry — container lifecycle is managed externally.

- **FR-019**: When `observability.captureContainerLogs` is `true` but a target container does not exist at daemon start, the daemon MUST emit `{ level: "warn", component: "container-capture", msg: "container not found, skipping log capture", fields: { container: "<name>" } }` and NOT spawn a goroutine for that container.

**Log Export (P3)**:

- **FR-020**: The daemon MUST expose a log export endpoint at `GET /api/v1/logs/export` (control plane API, requires `read` scope). Parameters: `from` and `to` (ISO-8601 timestamps, URL-encoded). The response is a `.tar.gz` file containing all JSONL log files with lines in the requested date range.

- **FR-021**: When `observability.scrubPii` is `true` (default: `false`), the export endpoint MUST mask client IPs to `***.***.***.<last-octet>` and replace peer names with `peer-<id>` in the exported content. Scrubbing applies ONLY to the exported tarball, NOT to on-disk files or the live SSE stream.

**Runtime Configurability (P2)**:

- **FR-022**: The following observability settings MUST be hot-reloadable without daemon restart (loaded from `~/.unet/config.json` with file-watcher or signal-based reload):
  - `observability.logLevels` (global + per-component thresholds)
  - `observability.retentionDays`
  - `observability.metrics.enabled`
  - `observability.captureContainerLogs`
  - `observability.scrubPii`
  Rotation size threshold (`observability.maxFileSizeMB`) and SSE buffer size (`observability.sseClientBuffer`) require restart — document this limitation.

### Key Entities

- **LogEntry**: A single structured log line. Schema per FR-001. Immutable once written to file. In-memory ring buffer holds the last 200 entries for admin UI initial load.
- **LogArchive**: A rotated log file (`daemon-YYYY-MM-DD.N.jsonl`). Sealed, never modified after rotation. Deleted by retention policy.
- **SSESubscriber**: An active SSE connection. Attributes: subscriber ID, connection time, filter params (level, component, query), per-client buffer (bounded), context cancellation handle.
- **MetricsSnapshot**: A point-in-time collection of all metric values. Refreshed on each `/metrics` scrape from the daemon's internal counters and `awg show` output.

## Assumptions

- **Same logging library**: The daemon uses a single structured logging library (e.g., Go's `log/slog` or `zerolog`) for all output. No mixed `log.Printf` + structured logger calls. The FR-001 schema is enforced at the library level, not by manual formatting.
- **In-memory ring buffer**: The daemon maintains a bounded in-memory ring buffer of the last 200 log entries for fast admin UI initial load. This buffer is NOT persisted — it's rebuilt from the current session's log output. On daemon restart, the admin UI initial load reads the last 200 lines from the active log file.
- **SSE over control plane**: The log streaming endpoint lives under the control plane API path (`/api/v1/`) and shares its authentication (spec 002). The localhost admin UI accesses it without a separate token — the daemon recognizes localhost connections and bypasses Bearer auth for the UI's EventSource connections.
- **Metrics from existing data sources**: Bandwidth metrics come from `awg show` transfer counters (already used by spec 001). Peer count comes from the daemon's peer state. Error counters are accumulated by the structured logger. No new instrumentation points needed beyond hooking into the existing error paths.
- **File-based log storage is sufficient**: For v0.1, logs live on the local filesystem. External shipping (Loki, Elasticsearch, syslog) is explicitly out of scope — a future spec will define a log shipper plugin interface.
- **No structured log parsing for container output**: Container log lines from `docker logs` are treated as opaque strings in `msg`. The daemon does not attempt to parse Caddy or AmneziaWG log formats. If structured container log parsing is needed, it belongs in a future spec.

## Out of Scope (for this spec)

- External log shipping (Loki, Elasticsearch, Fluentd, syslog) — future spec for log shipper plugin interface
- Distributed tracing (OpenTelemetry, Jaeger, Zipkin) — no request tracing across daemon ↔ VPS ↔ Caddy
- Commercial APM integrations (Datadog, New Relic, Honeycomb)
- WebSocket transport for log streaming (SSE-only for v0.1)
- Multi-daemon log aggregation (centralized logging across multiple unet instances)
- Log-based alerting rules (if error rate > X, trigger notification) — belongs in external monitoring
- Structured parsing of container log formats (Caddy JSON logs, AmneziaWG log format)
- Grafana dashboard templates (operator creates their own)

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: End-to-end log line latency from event occurrence to SSE subscriber delivery is < 1 second (P95), measured with a test subscriber on localhost
- **SC-002**: Log file rotation triggers within 5 seconds of the configured size threshold being breached — verified by generating log volume and checking file creation timestamp
- **SC-003**: The SSE endpoint supports 10 concurrent subscribers with each receiving events within 1 second (P95) — no per-subscriber latency degradation as subscriber count increases from 1 to 10
- **SC-004**: Prometheus `/metrics` scrape latency is < 100ms (P95) under normal load (≤50 active peers, ≤20 routes) — measured with `curl -o /dev/null -w '%{time_total}'`
- **SC-005**: Log files consume < 100MB per day at default verbosity (info level) under normal traffic (≤10 tunnel connect/disconnect cycles, ≤50 Caddy route changes, continuous heartbeat logging at debug frequency)
- **SC-006**: Admin UI log viewer displays new log lines within 2 seconds of the event occurring, without page refresh, measured with a manual action (e.g., tunnel connect) and visual confirmation of log line appearance
- **SC-007**: Zero secret values (private keys, tokens, passwords) appear in any log output — verified by grep test over 24h of log data for known secret patterns

## Cross-References

- **Depends on**: `specs/002-api-control-plane/` — SSE endpoint and export endpoint live under the control plane API path (`/api/v1/logs/`), share authentication (Bearer tokens), and follow the same error response format (FR-014 of spec 002)
- **Depends on**: `specs/001-init/` — secret redaction extends FR-011 of spec 001. Log rotation directory (`~/.unet/logs/`) is adjacent to existing `~/.unet/config.json`. Per-component log levels reference the component names defined by the daemon's internal architecture (tunnel, caddy-client, dns, ssh, config)
- **Used by**: `specs/004-desktop-integration/` — network-change events from desktop integration flow into the log stream with `component: "desktop"`
- **Consumed by**: undevops plugin — external API consumer pattern (from undevops plugin-sdk.md §External API Consumer Pattern) subscribes to the SSE endpoint for real-time monitoring