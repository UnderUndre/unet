# Bootstrap Protocol

Sequence of SSH commands the Bootstrapper runs on a clean VPS to deploy the unet
daemon stack. Every command is idempotent — re-running the full sequence on an
already-bootstrapped host must leave it in the same working state.

---

## Phase 1 — Preflight

### 1.1 Verify architecture

```
uname -m
```

| Field              | Value                         |
|--------------------|-------------------------------|
| Expected output    | `x86_64` or `aarch64`         |
| Failure handling   | Abort. Unsupported arch.      |
| Idempotency        | Read-only; safe to re-run.    |

### 1.2 Verify OS

```
cat /etc/os-release
```

| Field              | Value                                                    |
|--------------------|----------------------------------------------------------|
| Expected output    | Lines containing `VERSION_ID="22.04"` or `VERSION_ID="24.04"` and `ID=ubuntu` |
| Failure handling   | Abort if not Ubuntu 22.04/24.04. Warn on untested minor. |
| Idempotency        | Read-only.                                                |

### 1.3 Check disk space

```
df -h /
```

| Field              | Value                                                         |
|--------------------|---------------------------------------------------------------|
| Expected output    | `Avail` column ≥ 2 GB                                        |
| Failure handling   | Abort. Insufficient disk. User must resize or clean volume.   |
| Idempotency        | Read-only.                                                    |

### 1.4 Verify passwordless sudo

```
sudo -n true
```

| Field              | Value                                                       |
|--------------------|-------------------------------------------------------------|
| Expected output    | Exit code 0 (no output).                                    |
| Failure handling   | Abort. SSH user lacks NOPASSWD sudo. Guide user to visudo.  |
| Idempotency        | Side-effect-free check.                                     |

---

## Phase 2 — Docker Install (Idempotent)

### 2.1 Check if Docker is already installed

```
command -v docker
```

| Field              | Value                                                        |
|--------------------|--------------------------------------------------------------|
| Expected output    | Path such as `/usr/bin/docker`, or exit code 1 if absent.    |
| Failure handling   | Non-zero exit → proceed to install.                          |
| Idempotency        | Only checks; does not modify anything.                       |

### 2.2 Install Docker (conditional)

Only executed if `command -v docker` returned non-zero.

```
curl -fsSL https://get.docker.com | sh
```

| Field              | Value                                                        |
|--------------------|--------------------------------------------------------------|
| Expected output    | Final line: `Docker installation successful` or similar.     |
| Failure handling   | Abort. Log full output. Rollback: none needed (nothing to remove yet). |
| Idempotency        | `get.docker.com` script is itself idempotent — skips if Docker present. |

### 2.3 Verify Docker Compose plugin

```
docker compose version
```

| Field              | Value                                                        |
|--------------------|--------------------------------------------------------------|
| Expected output    | `Docker Compose version v2.Y.Z`                              |
| Failure handling   | If missing, attempt: `sudo apt-get install -y docker-compose-plugin`. Abort if that fails. |
| Idempotency        | Read-only check.                                             |

---

## Phase 3 — Compose Stack Deploy

### 3.1 Create deployment directory

```
sudo mkdir -p /opt/unet
```

| Field              | Value                                                        |
|--------------------|--------------------------------------------------------------|
| Expected output    | Exit code 0 (directory exists or is created).                |
| Failure handling   | Abort if cannot create.                                      |
| Idempotency        | `mkdir -p` is inherently idempotent.                         |

### 3.2 Write version file

```
sudo tee /opt/unet/version > /dev/null <<'EOF'
<DAEMON_SEMVER>
EOF
```

`<DAEMON_SEMVER>` is replaced at render time (e.g. `0.3.0`).

| Field              | Value                                                        |
|--------------------|--------------------------------------------------------------|
| Expected output    | Exit code 0. File `/opt/unet/version` contains semver.       |
| Failure handling   | Abort. Cannot write — check permissions.                     |
| Idempotency        | Overwrites with same value; no side effects.                 |

### 3.3 Render docker-compose.yml

```
sudo tee /opt/unet/docker-compose.yml > /dev/null <<'COMPOSEOF'
<COMPOSE_CONTENT>
COMPOSEOF
```

