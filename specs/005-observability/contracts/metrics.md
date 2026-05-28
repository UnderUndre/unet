# Prometheus Metrics Catalog

**Spec**: `specs/005-observability/spec.md`
**Created**: 2026-05-28

---

## Endpoint

```
GET /metrics
```

Separate HTTP listener from the control plane API. Default bind: `127.0.0.1:9090`.

## Exposition Format

Prometheus text exposition format per round 1 clarification:

```
Content-Type: text/plain; version=0.0.4; charset=utf-8
```

No OpenMetrics, no Protobuf — text only.

## Configuration

```json
{
  "observability": {
    "metrics": {
      "enabled": false,
      "listenAddr": "127.0.0.1:9090",
      "bearerToken": ""
    }
  }
}
```

- `enabled`: Default `false`. When `false`, `/metrics` returns 404.
- `listenAddr`: Default `127.0.0.1:9090` (loopback-only per round 1 clarification).
- `bearerToken`: Required when `listenAddr` is non-loopback. Empty = no auth on loopback.

## Authentication

- **Loopback bind** (`127.0.0.1`): No authentication required.
- **Non-loopback bind** (`0.0.0.0`): Requires `Authorization: Bearer <token>` matching `observability.metrics.bearerToken`. Constant-time compare. Missing or invalid → `401 Unauthorized`.
- **Non-loopback without bearer token configured**: Daemon emits `warn` log at startup: `"Metrics endpoint bound to non-loopback address without bearer token. Metrics are accessible without authentication. Set observability.metrics.bearerToken for security."`

## Counters

### `unet_api_requests_total`

Total API requests served by the control plane (spec 002).

```
# TYPE unet_api_requests_total counter
unet_api_requests_total{method="GET",path="/v1/peers",status="200"} 142
unet_api_requests_total{method="POST",path="/v1/peers",status="201"} 3
unet_api_requests_total{method="DELETE",path="/v1/peers/abc-123",status="200"} 1
```

Labels:
- `method`: HTTP method (`GET`, `POST`, `PUT`, `DELETE`)
- `path`: Route path (`/v1/peers`, `/v1/routes`, `/v1/tunnel/status`, `/v1/status`, `/v1/tokens`, `/v1/audit`, `/v1/logs/stream`, `/v1/logs/export`)
- `status`: HTTP status code (`200`, `201`, `400`, `401`, `403`, `404`, `409`, `429`, `500`, `503`)

### `unet_errors_total`

Cumulative error count by category.

```
# TYPE unet_errors_total counter
unet_errors_total{class="tunnel"} 7
unet_errors_total{class="caddy"} 2
unet_errors_total{class="dns"} 1
unet_errors_total{class="ssh"} 0
unet_errors_total{class="config"} 0
```

Labels:
- `class`: Error category (`tunnel`, `caddy`, `dns`, `ssh`, `config`)

Incremented by the structured logger when `level: "error"` and `fields.error_class` is set.

### `unet_peer_handshakes_total`

Cumulative peer handshake events (new connections + re-handshakes).

```
# TYPE unet_peer_handshakes_total counter
unet_peer_handshakes_total 89
```

No labels. Incremented each time `awg show` reports a new latest-handshake for any peer.

### `unet_bandwidth_bytes_total`

Cumulative bytes transferred through the tunnel.

```
# TYPE unet_bandwidth_bytes_total counter
unet_bandwidth_bytes_total{direction="in"} 1048576000
unet_bandwidth_bytes_total{direction="out"} 524288000
```

Labels:
- `direction`: `in` (download) or `out` (upload)

Source: `awg show` transfer counters. Polled periodically (same interval as spec 001 drift check).

## Gauges

### `unet_peers_connected`

Number of peers with recent handshake (< 3 × PersistentKeepalive = 75 seconds).

```
# TYPE unet_peers_connected gauge
unet_peers_connected 3
```

### `unet_routes_active`

Number of active Caddy ingress routes.

```
# TYPE unet_routes_active gauge
unet_routes_active 5
```

