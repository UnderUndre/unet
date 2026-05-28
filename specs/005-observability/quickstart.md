# Developer Quickstart: Observability

**Spec**: `specs/005-observability/spec.md`
**Created**: 2026-05-28

---

## Prerequisites

- unet daemon running (spec 001-init)
- Control plane API enabled (spec 002) for SSE and export endpoints
- `curl` for testing
- `jq` for JSON log inspection
- Docker running (for container log capture)

## 1. Verify Structured Logging

Start the daemon. Log files appear in `~/.unet/logs/`:

```bash
# Check log file exists and is valid JSONL
ls -la ~/.unet/logs/
cat ~/.unet/logs/daemon-$(date -u +%Y-%m-%d).jsonl | head -5 | jq .

# Expected output:
# {
#   "ts": "2026-05-28T10:15:30.123Z",
#   "level": "info",
#   "component": "daemon",
#   "source": "daemon",
#   "msg": "daemon started",
#   "seq": 1,
#   "fields": {}
# }
```

### Query logs with jq

```bash
# Error-level logs only
cat ~/.unet/logs/daemon-2026-05-28.jsonl | jq 'select(.level == "error")'

# Logs from tunnel component
cat ~/.unet/logs/daemon-2026-05-28.jsonl | jq 'select(.component == "tunnel")'

# Logs for a specific peer
cat ~/.unet/logs/daemon-2026-05-28.jsonl | jq 'select(.fields.peer_id == "abc-123")'

# Count logs by level
cat ~/.unet/logs/daemon-2026-05-28.jsonl | jq -r '.level' | sort | uniq -c
```

## 2. Enable Prometheus Metrics

Add to `~/.unet/config.json`:

```json
{
  "observability": {
    "metrics": {
      "enabled": true,
      "listenAddr": "127.0.0.1:9090"
    }
  }
}
```

Restart the daemon (or trigger hot-reload if supported). Verify:

```bash
# Check metrics endpoint
curl -s http://127.0.0.1:9090/metrics | head -20

# Verify specific metrics
curl -s http://127.0.0.1:9090/metrics | grep unet_peers_connected
curl -s http://127.0.0.1:9090/metrics | grep unet_uptime_seconds

# Time the scrape (should be < 100ms)
curl -o /dev/null -s -w '%{time_total}\n' http://127.0.0.1:9090/metrics
```

### Prometheus scraper config

Add to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'unet'
    static_configs:
      - targets: ['127.0.0.1:9090']
    scrape_interval: 15s
```

For remote scraping (non-loopback):

```json
{
  "observability": {
    "metrics": {
      "enabled": true,
      "listenAddr": "0.0.0.0:9090",
      "bearerToken": "your-secret-token-here"
    }
  }
}
```

```yaml
scrape_configs:
  - job_name: 'unet'
    static_configs:
      - targets: ['your-server:9090']
    bearer_token: 'your-secret-token-here'
```

## 3. Subscribe to Log Stream (SSE)

```bash
# Connect to SSE log stream (requires API token from spec 002)
curl -N -H "Authorization: Bearer unet_YOUR_TOKEN_HERE" \
  https://localhost:8443/v1/logs/stream

# Filter: errors only
curl -N -H "Authorization: Bearer unet_YOUR_TOKEN_HERE" \
  "https://localhost:8443/v1/logs/stream?level=error"

# Filter: tunnel component only
curl -N -H "Authorization: Bearer unet_YOUR_TOKEN_HERE" \
  "https://localhost:8443/v1/logs/stream?component=tunnel"

# Filter: warn+ errors from container source
curl -N -H "Authorization: Bearer unet_YOUR_TOKEN_HERE" \
  "https://localhost:8443/v1/logs/stream?level=warn&source=container"

# Localhost (no auth needed)
curl -N http://localhost:8080/v1/logs/stream
```

### Verify keepalive

```bash
# Wait 15+ seconds — should see keepalive comments
curl -N http://localhost:8080/v1/logs/stream 2>&1 | grep keepalive
```

### Test backpressure

```bash
# Generate log volume and observe
# In another terminal, trigger many API calls
for i in $(seq 1 100); do
  curl -s -H "Authorization: Bearer unet_YOUR_TOKEN" \
    https://localhost:8443/v1/peers > /dev/null