`<COMPOSE_CONTENT>` is the embedded template rendered with:
- Image tag matching daemon version.
- Volume mounts for `/opt/unet/data`.
- Port mapping for WireGuard endpoint + daemon HTTP API.

| Field              | Value                                                        |
|--------------------|--------------------------------------------------------------|
| Expected output    | Exit code 0. YAML file written.                              |
| Failure handling   | Abort. Log write error.                                      |
| Idempotency        | Overwrites; compose handles the rest.                        |

### 3.4 Write Dockerfile (if needed, local build path)

```
sudo tee /opt/unet/Dockerfile > /dev/null <<'DOCKEREOF'
<DOCKERFILE_CONTENT>
DOCKEREOF
```

Only written when no pre-built registry image is available (development builds).

| Field              | Value                                                        |
|--------------------|--------------------------------------------------------------|
| Expected output    | Exit code 0.                                                 |
| Failure handling   | Abort.                                                       |
| Idempotency        | Overwrites same content.                                     |

### 3.5 Pull / build images

With registry image:

```
cd /opt/unet && sudo docker compose pull
```

Without registry (local build):

```
cd /opt/unet && sudo docker compose build
```

| Field              | Value                                                        |
|--------------------|--------------------------------------------------------------|
| Expected output    | `Pulled` / `Built` confirmation per service.                 |
| Failure handling   | Retry once. Abort on second failure.                         |
| Idempotency        | `pull` downloads newer layers only; `build` uses Docker cache. |

### 3.6 Start stack

```
cd /opt/unet && sudo docker compose up -d
```

| Field              | Value                                                        |
|--------------------|--------------------------------------------------------------|
| Expected output    | `Container unet Started` or similar.                         |
| Failure handling   | Proceed to Phase 4 health check; if that fails, trigger rollback. |
| Idempotency        | `up -d` reconciles declared state — restarts only if config changed. |

---

## Phase 4 — Health Verification

### 4.1 Poll containers running

```
sudo docker ps --filter name=unet --format '{{.Status}}'
```

Poll every 5 s, max 120 s (24 attempts).

| Field              | Value                                                        |
|--------------------|--------------------------------------------------------------|
| Expected output    | `Up <duration>` for all unet containers.                     |
| Failure handling   | After 120 s, trigger rollback.                               |
| Idempotency        | Read-only.                                                   |

### 4.2 Verify awg0 interface exists inside container

```
sudo docker exec unet ip -br link show awg0
```

| Field              | Value                                                        |
|--------------------|--------------------------------------------------------------|
| Expected output    | `awg0 ... <LOWER_UP or UNKNOWN>`                             |
| Failure handling   | Daemon may need a moment to create interface. Retry up to 30 s. Then rollback. |
| Idempotency        | Read-only.                                                   |

### 4.3 Read server public key and endpoint port

```
sudo docker exec unet cat /etc/unet/server.json
```

| Field              | Value                                                        |
|--------------------|--------------------------------------------------------------|
| Expected output    | JSON with `publicKey` and `listenPort` fields.               |
| Failure handling   | File missing → daemon not initialised. Rollback.             |
| Idempotency        | Read-only.                                                   |

### 4.4 Return connection params

The bootstrapper collects and returns to the daemon:

```json
{
  "host": "<VPS_IP>",
  "port": <listenPort>,
  "publicKey": "<server_public_key>",
  "version": "<DAEMON_SEMVER>"
}
```

---

## Rollback Procedure (on any failure after Phase 2)

Executed in reverse order to leave the host clean:

```
cd /opt/unet && sudo docker compose down --remove-orphans 2>/dev/null
sudo rm -rf /opt/unet/*
```

Docker itself is left installed — it is a generally useful dependency and its
removal could surprise operators.

Rollback does **not** run if the failure occurred in Phase 1 (preflight),
because no modifications have been made.

---

## Summary of Idempotency Guarantees

| Phase     | Safe to re-run? | Reason                                        |
|-----------|-----------------|-----------------------------------------------|
| Preflight | Yes             | Read-only checks.                             |
| Docker    | Yes             | `get.docker.com` + `compose plugin` skip.     |
| Deploy    | Yes             | `tee` overwrites; `up -d` reconciles.         |
| Health    | Yes             | Read-only polls.                              |

The full bootstrap sequence may be safely retried end-to-end without manual
cleanup.
