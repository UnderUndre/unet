# Implementation Plan: Peer Onboarding Wizard

**Spec**: `specs/006-peer-onboarding/spec.md`
**Branch**: `006-peer-onboarding`
**Created**: 2026-05-28
**Status**: Draft

---

## Constitution Check

### Principle VI — Cross-AI Review Gate

This is `/speckit.plan` — NOT `/speckit.implement`. No code is being written. The review gate **does not apply** at the planning stage. When this plan proceeds to implementation via `/speckit.implement`, the gate WILL require:

1. `specs/006-peer-onboarding/reviews/analyze.md` with `verdict: PASS` or `verdict: MEDIUM`.
2. ≥2 external reviewer PASS files from different AI providers.
3. No contradicting `_gate-override.md`.

**Verdict**: PASS (planning stage, gate not yet active).

### Principle VII — Artifact Versioning

The `snapshot-stage.{sh,ps1}` scripts do not exist in this repo (TODO_SNAPSHOT_SCRIPT per constitution §VII). This plan does NOT attempt to call missing scripts.

Per the constitution's graceful-degradation clause: "[if the script is missing] the stage command MUST log a `[snapshot-deferred]` warning but still complete."

Manual tag `plan/006-peer-onboarding/v1` is encouraged after commit. Snapshot-stage tooling is aspirational and cannot be invoked until implemented.

**Verdict**: SKIPPED — tooling not yet available.

### Principle VIII — Knowledge Self-Maintenance

**Drift check**: `specs/main/architecture.md` was recently updated with the 4-layer model (Data Plane, Control Plane, Operations, Admin Surface). Checking for 006-specific gaps:

1. **Admin Surface** currently describes "Functional for single-user local management (spec 001-init, User Story 4)" and "Future evolution: remote-accessible mode." The wizard adds a major new Admin Surface capability (first-run multi-step wizard) not reflected here. **Gap: no mention of wizard/onboarding flow in Admin Surface description.**
2. **Control Plane** route surface lists `peers`, `routes`, `tunnel/status`, `tokens`, `audit`, `status`, `logs/stream`, `logs/export`. The wizard adds `/v1/wizard/*` endpoints and `/v1/peers/{id}/qr`, `/v1/peers/{id}/invite`, `/v1/routes/expose`. **Gap: wizard API endpoints not listed.**
3. **Operations** layer has Lifecycle and Observability subsystems. The wizard doesn't add a new subsystem — it orchestrates existing ones (Lifecycle bootstrap, Control Plane peer/route creation). **No gap.**
4. **Spec Registry** does not list `specs/006-peer-onboarding/`. **Gap.**
5. **Technology Stack** table doesn't mention QR generation library (`skip2/go-qrcode`) or WireGuard deeplink URI scheme. **Minor gap — may be omitted as implementation detail.**

**Follow-up**: Update `architecture.md` to:
- Add wizard/onboarding to Admin Surface description.
- Add wizard endpoints to Control Plane route surface.
- Add 006 to Spec Registry table.
- Optionally add QR generation library to Technology Stack.

**Verdict**: NOTE — architecture.md missing wizard-specific descriptions. Non-blocking for plan, tracked as follow-up before implementation merge.

---

## Technical Approach Summary

### Language & framework

- **Go** backend (same daemon). No new languages introduced.
- **React** frontend (existing admin UI, embedded in Go binary via `go embed`).
- QR generation: `github.com/skip2/go-qrcode` — pure Go, zero CGO, battle-tested, generates PNG bytes directly. Industry standard for Go QR libraries.
- State machine: `xstate` (React frontend) OR plain `useReducer` + enum states. **Decision: `useReducer`** — wizard is a linear step sequence with back-navigation, not a complex concurrent state machine. xstate adds bundle size (~15KB min) for no material benefit over a well-typed reducer.
- DNS validation: Go stdlib `net.LookupHost` + Cloudflare API via `github.com/cloudflare/cloudflare-go` for zone/DNS record management.
- WireGuard config generation: existing daemon logic from spec 001/002 (already generates `[Interface]`/`[Peer]` with AmneziaWG params).
- Invite link HMAC: `crypto/hmac` + `crypto/sha256` (Go stdlib). Short-code generation: `crypto/rand` (Go stdlib) for 8-digit numeric codes.
- nip.io: zero library dependency. Subdomain pattern `<label>.<IP-dashed>.nip.io` resolves automatically.

