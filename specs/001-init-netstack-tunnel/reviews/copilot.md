# SpecKit Review: 001-init-netstack-tunnel

**Reviewer**: copilot
**Reviewed at**: 2026-07-19T02:15:43Z
**Commit**: e358d118664730a53f7bf56e4998ef3084f261bf
**Artifacts reviewed**: spec.md, plan.md, tasks.md, data-model.md, research.md, quickstart.md, contracts/config.schema.json, contracts/interfaces.md, .specify/memory/constitution.md

## Summary

The current Unet artifacts have addressed several major issues from the older `claude` review: DNS leak prevention, key-file permissions, port allow-listing, MTU/MSS, and two-level reconnect are now explicit. The remaining risk is mostly at the interface boundary: reverse forwarding depends on a gateway/Caddy counterpart that is not actually specified or tasked, and the local HTTP proxy plus secret-bearing config fields are weaker than the security model implies.

## Findings

| ID | Severity | Area | Finding | Recommendation |
| -- | -------- | ---- | ------- | -------------- |
| F1 | HIGH | Architecture / traceability | `spec.md:64` requires an incoming public Caddy request to be mapped to a `yamux` channel, but `contracts/interfaces.md:35-61` specifies only the client-side yamux control frame and `tasks.md:22-27` only tasks the Unet reverse-forward client. There is no gateway/Caddy component contract, route ownership, token/allow-list authority, or task that makes the public endpoint actually open a stream back to Unet. | Add a gateway-side contract and task, or explicitly declare the gateway as an external prerequisite with a concrete API/config contract, Caddy route shape, authorization store, and integration test for public request -> yamux stream -> local port. |
| F2 | HIGH | Contract / missing surface | `spec.md:73` and `tasks.md:15-20` require SOCKS5/HTTP proxy support, but `contracts/interfaces.md:7-31` defines only the SOCKS5 behavior and `tasks.md:50-55` names no HTTP proxy test. HTTP proxy requests have distinct semantics (`CONNECT`, absolute-form URLs, host header resolution, auth challenges), and DNS-leak prevention must be specified there too. | Add an HTTP proxy contract covering `CONNECT` and absolute-form requests, auth behavior, in-tunnel DNS/fail-closed rules, reply/status mapping, and T7 tests parallel to the SOCKS5 no-DNS-leak test. |
| F3 | HIGH | Security / secrets | `contracts/config.schema.json:70-84` allows `proxy.password` and `gateway.capability_token` in the local JSON config, but the permission enforcement described in `spec.md:87` and `contracts/interfaces.md:71` is conditioned on a plaintext WG private key. If the private key is supplied via env, a config file can still contain other secrets while the permission check is relaxed to a warning. | Treat any secret-bearing field (`private_key`, proxy password, capability token, future PSK/token fields) as requiring owner-only permissions, or support env/secret references for all of them. Update quickstart and schema descriptions accordingly. |
| F4 | HIGH | Failure modes / config | `spec.md:82` says the system must define a terminal failure condition to prevent unbounded reconnect storms, but `contracts/config.schema.json:109-110` permits `max_total_duration_ms: 0` and omits `max_attempts`, with `0 => never give up`. That contradicts the stated anti-flapping requirement. | Make the default terminal condition nonzero, require at least one of `max_total_duration_ms` or `max_attempts`, or force an explicit `retry_forever: true` opt-in with loud logs/health status so the default cannot storm forever. |
| F5 | MEDIUM | DNS / validation | `data-model.md:17` says `dns` is tunnel-side IP resolver(s), but `contracts/config.schema.json:53-56` accepts any string and the root schema does not require either `dns` or an explicit `remote_resolution` capability. This leaves a misconfigured resolver path to implementation heuristics, which is dangerous for the no-leak guarantee in `spec.md:88-90`. | Tighten the schema with IP address validation and a `oneOf`: either nonempty tunnel-side DNS IPs or an explicit remote-side-resolution mode. Fail config load early when neither is available. |
| F6 | MEDIUM | Input validation | `contracts/config.schema.json:26-30` validates `peer.endpoint` with `^.+:[0-9]{1,5}$`, and `gateway.address` at `contracts/config.schema.json:83` is an unconstrained string. These accept invalid ports such as `99999`, ambiguous IPv6 literals, and malformed host:port values that will fail later in networking code. | Use structured host/port parsing rules in the schema or validate at config load: port 1-65535, bracketed IPv6, no empty host, and consistent validation for `peer.endpoint` and `gateway.address`. |
| F7 | MEDIUM | Task ownership | T2 is core to the product (`tasks.md:15-20`), but the agent dispatch plan at `tasks.md:74-79` does not assign T2 to any owner, and line 16 names `frontend-specialist` for a Go networking proxy. This is likely to strand the SOCKS5/HTTP implementation while the critical path proceeds through reverse-forwarding. | Assign T2 unambiguously to `backend-specialist` (with test-engineer support for leak tests) and update the dispatch plan so every task is owned once. |

## Alternative approaches considered

- **Gateway ownership**: either keep the gateway outside this repo and make its API/config contract explicit, or include a minimal gateway/Caddy adapter in this feature. The current middle position is the risky one: the user story depends on a gateway but no artifact owns it.
- **Secret storage**: keeping all config in one JSON file is simple, but it means file-permission enforcement must cover every secret, not only the WireGuard private key. Env references for proxy password/capability token would preserve the single-file shape without broadening plaintext-at-rest risk.

## VERDICT

```yaml
verdict: HIGH
reviewer: copilot
reviewed_at: 2026-07-19T02:15:43Z
commit: e358d118664730a53f7bf56e4998ef3084f261bf
critical_count: 0
high_count: 4
medium_count: 3
low_count: 0
```
