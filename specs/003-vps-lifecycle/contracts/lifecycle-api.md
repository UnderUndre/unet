# Lifecycle API

How VPS lifecycle operations surface in the daemon HTTP API. This extends:

- The **localhost API** defined in spec `001-init`
- The **remote /v1/ API** defined in spec `002`

> **Cross-spec follow-up:** The OpenAPI document from spec 002 will need to be
> extended with the endpoints below. This should be done as a separate PR that
> references this contract.

---

## New Localhost Endpoints

All local endpoints listen on `127.0.0.1:<daemonPort>` (default 9300).

---

### POST /api/vps/bootstrap

Trigger a bootstrap sequence on a target host.

**Request:**

```json
{
  "host": "203.0.113.42",
  "sshPort": 22,
  "user": "root",
  "authMode": "ssh-key",
  "sshPrivateKeyPath": "/home/user/.ssh/id_ed25519",
  "daemonVersion": "0.3.0",
  "composeTemplate": "default"
}
```

**Response (202 Accepted):**

```json
{
  "taskId": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
  "status": "running",
  "startedAt": "2026-05-28T14:00:00Z"
}
```

Poll `GET /api/vps/lifecycle` for progress.

| Field            | Value                                                             |
|------------------|-------------------------------------------------------------------|
| Auth             | Localhost only (no token required)                                |
| Async            | Yes — returns taskId immediately                                  |
| Failure handling | Task record stores error; lifecycle endpoint exposes it           |

---

### POST /api/vps/attach

Trigger attach detection and sync for a VPS the daemon discovers on the local
network or via manual configuration. State taxonomy follows spec FR-003 (blank/old/current/incompatible).

**Request:**

```json
{
  "host": "203.0.113.42",
  "sshPort": 22,
  "user": "root",
  "authMode": "ssh-key",
  "sshPrivateKeyPath": "/home/user/.ssh/id_ed25519"
}
```

**Response (200 OK):**

```json
{
  "classification": "current",
  "reason": "unet daemon detected at /opt/unet, version 0.3.0 matches local daemon",
  "vpsId": "vps_abc123",
  "daemonVersion": "0.3.0",
  "peerCount": 12,
  "lastSyncAt": "2026-05-28T14:05:00Z"
}
```

Classification values:
- `"blank"` — no Docker, no unet artifacts, fresh OS (redirect to bootstrap)
- `"old"` — unet installed but version behind daemon (within compatible range; prompt for upgrade)
- `"current"` — unet installed, version matches daemon, compose matches canonical
- `"incompatible"` — unet installed but major version mismatch or config schema incompatible (attach refused)

| Field            | Value                                                             |
|------------------|-------------------------------------------------------------------|
| Auth             | Localhost only                                                    |
| Async            | No — blocks until classification complete (max 30 s)             |
| Failure handling | Returns 502 with error detail if SSH fails                        |

---

### GET /api/vps/lifecycle

Returns current VPS lifecycle status including classification, last probe
timestamp, and reconnect state.

**Response (200 OK):**

```json
{
  "vpsId": "vps_abc123",
  "classification": "current",
  "bootstrappedAt": "2026-05-20T10:00:00Z",
  "lastProbeAt": "2026-05-28T14:10:00Z",
  "lastProbeStatus": "healthy",
  "reconnectState": "connected",
  "endpoint": "203.0.113.42:51820",
  "daemonVersion": "0.3.0",
  "activeTask": {
    "taskId": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
    "type": "bootstrap",
    "status": "running",
    "progress": 0.6,
    "phase": "compose-deploy"
  },
  "snapshots": [
    {
      "id": "snap_001",
      "tag": "pre-migration-1748440800",
      "createdAt": "2026-05-28T14:00:00Z",
      "sizeBytes": 204800
    }
  ]
}
```

`reconnectState` values:
- `"connected"` — daemon has active SSH + API connection to VPS
- `"reconnecting"` — connection lost, automatic retry in progress
- `"disconnected"` — VPS unreachable, manual intervention needed
- `"none"` — no VPS configured

`activeTask` is `null` when no background task is running.

| Field            | Value                                                             |
|------------------|-------------------------------------------------------------------|
| Auth             | Localhost only                                                    |
| Async            | No — instant response from cached state                           |

---

### POST /api/vps/rollback

Roll back to the most recent snapshot.

**Request:**

```json
{
  "snapshotId": "snap_001",
  "confirm": true
}
```

