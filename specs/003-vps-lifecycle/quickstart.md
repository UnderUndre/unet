# Developer Quickstart: VPS Lifecycle Management

**Spec**: `specs/003-vps-lifecycle/spec.md`
**Audience**: Developers working on unet daemon lifecycle features.

---

## Prerequisites

- Go 1.22+ (same as main daemon)
- Docker + Docker Compose (for local DinD testing)
- SSH client on PATH
- AmneziaWG (`awg-quick`) installed locally (for live testing)
- Optional: S3-compatible storage credentials (R2/B2/MinIO) for backup sync testing

---

## Bootstrap a Fresh VPS

```bash
# Via daemon CLI
unet bootstrap root@1.2.3.4 --ssh-port 22 --auth-mode key --key ~/.ssh/id_ed25519

# Via localhost API
curl -X POST http://localhost:8080/api/vps/bootstrap \
  -H "Content-Type: application/json" \
  -d '{
    "host": "1.2.3.4",
    "sshPort": 22,
    "username": "root",
    "authMode": "key",
    "privateKeyPath": "~/.ssh/id_ed25519"
  }'

# Via remote API (requires admin token)
curl -X POST https://localhost:8443/v1/vps/bootstrap \
  -H "Authorization: Bearer unet_YOUR_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "host": "1.2.3.4", "sshPort": 22, "username": "root", "authMode": "key", "privateKeyPath": "~/.ssh/id_ed25519" }'
```

**What happens**: Preflight checks → Docker install (if missing) → compose render → `docker compose up -d` → health probe wait → success.

**Idempotency**: Re-running on an already-bootstrapped VPS at the same version produces zero changes. No containers restarted, no config rewritten.

**Timeout**: 5 min cold start (first Docker image build), 2 min warm.

---

## Attach to Existing VPS

```bash
# Via daemon CLI
unet attach root@1.2.3.4 --ssh-port 22 --auth-mode key --key ~/.ssh/id_ed25519

# Via localhost API
curl -X POST http://localhost:8080/api/vps/attach \
  -H "Content-Type: application/json" \
  -d '{ "host": "1.2.3.4", "sshPort": 22, "username": "root", "authMode": "key", "privateKeyPath": "~/.ssh/id_ed25519" }'
```

**What happens**: SSH connect → detection probe (classifies VPS as `blank`/`old`/`current`/`incompatible`) → if `current`, syncs `awg0.conf` + `clientsTable` + Caddy routes to local state without disrupting connected peers.

**Classification result example**:
```json
{
  "classification": "current",
  "detectedVersion": "0.3.0",
  "canonicalVersion": "0.3.0",
  "composeHash": "abc123...",
  "lastChecked": "2026-05-28T10:00:00Z",
  "sshReachable": true,
  "dockerRunning": true,
  "composeHealthy": true
}
```

---

## Export and Verify State Bundle

```bash
# Export (encrypted with passphrase)
unet state export --output ./my-backup.unet-bundle
# Prompts for passphrase (age encryption)

# Verify bundle integrity (dry-run import)
unet state verify ./my-backup.unet-bundle
# Prompts for passphrase, validates manifest + hash, does NOT import

# Import on new machine
unet state import ./my-backup.unet-bundle
# Prompts for passphrase, validates, overwrites ALL local state after confirmation

# Export with S3 sync
unet state export --output ./my-backup.unet-bundle \
  --s3-endpoint https://account.r2.cloudflarestorage.com \
  --s3-bucket unet-backups \
  --s3-region auto \
  --s3-prefix daily/
# Requires AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY env vars
```