### What's reused

- **003 Bootstrap**: Wizard's "commit" step calls `internal/lifecycle/bootstrap.Bootstrap(ctx, sshCoords, opts)` directly. Zero reimplementation of Docker/compose/health-check logic. The wizard is a UI wrapper around the bootstrapper.
- **002 Peer creation**: Wizard's "create first peer" step calls `POST /v1/peers` internally (in-process handler call, not HTTP loopback). Reuses existing key generation, IP allocation, `awg0.conf` update.
- **002 Route creation**: One-click "Expose Port" calls `POST /v1/routes` internally. Same pattern — in-process handler call.
- **005 Log stream**: Wizard events emit structured log records via `slog` that flow into 005's unified log stream. No separate event pipeline.
- **SSH session pool** (spec 003): Wizard SSH validation uses the same `internal/ssh/` pool. No ad-hoc SSH connections.
- **Auth middleware** (spec 002): Wizard API endpoints reuse existing PAT/JWT Bearer auth. Wizard session is a local-only concept (no extra auth beyond the admin UI session).
- **Atomic-write pattern** (spec 001): Wizard state persistence uses the same temp+rename protocol.

### What's new

| Package | Purpose |
|---------|---------|
| `src/internal/wizard/` | Orchestrator: state machine, session persistence, step validation, commit coordination |
| `src/internal/wizard/preflight/` | Distro check (Ubuntu 22.04+/Debian 12+), disk ≥ 2GB, sudo+docker validation over SSH |
| `src/internal/wizard/dnscheck/` | A-record lookup, Cloudflare nameserver detection, token scope validation |
| `src/internal/qr/` | QR PNG generation + WireGuard deeplink URI construction |
| `src/internal/invite/` | HMAC-signed invite URL generation, short-code store, validation + consumption |
| `src/web/wizard/` | React components: step UIs, reducer state machine, wizard API client |

### Key decisions locked by spec

1. **VPS distros**: Ubuntu 22.04+ and Debian 12+ only (Clarification round 1). Others fail preflight with clear message.
2. **Invite auth**: Both HMAC-signed URL (default) and short-code (advanced) modes (Clarification round 1).
3. **Telemetry**: Strict opt-in, default OFF. SC-004 becomes aspirational/manual (Clarification round 1).
4. **nip.io TLS**: Let's Encrypt HTTP-01, Caddy auto-issues (Clarification round 1).

---

## Project Structure

New code goes under `src/internal/` (backend) and `src/web/` (frontend):

```
src/
├── internal/
│   ├── wizard/                        # NEW: wizard orchestrator
│   │   ├── wizard.go                  # Orchestrator: session CRUD, step transitions, commit
│   │   ├── state.go                   # WizardState struct, JSON persistence, resume logic
│   │   ├── steps.go                   # Step enum, validation dispatch, transition guards
│   │   ├── commit.go                  # Final commit: bootstrap → create peer → expose first URL
│   │   ├── wizard_test.go
│   │   ├── state_test.go
│   │   └── commit_test.go
│   ├── wizard/preflight/              # NEW: VPS preflight checks
│   │   ├── preflight.go               # Run(ctx, sshSession) → PreflightResult
│   │   ├── distro.go                  # OS detection: /etc/os-release parsing, Ubuntu/Debian check
│   │   ├── distro_test.go
│   │   └── preflight_test.go
│   ├── wizard/dnscheck/               # NEW: domain validation
│   │   ├── dnscheck.go                # Validate(ctx, domain, vpsIP) → DomainCheckResult
│   │   ├── cloudflare.go              # CF nameserver detection, token validation, zone lookup
│   │   ├── dnscheck_test.go
│   │   └── cloudflare_test.go
│   ├── qr/                            # NEW: QR + deeplink generation
│   │   ├── qr.go                      # GeneratePNG(config string) ([]byte, error)
│   │   ├── deeplink.go                # BuildDeeplink(config string) (uri string, err error)
│   │   ├── qr_test.go
│   │   └── deeplink_test.go
│   ├── invite/                        # NEW: invite link management
│   │   ├── invite.go                  # Create(ctx, peerID, mode, ttl) → InviteLink
│   │   ├── hmac.go                    # HMAC-SHA256 URL signing
│   │   ├── shortcode.go               # 8-digit numeric code generation + store
│   │   ├── validate.go                # Consume(ctx, token/code) → config or error
│   │   ├── store.go                   # Invite store: encrypted JSONL at ~/.unet/invites.jsonl
│   │   ├── invite_test.go
│   │   ├── hmac_test.go
│   │   ├── shortcode_test.go
│   │   └── validate_test.go
│   └── daemon/                        # EXISTING — modified
│       └── main.go                    # Register wizard HTTP handlers
├── web/
│   └── wizard/                        # NEW: React frontend
│       ├── WizardApp.tsx              # Root wizard component, useReducer state machine
│       ├── steps/                     # Individual step components
│       │   ├── WelcomeStep.tsx
│       │   ├── SSHCredentialsStep.tsx
│       │   ├── PreflightStep.tsx
│       │   ├── DomainModeStep.tsx
│       │   ├── DomainCheckStep.tsx
│       │   ├── BootstrapStep.tsx
│       │   ├── CreatePeerStep.tsx
│       │   └── SuccessStep.tsx
│       ├── api.ts                     # Wizard API client (fetch wrapper)
│       ├── state.ts                   # Step enum, state types, reducer
│       └── components/
│           ├── QRDisplay.tsx           # QR code + copyable config + download .conf
│           └── InviteLinkDisplay.tsx   # Share link UI + short-code display
```