done

# SSE subscriber should see all events (within buffer)
```

## 4. Verify Log Rotation

### Check rotation settings

Default: 100MB per file, 30-day retention.

```bash
# Current log file size
ls -lh ~/.unet/logs/daemon-$(date -u +%Y-%m-%d).jsonl

# List all log files (including rotated)
ls -la ~/.unet/logs/
```

### Force rotation for testing

Lower the rotation threshold in config:

```json
{
  "observability": {
    "maxFileSizeMB": 1
  }
}
```

Requires daemon restart (per FR-022: `maxFileSizeMB` is not hot-reloadable). Then generate log volume until rotation triggers:

```bash
# Watch for new rotated files
watch -n 1 'ls -la ~/.unet/logs/'

# Verify rotated file is sealed (no longer growing)
stat ~/.unet/logs/daemon-2026-05-28.1.jsonl
```

### Verify retention cleanup

```bash
# Check retention setting (default 30 days)
grep retentionDays ~/.unet/config.json

# Manually create an "old" file to test cleanup
touch -t 202604010000 ~/.unet/logs/daemon-2026-04-01.jsonl
# Restart daemon → retention cleanup should delete files older than 30 days
```

## 5. Test Container Log Capture

Container log capture is enabled by default (per round 1 clarification).

```bash
# Verify capture is enabled
grep captureContainerLogs ~/.unet/config.json

# Check that container logs appear in the unified stream
curl -N http://localhost:8080/v1/logs/stream?source=container 2>&1 | head -20

# Trigger a Caddy event (add/remove a route) and watch container logs
curl -N http://localhost:8080/v1/logs/stream?component=container.unet-caddy &
# In another terminal, create a route
curl -X POST -H "Authorization: Bearer unet_YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"subdomain":"test","localPort":3000}' \
  https://localhost:8443/v1/routes
```

### Docker Compose test (manual)

```bash
# Start a test container to verify log capture
docker run -d --name test-nginx nginx:alpine

# If test-nginx is NOT in the managed containers list, it won't be captured
# Only unet-amnezia-awg and unet-caddy are auto-captured
# This is expected behavior — container capture is limited to managed containers
```

## 6. Export Logs for Support

```bash
# Export last 24 hours
curl -H "Authorization: Bearer unet_YOUR_TOKEN" \
  "https://localhost:8443/v1/logs/export?from=$(date -u -d '1 day ago' +%Y-%m-%dT00:00:00Z)&to=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o logs-export.tar.gz

# Verify export
tar tzf logs-export.tar.gz
tar xzf logs-export.tar.gz -C /tmp/logs-export/
cat /tmp/logs-export/*.jsonl | jq . | head -5

# Export with PII scrubbing enabled
# Set in config: observability.scrubPii = true
# Then re-run the export command
# Verify IPs are masked:
tar xzf logs-export.tar.gz -O | grep -o '"peer_id":"[^"]*"' | head
# Should show peer IDs, not names
```

## Configuration Reference

All settings in `~/.unet/config.json` under `observability`:

```json
{
  "observability": {
    "enabled": true,
    "logLevels": {
      "global": "info",
      "tunnel": "debug",
      "caddy-client": "warn"
    },
    "maxFileSizeMB": 100,
    "retentionDays": 30,
    "captureContainerLogs": true,
    "scrubPii": false,
    "sseClientBuffer": 1000,
    "logToStdout": true,
    "metrics": {
      "enabled": false,
      "listenAddr": "127.0.0.1:9090",
      "bearerToken": ""
    }
  }
}
```

**Hot-reloadable** (no restart needed):
- `logLevels`
- `retentionDays`
- `metrics.enabled`
- `captureContainerLogs`
- `scrubPii`

**Requires restart**:
- `maxFileSizeMB`
- `sseClientBuffer`
- `metrics.listenAddr`
- `metrics.bearerToken`
