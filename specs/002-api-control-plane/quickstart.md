# Developer Quickstart: Remote Control Plane API

**Spec**: `specs/002-api-control-plane/spec.md`

---

## Enable Remote API

Add to `~/.unet/config.json`:

```json
{
  "remoteApi": {
    "enabled": true,
    "listenAddr": "0.0.0.0:8443"
  }
}
```

Start (or restart) the daemon:

```bash
# Linux/macOS
sudo unet daemon start

# Windows (elevated shell)
unet daemon start
```

On first start with `remoteApi.enabled: true`:

- Self-signed TLS cert generated at `~/.unet/cert.pem` + `~/.unet/key.pem`.
- Bootstrap admin token written to `~/.unet/bootstrap-token` (mode 0600).
- Daemon logs: `remote API listening on 0.0.0.0:8443 (TLS)`.

To disable remote access: set `listenAddr: "127.0.0.1:8443"` or `"enabled": false`.

---

## Create Your First PAT

### Using the bootstrap token

```bash
# Read the one-time bootstrap token
BOOTSTRAP=$(cat ~/.unet/bootstrap-token)

# Create a real admin token
curl -k -X POST https://localhost:8443/v1/tokens \
  -H "Authorization: Bearer $BOOTSTRAP" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-admin", "scope": "admin"}'
```

Response:

```json
{
  "id": "uuid-here",
  "name": "my-admin",
  "token": "unet_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6...",
  "scope": "admin",
  "createdAt": "2026-05-27T10:00:00Z"
}
```

**Store the `token` value.** It will never be shown again.

The bootstrap token file is deleted after first successful token creation.

### Create a read-only token for external tools

```bash
TOKEN="unet_a1b2c3d4..."  # your admin token

curl -k -X POST https://localhost:8443/v1/tokens \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "undevops-readonly", "scope": "read"}'
```

---

## Make API Calls

```bash
TOKEN="unet_your-token-here"
BASE="https://your-server:8443/v1"

# List peers
curl -k $BASE/peers \
  -H "Authorization: Bearer $TOKEN"

# Create a peer (get WireGuard client config)
curl -k -X POST $BASE/peers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-phone"}'

# Tunnel status
curl -k $BASE/tunnel/status \
  -H "Authorization: Bearer $TOKEN"

# List routes
curl -k $BASE/routes \
  -H "Authorization: Bearer $TOKEN"

# Create a route
curl -k -X POST $BASE/routes \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"localPort": 3000, "subdomain": "app"}'

# System status
curl -k $BASE/status \
  -H "Authorization: Bearer $TOKEN"

# Query audit log (admin only)
curl -k "$BASE/audit?limit=10&action=create_peer" \
  -H "Authorization: Bearer $TOKEN"
```

### Skip TLS verification

The `-k` flag skips TLS verification (self-signed cert). For production, either:

- Use a CA-signed cert (set paths in config).
- Pin the self-signed cert fingerprint in your client.
- Export the cert and add to your trust store:

```bash
# Export cert
cp ~/.unet/cert.pem /path/to/trusted-certs/unet.pem

# Use with curl
curl --cacert /path/to/trusted-certs/unet.pem https://...
```

---

## Run Integration Tests

```bash
# From the unet repo root
cd src/

# All tests (unit + integration)
go test ./internal/api/v1/... ./internal/auth/... ./internal/audit/... -v

# Unit tests only (no daemon, no VPS)
go test -short ./internal/... -v

# Integration tests (requires running daemon with remote API)
# Set env vars for test config
export UNET_TEST_REMOTE_API=https://localhost:8443
export UNET_TEST_TOKEN=unet_your-test-token
go test ./internal/api/v1/... -run Integration -v

# Test auth flows specifically
go test ./internal/auth/... -run TestPAT -v
go test ./internal/auth/... -run TestJWT -v
```

### Mock-based tests (no VPS needed)

Most integration tests use `httptest.NewServer` with the full middleware chain and mocked daemon core functions. No real VPS required.

```bash
go test ./internal/api/v1/... -run TestHandler -v
```

---

## Where to Find Logs

```bash
# Daemon logs (stdout/stderr or systemd journal)
# Look for remote API log lines:
#   "remote API listening on 0.0.0.0:8443 (TLS)"
#   "TLS cert generated: ~/.unet/cert.pem"
#   "bootstrap token written to ~/.unet/bootstrap-token"

# If running as systemd service:
journalctl -u unet -f

# Config file (tokens, routes, peers):
cat ~/.unet/config.json | jq .

# Audit log (JSONL — one JSON object per line):
tail -f ~/.unet/audit.jsonl | jq .

# TLS certificate info:
openssl x509 -in ~/.unet/cert.pem -text -noout
```

---

## Common Troubleshooting

| Symptom | Check |
|---------|-------|
| Connection refused on :8443 | `remoteApi.enabled: true` in config? Daemon running? |
| TLS handshake error | Cert files exist at `~/.unet/cert.pem`? Not expired? |
| 401 on every request | Bearer token correct? Token not revoked? Using `-k` for self-signed? |
| 403 on write operations | Token scope is `read` — need `write` or `admin` |
| 429 Too Many Requests | Rate limit hit (60/min default). Wait or reduce frequency. |
| 503 VPS unreachable | Daemon can't SSH to VPS. Check network, SSH creds. |
| Bootstrap token file missing | Already used on first token creation, or daemon not started with `remoteApi.enabled`. |
