# Migration Protocol

Step-by-step cutover sequence for migrating a unet VPS deployment from **VPS_A**
(source) to **VPS_B** (target) with zero-downtime for connected peers.

---

## Overview

```
     VPS_A (source)                     VPS_B (target)
     ┌──────────┐                       ┌──────────┐
     │  Daemon  │  ──── bootstrap ────> │  Daemon  │
     │  Stack   │  ──── state sync ───> │  Stack   │
     │          │  <── health probe ─── │          │
     │          │                       │          │
     │  ACTIVE  │  ──── DNS cutover ──> │  ACTIVE  │
     │  DRAINING│  <── drain timeout ── │  SERVING │
     │  ARCHIVED│                       │  SERVING │
     └──────────┘                       └──────────┘
```

---

## Timing Diagram

```
T+0    ───────────────────────────────────────────────────────
       Step 1: Pre-flight connectivity check
       Step 2: Snapshot on VPS_A
T+5s   ───────────────────────────────────────────────────────
       Step 3: Bootstrap VPS_B  ──── parallel with step 2 ──>
T+30s  ───────────────────────────────────────────────────────
       Step 4: Export state bundle from VPS_A
T+45s  ───────────────────────────────────────────────────────
       Step 5: Import state bundle to VPS_B
T+60s  ───────────────────────────────────────────────────────
       Step 6: Verify VPS_B health
T+90s  ───────────────────────────────────────────────────────
       Step 7: DNS cutover — update A-records
       ┊ wait DNS_TTL × 2 for propagation  ┊
T+90s+DNS_TTL*2  ────────────────────────────────────────────
       Step 8: Drain VPS_A — wait for last handshake expiry
       ┊ configurable drain timeout       ┊
T+drain  ────────────────────────────────────────────────────
       Step 9: Decommission VPS_A
       Step 10: Update local VPSProfile → VPS_B
```

---

## Step-by-step Sequence

### Step 1 — Pre-flight: validate SSH connectivity to both VPS

**VPS_A check:**
```
ssh -p <sshPort> <user>@<vpsA_host> "echo ok"
```

**VPS_B check:**
```
ssh -p <sshPort> <user>@<vpsB_host> "echo ok"
```

| Field                | Value                                                         |
|----------------------|---------------------------------------------------------------|
| Expected output      | `ok` from both hosts                                          |
| Failure — VPS_A      | Abort. Cannot proceed without source state.                   |
| Failure — VPS_B      | Abort. Target unreachable; user must fix SSH/auth.            |
| Recovery             | None automatic. User reconfigures and retries from step 1.    |
| Abort condition      | Either host unreachable after SSH timeout (30 s).             |

---

### Step 2 — Create pre-migration snapshot on VPS_A

```
ssh <vpsA> "sudo docker exec unet unet-cli snapshot create --tag pre-migration-$(date +%s)"
```

Alternatively, if daemon exposes snapshot API:
```
curl -s -X POST http://<vpsA_daemon>/api/state/export \
  -H 'Content-Type: application/json' \
  -d '{"outputPath":"/opt/unet/snapshots/pre-migration.bundle","passphrase":"<snapshot_key>"}'
```

| Field                | Value                                                         |
|----------------------|---------------------------------------------------------------|
| Expected output      | Snapshot ID / bundle path confirmed.                          |
| Failure              | Abort. Source state not preserved — unsafe to proceed.        |
| Recovery             | Check disk space on VPS_A. Retry once.                        |
| Abort condition      | Snapshot creation fails or times out (60 s).                  |

---

### Step 3 — Bootstrap VPS_B (parallel with step 2)

Follow the full **bootstrap-protocol.md** sequence against VPS_B.

Started in parallel with step 2 to reduce total wall-clock time.

| Field                | Value                                                         |
|----------------------|---------------------------------------------------------------|
| Expected output      | Bootstrap complete; VPS_B daemon running with empty state.    |
| Failure              | Abort entire migration. VPS_B cannot host the stack.          |
| Recovery             | Bootstrap-protocol rollback runs automatically.               |
| Abort condition      | Any bootstrap phase fails (see bootstrap-protocol).           |

**Parallelism note:** Steps 2 and 3 run concurrently. If step 2 finishes first,
the controller waits for step 3 before proceeding. If step 3 fails, step 2's
snapshot is kept as a backup (no rollback needed on VPS_A).

