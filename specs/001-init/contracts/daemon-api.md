# Daemon REST API Contract

Local daemon API served at `http://localhost:<PORT>/api/*` (default port 8080).

**Authentication**: None (localhost only). Any local process can access. Documented as accepted risk for MVP.

**Content-Type**: `application/json` for all request/response bodies.

---

## Status & Health

### `GET /api/status`

Returns aggregate system status.

**Response 200:**

```json
{
  "privileged": true,
  "vps": {
    "configured": true,
    "provisioned": true,
    "host": "1.2.3.4"
  },
  "tunnel": {
    "status": "connected",
    "localIp": "10.8.1.2",
    "serverIp": "10.8.1.1"
  },
  "ports": [
    {
      "id": "uuid-1",
      "localPort": 3000,
      "subdomain": "app.mydomain.com",
      "status": "active"
    }
  ],
  "daemonPort": 8080
}
```

---

## VPS Management

### `POST /api/vps/configure`

Save VPS credentials and trigger provisioning.

**Request:**

```json
{
  "host": "1.2.3.4",
  "sshPort": 22,
  "username": "root",
  "authMode": "key",
  "privateKeyPath": "/home/user/.ssh/id_rsa",
  "password": ""
}
```

`authMode`: `"key"` or `"password"`. When `"key"`, `privateKeyPath` is required. When `"password"`, `password` is required.

**Response 202:**

```json
{
  "taskId": "provision-abc123",
  "status": "provisioning"
}
```

**Response 400:**

```json
{
  "error": "invalid_credentials",
  "message": "privateKeyPath does not exist: /foo/bar"
}
```

### `GET /api/vps/status`

**Response 200:**

```json
{
  "configured": true,
  "provisioned": true,
  "host": "1.2.3.4",
  "lastProvisionAt": "2026-05-15T10:00:00Z"
}
```

---

## Tunnel Management

### `POST /api/tunnel/connect`

Start the WireGuard tunnel via `awg-quick`.

**Response 202:**

```json
{
  "status": "connecting"
}
```

**Response 409:**

```json
{
  "error": "already_connected",
  "message": "Tunnel is already in connected state"
}
```

**Response 503:**

```json
{
  "error": "not_privileged",
  "message": "Administrator/root privileges required to manage network interfaces"
}
```

### `POST /api/tunnel/disconnect`

Stop the WireGuard tunnel.

**Response 200:**

```json
{
  "status": "disconnected"
}
```

### `GET /api/tunnel/status`

**Response 200:**

```json
{
  "status": "connected",
  "localIp": "10.8.1.2",
  "serverIp": "10.8.1.1",
  "serverEndpoint": "1.2.3.4:31075",
  "connectedAt": "2026-05-15T10:05:00Z"
}
```

`status` enum: `"disconnected"`, `"connecting"`, `"connected"`, `"error"`.

---

## Port Exposure

### `GET /api/ports`

**Response 200:**

```json
[
  {
    "id": "uuid-1",
    "localPort": 3000,
    "subdomain": "app.mydomain.com",
    "status": "active"
  }
]
```

### `POST /api/ports`

Expose a local port publicly.

**Request:**

```json
{
  "localPort": 3000,
  "subdomain": "app.mydomain.com"
}
```

**Response 201:**

```json
{
  "id": "uuid-1",
  "localPort": 3000,
  "subdomain": "app.mydomain.com",
  "status": "active"
}
```

**Response 400 (subdomain format):**

```json
{
  "error": "invalid_subdomain",
  "message": "subdomain must match: *.domain.com and contain only a-z, 0-9, hyphens"
}
```

**Response 400 (subdomain depth — Cloudflare mode only):**

```json
{
  "error": "invalid_subdomain_depth",
  "message": "Cloudflare mode uses a wildcard certificate (*.mydomain.com) which covers only single-label subdomains. 'app.dev.mydomain.com' has 2 labels under baseDomain — switch to manual DNS mode for multi-level subdomains, or use 'app-dev.mydomain.com'.",
  "context": {
    "subdomain": "app.dev.mydomain.com",
    "baseDomain": "mydomain.com",
    "labelsUnderBase": 2,
    "maxAllowedInCloudflareMode": 1,
    "remediation": "rename | switch_dns_mode"
  }
}
```

Returned when `dns.mode == "cloudflare"` AND the proposed subdomain has more than one label between the leftmost dot and `dns.baseDomain` (per FR-009 single-label constraint).

**Response 409:**

```json
{
  "error": "subdomain_conflict",
  "message": "app.mydomain.com is already exposed on port 3000"
}
```

**Response 412:**

```json
{
  "error": "tunnel_not_connected",
  "message": "Cannot expose ports without an active tunnel"
}
```

### `DELETE /api/ports/:id`

**Response 200:**

```json
{
  "id": "uuid-1",
  "status": "removed"
}
```

**Response 404:**

```json
{
  "error": "not_found",
  "message": "No exposed port with id: uuid-1"
}
```

---

## Configuration

### `GET /api/config`

Returns current configuration (secrets masked).

**Response 200:**

```json
{
  "dns": {
    "mode": "cloudflare",
    "baseDomain": "mydomain.com",
    "cloudflareToken": "****masked"
  }
}
```

### `PUT /api/config/dns`

Set DNS configuration mode.

**Request (Cloudflare mode):**

```json
{
  "mode": "cloudflare",
  "baseDomain": "mydomain.com",
  "cloudflareToken": "cf-api-token-here"
}
```

**Request (Manual wildcard mode):**

```json
{
  "mode": "manual",
  "baseDomain": "mydomain.com"
}
```

**Response 200:**

```json
{
  "mode": "cloudflare",
  "baseDomain": "mydomain.com"
}
```
