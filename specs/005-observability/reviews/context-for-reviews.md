# SpecKit Review Context: 005-observability

This document contains the complete context for a critical review of the **Observability — Logging, Metrics & Log UI** feature for the **unet** project.

**Feature Slug**: 005-observability
**Context gathered at**: 2026-05-30
**Repository Constitution Version**: 1.4.0

---

## 1. PROJECT CONSTITUTION (.specify/memory/constitution.md)
Governs principles, quality gates, and cross-AI review rules. (See Principles I, VI, VII).

---

## 2. GLOBAL ARCHITECTURE (specs/main/architecture.md)
Shows how the Observability Layer fits into the unet ecosystem (as a cross-cutting subsystem).

---

## 3. FEATURE SPECIFICATION (specs/005-observability/spec.md)
The "What" and "Why".

```markdown
# Feature Specification: Observability

## Resolved Decisions
- **Metrics format**: Prometheus text exposition.
- **Log storage**: Structured JSONL + size/date rotation via lumberjack.
- **Streaming**: SSE-only (no WebSocket for v0.1).
- **Aggregation**: Capture container logs (Caddy, AmneziaWG) by default.
- **Auth**: Metrics loopback-only default; external bind requires bearer token.

## User Scenarios
1. Live Log Tail in Admin UI (P1) - Real-time feedback with pause/resume.
2. Structured Log Files (P1) - Persistent, queryable, rotated storage.
3. External Tool Stream (P2) - API token authenticated SSE subscription.
4. Prometheus Metrics (P2) - Bandwidth, peers, routes, error counters.
5. Log Export (P3) - Tarball with optional PII scrubbing.
6. Container Log Aggregation (P3) - Unified view of daemon + infra logs.

## Requirements (Highlights)
- **FR-001**: JSONL schema (ts, level, component, source, msg, fields, seq).
- **FR-005**: Global secret redaction pipeline (passwords, keys -> <redacted>).
- **FR-006**: SSE endpoint at /v1/logs/stream with server-side filters.
- **FR-009**: Subscriber limit (10) and backpressure (1k event buffer).
- **FR-013**: Metric catalog (bandwidth, Connected peers, tunnel info).
- **FR-017**: Docker Engine API capture for unet-amnezia-awg and unet-caddy.
- **FR-021**: PII scrubbing during export (mask IPs, anonymize peer names).
```

---

## 4. DATA MODEL (specs/005-observability/data-model.md)
Schema and persistence details.

```markdown
# Data Model

### LogRecord
- Durable in `daemon-YYYY-MM-DD.jsonl`.
- Ephemeral in 200-entry in-memory ring buffer.

### LogStreamSubscriber
- 1000-event per-client buffer.
- Disconnect on overflow.

### MetricSeries
- Standard Prometheus counters, gauges, histograms.
```

---

## 5. IMPLEMENTATION PLAN (specs/005-observability/plan.md)
The "How".

```markdown
# Implementation Plan

- **Language**: Go (log/slog stdlib).
- **Libs**: lumberjack.v2, prometheus/client_golang, docker/client.
- **Approach**:
  - `internal/logger`: Custom slog.Handler with dual-write.
  - `internal/logstream`: Lock-free ring buffer + SSE fan-out hub.
  - `internal/metrics`: Separate net/http.Server for /metrics.
  - `internal/loguicapi`: Container log aggregator + export tarballer.
```

---

## 6. TASKS (specs/005-observability/tasks.md)
The "When".

```markdown
# Tasks (6 Phases)
- Phase 1: Foundation (slog handler, ring buffer, redaction).
- Phase 2: File Output & Rotation (lumberjack integration).
- Phase 3: Container Aggregation (Docker SDK).
- Phase 4: SSE Log Stream (Handler, Filters, Reconnect).
- Phase 5: Prometheus Metrics (Registry, Exposer, Instrumentation).
- Phase 6: Export & Testing (Tarballer, Benchmarks, Security Audit).
```

---

## 7. CONTRACTS & PROTOCOLS (specs/005-observability/contracts/)

### Log Record Schema (log-record.schema.json)
- JSON Schema for the unified log record format.

### SSE Protocol (sse-protocol.md)
- `GET /v1/logs/stream` parameters: `level`, `component`, `source`, `q`.
- Event types: `log`, `overflow`.
- Last-Event-ID reconnection logic.

### Prometheus Metrics (metrics.md)
- Catalog of all counters, gauges, and histograms.
- Bind and auth logic for non-loopback scraping.

### Admin UI Log Tail (log-ui-events.md)
- Backend expectations for the React log viewer component.

---

## 8. CROSS-ARTIFACT ANALYSIS
(To be performed by /speckit.analyze)

---

**ACTION FOR REVIEWER**: 
Perform a critical adversarial review using the lenses defined in `.agent/workflows/speckit.review.md`:
- **Logical consistency**: (e.g. Does secret redaction apply correctly to aggregated container logs?)
- **Hidden assumptions**: (e.g. Does using `log/slog` for container output introduce double-timestamping confusion?)
- **Missing edge cases**: (e.g. Behavior when Docker daemon is unresponsive during log capture spawn.)
- **Failure modes**: (e.g. Impact of log file rotation during a massive log storm.)
- **Security & privacy threats**: (e.g. Metrics endpoint scraping via DNS rebinding if unauthenticated.)
- **Performance & scale**: (e.g. CPU overhead of 1k events/sec fan-out to 10 SSE subscribers.)
- **Alternative approaches**: (e.g. Why custom SSE instead of WebSocket?)
- **Constitution alignment**.

Write your report to `specs/005-observability/reviews/<provider>.md`.