---

### Step 4 — Export state bundle from VPS_A (local export)

```
curl -s -X POST http://<vpsA_daemon>/api/state/export \
  -H 'Content-Type: application/json' \
  -d '{
    "outputPath": "/tmp/unet-migration.bundle",
    "passphrase": "<migration_passphrase>"
  }'
```

Download the bundle to the orchestrator (local machine):
```
scp <user>@<vpsA_host>:/tmp/unet-migration.bundle /tmp/unet-migration.bundle
```

| Field                | Value                                                         |
|----------------------|---------------------------------------------------------------|
| Expected output      | Bundle downloaded; manifest `payloadHash` verified locally.   |
| Failure              | Abort. Cannot migrate without state.                          |
| Recovery             | Retry export once. If persistent, check VPS_A daemon health.  |
| Abort condition      | Export fails, bundle hash mismatch, or download incomplete.   |

---

### Step 5 — Import state bundle to VPS_B

Upload bundle to VPS_B:
```
scp /tmp/unet-migration.bundle <user>@<vpsB_host>:/tmp/unet-migration.bundle
```

Trigger import via VPS_B daemon API:
```
curl -s -X POST http://<vpsB_daemon>/api/state/import \
  -H 'Content-Type: application/json' \
  -d '{
    "bundlePath": "/tmp/unet-migration.bundle",
    "passphrase": "<migration_passphrase>"
  }'
```

| Field                | Value                                                         |
|----------------------|---------------------------------------------------------------|
| Expected output      | Import success; VPS_B daemon reloads with VPS_A's state.      |
| Failure              | Abort. Clean VPS_B (stop compose, remove data).              |
| Recovery             | Verify passphrase. Retry import once.                         |
| Abort condition      | Import fails validation or daemon cannot reload state.        |

---

### Step 6 — Verify VPS_B health

Full health check per bootstrap-protocol Phase 4:

1. `docker ps` — all containers running
2. `awg0` interface exists in container
3. Peer count matches manifest `peerCount`
4. Server public key matches VPS_A's key (same key = clients reconnect seamlessly)

```
curl -s http://<vpsB_daemon>/api/health/probe
```

Expected response:
```json
{
  "status": "healthy",
  "wireguard": {
    "interface": "awg0",
    "listenPort": 51820,
    "peerCount": 12
  },
  "daemon": {
    "version": "0.3.0",
    "uptimeSeconds": 42
  }
}
```

| Field                | Value                                                         |
|----------------------|---------------------------------------------------------------|
| Expected output      | All health checks pass; peer counts match.                    |
| Failure              | Abort. Rollback VPS_B (stop compose, remove data).            |
| Recovery             | Retry probe up to 3 times at 10 s intervals.                  |
| Abort condition      | Health check fails after retries; peers cannot connect.       |

---

### Step 7 — DNS cutover

Update DNS A-records for the unet endpoint hostname to point to VPS_B's IP.

```
# Example: Cloudflare API
curl -s -X PUT "https://api.cloudflare.com/client/v4/zones/<zone_id>/dns_records/<record_id>" \
  -H "Authorization: Bearer <cf_token>" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "A",
    "name": "unet.example.com",
    "content": "<VPS_B_IP>",
    "ttl": 60,
    "proxied": false
  }'
```

**Wait for propagation:** `DNS_TTL × 2` seconds. Default DNS_TTL = 60 s → wait 120 s.

| Field                | Value                                                         |
|----------------------|---------------------------------------------------------------|
| Expected output      | DNS API returns success; dig from multiple resolvers confirms VPS_B IP. |
| Failure              | Pause. Do NOT proceed to drain. DNS must resolve to VPS_B first. |
| Recovery             | Retry DNS update. Check API credentials. Manual dig verification. |
| Abort condition      | DNS update API error that cannot be retried (auth, zone not found). |

**Both VPS_A and VPS_B serve traffic during propagation** — WireGuard peers use
IP directly or cached DNS, so both endpoints remain valid as long as state is
identical.

---

### Step 8 — Drain VPS_A

Wait for all WireGuard peer sessions to expire or migrate to VPS_B.

Drain strategy:
1. Stop accepting new handshakes on VPS_A (daemon setting: `acceptNewHandshakes: false`).
2. Poll VPS_A for active handshakes: `curl http://<vpsA_daemon>/api/health/probe`.
3. Wait until zero active handshakes OR `drainTimeout` elapsed.

