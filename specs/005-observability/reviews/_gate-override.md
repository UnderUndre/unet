# Gate Override: 005-observability

**Date**: 2026-06-01
**Override by**: Undre (repo owner)
**Reason**: Manual review completed. Gemini CRITICAL (F1: metrics TLS) accepted as known limitation — metrics listener defaults to loopback-only; non-loopback + bearer-over-plain-HTTP documented as operator responsibility. F2-F5 will be addressed during implementation.

**Findings disposition**:
- F1 (CRITICAL): Metrics TLS — loopback-only default sufficient for v0.1. Non-loopback bearer-over-HTTP documented in config comments + startup warn. TLS deferred to future spec.
- F2 (HIGH): Docker Events for container restart detection — will implement periodic reconciliation loop in TASK-3.1.
- F3 (HIGH): Sync export 30d limit — will implement streaming response + max 7-day range cap.
- F4 (MEDIUM): Disk-full cleanup blocking — will dispatch async in TASK-2.3.
- F5 (MEDIUM): Container stderr → slog.Error — will map stderr to warn level in TASK-3.1.

**Verdict**: OVERRIDE — proceeding to implementation.
