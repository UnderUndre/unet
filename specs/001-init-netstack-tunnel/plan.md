# Implementation Plan: 001-init-netstack-tunnel (unet)

**Branch**: `001-init-netstack-tunnel` | **Date**: 2026-07-18 | **Spec**: `spec.md` (same folder)
**Input**: Rebased `unet` onto `amneziawg-go` with embedded `gVisor netstack` and in-process SOCKS5/HTTP proxy.

## Summary

Deliver a single standalone Go binary that embeds `amneziawg-go` and `tun/netstack` to provide a rootless, user-space WireGuard-like tunnel with:

- Local SOCKS5/HTTP proxy on `127.0.0.1:1080` (auth optional)
- Yamux-multiplexed reverse port forwarding carried inside the AmneziaWG stream
- Explicit gateway/Caddy counterpart contract for mapping public requests to client yamux streams
- Auto-generated WireGuard keypair + local JSON config provisioning
- Auto-reconnect behavior with exponential backoff + jitter, finite terminal give-up by default
Primary acceptance: binary runs rootless on Windows/macOS/Linux, meets router-class resource envelope (idle ≤64MB RSS) and acceptable throughput (~≥50% native WG).

## Technical Context

**Language/Version**: Go (1.25.0 floor — `go.mod` of the inherited amneziawg-go base requires it; zcode F12).  
**Primary Dependencies**: `amneziawg-go` (local), `gVisor netstack` (`tun/netstack`), `hashicorp/yamux`.  
**Storage**: Local JSON config for WireGuard keys and peer coordinates (path via `--config` or env).  
**Testing**: `go test` (unit), integration harness that exercises loopback tunnel and yamux; benchmark harness for throughput/memory.  
**Target Platform**: Windows, macOS, Linux (user-space only).  
**Project Type**: CLI/daemon (single binary).  
**Performance Goals**: Idle ≤ 64 MB RSS; sustain data-plane throughput ≥ 50% of native WireGuard.  
**Constraints**: Rootless operation (no kernel driver), single long-lived outbound TCP session for reverse forwarding, loopback trust boundary for proxy.

## Constitution Check

- Gate: Feature spec approved; plan proceeds. Re-check after Phase 1 design artifacts are generated.
- Phase 1 design artifacts now present in this folder: `research.md`, `data-model.md`, `contracts/`, `quickstart.md` (constitution deliverable gate satisfied).

## Project Structure (selected)

Source layout (repository `repos/unet`):

```text
repos/unet/
├── cmd/unet/            # main binary
├── device/              # amneziawg transport code
├── tun/netstack/        # gVisor netstack integration
├── proxy/               # SOCKS5/HTTP server
├── yamux/               # yamux wrapper & reverse-forward logic
├── internal/config/     # JSON config handling & provisioning helpers
├── internal/cli/        # flags and env parsing
└── test/                # integration & benchmark harness
```

**Structure Decision**: Single repository binary. Embed `tun/netstack` as an internal package compiled into the binary.

**Repo-layout disposition (zcode F12)**: the inherited root `main.go` / `main_windows.go` / `Makefile` / `Dockerfile*` build the upstream-compatible `amneziawg-go` daemon and STAY untouched; the unet CLI is NEW code under `cmd/unet/` (quickstart build path). `outline/` stays (Outline SDK transport experiments, upstream glue #106) — out of 001 scope, evaluated separately later.

## Phase 0 — Research (short)

- Validate `gVisor netstack` compatibility with toolchain and Windows cross-compile.
- Benchmark native WireGuard vs `tun/netstack` loopback throughput to set tuning knobs; establish the documented **basis for the ≤64MB idle / ≥50% throughput budgets (SC-004)** and the concurrency envelope (SC-005) — including nested-encapsulation MTU/MSS effects (FR-010).
- Confirm `yamux` behavior under many concurrent streams and measure per-stream memory (SC-005 per-stream ceiling).
- Decide gateway authorization model: public-key-only + port allow-list vs per-tunnel capability token (FR-006.001/.002).
- Decide in-tunnel DNS strategy (remote-side resolution vs tunnel-side resolver), the anti-leak default, and fail-closed behavior (FR-009).

Deliverable: `research.md` (Phase 0 decisions + benchmark methodology + tuned defaults; estimates flagged where a benchmark is pending).

## Phase 1 — Design & Implementation Plan

Milestones (high level):

1. Integration skeleton — embed `amneziawg-go` and `tun/netstack`, add config parsing, basic connect flow. (2 dev days)
2. Local proxy — implement SOCKS5/HTTP listener (`CONNECT` + absolute-form requests), config flag for optional auth, anti-DNS-leak unit tests. (1.5 dev days)
3. Yamux reverse-forward — open long-lived TCP session over tunnel-internal address, multiplex control/data channels, enforce allow-list, and provide gateway/Caddy integration fixture/contract. (2 dev days)
4. Key provisioning — auto-generate WireGuard keypair on first run, allow env/secret references for secret-bearing fields, JSON config writer. (1 dev day)
5. Reconnect & resilience — exponential backoff + jitter, graceful stream failures, listener persistence. (1 day)
6. Benchmark & memory tuning — instrument and optimize for ≤64MB idle RSS. (1.5 dev days)
7. Integration tests & CI — integration harness, release build. (2 days)

**Estimate (rough)**: 11–12 developer-days (parallelize proxy + yamux + skeleton where possible).

## Deliverables (Phase 1)

- `plan.md`, `research.md`, `data-model.md`, `quickstart.md`, `contracts/` (config JSON Schema + control-frame/SOCKS5 interface contract), `tasks.md`.
- Binary artifacts for Linux, macOS, Windows (CI cross-compile).
- Integration and benchmark reports.

## Risks & Mitigations

- Cross-platform compile or gVisor Windows compatibility risk — mitigation: early CI smoke with cross-compile; fallback: document Windows limitations in v1.
- Memory overhead from netstack — mitigation: tune GC flags, minimize buffers, and measure early in Phase 0.

## Next Steps (for `/speckit.tasks`)

- Generate `tasks.md` with fine-grained tickets, owners, estimates, agent dispatch plan (test/integration/release), and critical path.
