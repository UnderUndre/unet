# Implementation Plan: Unet Core Architecture

**Branch**: `001-init` | **Date**: 2026-05-14 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/001-init/spec.md`

## Summary

The core implementation involves building a cross-platform Go-based local daemon with an embedded React frontend. The Go application will establish an SSH connection to a remote VPS to provision a custom-built AmneziaWG Docker image (Alpine 3.19 + amneziawg-tools + amneziawg-go) with a persistent named volume for state, and a Caddy container with the `caddy-dns/cloudflare` plugin. It will then configure a local AmneziaWG tunnel using the `awg-quick` CLI — propagating the full obfuscation parameter set (Jc/Jmin/Jmax, S1–S4, H1–H4, I1–I5) fetched from the server via SSH + `docker exec` — and dynamically update Caddy's routing rules via the admin REST API (IP-bound to the WG-internal address, optionally mTLS-secured) to expose local ports securely over the internet.

Peer management on the server uses `awg syncconf <iface> <(awg-quick strip <conf>)` for hot-reload without dropping active sessions. See **appendix-peer-add-flow.md** for concrete SSH+docker exec command sequences.

## Technical Context

**Language/Version**: Go 1.22+, TypeScript/React 18+  
**Primary Dependencies**: `os/exec` via `awg-quick` CLI (AmneziaWG tunnel management), `golang.org/x/crypto/ssh` (Server provisioning), `embed` (Go 1.16+ built-in), Vite (React build)  
**Storage**: Local JSON configuration file (`~/.unet/config.json`, atomic writes)  
**Testing**: `go test`, Jest/Vitest for frontend  
**Target Platform**: Linux, macOS, Windows  
**Project Type**: Daemon application with embedded web UI  
**Performance Goals**: <5s for server provisioning checks, <200ms for Caddy API updates  
**Constraints**: Must run as Administrator/root to modify network interfaces. AmneziaWG client (`awg-quick`) must be installed on the local machine.  
**Scale/Scope**: Single user per VPS, targeting small developer teams  

## Constitution Check

**Constitution**: [`.specify/memory/constitution.md`](../../.specify/memory/constitution.md) v1.4.0

| Principle | Status | Notes |
|-----------|--------|-------|
| I — Spec-First Development | ✅ COMPLIANT | Feature completed `/speckit.specify → clarify → plan → tasks` pipeline before implementation. |
| II — Atomicity (WRAP) | ✅ COMPLIANT | Each task is individually atomic; no task bundles refactor + feature. |
| III — Secrets Discipline | ⚠ PARTIAL | File perms + atomic writes ✅. Log-redaction and API-response masking codified in FR-011 (extended). |
| IV — Type Safety & Error Discipline | ✅ IMPLEMENTATION-LEVEL | Enforced at code-review time; no spec-level violations. |
| V — Source-of-Truth: `.claude/` | N/A | Feature is the Go daemon, not `.claude/` template content. |
| VI — Cross-AI Review Gate | 🔄 IN PROGRESS | `analyze.md` PASS (this run). ≥2 external reviewer PASS required before `/speckit.implement`. |
| VII — Artifact Versioning | ⏸ DEFERRED | `snapshot-stage.{sh,ps1}` not yet implemented (TODO_SNAPSHOT_SCRIPT). Manual `git tag` encouraged. |

## Project Structure

### Documentation (this feature)

```text
specs/001-init/
├── plan.md                          # This file
├── research.md                      # Phase 0 output (incl. Dockerfile + full param table)
├── data-model.md                    # Phase 1 output
├── quickstart.md                    # Phase 1 output
├── appendix-peer-add-flow.md        # Concrete SSH + docker exec command sequences for T013a/b
├── contracts/                       # Phase 1 output (REST API, Caddy Configs)
└── reviews/                         # Cross-AI review verdicts (analyze.md, <provider>.md, …)
```

### Source Code (repository root)

```text
src/
├── cmd/
│   └── unet/            # Main entrypoint
├── internal/
│   ├── config/          # Local JSON config management (atomic writes)
│   ├── daemon/          # HTTP server for the embedded UI API
│   ├── dns/             # Cloudflare API client + DNS mode selector
│   ├── provisioner/     # SSH and Docker-compose logic
│   │   ├── compose.go       # docker-compose.yml generator (T008)
│   │   ├── dockerfile.go    # Dockerfile + start.sh + iptables rules (T008b/c)
│   │   ├── firewall.go      # UFW detection + rule provisioning (T008d)
│   │   ├── setup.go         # Idempotent provisioning orchestrator (T009)
│   │   └── awg_init.go      # Server-side AWG initial config + key generation (T009b)
│   ├── tunnel/          # awg-quick integration (.conf generation + exec)
│   │   ├── awg.go            # awg-quick CLI wrapper (T012)
│   │   ├── server_config.go  # SSH+docker exec server-config parser (T013a)
│   │   ├── peer.go           # Peer add/remove via SSH + awg syncconf (T013b)
│   │   └── manager.go        # Tunnel lifecycle orchestrator + watchdog (T013)
│   └── proxy/           # Caddy REST API client (mutex-guarded)
│       ├── caddy.go          # Caddy admin API client (T016)
│       └── caddy_mtls.go     # mTLS bootstrap for Caddy admin (T016d)
└── web/
    ├── src/             # React frontend source
    ├── dist/            # Vite output (embedded into Go)
    ├── package.json
    └── vite.config.ts
```

**Structure Decision**: Using standard Go project layout (`cmd`, `internal`) alongside a `web` directory containing the React frontend. The Vite build output in `web/dist` will be embedded using `go:embed`. Tunnel management uses `awg-quick` CLI (not `wgctrl`) for AmneziaWG DPI-bypass compatibility. DNS management via Cloudflare API in `internal/dns/`.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Embedded Frontend | Seamless cross-platform GUI distribution | Separate Electron/Tauri app is too heavy; pure CLI lacks ease of use for exposing ports dynamically. |
