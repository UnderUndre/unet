# unet Architecture

**Version**: 0.5.0
**Updated**: 2026-06-01
**Status**: Living document — tracks implementation progress across specs.

## Product Positioning

**unet** is a self-hosted ngrok/Tailscale alternative built on AmneziaWG + Caddy. It provides secure tunnel connectivity from local machines to a VPS, with DNS-terminated HTTPS ingress for exposing local services publicly.

- **License**: MIT (OSS)
- **Target user**: Solo developer or small team who wants ngrok-like convenience without trusting a third party with their traffic.
- **Planned enterprise tier**: Multi-user management, device count limits, team-based access controls, and premium features. The control plane API is the architectural foundation for this split.

## Layered Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        ADMIN SURFACE                             │
│              Embedded React UI (Go binary, localhost:8080)       │
├──────────────────────────────────────────────────────────────────┤
│                        CONTROL PLANE                             │
│        Remote HTTP API (network-accessible at :8443)             │
│        Authenticated, TLS, routes: /v1/*                         │
├──────────────────────────────────────────────────────────────────┤
│                  OPERATIONS (cross-cutting)                      │
│  ┌────────────────────────────┐ ┌────────────────────────────┐  │
│  │  Lifecycle Subsystem       │ │  Observability Subsystem   │  │
│  │  Bootstrap · Attach ·      │ │  Structured logs · SSE ·   │  │
│  │  Detect · Migrate ·        │ │  Prometheus :9090 ·        │  │
│  │  Backup · Health ·         │ │  Container aggregation ·   │  │
│  │  Snapshot                  │ │  Log export                │  │
│  └────────────────────────────┘ └────────────────────────────┘  │
├──────────────────────────────────────────────────────────────────┤
│                         DATA PLANE                               │
│  ┌─────────────┐   ┌─────────────┐   ┌──────────────────────┐   │
│  │ AmneziaWG   │   │   Caddy     │   │ net-pause container  │   │
│  │ Transport   │   │ Ingress     │   │ (network namespace)  │   │
│  │ (awg0, UDP) │   │ (HTTPS/443) │   │ shared netns holder  │   │
│  └──────┬──────┘   └──────┬──────┘   └──────────┬───────────┘   │
│         │                 │                      │               │
│         └────────┬────────┘──────────────────────┘               │
│                  │                                               │
│         Docker Compose on VPS                                    │
└──────────────────────────────────────────────────────────────────┘
```

## Subsystem Overviews

### Data Plane
Handles network traffic. Runs AmneziaWG (tunneling) and Caddy (HTTPS/Ingress) in a shared network namespace on the VPS. 
**Spec**: `specs/001-init/`

### Control Plane
Exposes a network-accessible, authenticated HTTP API for programmatic management (routes, peers, tunnel status). 
**Spec**: `specs/002-api-control-plane/`

### Lifecycle Operations
Cross-cutting subsystem managing the full VPS lifecycle: provisioning, health monitoring, drift detection, migration, and backups.
**Spec**: `specs/003-vps-lifecycle/`

### Observability
Provides structured logging, log streaming (SSE), container log aggregation, and Prometheus metrics for the daemon and its managed infrastructure.
**Spec**: `specs/005-observability/`

## Spec Registry

| Spec | Scope | Status |
|---|---|---|
| `specs/001-init/` | Core data plane, daemon, local UI | Implemented |
| `specs/002-api-control-plane/` | Remote control plane API (routes, auth, audit) | Implemented |
| `specs/003-vps-lifecycle/` | VPS lifecycle, bootstrap, health, backup | Implemented |
| `specs/004-desktop-integration/`| Tray app, OS integration | Implemented |
| `specs/005-observability/` | Structured logging, SSE streaming, Prometheus | Implemented |
| `specs/006-peer-onboarding/` | Onboarding wizard, invite flows | In Progress |

## Technology Stack

| Component | Technology |
|---|---|
| Daemon binary | Go (single binary, embedded React UI) |
| Tunnel transport | AmneziaWG (`awg-quick` CLI) |
| Ingress proxy | Caddy v2 (admin API) |
| Local config | `~/.unet/config.json` (0600) |
| API auth | Hashed API tokens (PAT-style) |
| Structured logs | Go `log/slog` |
| Metrics | Prometheus `client_golang` |
| SSH sessions | Pooled `golang.org/x/crypto/ssh` |
| State encryption | `filippo.io/age` |
