# SpecKit Review: 002-api-control-plane

**Reviewer**: gemini
**Reviewed at**: 2026-05-31T00:00:00Z
**Commit**: HEAD
**Artifacts reviewed**: spec.md, plan.md, tasks.md, data-model.md, contracts/

## Summary

The API control plane feature is well-scoped for a single-host MVP, correctly identifying the need to separate the daemon's internal API from an external-facing surface. However, the design harbors a critical local privilege escalation vulnerability and significant risks around bcrypt-induced CPU exhaustion that must be addressed before implementation.

## Findings

| ID | Severity | Area | Finding | Recommendation |
|---|---|---|---|---|
| F1 | CRITICAL | Security | Localhost Auth Bypass: The spec states "Loopback :8443 → unconditional admin scope". On multi-user systems, ANY local user process can hit `127.0.0.1:8443` and gain full admin access to `unet`. | Require token authentication even on localhost, OR use a Unix domain socket with `SO_PEERCRED` for local IPC instead of a TCP port, OR restrict this behavior strictly to single-user environments (but Unix sockets are safer). |
| F2 | HIGH | Security / Performance | CPU Exhaustion (DoS) via bcrypt: The use of bcrypt (cost 12) for token validation is standard, but without pre-auth rate limiting, an attacker can spam requests with invalid tokens, forcing constant bcrypt comparisons and exhausting server CPU. | Implement IP-based rate limiting *before* token hashing, or use a faster hashing algorithm for tokens like SHA-256 (tokens are high-entropy generated secrets, unlike user passwords, so bcrypt is actually unnecessary and detrimental). |
| F3 | HIGH | Security | Revocation Lag: The 5-minute LRU cache for valid tokens implies a revoked token will remain fully authorized for up to 5 minutes after deletion from `config.json`. | Invalidate the cache explicitly when a token is deleted or modified via the API, rather than relying purely on TTL. |
| F4 | HIGH | Edge Case | Mutex Starvation: Plan states "implementación MUST hold mutex across entire alloc+write+sync sequence" for peer creation. If the SSH connection to the VPS hangs or times out during the `awg` config sync, the entire API and daemon will be deadlocked. | Enforce strict, short timeouts on all remote VPS SSH operations called within the mutex, or decouple the local config allocation from the remote sync phase. |
| F5 | MEDIUM | Edge Case | Audit Log Growth: `~/.unet/audit.jsonl` is append-only. Over time, this file will grow unbounded, potentially filling disk space or making parsing slow. | Define a log rotation strategy (e.g., rotate at 10MB) or limit retention. |
| F6 | MEDIUM | Stakeholder | TLS Certificate Trust: Auto-generating a self-signed cert means external tools (like curl, undevops) will reject the connection by default. The spec does not explain how clients obtain or trust this cert. | Specify a CLI command to export the public CA/cert (e.g., `unet cert export`) so external tools can configure their trust stores. |

## Alternative approaches considered

- **Token Hashing**: Since API tokens are cryptographically secure random strings (unlike human passwords), using bcrypt is an anti-pattern. A simple SHA-256 hash is sufficient, completely eliminating the CPU DoS risk and the need for a complex LRU cache, simplifying Phase 1 (Foundation) significantly.
- **Local IPC**: Using a Unix Domain Socket (e.g., `/var/run/unet.sock`) for local daemon communication inherently provides access control via file system permissions and eliminates the need for localhost TCP auth bypass hacks.

## VERDICT

```yaml
verdict: CRITICAL
reviewer: gemini
reviewed_at: 2026-05-31T00:00:00Z
commit: HEAD
critical_count: 1
high_count: 3
medium_count: 2
low_count: 0
```
