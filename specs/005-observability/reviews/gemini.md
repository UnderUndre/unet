# SpecKit Review: 005-observability

**Reviewer**: gemini
**Reviewed at**: 2026-05-31T00:00:00Z
**Commit**: HEAD
**Artifacts reviewed**: spec.md, plan.md, tasks.md, data-model.md, quickstart.md

## Summary

The observability architecture provides a solid, unified structured logging foundation with minimal external dependencies. However, there are significant gaps in the container lifecycle tracking, a critical security flaw in the standalone metrics listener, and a severe performance bottleneck in the synchronous log export handler.

## Findings

| ID | Severity | Area | Finding | Recommendation |
|---|---|---|---|---|
| F1 | CRITICAL | Security | `FR-011` and `TASK-5.2` configure the Prometheus metrics endpoint on a separate `:9090` listener with a static Bearer token for auth when bound to a non-loopback address. There is no mention of TLS for this listener. Sending a static Bearer token over unencrypted HTTP exposes it to network sniffing. | The metrics endpoint must either integrate with the existing TLS-enabled control plane `:8443` listener, or explicitly support TLS certificate configuration for the `:9090` listener when bound to non-loopback. |
| F2 | HIGH | Edge case | `spec.md` states "If the container restarts, a new goroutine is spawned on next container-appears detection cycle." However, `plan.md` and `tasks.md` (TASK-3.1, TASK-3.2) only start the `ContainerAggregator` once at daemon init. There is no periodic polling or Docker `Events` API listener to detect restarts. Once a container exits, its logs are lost until the unet daemon restarts. | Implement a Docker `Events` API listener in `ContainerAggregator` to detect `start` events for managed containers and respawn the `ContainerLogs` goroutine, or use a periodic reconciliation loop. |
| F3 | HIGH | Performance | `TASK-6.1` allows exporting up to 30 days of logs synchronously in an HTTP handler. At 100MB/day, this is up to 3GB of data. Reading, regex-scrubbing (if PII scrub enabled), and gzip-compressing this into a temp file will cause CPU spikes, massive I/O, and likely trigger HTTP timeouts before the file finishes generating. | Impose a smaller date range limit (e.g., max 3 days or 500MB), stream the tarball generation directly to the HTTP response writer to avoid temp file double I/O, or implement the export as an async job. |
| F4 | MEDIUM | Failure modes | `TASK-2.3` states that on write error (disk full), the handler will "attempt to delete oldest archive files". Doing this synchronously inside the `slog.Handler` (which may be holding a mutex) will block all logging goroutines across the daemon while `os.Remove` operations complete, causing cascading latency. | Dispatch the disk-full cleanup to an asynchronous background goroutine and immediately degrade to dropping logs or issuing an SSE-alert without blocking the caller. |
| F5 | MEDIUM | Logical consistency | `TASK-3.1` states that `stdcopy.StdCopy` splits stdout/stderr and both go to the same slog pipeline as `slog.Info`. If container stderr is emitted as `info`, critical container errors (e.g., Caddy panics) will not trigger the error-level metrics increment (`TASK-5.1`) or appear in `level=error` filters in the UI. | Map the Docker stderr stream to `slog.Error` during re-emission, and assign a default `error_class` (e.g., `container_error`) so metrics are accurately incremented. |

## Alternative approaches considered

Instead of a custom `:9090` HTTP server for Prometheus metrics, mounting the `/metrics` endpoint on the existing `:8443` control plane mux under a special path (e.g., `/api/v1/metrics`) would inherit the existing PAT/JWT auth, TLS encryption, and eliminate the need for a separate `observability.metrics.bearerToken` and the associated unencrypted HTTP security risks.

## VERDICT

```yaml
verdict: CRITICAL
reviewer: gemini
reviewed_at: 2026-05-31T00:00:00Z
commit: HEAD
critical_count: 1
high_count: 2
medium_count: 2
low_count: 0
```