**Response (202 Accepted):**

```json
{
  "taskId": "b2c3d4e5-f6a7-4b8c-9d0e-1f2a3b4c5d6e",
  "status": "running",
  "snapshotId": "snap_001",
  "startedAt": "2026-05-28T14:15:00Z"
}
```

| Field            | Value                                                             |
|------------------|-------------------------------------------------------------------|
| Auth             | Localhost only                                                    |
| Async            | Yes                                                               |
| Failure handling | Task stores error; rollback is stopped at safe point              |
| Safety           | Requires `"confirm": true`; rejects without it                    |

---

### POST /api/vps/migrate

Initiate migration from the current VPS to a new target host.

**Request:**

```json
{
  "targetHost": "198.51.100.7",
  "targetSshPort": 22,
  "targetUser": "root",
  "targetAuthMode": "ssh-key",
  "targetSshPrivateKeyPath": "/home/user/.ssh/id_ed25519",
  "passphrase": "migration-secret-passphrase",
  "dnsTtl": 60,
  "drainTimeoutSeconds": 300,
  "dryRun": false
}
```

**Response (202 Accepted):**

```json
{
  "taskId": "c3d4e5f6-a7b8-4c9d-0e1f-2a3b4c5d6e7f",
  "status": "planning",
  "steps": [
    { "step": 1, "name": "preflight", "status": "pending" },
    { "step": 2, "name": "snapshot-source", "status": "pending" },
    { "step": 3, "name": "bootstrap-target", "status": "pending" },
    { "step": 4, "name": "export-state", "status": "pending" },
    { "step": 5, "name": "import-state", "status": "pending" },
    { "step": 6, "name": "verify-target-health", "status": "pending" },
    { "step": 7, "name": "dns-cutover", "status": "pending" },
    { "step": 8, "name": "drain-source", "status": "pending" },
    { "step": 9, "name": "decommission-source", "status": "pending" },
    { "step": 10, "name": "update-profile", "status": "pending" }
  ],
  "startedAt": "2026-05-28T14:20:00Z"
}
```

When `"dryRun": true`, the response returns the planned steps but does not
execute them.

| Field            | Value                                                             |
|------------------|-------------------------------------------------------------------|
| Auth             | Localhost only                                                    |
| Async            | Yes — long-running (minutes)                                     |
| Failure handling | Stops at failing step; see migration-protocol abort conditions    |

---

### GET /api/vps/migrate

Get status of in-progress or most recent migration.

**Response (200 OK):**

```json
{
  "taskId": "c3d4e5f6-a7b8-4c9d-0e1f-2a3b4c5d6e7f",
  "status": "running",
  "currentStep": 6,
  "currentStepName": "verify-target-health",
  "steps": [
    { "step": 1, "name": "preflight", "status": "completed", "startedAt": "...", "finishedAt": "..." },
    { "step": 2, "name": "snapshot-source", "status": "completed", "startedAt": "...", "finishedAt": "..." },
    { "step": 3, "name": "bootstrap-target", "status": "completed", "startedAt": "...", "finishedAt": "..." },
    { "step": 4, "name": "export-state", "status": "completed", "startedAt": "...", "finishedAt": "..." },
    { "step": 5, "name": "import-state", "status": "completed", "startedAt": "...", "finishedAt": "..." },
    { "step": 6, "name": "verify-target-health", "status": "running", "startedAt": "..." },
    { "step": 7, "name": "dns-cutover", "status": "pending" },
    { "step": 8, "name": "drain-source", "status": "pending" },
    { "step": 9, "name": "decommission-source", "status": "pending" },
    { "step": 10, "name": "update-profile", "status": "pending" }
  ],
  "sourceHost": "203.0.113.42",
  "targetHost": "198.51.100.7",
  "startedAt": "2026-05-28T14:20:00Z",
  "estimatedCompletionAt": "2026-05-28T14:30:00Z"
}
```

`status` values: `"planning"`, `"running"`, `"completed"`, `"failed"`, `"aborted"`.

| Field            | Value                                                             |
|------------------|-------------------------------------------------------------------|
| Auth             | Localhost only                                                    |
| Async            | No — instant response                                             |

---

### POST /api/vps/migrate/abort

Abort an in-progress migration.

**Request:**

```json
{
  "confirm": true,
  "force": false
}
```

**Response (200 OK):**

