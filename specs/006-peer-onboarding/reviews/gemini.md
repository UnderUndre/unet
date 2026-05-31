# SpecKit Review: 006-peer-onboarding

**Reviewer**: gemini
**Reviewed at**: 2026-05-31T00:00:00Z
**Commit**: a27390741ccce0778be56e6b2aae2bdb04e69a88
**Artifacts reviewed**: spec.md, plan.md, tasks.md, data-model.md

## Summary

The Peer Onboarding Wizard provides a comprehensive and highly necessary UX improvement for network setup, significantly lowering the barrier to entry. However, the current specification contains a critical networking logic flaw regarding `nip.io` routing that completely breaks the "Expose Port" feature for non-domain users, along with a high-severity resilience risk during the long-running bootstrap phase.

## Findings

| ID | Severity | Area | Finding | Recommendation |
|---|---|---|---|---|
| F1 | CRITICAL | Logical consistency | `FR-009` and `TASK-6.1` specify generating `nip.io` subdomains in the format `<label>.<wg-client-ip-dashed>.nip.io` (e.g., `app.10-8-0-2.nip.io`). Because `nip.io` resolves to the first IP address found in the domain string, this resolves to the private WireGuard client IP (`10.8.0.2`). Public internet traffic will never reach the VPS Caddy reverse proxy because it is routing to a private IP. This breaks User Story 3 for `nip.io` users. | The `nip.io` subdomain MUST encode the **VPS public IP**, not the WireGuard client IP. The format should be `<label>.<vps-public-ip-dashed>.nip.io`. Caddy can then use its internal mapping to route that specific label to the correct `10.8.0.x` WireGuard client IP. |
| F2 | HIGH | Failure modes | `TASK-6.2` orchestrates the 2-5 minute `bootstrap.Bootstrap(ctx)` operation synchronously within the `POST /v1/wizard/sessions/{id}/commit` HTTP handler. If the HTTP request's context (`r.Context()`) is passed directly to the bootstrapper, a browser disconnection, page reload, or transient network failure will cancel the context, aborting the SSH/Docker provisioning midway and leaving the VPS in a corrupted state. | The bootstrap orchestrator MUST run in a detached context (e.g., `context.WithoutCancel(req.Context())` or `context.Background()`). The HTTP handler should start the goroutine, then either stream progress via SSE or rely on the frontend to subscribe to the global 005 log stream independently. |
| F3 | HIGH | Logical consistency | `TASK-5.4` specifies a security rule for short-code invites: "After 20 failed attempts per code, invalidate." However, if a user enters a generic incorrect 8-digit code (e.g., `11111111`) on the landing page, the server hashes it and finds no match in the database. Because the server does not know *which* valid invite the attacker is trying to guess, it is impossible to accrue failed attempts against a specific valid code. | Remove the "20 failed attempts per code" rule. Instead, enforce a strict global or per-IP rate limit for short-code validation attempts (e.g., 5 attempts / IP / minute), optionally paired with an exponential backoff or fail2ban-style temporary IP ban. |
| F4 | MEDIUM | Edge case | `TASK-5.1` generates QR codes as 256x256 PNG images with error correction level `M`. WireGuard configurations can be lengthy (over 500 characters) when including all AmneziaWG obfuscation parameters, IPv6 addresses, and long AllowedIPs lists. Encoding this much data into a 256x256 grid results in very dense, tiny modules that are difficult to scan reliably on lower-quality mobile cameras or glossy screens. | Increase the default QR code PNG size to at least `512x512`. The file size difference is negligible, but the scanning reliability improves drastically. |
| F5 | LOW | Security & privacy threats | Short codes are exactly 8 digits (`10000000-99999999`), yielding ~90 million combinations. While the 24-72h TTL and 5/min IP rate limit provide adequate baseline protection, the entropy is relatively low for an authentication secret. | Consider using an alphanumeric or base58-encoded string for the short code (e.g., `8 characters of base58`), which exponentially increases the entropy space without making it substantially harder for humans to copy/paste. |

## Alternative approaches considered

For the Wizard Commit Phase (`TASK-6.2`), instead of maintaining a 5-minute synchronous HTTP connection, the system could adopt an asynchronous Job pattern. `POST /commit` would immediately return `202 Accepted` with a Job ID. The frontend would then poll `GET /v1/wizard/sessions/{id}` or listen to the SSE event stream to determine when the job transitions from `in_progress` to `success` or `error`. This eliminates all risks associated with dropped HTTP connections and aligns better with standard long-running operation patterns.

## VERDICT

```yaml
verdict: CRITICAL
reviewer: gemini
reviewed_at: 2026-05-31T00:00:00Z
commit: a27390741ccce0778be56e6b2aae2bdb04e69a88
critical_count: 1
high_count: 2
medium_count: 1
low_count: 1
```