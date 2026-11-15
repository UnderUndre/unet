# Feature Specification: 003-ecosystem-integration (unet × undrlla × undevops)

**Feature Branch**: `specs/003-ecosystem-integration`
**Created**: 2026-11-15
**Status**: Draft (PARKED — see Park Gate below)
**Input**: Requirements relocated from `specs/001-init-netstack-tunnel/spec.md` on 2026-11-15 (spec surgery): FR-011 (compute renting) and FR-012 (IdP entitlements) arrived post-review (2026-07-23 ECOSYSTEM note), had zero coverage in plan/tasks/contracts, and are out of scope for the tunnel engine. Also absorbs grok-001-F1 (undrlla peer provisioning REST).

## Context

`001-init-netstack-tunnel` ships a self-contained tunnel engine driven by a local config file (FR-008) with a gateway contract (FR-006.003/.004) and a revocation endpoint (FR-006.004) whose authorization source is deliberately stubbed to an admin CLI in v1. Ecosystem coupling — billing, identity, compute marketplace — is parked in this spec so the engine can ship with **zero undrlla/undevops dependency**.

## Park Gate (hard precondition)

This spec MUST NOT enter plan/implement until BOTH hold:

1. `001-init-netstack-tunnel` is implemented, verified, and living in production on at least the author's own fleet (VPS + real devices) for some time.
2. At least one paying undrlla client needs managed peer provisioning. If nobody pays for managed tunnels, this spec is deleted, not built.

## Requirements

- **FR-E-001 (Peer provisioning REST, ex grok-001-F1)**: The undrlla control plane MUST expose an authenticated REST surface to: (a) create a peer — returns WG keypair material or accepts a client-generated public key; (b) list devices per subscription with per-plan device limits (ex grok-001-F5); (c) revoke a peer — propagation MUST target the gateway revocation endpoint defined in `001-init/FR-006.004` (`POST /v1/gateway/revoke`); (d) write the resulting client config (FR-008 hand-off: "undrlla or installer writes the config file").
- **FR-E-002 (IdP capability entitlements, ex 001 FR-012)**: In ecosystem-connected mode, peer registrations and subscription capability tokens MUST be authenticated as **RS256 JWT** issued by **Undrlla IdP (`id.undrlla.network`)** carrying the `unet:tunnel` capability scope, validated via public JWKS (`/.well-known/jwks.json`) per `undrlla/specs/005-sso-jwt-contract.md`. In 001 terms this hardens the optional `capability_token` (FR-006.002) from a shared HMAC secret into a signed entitlement.
- **FR-E-003 (Compute & GPU node renting, ex 001 FR-011)**: Optional `--enable-compute` mode: the node registers host compute capabilities (CPU cores, RAM, GPU model / VRAM via `nvidia-smi`/ROCm probe) with the undevops control plane inside the established AmneziaWG tunnel; undevops MAY dispatch isolated Docker workloads (AI inference, LLM hosting, rendering) to the node; payouts for compute uptime and completed tasks MUST be automatically credited via `undrepay` (10-15% platform commission). **NOTE**: this is a separate product hypothesis, not tunnel engineering; it likely belongs to undevops/undrepay specs, and MUST NOT be built inside the unet binary before its own validation.

## Out of Scope

- Everything owned by 001 (the engine owns nothing here); no new transport features; no consumer VPN UX; no mesh/coordination plane (that is a potential future 002, gated on real pull — see the 2026-11-15 review conversation).

## References

- 001 spec: [`../001-init-netstack-tunnel/spec.md`](../001-init-netstack-tunnel/spec.md) — FR-006.002 (capability token), FR-006.003/.004 (gateway contract + revoke), FR-008 (config hand-off)
- grok review: [`../001-init-netstack-tunnel/reviews/grok.md`](../001-init-netstack-tunnel/reviews/grok.md) — F1 (billing REST), F5 (device limits)
- External: `undrlla/specs/005-sso-jwt-contract.md`, `undrlla` flagship FR-043
