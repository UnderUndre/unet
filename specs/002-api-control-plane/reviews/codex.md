# SpecKit Review: 002-api-control-plane

**Reviewer**: codex
**Reviewed at**: 2026-05-31T13:59:09.8499313+03:00
**Commit**: a27390741ccce0778be56e6b2aae2bdb04e69a88
**Artifacts reviewed**: spec.md, plan.md, tasks.md, data-model.md, contracts/api.openapi.yaml, contracts/auth-flows.md, quickstart.md, reviews/analyze.md, reviews/context-for-reviews.md, .specify/memory/constitution.md

## Summary

The feature is well scoped and most behavior is traced through spec, plan, tasks, and contracts. The weak points are all at trust boundaries: unauthenticated bcrypt work can be abused for CPU exhaustion, the loopback auth bypass becomes dangerous behind a local reverse proxy, audit logging is specified as mandatory but implemented as best-effort async, and the OpenAPI security contract is not valid for bearer scopes. This should not proceed to implementation unchanged.

## Findings

| ID | Severity | Area | Finding | Recommendation |
|---|---|---|---|---|
| F1 | HIGH | Security / Performance | Invalid Bearer tokens can force expensive bcrypt work before any rate limit applies. `auth-flows.md:85-90` says every cache miss iterates all stored hashes with bcrypt; `plan.md:297` estimates cost 12 at about 250ms; rate limiting is per token and happens after auth (`plan.md:156`, `auth-flows.md:111-118`, `tasks.md:353-361`). Random invalid tokens have no token ID, so the specified limiter does not protect the CPU path. | Add a pre-auth IP/global limiter and bounded bcrypt worker pool. Prefer token lookup by non-secret selector plus keyed digest/HMAC so only one candidate reaches bcrypt/argon2. Add an invalid-token flood test that proves 401s stay cheap and bounded. |
| F2 | HIGH | Security | Loopback auth bypass becomes an admin bypass if the API is placed behind a local reverse proxy. `auth-flows.md:240-245` grants admin to loopback and `auth-flows.md:275` defers trusted proxy handling, while the remote API defaults to `0.0.0.0:8443` (`spec.md:172-173`, `plan.md:144`). A common Caddy/nginx TLS proxy on the same host would make public traffic arrive from `127.0.0.1`, skipping auth entirely. | Make loopback bypass conditional on the listener itself being loopback-only, or require auth on `:8443` always and keep unauth only on the existing `:8080/api/*` listener. If reverse proxy support is out of scope, explicitly reject/guard proxy deployment and add tests for proxied `RemoteAddr`. |
| F3 | HIGH | Audit / Failure modes | Audit is mandatory in the spec but best-effort in the plan/tasks. `spec.md:181-182` says every state-changing API call MUST be recorded; `spec.md:237` requires entries queryable within 1 second. But `plan.md:213` and `tasks.md:366-375` make audit writes async/non-blocking after success, and task acceptance even excludes loopback mutations. A crash, disk-full, fsync failure, or goroutine failure can leave a successful mutation with no audit entry. | Choose the invariant: either make audit durability part of the mutation success path for remote writes, or weaken FR-016 to best-effort. If kept mandatory, write and fsync audit synchronously before returning 2xx, surface audit write failures, and test disk/error paths. |
| F4 | HIGH | API Contract | The OpenAPI file encodes scopes on an HTTP bearer scheme (`BearerAuth: [read]`, `[write]`, `[admin]`; e.g. `api.openapi.yaml:50`, `82`, `284`, `423`). For non-OAuth/OpenID security schemes, the security requirement array should be empty; scope arrays are not valid machine-readable bearer scopes. Validators/generators may reject this or silently ignore scope requirements. | Change operations to `BearerAuth: []` and add `x-required-scope: read/write/admin` (or a documented extension table). Add an OpenAPI validation task to Phase 6 so contract breakage is caught before clients generate code. |
| F5 | MEDIUM | Auth consistency | JWT session scope requirements conflict across artifacts. `data-model.md:75` says the PAT must be `admin` or `write`; `tasks.md:296` and `api.openapi.yaml:423` allow `read`; `auth-flows.md:134-156` illustrates an admin PAT and admin-scoped JWT for the admin UI. This ambiguity will leak into implementation and tests. | Pick one rule. If `/auth/session` is for admin UI, require `admin`; if it is a general PAT-to-JWT exchange, rename the story and document read/write JWT behavior explicitly. Align data model, OpenAPI, tasks, and auth-flow examples. |
| F6 | MEDIUM | Secrets / Bootstrap | SC-006 says token plaintext is shown once and never again (`spec.md:233`), but bootstrap writes an admin token to a predictable file (`auth-flows.md:45-48`, `tasks.md:249-257`, `quickstart.md`). File mode 0600 helps, but the exception is not called out in the security trade-offs and the token can sit around if setup is abandoned. | Add an explicit bootstrap-token exception and lifecycle: short expiry, delete/rotate on next daemon start or after first use, clear warning in status, and tests for permissions plus cleanup. Better: create the first token via local CLI/loopback flow instead of leaving an admin PAT on disk. |
| F7 | MEDIUM | Contract / Compatibility | OpenAPI advertises `http://localhost:8080/v1` as a local unauthenticated server (`api.openapi.yaml:19-20`), but the plan says localhost stays `:8080/api/*` and remote is `:8443/v1/*` (`plan.md:228-241`); SC-008 also references `localhost:8080/api/*` (`spec.md:235`). Generated clients and tests may target a non-existent local surface. | Remove the `localhost:8080/v1` server or replace it with `https://localhost:8443/v1` for loopback remote API. Keep the legacy `:8080/api/*` surface out of this OpenAPI contract unless it is also fully specified. |
| F8 | MEDIUM | Performance / State contention | Successful auth updates `lastUsedAt` and `requestCount` on every request (`data-model.md:24-25`, `auth-flows.md:92`, `tasks.md:97-106`). Since tokens live in `config.json` and writes use temp+fsync+rename (`data-model.md:241-244`), read traffic becomes write traffic and can contend with token/peer/route mutations. | Make usage counters best-effort and buffered, or flush periodically under a separate lock. Do not block auth on metadata persistence. Add a contention test with concurrent GETs plus token/peer mutation. |
| F9 | LOW | Validation | Token name validation is inconsistent: data model requires `[a-zA-Z0-9_-]` plus spaces (`data-model.md:41`), while OpenAPI only has length bounds (`api.openapi.yaml:681-684`). | Add the regex/pattern to OpenAPI and task acceptance, or relax the data model. |
| F10 | LOW | Scope clarity | `rotate_cert` exists in the audit enum (`data-model.md:177`, `api.openapi.yaml:730-737`) but no cert rotation endpoint/task exists. Analyze already notes this as reserved. | Mark `rotate_cert` as reserved/future in the enum description, or remove until a rotation action exists. |

## Alternative approaches considered

- Use a keyed token fingerprint (`HMAC(serverSecret, rawToken)`) for lookup and revocation, with bcrypt/argon2 retained only if password-style offline resistance is still desired. This avoids O(N) bcrypt on every miss.
- Keep the existing unauthenticated `:8080/api/*` localhost surface for local tooling, and make the new `:8443/v1/*` listener authenticated regardless of client source. Cleaner trust boundary, fewer proxy surprises.
- Treat audit writes as a small transaction boundary for remote mutations: mutate daemon/VPS state, fsync audit, then return success. If that latency is unacceptable, the spec should say audit is best-effort rather than mandatory.
- Consider optional mTLS or pinned self-signed CA for machine-to-machine consumers later; PATs alone are fine for MVP, but proxy/TLS deployment guidance needs sharper edges.

## VERDICT

```yaml
verdict: HIGH
reviewer: codex
reviewed_at: 2026-05-31T13:59:09.8499313+03:00
commit: a27390741ccce0778be56e6b2aae2bdb04e69a88
critical_count: 0
high_count: 4
medium_count: 4
low_count: 2
```