```json
{
  "taskId": "c3d4e5f6-a7b8-4c9d-0e1f-2a3b4c5d6e7f",
  "status": "aborted",
  "abortStep": 5,
  "abortStepName": "import-state",
  "rollbackActions": [
    "Stopped import on target",
    "Target compose stopped",
    "Source VPS unchanged"
  ],
  "message": "Migration aborted before DNS cutover. Source VPS is unaffected."
}
```

If `force` is `true`, the abort proceeds even after DNS cutover (step 7),
triggering a DNS revert. Default (`force: false`) rejects abort requests
after step 7 has completed.

| Field            | Value                                                             |
|------------------|-------------------------------------------------------------------|
| Auth             | Localhost only                                                    |
| Async            | Partial — abort is acknowledged immediately, cleanup continues   |
| Failure handling | If abort itself fails, log critical error for manual recovery     |

---

### POST /api/state/export

Trigger a state bundle export.

**Request:**

```json
{
  "outputPath": "/tmp/unet-export.bundle",
  "passphrase": "strong-passphrase-here",
  "s3Sync": {
    "enabled": true,
    "endpoint": "https://s3.us-east-1.amazonaws.com",
    "bucket": "my-unet-backups",
    "region": "us-east-1",
    "prefix": "backups/"
  }
}
```

`s3Sync` is optional. When enabled, the bundle is uploaded to the specified
S3-compatible bucket after local creation.

**Response (202 Accepted):**

```json
{
  "taskId": "d4e5f6a7-b8c9-4d0e-1f2a-3b4c5d6e7f8a",
  "status": "running",
  "startedAt": "2026-05-28T14:25:00Z"
}
```

Completion can be polled via `GET /api/vps/lifecycle` (`activeTask` field).

| Field            | Value                                                             |
|------------------|-------------------------------------------------------------------|
| Auth             | Localhost only                                                    |
| Async            | Yes — export may take several seconds for large state             |
| Failure handling | Task stores error; partial bundle is cleaned up                   |

---

### POST /api/state/import

Trigger a state bundle import.

**Request:**

```json
{
  "bundlePath": "/tmp/unet-export.bundle",
  "passphrase": "strong-passphrase-here"
}
```

**Response (202 Accepted):**

```json
{
  "taskId": "e5f6a7b8-c9d0-4e1f-2a3b-4c5d6e7f8a9b",
  "status": "running",
  "startedAt": "2026-05-28T14:26:00Z",
  "manifestPreview": {
    "version": "1.0.0",
    "sourceHost": "203.0.113.42",
    "peerCount": 12,
    "payloadSizeBytes": 524288
  }
}
```

The import validates the manifest, decrypts the payload, reconciles state, and
reloads the daemon.

| Field            | Value                                                             |
|------------------|-------------------------------------------------------------------|
| Auth             | Localhost only                                                    |
| Async            | Yes                                                               |
| Failure handling | Bad passphrase → 400; corrupt bundle → 400; daemon reload failure → task error |

---

### GET /api/health/probe

Latest health snapshot of the connected VPS.

**Response (200 OK):**

```json
{
  "status": "healthy",
  "checkedAt": "2026-05-28T14:30:00Z",
  "vps": {
    "host": "203.0.113.42",
    "classification": "current",
    "uptimeSeconds": 864000
  },
  "wireguard": {
    "interface": "awg0",
    "listenPort": 51820,
    "publicKey": "abc123...",
    "peerCount": 12,
    "activeHandshakes": 8,
    "totalRxBytes": 1073741824,
    "totalTxBytes": 2147483648
  },
  "docker": {
    "containersRunning": 2,
    "imagesPulled": 1,
    "diskUsageBytes": 536870912
  },
  "daemon": {
    "version": "0.3.0",
    "pid": 12345,
    "uptimeSeconds": 3600,
    "lastConfigReloadAt": "2026-05-28T13:00:00Z"
  },
  "system": {
    "cpuUsagePercent": 5.2,
    "memoryUsagePercent": 34.1,
    "diskFreeBytes": 10737418240,
    "loadAvg": [0.5, 0.4, 0.3]
  }
}
```

`status` values: `"healthy"`, `"degraded"`, `"unhealthy"`, `"unknown"`.

| Field            | Value                                                             |
|------------------|-------------------------------------------------------------------|
| Auth             | Localhost only                                                    |
| Async            | No — returns cached snapshot (refreshed every 30 s)              |

---

## Remote API Equivalents (/v1/ prefix)