**Files touched**: `internal/daemon/main.go` (register wizard handlers on 002's mux), `web/app.tsx` or router (add `/wizard/*` routes). All other files are new.

**Dependencies added**:
- `github.com/skip2/go-qrcode` — QR PNG generation (pure Go, no CGO)
- `github.com/cloudflare/cloudflare-go` — Cloudflare API (optional, for DNS-01 mode only)

**Dependencies reused** (already in go.mod):
- `golang.org/x/crypto/ssh` — SSH validation (spec 003)
- `filippo.io/age` — optional wizard-state encryption (spec 003)
- `log/slog` — structured logging (spec 005)

---

## Component Breakdown

### 1. WizardOrchestrator (`internal/wizard/`)

State machine orchestrator managing wizard sessions. Entry point `StartSession()` creates a new `WizardState` with UUID, persists to `~/.unet/wizard-state.json`. `SubmitStep(ctx, sessionID, step, input)` validates step input, runs the appropriate check (SSH test, preflight, DNS lookup), transitions state, persists progress. `CommitSession(ctx, sessionID)` triggers the irreversible bootstrap sequence. `ResumeSession(ctx)` loads persisted state and returns current step + pre-filled inputs.

**Session lifecycle**:
- Created on `POST /v1/wizard/sessions`
- Persisted after each step completion
- Deleted on successful commit (wizard complete → `wizard-state.json` removed)
- Can be abandoned (file remains, auto-resumes on next admin UI load if `vps.isProvisioned == false`)

**Concurrency**: single-session model. If a session exists, `POST /v1/wizard/sessions` returns `409 session_exists` with the existing session ID (resume it instead).

### 2. SSHValidator

Not a separate package — lives in `wizard/steps.go` as `validateSSH(ctx, host, port, user, authType, keyValue)`. Uses the SSH session pool from spec 003 (`internal/ssh/pool.go`). Validates: TCP connect within 10s → SSH auth success → `sudo docker ps` exits 0. Returns specific error for each failure point (connection refused, auth failed, no sudo, no docker).

### 3. DistroPreflight (`internal/wizard/preflight/`)

Runs over SSH after SSH validation passes. Entry point `Run(ctx, sshSession) → PreflightResult`. Checks:
- `cat /etc/os-release` → parse `ID` and `VERSION_ID`. Accept: Ubuntu ≥ 22.04, Debian ≥ 12. Reject all others with "Unsupported OS: <detected>. Supported: Ubuntu 22.04/24.04, Debian 12."
- `df -h /` → Avail ≥ 2GB. Reject if insufficient.
- `sudo -n true` → exit 0. Reject if no passwordless sudo.
- `docker info` → exit 0. Warn (not reject) if Docker missing (bootstrap will install it).
- Port availability: `ss -tlnp | grep -E ':(443|80|<wg-port>)\s'` → reject if bound by non-unet process.

Blocking failures prevent progression. Warnings allow progression with user confirmation.

### 4. DomainValidator (`internal/wizard/dnscheck/`)

For BYO-domain mode. Entry point `Validate(ctx, domain, vpsIP) → DomainCheckResult`. Checks:
- `net.LookupHost(domain)` → A-record IPs. Match against VPS IP? Warn if mismatch (DNS propagation).
- Nameserver check: `net.LookupNS(domain)` → check if any NS ends with `.cloudflare.com`. If yes → offer Cloudflare DNS-01 integration.
- If no Cloudflare: Caddy HTTP-01 TLS feasibility (port 80 must be reachable from internet — already checked in preflight).
- Cloudflare token validation: `cloudflare.New(token, ...)` → `ListZones(ctx)` → find domain → verify `Zone:Read` + `DNS:Edit` scope via test read operation.

### 5. CloudflareIntegrator (within `dnscheck/cloudflare.go`)

Optional DNS-01 cert flow. When Cloudflare is detected and user provides API token:
- Validates token scope (FR-010 from spec).
- Stores token in `~/.unet/config.json` with mode 0600, redacted in logs.
- During commit: configures Caddy with `caddy-dns/cloudflare` plugin + token for DNS-01 wildcard cert.
- Wildcard cert (`*.domain.com`) issued once, covers all future subdomains without per-record DNS changes.

### 6. NipIoFallback

Not a separate package — logic in `wizard/steps.go` + `wizard/commit.go`. When user selects nip.io mode:
- Skip all DNS configuration steps entirely.
- Auto-generate subdomain as `<label>.<wg-client-ip-dashed>.nip.io` (e.g., `app.10-8-0-2.nip.io`).
- Caddy auto-issues LE cert per-subdomain via HTTP-01 (port 80 open, confirmed in preflight).
- No DNS record creation needed — nip.io resolves `<anything>.<IP-dashed>.nip.io` automatically.
- If nip.io DNS resolution fails during wizard, show warning "nip.io DNS unreachable. Proceed without DNS verification or use your own domain." Allow user to continue or switch to BYO-domain.

### 7. QRGenerator (`internal/qr/`)

Generates QR PNG and WireGuard deeplink URI from client config text. Entry point `Generate(config string) → QRResult`. Uses `skip2/go-qrcode` with:
- PNG dimensions: 256×256 (sufficient for mobile scanning at standard distance).
- Error correction level M (~15% recovery — balances size vs robustness).
- Deeplink URI: `wireguard://import?config=<base64url(config)>` — works on both Android and iOS WireGuard apps.

Returns `QRResult{ PNG: []byte, DeeplinkURI: string, ConfigText: string }`.

### 8. InviteLinkManager (`internal/invite/`)

Manages shareable peer-config invite links. Two modes:

**HMAC URL mode** (default):
- Generate random token (32 bytes, `crypto/rand`).
- Compute HMAC-SHA256 over `token + peerID + expiresAt` using daemon secret key.
- URL format: `https://<host>/invite/<peer-id>?t=<base64url(token)>&e=<unix(expiry)>&s=<base64url(hmac)>`
- Validation: recompute HMAC, compare constant-time, check expiry, check not consumed.
- One-time consumption: mark consumed after first successful validation. Store `consumed_at` + `consumed_by_ip` in invite store.

**Short-code mode** (advanced):
- Generate 8-digit numeric code via `crypto/rand` (reject codes < 10000000 to ensure 8 digits).
- Store `{code_hash: sha256(code), peer_id, expires_at, consumed}` in invite store.
- User shares code out-of-band (messaging, voice). Recipient enters at `https://<host>/invite` landing page.
- Rate limit: 5 attempts per IP per minute. After 20 failed attempts per code, invalidate.

**Invite store**: `~/.unet/invites.jsonl` — append-only JSONL file, one line per invite. GC: on daemon start, prune expired invites. File mode 0600. Contains encrypted config blob per invite (AES-256-GCM with daemon-secret key), not raw WG config.

### OneClickPublisher

Not a separate package — orchestration in `wizard/commit.go` + API handler for `POST /v1/routes/expose`. Flow:

> **Discovered endpoints**: In addition to the 8 endpoints originally documented in `contracts/wizard-api.md`, decomposition revealed 3 additional endpoints: invite landing (`GET /invite/{peerId}`) for users opening invite links, config download (`GET /invite/{peerId}/download`) as fallback when WG app is not installed, and one-click publish (`POST /v1/routes/expose`) for atomic route+DNS creation. All 11 endpoints are tracked in `tasks.md` §Coverage Validation → Endpoints: 11/11.

1. User clicks "Expose port 3000" in admin UI.
2. Frontend calls `POST /v1/routes/expose` with `{localPort: 3000, subdomain: "app"}` (subdomain optional — auto-generate if omitted).
3. Backend validates subdomain availability (check Caddy routes + existing DNS records).
4. Calls `POST /v1/routes` internally (in-process).
5. If Cloudflare mode: creates DNS A-record pointing to VPS IP.
6. Returns `{url: "https://app.domain.com"}` to frontend.
7. Atomic: if DNS creation fails after route creation, rollback route.

Auto-subdomain generation for nip.io: `svc-<random-4-chars>.<wg-ip-dashed>.nip.io`. For BYO-domain: `svc-<random-4-chars>.domain.com`.

### 10. WizardUI (`src/web/wizard/`)

React frontend — multi-step wizard embedded in existing admin UI. New routes: `/wizard/*` in React Router. State management: `useReducer` with typed actions:

```typescript
type WizardAction =
  | { type: 'STEP_COMPLETE'; step: WizardStep; data: StepData }
  | { type: 'STEP_ERROR'; step: WizardStep; error: string }
  | { type: 'STEP_BACK'; to: WizardStep }
  | { type: 'RESUME'; state: WizardState }
```

Steps rendered sequentially. Back navigation allowed before commit. After commit, forward-only (bootstrap is irreversible).

**First-run detection**: on app load, if `vps.isProvisioned == false`, redirect to `/wizard`. If `wizard-state.json` exists, resume from last step.

---

## Data Flow

### Wizard Happy Path

```
User opens localhost:8080 (first time, no VPS configured)
    │
    ├─── GET /v1/status → { tunnel: { status: "disconnected" }, vps: null }
    │    └─── Frontend detects no VPS → redirect to /wizard
    │
    ├─── POST /v1/wizard/sessions → { session_id: "..." }
    │
    ├─── Step 1: Welcome (static content, prerequisites)
    │    └─── POST /v1/wizard/sessions/{id}/steps/welcome → validate: none
    │
    ├─── Step 2: SSH credentials
    │    └─── POST /v1/wizard/sessions/{id}/steps/ssh
    │         ├── Input: { host, port, user, authType, keyValue }
    │         └── Validate: TCP connect + SSH auth + sudo docker ps
    │
    ├─── Step 3: Preflight
    │    └─── POST /v1/wizard/sessions/{id}/preflight
    │         ├── Run preflight checks over SSH
    │         └── Return: { distro, diskFree, hasSudo, hasDocker, ports }
    │
    ├─── Step 4: Domain mode selection
    │    └─── POST /v1/wizard/sessions/{id}/steps/domain-mode
    │         └── Input: { mode: "byo" | "nipio" }
    │
    ├─── Step 5 (conditional): Domain check + DNS config
    │    └─── POST /v1/wizard/sessions/{id}/steps/domain-check
    │         ├── BYO: validate A-record + Cloudflare detection + TLS strategy
    │         └── nip.io: skip entirely
    │
    ├─── Step 6: Commit (bootstrap)
    │    └─── POST /v1/wizard/sessions/{id}/commit
    │         ├── Calls bootstrap.Bootstrap(ctx, sshCoords) [spec 003]
    │         ├── Waits for health probe success
    │         ├── Creates first peer: POST /v1/peers [spec 002]
    │         ├── Generates QR: qr.Generate(peer.clientConfig)
    │         ├── Deletes wizard-state.json on success
    │         └── Returns: { peer, qrCode, firstUrl }
    │
    └─── Step 7: Success page
         └─── Displays first public URL + QR code for first peer
```

### Peer Onboarding (after wizard)

```
User clicks "Add Peer" in admin UI
    │
    ├─── POST /v1/peers { name: "phone" } [spec 002]
    │    └── Returns { peer, clientConfig }
    │
    ├─── POST /v1/peers/{id}/qr
    │    └── Returns { png: base64, deeplinkUri, configText }
    │
    └─── Frontend displays QR + copyable config + download .conf button
```

### Invite Link Flow

```
User clicks "Share Peer" in admin UI
    │
    ├─── POST /v1/peers/{id}/invite { mode: "hmac_url", ttl: "24h" }
    │    └── Returns { url: "https://...", shortCode: null }
    │
    └─── OR { mode: "short_code", ttl: "24h" }
         └── Returns { url: null, shortCode: "84736291" }

Recipient opens invite URL (or enters short code):
    │
    ├─── GET /invite/{peer-id}?t=...&e=...&s=...
    │    └── Validate HMAC + expiry + one-time flag
    │
    ├─── IF valid:
    │    ├── Display QR code + download .conf + copyable config
    │    ├── Detect OS → link to WG client download
    │    └── Mark invite as consumed
    │
    └─── IF invalid (expired / consumed / bad sig):
         └── Display error page, no config exposed
```

### One-Click Expose Flow

```
User clicks "Expose port 3000"
    │
    ├─── POST /v1/routes/expose { localPort: 3000, subdomain: "app" }
    │    ├── Validate subdomain (FR-012 rules)
    │    ├── IF conflict → 409 route_conflict + suggest alternative
    │    ├── Create route via internal POST /v1/routes
    │    ├── IF Cloudflare mode → create DNS A-record
    │    └── Return { url: "https://app.domain.com" }
    │
    └─── Frontend displays public URL (reachable within 3s)
```

---

## Cross-Component Integration

### 003 VPS Lifecycle — Bootstrap Reuse

Wizard's commit step calls `bootstrap.Bootstrap(ctx, sshCoords, opts)` from `internal/lifecycle/bootstrap/`. The wizard does NOT reimplement any SSH+Docker+compose logic. The bootstrapper's idempotency guarantees apply: if wizard is interrupted mid-commit and resumed, re-running bootstrap on an already-current VPS exits with zero diff.

**Integration point**: `internal/wizard/commit.go` imports `internal/lifecycle/bootstrap`. Direct function call, not HTTP.

### 002 Control Plane — Peer + Route Creation

Wizard creates peers and routes through the same API handlers that serve `POST /v1/peers` and `POST /v1/routes`. Integration is in-process: `wizard/commit.go` calls the handler function directly (or the underlying service function if the handler is thin). This ensures auth, audit logging, and validation are identical whether the request comes from the wizard, admin UI, or external API consumer.

**Follow-up**: 002's OpenAPI contract (`contracts/api.openapi.yaml`) needs extension to include wizard endpoints (`/v1/wizard/*`, `/v1/peers/{id}/qr`, `/v1/peers/{id}/invite`, `/v1/routes/expose`). This is a documentation-only change to 002.

### 005 Observability — Event Streaming

Wizard emits structured log records via `slog` at each step transition. Events:
- `wizard.step_complete` — step name, duration, session ID.
- `wizard.step_error` — step name, error details, session ID.
- `wizard.commit_start` — session ID, SSH host.
- `wizard.commit_success` — session ID, duration, first URL, peer ID.
- `wizard.commit_failure` — session ID, error, rollback status.

These flow through 005's unified log pipeline (`slog` handler → ring buffer → SSE stream). Admin UI log viewer shows wizard events in real-time. Events tagged `component: "wizard"`, `source: "onboarding"`.

**Audit log**: Wizard commit triggers 002's audit trail with action `wizard_complete` + metadata (VPS host, domain, peer count).

### 004 Desktop Integration — Post-Bootstrap Notification

Wizard's commit success should trigger a system tray notification ("unet: VPS configured, tunnel active") via 004's `platform.Notifier` interface. **Cross-spec dependency flag**: spec 004 is not yet planned. Implementation approach:

1. Define a `Notifier` interface in `internal/wizard/` with a no-op default.
2. When spec 004 lands, inject the real notifier via dependency injection.
3. Wizard code never imports `internal/desktop/` directly — uses the interface.

This keeps 006 buildable independently of 004.

### 001-Init — Existing Manual Workflow

Wizard is purely additive. The existing manual SSH workflow (`unet bootstrap root@1.2.3.4` via CLI) continues to work. If `vps.isProvisioned == true` (user already has a VPS), the wizard is not shown. If user completed setup manually and later wants to add peers, the admin UI peer management page (not the wizard) handles it.

---

## Migration / Compat Strategy

### Wizard is additive — zero disruption

- Existing daemon API surface unchanged. Wizard endpoints are new routes under `/v1/wizard/`.
- Existing admin UI pages unchanged. Wizard is a new section under `/wizard/*`.
- Existing CLI commands (`unet bootstrap`, `unet status`) work identically.
- If wizard-state.json exists from a previous incomplete wizard, daemon offers resume on next admin UI load.

### First-run detection

- `GET /v1/status` returns `vps: null` when no VPS is configured.
- Frontend checks this on app load. If `vps == null` → redirect to `/wizard`.
- If `vps != null` → normal dashboard (no wizard).

### Wizard state persistence

- `~/.unet/wizard-state.json` — single file, one active session max.
- File deleted on successful commit. File remains on abandon/crash.
- File contents: JSON with `{session_id, current_step, inputs: {...}, created_at, updated_at}`.
- Sensitive inputs (SSH key, Cloudflare token): **stored encrypted with age IF SSH coordinates are present** (reuses age library from spec 003). If no SSH coords yet (step 1-2), stored in plain JSON — acceptable because file is localhost-only with 0600 permissions.
- On resume: re-validate current step inputs (SSH connection may have changed, domain DNS may have propagated since last attempt).

---

## Testing Strategy

### Unit tests

| Component | What's mocked | Tool |
|-----------|--------------|------|
| Wizard state machine (transitions) | N/A (pure function) | Table-driven: step → expected transition |
| Wizard state persistence | Filesystem (temp dir) | `t.TempDir()`, verify JSON round-trip |
| SSH validator | SSH session pool (interface) | Mock pool: inject success/failure per test |
| DistroPreflight | SSH session (interface) | Table-driven: os-release outputs → pass/fail |
| DomainValidator | DNS lookup (injectable resolver) | Mock `net.LookupHost` + `net.LookupNS` |
| CloudflareIntegrator | `cloudflare-go` API (interface) | Mock ListZones, CreateDNSRecord |
| QR generation | N/A (pure function) | Verify PNG bytes valid (decode + verify content) |
| Deeplink URI construction | N/A (pure function) | Table-driven: config → expected URI |
| HMAC invite signing | N/A (pure function) | Round-trip: sign → validate → consume |
| Short-code generation | N/A (crypto/rand) | Verify 8-digit, no collisions in 10k generates |
| Invite consumption | Filesystem (temp dir) | Verify one-time, expired, rate-limited |
| OneClickPublisher | Route creation + DNS (interface) | Verify atomic rollback on DNS failure |

### Integration tests

| Test | What runs real | What's mocked |
|------|---------------|---------------|
| Full wizard flow (SSH → preflight → domain → commit) | State machine + persistence | SSH (mock responses), bootstrap (mock) |
| Bootstrap integration | wizard.commit calls bootstrap.Bootstrap | SSH to VPS, Docker |
| Peer creation + QR | POST /v1/peers → qr.Generate | SSH to VPS, awg commands |
| Invite create → consume | Full invite lifecycle | N/A (in-memory store) |
| One-click expose | Route creation + DNS rollback | Cloudflare API, Caddy API |
| Wizard resume after interruption | State persistence + re-validation | SSH (mock) |
| Cloudflare token validation | cloudflare-go API call | CF API server (httptest mock) |

### End-to-end tests (Playwright)

- **Happy path**: Fresh admin UI → complete wizard → verify first URL reachable.
- **Interrupt + resume**: Complete through SSH step → close browser → reopen → resume from domain step.
- **QR scan flow**: Create peer → verify QR rendered → verify config text matches expected WG format.
- **Invite link**: Generate invite → open in new browser → verify config displayed → verify consumed on second visit.
- **One-click expose**: Expose port 3000 → verify URL returned → verify URL serves local app response.

---

## Open Risks

1. **SSH passphrase-protected keys**: Wizard runs in browser → daemon backend. Passphrase-protected SSH keys require interactive prompt, which doesn't map to the wizard's step-based model. **Design decision**: passphrase-protected keys are NOT supported in the wizard UI. Spec amended to SHOULD with clear error detection. Users with passphrase keys can use CLI `unet bootstrap` with ssh-agent forwarding instead. Wizard SSH step detects passphrase-protected keys and shows clear error: "This SSH key is passphrase-protected. Options: (1) Generate a passwordless key: `ssh-keygen -t ed25519 -N ''` (2) Use CLI setup with ssh-agent — see quickstart §SSH Keys & ssh-agent." Password authentication is fully supported as alternative.

2. **Cloudflare token scope errors**: Users frequently create tokens with incorrect scopes. Mitigation: wizard validates token by actually attempting a harmless operation (list zones + read DNS records). Specific error message: "Token missing DNS:Edit permission. Create a token with Zone:Read and DNS:Edit scopes." Link to Cloudflare token creation docs.

3. **Mobile WG app deeplink fragmentation**: Android WireGuard app uses `wireguard://` URI scheme. iOS also supports `wireguard://` but behavior varies across iOS versions. Some third-party WG clients (e.g., Tailscale, Netbird) don't support the standard scheme. Mitigation: always provide fallback (download .conf file + manual import instructions). Deeplink is a convenience, not a requirement.

4. **Invite link leak via URL**: HMAC-signed URLs contain the token in query params. Browser history, server logs, referrer headers can leak. Mitigation: short TTL (default 24h), one-time consumption, config blob is encrypted (not raw WG config in URL). Recommend using short-code mode for security-sensitive sharing.

5. **nip.io rate limits at Let's Encrypt**: Each nip.io subdomain triggers a new LE certificate request. With many subdomains, this can hit LE's rate limits (50 certificates per registered domain per week, where "nip.io" is the registered domain shared by ALL nip.io users). Mitigation: use wildcard names where possible, document that nip.io is for development/testing only. BYO-domain mode has no such limit.

6. **Wizard partial state encryption**: Decision deferred to implementation — encrypt `wizard-state.json` with age (adds dependency on user having age identity, complex key management) vs plain JSON with file permissions (simpler, localhost-only risk). **Recommendation**: plain JSON + file mode 0600 for v0.1. SSH keys stored in daemon's key store, not in wizard state. Wizard state only contains host/port/username, not private keys.

7. **Bootstrap duration UX**: Bootstrap can take 2-5 minutes (Docker pull + build). Wizard UI must show real-time progress (log stream via SSE). If SSE fails, show spinner with estimated time. Risk: user thinks wizard is hung. Mitigation: stream bootstrap logs to wizard UI via 005's SSE pipeline.

8. **Port 80 availability on VPS**: nip.io TLS via HTTP-01 requires port 80. If another service (nginx, apache) is bound to port 80, preflight must detect and warn. Mitigation: preflight checks port 80 availability. If bound, suggest stopping the service or switching to BYO-domain + DNS-01 mode.

---

## Decisions Made in Plan Beyond Spec

| Topic | Decision | Why |
|-------|----------|-----|
| State machine library | `useReducer` (React) over `xstate` | Wizard is linear steps with back-nav. xstate's concurrent state + guards are overkill. Bundle savings ~15KB. |
| QR library | `skip2/go-qrcode` | Pure Go, zero CGO, most starred Go QR library. Generates PNG directly. No alternatives come close in stability. |
| Wizard session model | Single active session (409 on second start) | One admin, one VPS. Multi-session wizard is unnecessary complexity. Resume replaces restart. |
| Wizard state encryption | Plain JSON + file mode 0600 for v0.1 | SSH private keys NOT stored in wizard state (only host/port/username). Age encryption adds key management complexity for minimal security gain on localhost-only file. |
| Peer creation integration | In-process function call (not HTTP loopback) | Avoid network roundtrip + auth overhead. Handler functions are accessible in same Go process. Same audit logging path. |
| Invite config storage | AES-256-GCM encrypted blob in JSONL | Raw WG config in invite store is a credential leak risk. Encrypt with daemon-secret key. Store encrypted, decrypt on validated consumption. |
| Short-code rate limit | 5 attempts/IP/min, 20 max per code | Prevents brute-force of 8-digit codes (100M space). 5/min = 300/hr = ~33hr to exhaust. 20 per code invalidates before exhaustion. |
| Auto-subdomain format | `svc-<random-4>` for both nip.io and BYO-domain | Consistent UX. 4 alphanumeric chars = 1.6M combinations. Collision detection via Caddy route check. |
| One-click atomicity | Route creation + DNS as single tx, rollback on failure | Prevents orphan routes (route exists but DNS missing → unreachable subdomain). Rollback = delete route if DNS fails. |
| 004 notification integration | Interface with no-op default | Keeps 006 buildable without 004. Real notification injected when 004 lands. No circular dependency. |