### `unet_uptime_seconds`

Daemon uptime in seconds.

```
# TYPE unet_uptime_seconds gauge
unet_uptime_seconds 86400
```

### `unet_tunnel_info`

Tunnel connection status. Gauge with value `1` for current status, `0` for others.

```
# TYPE unet_tunnel_info gauge
unet_tunnel_info{status="connected"} 1
unet_tunnel_info{status="disconnected"} 0
unet_tunnel_info{status="connecting"} 0
unet_tunnel_info{status="error"} 0
```

Labels:
- `status`: `connected`, `disconnected`, `connecting`, `error`

Always exactly one status has value `1`.

## Histograms

### `unet_api_request_duration_seconds`

API request latency distribution.

```
# TYPE unet_api_request_duration_seconds histogram
unet_api_request_duration_seconds_bucket{path="/v1/peers",le="0.005"} 98
unet_api_request_duration_seconds_bucket{path="/v1/peers",le="0.01"} 130
unet_api_request_duration_seconds_bucket{path="/v1/peers",le="0.025"} 140
unet_api_request_duration_seconds_bucket{path="/v1/peers",le="0.05"} 142
unet_api_request_duration_seconds_bucket{path="/v1/peers",le="0.1"} 142
unet_api_request_duration_seconds_bucket{path="/v1/peers",le="0.25"} 142
unet_api_request_duration_seconds_bucket{path="/v1/peers",le="0.5"} 142
unet_api_request_duration_seconds_bucket{path="/v1/peers",le="1"} 142
unet_api_request_duration_seconds_bucket{path="/v1/peers",le="2.5"} 142
unet_api_request_duration_seconds_bucket{path="/v1/peers",le="5"} 142
unet_api_request_duration_seconds_bucket{path="/v1/peers",le="10"} 142
unet_api_request_duration_seconds_bucket{path="/v1/peers",le="+Inf"} 142
unet_api_request_duration_seconds_sum{path="/v1/peers"} 1.234
unet_api_request_duration_seconds_count{path="/v1/peers"} 142
```

Labels:
- `path`: Same path values as `unet_api_requests_total`

Default buckets: `[.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10]` (Prometheus Go client defaults).

### `unet_log_write_duration_seconds`

Log write latency distribution (slog handler → file + ring buffer).

```
# TYPE unet_log_write_duration_seconds histogram
unet_log_write_duration_seconds_bucket{le="0.001"} 9500
unet_log_write_duration_seconds_bucket{le="0.005"} 9990
unet_log_write_duration_seconds_bucket{le="0.01"} 10000
...
```

No labels. Tracks the time from `slog.Handler.Handle()` call to completion of dual-write (file + ring buffer). Used for monitoring logging overhead.

Default buckets: `[.0001, .0005, .001, .005, .01, .025, .05, .1]` (finer granularity since log writes should be sub-millisecond).

## Scrape Example

```bash
$ curl -s http://127.0.0.1:9090/metrics | head -30

# HELP unet_api_requests_total Total API requests served
# TYPE unet_api_requests_total counter
unet_api_requests_total{method="GET",path="/v1/peers",status="200"} 142
unet_api_requests_total{method="POST",path="/v1/peers",status="201"} 3

# HELP unet_errors_total Cumulative errors by category
# TYPE unet_errors_total counter
unet_errors_total{class="tunnel"} 7
unet_errors_total{class="caddy"} 2

# HELP unet_peers_connected Number of connected peers
# TYPE unet_peers_connected gauge
unet_peers_connected 3

# HELP unet_routes_active Number of active ingress routes
# TYPE unet_routes_active gauge
unet_routes_active 5

# HELP unet_uptime_seconds Daemon uptime in seconds
# TYPE unet_uptime_seconds gauge
unet_uptime_seconds 86400

# HELP unet_tunnel_info Tunnel connection status
# TYPE unet_tunnel_info gauge
unet_tunnel_info{status="connected"} 1
unet_tunnel_info{status="disconnected"} 0
```