All remote endpoints require the authentication and authorization model defined
in spec 002 (API key + scope). The request/response bodies are identical to the
localhost equivalents above.

| Endpoint                          | Scope    | Notes                                            |
|-----------------------------------|----------|--------------------------------------------------|
| `POST /v1/vps/bootstrap`          | admin    | Async; returns taskId                            |
| `POST /v1/vps/attach`             | write    | Synchronous classification                       |
| `GET  /v1/vps/lifecycle`          | read     | Read-only status                                 |
| `POST /v1/vps/rollback`           | admin    | Async; requires confirm                          |
| `POST /v1/vps/migrate`            | admin    | Async; returns taskId + step plan                |
| `GET  /v1/vps/migrate`            | read     | Read-only migration status                       |
| `POST /v1/vps/migrate/abort`      | admin    | Requires confirm; force flag for post-DNS abort  |
| `POST /v1/state/export`           | admin    | Async; S3 params for remote backup storage       |
| `POST /v1/state/import`           | admin    | Async; bundle must be pre-uploaded to server     |
| `GET  /v1/health/probe`           | read     | Read-only health snapshot                        |

### Key differences from localhost API

1. **Authentication:** All remote endpoints require a valid API key in the
   `Authorization: Bearer <key>` header.

2. **State import bundle delivery:** On localhost, `bundlePath` points to a
   local file. On the remote API, the bundle must be uploaded first via a
   separate upload mechanism (e.g., `POST /v1/state/upload` returning a
   `bundleRef` that replaces `bundlePath` in the import request). The exact
   upload mechanism is deferred to the spec 002 OpenAPI extension.

3. **S3 credentials:** On localhost, S3 credentials may be read from the
   daemon's environment or config file. On the remote API, explicit credentials
   or a pre-configured storage profile must be provided in the request.

4. **Rate limiting:** Remote endpoints are subject to the rate limiting defined
   in spec 002. Admin-scoped mutation endpoints (bootstrap, migrate, rollback)
   have stricter limits (e.g., 5 requests/minute).

---

## Task Lifecycle

All async endpoints return a `taskId`. Tasks follow a consistent lifecycle:

```
pending → running → completed
                 ↘ failed
                 ↘ aborted
```

Task progress is observable via:
- `GET /api/vps/lifecycle` — `activeTask` field
- `GET /api/vps/migrate` — detailed step-by-step for migration tasks
- `GET /v1/vps/lifecycle` — remote equivalent

Tasks are retained for 24 hours after completion for debugging, then pruned.

---

## Error Response Format

All endpoints use a consistent error envelope:

```json
{
  "error": {
    "code": "VPS_BOOTSTRAP_PREFLIGHT_FAILED",
    "message": "Preflight check failed: unsupported architecture 'armv7l'",
    "details": {
      "step": "1.1",
      "command": "uname -m",
      "output": "armv7l",
      "expected": "x86_64 or aarch64"
    }
  }
}
```

Common error codes:

| Code                              | HTTP | Meaning                                        |
|-----------------------------------|------|------------------------------------------------|
| `VPS_SSH_UNREACHABLE`             | 502  | Cannot establish SSH connection                |
| `VPS_BOOTSTRAP_PREFLIGHT_FAILED`  | 422  | Preflight check failed (arch, OS, disk, sudo)  |
| `VPS_BOOTSTRAP_DOCKER_FAILED`     | 502  | Docker installation failed                     |
| `VPS_BOOTSTRAP_COMPOSE_FAILED`    | 502  | Compose deploy failed                          |
| `VPS_CLASSIFICATION_CONFLICT`     | 409  | Concurrent daemon already managing this VPS     |
| `VPS_MIGRATION_IN_PROGRESS`       | 409  | Migration already running                      |
| `VPS_MIGRATION_ABORT_TOO_LATE`    | 409  | Cannot abort after DNS cutover (use force)     |
| `VPS_SNAPSHOT_NOT_FOUND`          | 404  | Referenced snapshot does not exist             |
| `STATE_EXPORT_FAILED`             | 500  | Export process failed                          |
| `STATE_IMPORT_BAD_PASSPHRASE`     | 400  | Decryption failed — wrong passphrase           |
| `STATE_IMPORT_CORRUPT_BUNDLE`     | 400  | Bundle integrity check failed                  |
| `STATE_IMPORT_VALIDATION_FAILED`  | 422  | Bundle content failed schema validation        |
| `TASK_NOT_FOUND`                  | 404  | Referenced taskId does not exist               |