**Drain timeout:** configurable, default = `max(DNS_TTL * 4, 300)` seconds.

```
# Poll loop (pseudo-code)
while active_handshakes > 0 && elapsed < drainTimeout:
    sleep 10
    probe = GET /api/health/probe on VPS_A
    active_handshakes = probe.wireguard.activeHandshakes
```

| Field                | Value                                                         |
|----------------------|---------------------------------------------------------------|
| Expected output      | Zero active handshakes, or drain timeout reached with ≤ 0 handshakes. |
| Failure              | Timeout with remaining handshakes. Force-drain if user confirms. |
| Recovery             | Extend drain timeout. Or force-drain (clients auto-reconnect to VPS_B). |
| Abort condition      | Not an abort scenario — drain always completes (timeout is the upper bound). |

---

### Step 9 — Decommission VPS_A

1. Stop compose stack:
   ```
   ssh <vpsA> "cd /opt/unet && sudo docker compose down"
   ```
2. Archive state (keep last export):
   ```
   ssh <vpsA> "sudo mv /opt/unet /opt/unet.decommissioned.$(date +%s)"
   ```
3. Optionally delete VPS_A (user action, not automated).

| Field                | Value                                                         |
|----------------------|---------------------------------------------------------------|
| Expected output      | Compose stack stopped; directory archived.                    |
| Failure              | Non-critical. VPS_A is no longer serving traffic. Log warning. |
| Recovery             | Manual cleanup. VPS_A is already drained and DNS-removed.     |
| Abort condition      | None — decommission is best-effort.                           |

---

### Step 10 — Update local VPSProfile to point to VPS_B

Update the local daemon's VPSProfile configuration:

```json
{
  "id": "<same_profile_id>",
  "host": "<VPS_B_IP>",
  "sshPort": <VPS_B_sshPort>,
  "user": "<VPS_B_user>",
  "authMode": "<VPS_B_authMode>",
  "endpoint": "<VPS_B_IP>:51820",
  "classification": "self-managed",
  "bootstrappedAt": "<timestamp>",
  "migratedFrom": "<VPS_A_IP>"
}
```

| Field                | Value                                                         |
|----------------------|---------------------------------------------------------------|
| Expected output      | Profile saved; daemon reconnects to VPS_B.                   |
| Failure              | Critical. Manual fix required — edit profile JSON.            |
| Recovery             | Retry save. Daemon watches profile file and auto-reconnects.  |
| Abort condition      | None — this is the final step; migration is already complete. |

---

## Failure Recovery Matrix

| Step | Failure mode                    | Recovery action                                    | Data loss risk |
|------|---------------------------------|----------------------------------------------------|----------------|
| 1    | SSH unreachable                 | User fixes connectivity; retry from step 1         | None           |
| 2    | Snapshot fails                  | Check disk; retry; abort if persistent             | None           |
| 3    | Bootstrap fails                 | Auto-rollback on VPS_B; abort migration            | None           |
| 4    | Export fails                    | Retry once; check VPS_A health                     | None           |
| 5    | Import fails                    | Clean VPS_B; retry import; abort if persistent     | None           |
| 6    | Health check fails              | Retry probe; rollback VPS_B if persistent          | None           |
| 7    | DNS update fails                | Pause; retry; do NOT drain until resolved          | None           |
| 8    | Drain timeout with peers        | Extend timeout or force-drain (clients reconnect)  | None           |
| 9    | Decommission partial            | Log warning; manual cleanup                        | None           |
| 10   | Profile update fails            | Manual profile edit                               | None           |

**Key invariant:** VPS_A is never modified (only snapshotted + drained) until
VPS_B is verified healthy and DNS has cut over. At any point before step 7, the
migration can be aborted with zero impact on production traffic.

---

## Abort Conditions Summary

The migration can be safely aborted (no data loss, no service interruption)
at any point **before step 7 (DNS cutover)**. After DNS cutover, a partial
rollback requires:

1. Revert DNS back to VPS_A IP.
2. Wait DNS_TTL × 2.
3. Stop VPS_B compose (optional — state is already synced, no harm in leaving it).

After step 8 (drain) begins, rollback is not recommended — VPS_A may already
have stopped accepting handshakes.
