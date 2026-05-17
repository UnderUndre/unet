---
description: "Task list template for feature implementation with agent routing and dependency graph"
---

# Tasks: Unet Core Architecture

**Input**: Design documents from `/specs/001-init/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/, **appendix-peer-add-flow.md** (mandatory reading for T013a/T013b implementers)

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story. Each task is assigned to a specialist agent for domain-aware execution.

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization, basic structure, shared dependency installs

- [X] T001 [SETUP] Initialize Go module and root directory structure in `.`
- [X] T002 [SETUP] Initialize Vite/React project in `web/`
- [X] T003 [OPS] [P] Configure standard linting (golangci-lint, eslint) in `.golangci.yml` and `web/eslint.config.js`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete (phase = sync barrier)

- [X] T004 [BE] Implement local JSON configuration manager (atomic writes: temp file + rename; file mode 0600 POSIX / Windows ACL deny-others) in `internal/config/config.go`. **Must expose two distinct accessors** per FR-011: (a) `Get()` returns the raw config struct for internal use only; (b) `GetMasked()` returns a deep-cloned snapshot with all secret fields (SSH password, Cloudflare token, AmneziaWG private keys, Caddy mTLS client key, `uiToken`) replaced with `****<last-4>` for API responses, and a `RedactedString() string` method on every secret-bearing type returning `<redacted>` for logger output. All API handlers in `internal/daemon/api_*.go` MUST use `GetMasked()` for any response that echoes configuration; all `logger.With(...)` / `slog.Attr` constructions MUST use the redacted form.
- [X] T005 [BE] Set up HTTP server wrapper for the daemon — **bind 127.0.0.1 only**, never 0.0.0.0 — in `internal/daemon/server.go`
- [X] T006 [BE] Implement `go:embed` handler for the React frontend in `internal/daemon/static.go`
- [X] T020a [BE] Implement OS-level privilege check on daemon startup in `cmd/unet/main.go` — exit with clear error if not root/admin
- [X] T020c [BE] Implement `awg-quick` PATH check (`exec.LookPath`) on daemon startup; clear "install AmneziaWG client" error if absent — in `cmd/unet/main.go`
- [X] T020d [BE] Implement single-instance lock (pidfile on POSIX, named mutex on Windows) — second instance exits with clear error — in `cmd/unet/main.go`

> **Note on T021**: T021 was vacated during task decomposition (content folded into T020b privilege-check warning overlay). Numbering gap is intentional; renumbering T022–T028 would break cross-references in plan.md, dependency graph, and Parallel Lanes table.

**Checkpoint**: Foundation ready — user story implementation can now begin

---

## Phase 3: User Story 1 - Easy Server Provisioning (Priority: P1) 🎯 MVP

**Goal**: Automatically setup AmneziaWG and Caddy on the VPS via SSH without manual configuration.

**Independent Test**: Can be fully tested by providing fresh Ubuntu VPS credentials and verifying that Docker, AmneziaWG, and Caddy are running successfully.

### Implementation for User Story 1

- [X] T007 [BE] [US1] Implement SSH client wrapper (key + password auth, host validation against shell-metachar injection) in `internal/provisioner/ssh.go`
- [X] T008 [BE] [US1] Create `docker-compose.yml` generator in `internal/provisioner/compose.go`:
  - `unet-amnezia-awg` service: image `unet/amnezia-awg:local`, `cap_add: [NET_ADMIN, SYS_MODULE]`, sysctls for IPv4 + IPv6 forwarding, named volume `amnezia-awg-state` on `/opt/amnezia/awg`, `restart: unless-stopped`. **MUST declare ALL inbound port mappings** here (because the netns owner is the only service permitted to publish ports): `${UNET_AWG_PORT}/udp` + `443/tcp` + (conditionally on manual DNS mode) `80/tcp`.
  - `unet-caddy` service: image `unet/caddy-cloudflare:local` (when `dns.mode == "cloudflare"`, built locally from `caddy:2-alpine` + `caddy-dns/cloudflare` plugin) OR plain `caddy:2-alpine` (manual DNS mode), **`network_mode: "service:unet-amnezia-awg"`** (shares amnezia's netns — critical, see FR-001), `depends_on: [unet-amnezia-awg]`, named volumes `caddy-data` on `/data` and `caddy-config` on `/config`. **MUST NOT declare its own `ports:`** (compose will reject it given `network_mode: service:...`).
- [X] T008b [BE] [US1] Generate reference Dockerfile + entrypoint `start.sh` for AmneziaWG image (Alpine 3.19 + iproute2 + iptables + amneziawg-tools v1.0.20250901 + amneziawg-go); see `research.md` §3.1/3.2 — in `internal/provisioner/dockerfile.go`
- [X] T008c [BE] [US1] Generate iptables FORWARD + MASQUERADE rules into `start.sh` parameterised by tunnel subnet; verify `net.ipv4.ip_forward=1` via `sysctls` in compose — in `internal/provisioner/dockerfile.go`
- [X] T008d [BE] [US1] UFW detection: if `ufw status` returns active, open `<ListenPort>/udp` + `443/tcp` (+ `80/tcp` if manual DNS mode); SKIP gracefully if ufw not installed — in `internal/provisioner/firewall.go`
- [X] T009 [BE] [US1] Implement provisioning logic (execute scripts over SSH, idempotent: check container exists / image built / keys present before mutating) in `internal/provisioner/setup.go`
- [X] T009b [BE] [US1] Generate server-side AmneziaWG initial config: generate server keypair + PSK, pick free port in 30000-60000 range, pick subnet `10.8.X.0/24` non-overlapping with known private ranges, generate full obfuscation parameter set with sensible defaults (Jc=4, Jmin=10, Jmax=50, S1-S4 randomised within MTU budget, H1-H4 random non-overlapping integers, I1-I5 left empty unless user opts into mimicry presets) — in `internal/provisioner/awg_init.go`
- [X] T010 [BE] [US1] Add API endpoints for VPS configuration and provisioning in `internal/daemon/api_vps.go`
- [X] T011 [FE] [US1] Create VPS configuration form (key + password auth toggle, DNS-mode selector, Cloudflare token field shown conditionally) in `web/src/components/VPSForm.tsx`

**Checkpoint**: User Story 1 should be fully functional and testable independently

---

## Phase 4: User Story 2 - Local Tunnel Connection (Priority: P2)

**Goal**: Daemon establishes an AmneziaWG connection to the VPS so that the local machine joins the private network.

**Independent Test**: Can be fully tested by starting the connection and pinging the VPS internal IP (10.8.0.1) from the local machine.

### Implementation for User Story 2

- [X] T012 [BE] [US2] Implement `awg-quick` integration (generate .conf with **full obfuscation set** `Jc/Jmin/Jmax/S1-S4/H1-H4/I1-I5` + `PersistentKeepalive=25` + `MTU=1280`, exec up/down, stderr-aware error handling, dynamic interface name discovery for Windows/macOS) in `internal/tunnel/awg.go`
- [X] T013a [BE] [US2] Implement server-config parser: SSH + `docker exec <container> cat /opt/amnezia/awg/awg0.conf`, parse INI-like structure, extract `[Interface]` obfuscation params + `Address` + `ListenPort` + `PublicKey` and PSK files via separate `cat` calls. Hash full conf with SHA256 and store in `serverMirror.awgConfRaw` — in `internal/tunnel/server_config.go`
- [X] T013b [BE] [US2] Implement peer-add flow (server-side): generate client keypair locally with `awg genkey`/`awg pubkey`, allocate next free `.N` IP in subnet, SSH-append `[Peer]` block to server `awg0.conf` using **quoted heredoc** (no local interpolation of secret material). Hot-reload via **temp-file pattern** (`awg-quick strip … > /tmp/awg0-strip.conf && awg syncconf <iface> /tmp/awg0-strip.conf && rm /tmp/awg0-strip.conf`) — NOT bash `<(…)` process substitution (Alpine `ash` does not support it; antigravity F3). Update `clientsTable` JSON by **marshalling in Go with `encoding/json`** and pushing via `docker exec -i sh -c 'cat > …'` stdin — NEVER via shell-heredoc string interpolation of user-supplied `clientName` (antigravity F4 — injection vector). See `appendix-peer-add-flow.md` §2.3-§2.6 for the canonical command sequence including the mTLS pubkey-injection sub-step (T016d dependency). — in `internal/tunnel/peer.go`
- [X] T013 [BE] [US2] Implement tunnel manager: orchestrate T013a → T013b → write local `.conf` → `awg-quick up` → watchdog (auto-reconnect on `ping serverIp` failure, exponential backoff 1s→30s, server-config drift recheck every 30s) — in `internal/tunnel/manager.go`

> **Note on T013 numbering**: T013a and T013b are *prerequisites* of T013 (the orchestrator), not sub-steps. This is an intentional inversion — the base task ID was reserved for the final orchestration task, with letter-suffixed tasks providing the lower-level primitives. See dependency graph: `T013b → T013`.
- [X] T014 [BE] [US2] Add API endpoints for tunnel status/connect/disconnect in `internal/daemon/api_tunnel.go`
- [X] T015 [FE] [US2] Create tunnel status dashboard (status badge, local/server IP, server endpoint, drift-warning banner when out-of-band changes detected) in `web/src/components/TunnelStatus.tsx`

**Checkpoint**: User Stories 1 AND 2 should both work independently

---

## Phase 5: User Story 3 - Local Port Exposure (Priority: P3)

**Goal**: Specify a local port (e.g., 3000) and a subdomain, so that the local app is accessible publicly via HTTPS.

**Independent Test**: Can be fully tested by running a local web server, exposing it via the UI, and accessing it from a mobile device over cellular data.

### Implementation for User Story 3

- [X] T016 [BE] [US3] Implement Caddy admin REST API client (mutex-guarded host-match deletion, configurable auth via `caddyApi.authMode`) in `internal/proxy/caddy.go`
- [X] T016a [BE] [US3] Implement Cloudflare API client for DNS record management (A-record upsert, scope-token validation) in `internal/dns/cloudflare.go`
- [X] T016b [BE] [US3] Implement DNS mode selector (cloudflare → wildcard cert via DNS-01; manual → per-subdomain HTTP-01 with rate-limit warning surfaced) in `internal/dns/manager.go`
- [X] T016d [BE] [US3] Implement **mTLS provisioning for Caddy admin via SSH side-channel** (NOT via Caddy admin API — avoids multi-peer lockout, see `contracts/caddy-api.md` "mTLS Provisioning Flow"): generate self-signed ECDSA P-256 cert + key (10y validity) on each peer-add, store locally (file mode 0600 POSIX / ACL Windows; redacted in logs per FR-011), then SSH + `docker exec unet-caddy` to append the base64-DER pubkey to `/config/caddy/autosave.json` `admin.remote.access_control[0].public_keys[]`, signal `caddy reload`, on first-peer-only also flip `admin.listen` to TLS-wrapped listener. Subsequent peers are idempotent. **Recovery path** for lost client cert documented in `contracts/caddy-api.md` "Recovery from Client Cert Loss" — daemon re-runs provisioning over SSH; stale `public_keys[]` entries pruned during peer-rotate ops. — in `internal/proxy/caddy_mtls.go`

> **Note on T016c**: T016c was originally "Bearer-token auth for Caddy admin". Removed after spec session 2026-05-16 confirmed Caddy v2 has no native Bearer middleware (see FR-008). T016d retains its original letter to avoid breaking cross-references in other artifacts.
- [X] T017 [BE] [US3] Add API endpoints for adding/removing exposed ports in `internal/daemon/api_ports.go`. Validation per FR-012: subdomain regex (RFC 1035 label rules), port range `1..65535`, conflict detection (409). **When `dns.mode == "cloudflare"`**: additionally enforce single-label-under-baseDomain constraint (FR-009) — `subdomain.split('.')[:-len(baseDomain.split('.'))]` must have length 1; otherwise return `400 invalid_subdomain_depth` with the structured error payload defined in `contracts/daemon-api.md`. Show actionable remediation hints (rename to single-label or switch to manual DNS mode).
- [X] T018 [FE] [US3] Create port exposure management UI (input validation matching FR-012, error toast for `412 tunnel_not_connected` and `409 subdomain_conflict`) in `web/src/components/PortManager.tsx`

**Checkpoint**: User Stories 1, 2, and 3 should work independently

---

## Phase 6: User Story 4 - Local Web UI Configuration (Priority: P4)

**Goal**: Manage VPS connections and exposed ports via a local web interface (localhost:8080) for a clear, cross-platform GUI.

**Independent Test**: Can be fully tested by opening the browser to the daemon's port and configuring a new exposed service.

### Implementation for User Story 4

- [X] T019 [FE] [US4] Create main dashboard layout integrating all components in `web/src/App.tsx`
- [X] T020b [FE] [US4] Add privilege check warning overlay in `web/src/components/PrivilegeWarning.tsx`

**Checkpoint**: All user stories should work independently

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [X] T022 [OPS] Add multi-platform build scripts in `Makefile` + binary-size assertion via `goweight` (fail CI if >30MB per SC-003)
- [X] T023 [BE] Implement dynamic port allocation for daemon UI if 8080 is taken (still bind 127.0.0.1) in `cmd/unet/main.go`
- [X] T024 [OPS] Update `README.md` and `quickstart.md` with build/run instructions, AmneziaWG prerequisite, **client app MUST bind 0.0.0.0 (not 127.0.0.1)** caveat
- [X] T025 [SEC] Security review: SSH command injection prevention (FR-012 validation enforced), subdomain/port input validation, Caddy mTLS bootstrap correctness, config file permissions (FR-011), token redaction in logs
- [X] T026 [E2E] Integration test: provision VPS → connect tunnel → expose port → verify public access from a separate network (e.g. mobile cellular)
- [X] T027 [E2E] **SC-005 volume-persistence test**: `docker compose down && docker compose up -d --build`, verify existing client still connects without re-enrollment
- [X] T028 [E2E] **SC-006 drift-detection test**: out-of-band modify `awg0.conf` on VPS (e.g. add a peer via `awg set`), verify daemon detects within ≤30s and surfaces UI warning

---

## Dependency Graph

### Legend

- `→` means "unlocks" (left must complete before right can start)
- `+` means "all of these" (join point — ALL listed tasks must complete)
- Tasks not listed here have no dependencies and can start immediately within their phase

### Dependencies

```
T001 → T002, T004, T005, T007, T012, T016
T002 → T003, T011, T015, T018, T019, T020b
T004 + T005 → T006
T020a + T020c + T020d → T012        # daemon-startup gates
T007 + T008 + T008b + T008c → T008d  # ssh + compose + dockerfile + iptables before firewall
T008d + T009b → T009                  # firewall + initial awg config before provisioning
T009 → T010
T010 → T011
T012 + T013a → T013b                  # parser + peer-add primitives
T013b → T013                          # peer-add before manager orchestration
T013 → T014
T014 → T015
T016 → T016a, T016d                   # caddy client unlocks DNS + mTLS branches in parallel
T016a → T016b
T016b + T016d → T017                  # DNS + mTLS both feed port-exposure API
T017 → T018
T011 + T015 + T018 → T019
T006 + T019 → T023
T023 → T022
T022 → T024
T014 + T017 → T025                    # sec review after tunnel+ports complete
T025 → T026
```

---

## Parallel Lanes

| Lane | Agent Flow | Tasks | Blocked By |
|------|-----------|-------|------------|
| 1 | [SETUP] | T001, T002 | — |
| 2 | [BE] (Config/UI core) | T004, T005 → T006 | T001 |
| 3 | [BE] (US1 SSH+image) | T007, T008, T008b, T008c → T008d; T009b → T009 → T010 | T001 |
| 4 | [FE] (US1) | T011 | T002, T010 |
| 5 | [BE] (US2 tunnel) | T012; T013a → T013b → T013 → T014 | T001, T020a, T020c, T020d |
| 6 | [FE] (US2) | T015 | T002, T014 |
| 7 | [BE] (US3 proxy+dns+mtls) | T016 → (T016a → T016b ‖ T016d) → T017 | T001 |
| 8 | [FE] (US3) | T018 | T002, T017 |
| 9 | [FE] (US4) | T019 | T011 + T015 + T018 |
| 10 | [BE/FE] (Sys) | T020a, T020c, T020d (Phase 2); T023 (Phase 7); T020b (Phase 6) | T001 |
| 11 | [OPS] | T003, T022 → T024 | T002, T023 |
| 12 | [SEC] | T025 | T014, T017 |
| 13 | [E2E] | T026 | T025 |

Note on Lane 7: `(T016a → T016b ‖ T016d)` means T016a → T016b runs in parallel with T016d; both must complete before T017.

---

## Agent Summary

| Agent | Task Count | Can Start After |
|-------|-----------|-----------------|
| [SETUP] | 2 | immediately |
| [BE] | 24 | T001 |
| [FE] | 6 | T002 + BE API endpoints |
| [OPS] | 3 | T002 |
| [SEC] | 1 | T014 + T017 |
| [E2E] | 1 | T025 |

**Critical Path** (longest chain): T001 → T020a → T012 → T013a → T013b → T013 → T014 → T015 → T019 → T023 → T022 → T024

Critical path now has two extra hops (T013a + T013b) reflecting the real AmneziaWG control-plane via SSH + `docker exec` rather than a fictional management API.

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL — blocks all stories)
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: Test User Story 1 independently (provisioning works via SSH).
5. Only then move to tunnel connections.

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Deploy/Demo (MVP!)
3. Add User Story 2 → Test independently → Local machine connects to VPN
4. Add User Story 3 → Test independently → Public domains point to local ports
5. Add User Story 4 → UI polish
6. Security review + E2E → validate end-to-end
7. Each story adds value without breaking previous stories