**State bundle contents** (after decryption):
- Peers (same shape as 002's Peer)
- Routes (same shape as 002's IngressRoute)
- Token hashes (NOT plaintext tokens — re-create tokens after import)
- Tunnel config (obfuscation params, MTU, keepalive — no private keys)
- VPS connection metadata (host, port, user — no password/key)
- DNS config (mode, base domain — no Cloudflare token)
- Last 100 audit entries

**Bundle NOT included**: SSH private keys, WireGuard private keys, Cloudflare API tokens, token plaintext. You must re-provide credentials after import.

---

## Migrate VPS_A to VPS_B

```bash
# Via daemon CLI
unet migrate \
  --target-host 5.6.7.8 \
  --target-ssh-port 22 \
  --target-user root \
  --target-auth-mode key \
  --target-key ~/.ssh/id_ed25519 \
  --dns-ttl 300

# Monitor migration progress
unet migrate status

# Abort if needed (rolls back to VPS_A)
unet migrate abort
```

**What happens** (cutover migration):
1. Pre-flight: validate both VPS reachable
2. Snapshot VPS_A
3. Bootstrap VPS_B (parallel)
4. Export state from VPS_A → import to VPS_B
5. Verify VPS_B health
6. DNS cutover: update A-records to VPS_B IP
7. Wait 2 × DNS TTL for propagation
8. Drain VPS_A (wait for last client)
9. Decommission VPS_A
10. Update local profile → VPS_B

**Cutover window**: DNS TTL × 2 (default 10 minutes with TTL=300). Clients disconnect briefly and reconnect to VPS_B automatically.

**Crash recovery**: If daemon dies mid-migration, restart reads `~/.unet/migration.json` and offers resume or rollback.

---

## Run Lifecycle Tests Locally

### Docker-in-Docker (DinD) as fake VPS

```bash
# Start DinD container acting as VPS
docker run -d --name unet-test-vps \
  --privileged \
  -p 2222:22 \
  -e DOCKER_TLS_CERTDIR="" \
  docker:dind

# Wait for SSH (install openssh if not in image)
docker exec unet-test-vps sh -c "
  apk add --no-cache openssh sudo &&
  ssh-keygen -A &&
  echo 'PermitRootLogin yes' >> /etc/ssh/sshd_config &&
  /usr/sbin/sshd
"

# Copy test SSH key
docker cp ~/.ssh/id_ed25519.pub unet-test-vps:/root/.ssh/authorized_keys

# Run bootstrap test against DinD
go test ./internal/lifecycle/bootstrap/ -run TestBootstrapClean -tags integration \
  -vps-host localhost -vps-port 2222 -vps-user root

# Run attach test
go test ./internal/lifecycle/attach/ -run TestAttachCurrent -tags integration \
  -vps-host localhost -vps-port 2222 -vps-user root

# Run full lifecycle E2E
go test ./internal/lifecycle/ -run TestLifecycleE2E -tags e2e \
  -vps-host localhost -vps-port 2222 -vps-user root -timeout 10m

# Cleanup
docker rm -f unet-test-vps
```

### Unit tests (no VPS needed)

```bash
# All lifecycle packages
go test ./internal/lifecycle/... -v

# Specific package
go test ./internal/lifecycle/backup/ -v          # Age encrypt/decrypt, JSONL marshal
go test ./internal/lifecycle/detect/ -v           # Version classifier
go test ./internal/lifecycle/compose/ -v          # Template rendering
go test ./internal/lifecycle/health/ -v           # Probe logic with mocked ICMP/HTTP
go test ./internal/state/ -v                      # State persistence
go test ./internal/ssh/ -v                        # SSH session pool
```

---

## Where Lifecycle Logs Go

| Log Type | Location | Format |
|----------|----------|--------|
| Lifecycle audit | `~/.unet/lifecycle-audit.jsonl` | JSONL (one entry per line) |
| API audit (002) | `~/.unet/audit.jsonl` | JSONL (separate from lifecycle) |
| Daemon stdout | Console / systemd journal | Structured (slog/zerolog) |
| Health probe | `~/.unet/health.json` (latest snapshot) | JSON |
| Migration state | `~/.unet/migration.json` | JSON (in-memory + persisted on phase change) |
| Reconnect state | In-memory only | Lost on daemon restart |

**Log levels**:
- `INFO`: lifecycle events (bootstrap start/complete, attach, migrate phase transitions)
- `WARN`: degraded states (probe failure, version drift detected)
- `ERROR`: operation failures (SSH connection lost, compose up failed, export failed)
- `DEBUG`: SSH command output, probe details, template rendering

---

## Key Files Reference

| File | Purpose |
|------|---------|
| `~/.unet/config.json` | Main daemon config (peers, routes, tokens, DNS) |
| `~/.unet/vps.json` | VPS profile (SSH coords, WG params, status) |
| `~/.unet/migration.json` | In-progress migration state (ephemeral) |
| `~/.unet/ssh/` | SSH private keys (mode 0600) |
| `~/.unet/lifecycle-audit.jsonl` | Lifecycle-specific audit log |
| `~/.unet/health.json` | Latest health snapshot |
| `/opt/unet/` (on VPS) | Compose files, version, snapshots |
| `/opt/unet/version` (on VPS) | Daemon semver written at bootstrap |

---

## Common Workflows

### Replace laptop (state transfer)
```bash
# On old machine
unet state export --output /tmp/my-unet.unet-bundle
# Copy .unet-bundle to new machine via USB/cloud

# On new machine
unet state import /tmp/my-unet.unet-bundle
# Re-provide SSH key path, Cloudflare token
unet attach root@1.2.3.4
```

### VPS provider change
```bash
unet migrate --target-host new-vps-ip --target-user root --target-auth-mode key --target-key ~/.ssh/id_ed25519
# Wait for cutover (~10 min default)
unet migrate status  # check progress
```

### Recover from botched upgrade
```bash
unet rollback  # restores last pre-mutation snapshot from VPS
```

### Check VPS health
```bash
curl http://localhost:8080/api/health/probe
# Returns: { reachable, wgHandshake, containerStatus, errors }
```
