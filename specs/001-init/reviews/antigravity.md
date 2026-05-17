# SpecKit Review: 001-init

**Reviewer**: antigravity
**Reviewed at**: 2026-05-16T19:57:05Z
**Commit**: 557b73d1e44ef52137959ae50e984d740df2b8c7
**Artifacts reviewed**: spec.md, plan.md, tasks.md, data-model.md, contracts/caddy-api.md, contracts/daemon-api.md, appendix-peer-add-flow.md

## Summary

The core specification is highly detailed and accurately captures the complexity of interacting with AmneziaWG's undocumented state via SSH and Docker. However, the architectural design suffers from a critical networking flaw regarding container interoperability, and introduces a major concurrency hazard in the mTLS multi-user scenario.

## Findings

| ID | Severity | Area | Finding | Recommendation |
|---|---|---|---|---|
| F1 | CRITICAL | Architecture | `unet-caddy` and `unet-amnezia-awg` are modeled as separate containers. Caddy cannot bind to the WireGuard internal IP (`10.8.1.1`) or route traffic to `10.8.1.2` because it does not share the network namespace with the `awg0` interface. | Configure `unet-caddy` with `network_mode: "service:unet-amnezia-awg"` in `docker-compose.yml` (T008) so they share the `awg0` interface, or add static host routing. |
| F2 | HIGH | Security / Multi-user | The mTLS bootstrap flow (`contracts/caddy-api.md` & `spec.md` FR-008) flips the Caddy admin API to TLS and registers a single client's public key. Any subsequent user on the same VPS will be permanently locked out, as they cannot access the API to append their own key. | mTLS keys should be provisioned via SSH during the `awg syncconf` peer-add flow, avoiding the IP-only race condition and lockout. |
| F3 | HIGH | Implementation | `appendix-peer-add-flow.md` §2.3 and `tasks.md` T013b instruct running `sh -c 'awg syncconf <iface> <(awg-quick strip ...)'`. Process substitution `<(...)` is a bash/zsh feature and will fail in Alpine's default `ash` shell. | Replace process substitution with a temporary file: `awg-quick strip ... > /tmp/cfg && awg syncconf awg0 /tmp/cfg`. |
| F4 | MEDIUM | Security | `appendix-peer-add-flow.md` §2.4 generates `clientsTable.new` JSON via shell string interpolation (`"clientName": "<CLIENT_NAME>"`). If the client name contains quotes, this produces invalid JSON or allows injection. | Use `jq` to safely construct JSON, or construct the JSON locally in Go and `scp` (or `cat`) the complete structure directly to the file. |
| F5 | MEDIUM | Edge case | Cloudflare mode (`spec.md` FR-009) generates a wildcard certificate `*.basedomain`. This will fail to secure multi-level subdomains (e.g., `app.dev.basedomain.com`). | Validate that subdomains are exactly one level deep in `api_ports.go`, or update the Caddy config to issue specific certificates for multi-level subdomains. |

## Alternative approaches considered

- Instead of manually managing `awg0.conf` via `cat >>` and `awg syncconf` (which runs the risk of getting clobbered if `SaveConfig = true` is enabled), consider relying entirely on `awg set` for runtime updates and `awg-quick save` for persistence. This removes the need for manual file manipulation and JSON injection.

## VERDICT

```yaml
verdict: CRITICAL
reviewer: antigravity
reviewed_at: 2026-05-16T19:57:05Z
commit: 557b73d1e44ef52137959ae50e984d740df2b8c7
critical_count: 1
high_count: 2
medium_count: 2
low_count: 0
```
