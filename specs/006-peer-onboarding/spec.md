# Feature Specification: Peer Onboarding Wizard

**Feature Branch**: `006-peer-onboarding`
**Created**: 2026-05-27
**Status**: Draft
**Input**: First-run VPS setup wizard, mobile peer onboarding via QR codes, one-click "share localhost" UX — collapsing the SSH+manual-config cliff that blocks OSS adoption.

## Clarifications

### Session 2026-05-27

- Q: Which VPS provider/distros to validate in preflight? → **Decision: Ubuntu 22.04+ and Debian 12+ only (others = best-effort, no guarantee)** — Covers ~80% of cloud VPS; tight test matrix; clear support boundary for OSS users.
- Q: Mobile WG app deeplinks — Android intent scheme vs iOS universal link? → A: [NEEDS CLARIFICATION: support both at launch, or Android-only for v0.1? Recommendation: both at launch. Android uses `intent://` scheme with `com.wireguard.android`; iOS uses `wireguard://` URL scheme. Both documented in WireGuard app source.]
- Q: Invite-link transport mechanism? → A: [NEEDS CLARIFICATION: in-app share sheet (Web Share API), copyable URL, email integration, or all three? Recommendation: copyable URL + Web Share API fallback for mobile. Email integration is a separate feature — out of scope for v0.1.]
- Q: nip.io vs sslip.io for default no-domain mode? → **Decision: nip.io, TLS via Let's Encrypt HTTP-01** — Caddy auto-issues, works out-of-box; requires port 80 on VPS but that's already needed for ingress anyway.
- Q: Auth for invite links — pre-shared secret in URL query, short code requiring out-of-band exchange, or both modes? → **Decision: Both modes supported — default HMAC-signed URL, optional short-code as advanced mode** — Default UX-friendly; security-conscious users (or out-of-band sharing scenarios) can opt into short-code.
- Q: Wizard telemetry for the 95% usability target? → **Decision: Strict opt-in, default OFF; success criterion becomes aspirational/manually-tested if telemetry disabled** — OSS principle: no tracking without explicit consent; ethical default outweighs measurement precision.

### Session 2026-05-27 (round 1)

| Topic | Decision |
|---|---|
| VPS distro preflight scope | Ubuntu 22.04+ and Debian 12+ only (others = best-effort, no guarantee) |
| Invite link auth model | Both modes — default HMAC-signed URL, optional short-code as advanced mode |
| Telemetry for usability metrics (95% SC) | Strict opt-in, default OFF; SC becomes aspirational if telemetry disabled |
| nip.io quick-start TLS strategy | Let's Encrypt HTTP-01 |

See inline notes in each FR / section for full rationale.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Zero-to-First-URL via Wizard (Priority: P1)

As a new user, I open the admin UI for the first time and a wizard walks me through providing VPS SSH credentials and a domain (or choosing nip.io) so that within 5 minutes I see my first working public URL — without touching a terminal.

**Why this priority**: This IS the adoption gate. If the wizard doesn't work smoothly, nothing else matters. Every other feature assumes the user already has a running VPS+tunnel.

**Independent Test**: Provision a fresh Ubuntu VPS. Open `localhost:8080` for the first time. Complete the wizard. Verify that `https://<subdomain>.<domain>` returns a response from a local test server.

**Acceptance Scenarios**:

1. **Given** a fresh unet binary and a fresh Ubuntu 22.04 VPS with SSH access, **When** I open the admin UI and complete the wizard (enter VPS IP, SSH user, SSH key/password, choose domain or nip.io), **Then** the wizard bootstraps AmneziaWG + Caddy on the VPS, creates the first peer, establishes the tunnel, and displays a working `https://` URL — all in under 5 minutes.
2. **Given** the wizard is on the SSH step, **When** I enter invalid SSH credentials, **Then** the wizard shows a clear error ("Connection refused" / "Auth failed") immediately and does NOT proceed to the next step.
3. **Given** the wizard is on the domain step, **When** I choose nip.io, **Then** the wizard skips DNS configuration entirely and auto-generates a `<wg-ip>.nip.io` subdomain, proceeding directly to bootstrap.

---

### User Story 2 - Add Mobile Peer via QR Code (Priority: P1)

As a user, I add a peer named "phone" in the admin UI, scan the displayed QR code with my mobile WireGuard app, and am connected to the tunnel within 30 seconds — without manually copying any keys or IP addresses.

**Why this priority**: Peer onboarding is the second-worst friction point after VPS setup. QR codes are the standard UX (Tailscale, WireGuard, 1.1.1.1 all use them). Without this, adding a phone requires manual config file creation and transfer.

**Independent Test**: Create a peer named "phone" via the admin UI. Scan the QR code with the WireGuard app on Android or iOS. Verify the tunnel connects (ping 10.8.x.1 from the phone).

**Acceptance Scenarios**:

1. **Given** an active VPS + tunnel, **When** I click "Add Peer" and enter the name "phone", **Then** a QR code is displayed containing the full WireGuard client config (including AmneziaWG obfuscation params).
2. **Given** the QR code is displayed, **When** I scan it with the WireGuard Android app, **Then** the tunnel profile is imported and connects successfully within 30 seconds.
3. **Given** the mobile device does not have the WireGuard app installed, **When** I scan the QR code with the device camera, **Then** the decoded content is a plain-text WireGuard config that can be manually imported, with instructions displayed alongside the QR code.

---

### User Story 3 - One-Click Port Exposure (Priority: P1)

As a user, I click "Expose port 3000" in the admin UI and immediately see the public URL — no manual Caddy config, no DNS record creation, no terminal commands.

**Why this priority**: This is the core "ngrok-like" value proposition. The user's mental model is "click button, get URL". If this requires more than one click, we've failed.

**Independent Test**: Run a local HTTP server on port 3000. Click "Expose port 3000" in the admin UI. Verify the displayed URL serves the local app's response within 3 seconds.

**Acceptance Scenarios**:

1. **Given** an active tunnel and a local app on port 3000, **When** I click "Expose port 3000", **Then** the admin UI shows the public URL (e.g., `https://app.mydomain.com`) and the URL is reachable within 3 seconds.
2. **Given** Cloudflare DNS mode is active, **When** I expose a port, **Then** the system creates the DNS A-record AND updates Caddy config atomically — both succeed or both roll back.
3. **Given** the requested subdomain is already in use, **When** I attempt to expose the port, **Then** the admin UI shows a clear conflict error and suggests an alternative subdomain.

---

### User Story 4 - Cloudflare Auto-DNS via Wizard (Priority: P2)

As a user with a Cloudflare-managed domain, the wizard detects this and offers to set up DNS automatically via a Cloudflare API token — so I don't need to manually create A-records or configure DNS-01 challenges.

**Why this priority**: Eliminates the most error-prone manual step for BYO-domain users. P2 because nip.io users skip it entirely and BYO-domain users CAN do it manually.

**Independent Test**: During wizard, enter a domain managed by Cloudflare. Provide a Cloudflare API token with correct scopes. Verify the wizard creates the required DNS records and Caddy TLS certificate without manual intervention.

**Acceptance Scenarios**:

1. **Given** a domain with DNS nameservers pointing to Cloudflare, **When** the wizard detects this, **Then** it prompts for a Cloudflare API token with clear scope requirements (`Zone:Read`, `DNS:Edit`).
2. **Given** a valid Cloudflare token with correct scopes, **When** the wizard completes, **Then** a wildcard certificate is issued via DNS-01 challenge and all future subdomains work without per-record DNS changes.
3. **Given** a Cloudflare token with insufficient scopes (e.g., `Zone:Read` only), **When** the wizard validates the token, **Then** it shows a specific error listing the missing scopes.

---

### User Story 5 - Shareable Peer Invite Link (Priority: P2)

As a user, I generate a secure shareable link for a peer configuration so that a non-technical recipient can import the WireGuard config by clicking the link in their browser — without installing unet or accessing the admin UI.

**Why this priority**: Enables collaborative onboarding (team member joins the network). P2 because the primary flow (QR code) covers the single-user case; invite links add multi-user convenience.

**Independent Test**: Create a peer. Generate an invite link. Open the link on a different device. Verify the page guides the user through importing the WireGuard config (download .conf file or display QR code).

**Acceptance Scenarios**:

1. **Given** a peer with a generated config, **When** I click "Share Peer", **Then** a one-time-use link is generated with a configurable TTL (default: 24 hours).
2. **Given** an unexpired invite link, **When** the recipient opens it in a browser, **Then** the page displays the WireGuard config as a downloadable file AND as a QR code for mobile scanning.
3. **Given** an invite link that has already been consumed, **When** the recipient opens it, **Then** the page shows "This invite has already been used" with no config exposed.
4. **Given** an invite link past its TTL, **When** the recipient opens it, **Then** the page shows "This invite has expired" with no config exposed.

---

### User Story 6 - BYO-Domain vs nip.io Trade-Off (Priority: P3)

As a user, the wizard clearly explains the trade-offs between bringing my own domain (full TLS, custom subdomains) and using nip.io (zero-config, but limited TLS options) so I can make an informed choice.

**Why this priority**: P3 because both paths must work regardless of explanation quality, and the choice is obvious to most technical users. The explanation is a UX polish item.

**Independent Test**: Reach the domain-selection step in the wizard. Verify both options are presented with clear pros/cons. Select each. Verify the subsequent wizard flow adapts correctly.

**Acceptance Scenarios**:

1. **Given** the wizard's domain-selection step, **When** both options are presented, **Then** BYO-domain shows: "Custom HTTPS URLs, your brand, requires DNS access" and nip.io shows: "Instant setup, auto-generated URLs, no domain needed, limited TLS".
2. **Given** I select BYO-domain, **When** I proceed, **Then** the wizard asks for the domain name and DNS configuration details.
3. **Given** I select nip.io, **When** I proceed, **Then** the wizard skips all DNS configuration and auto-assigns a subdomain.

---

### User Story 7 - Wizard Resume After Interruption (Priority: P3)

As a user, if the wizard is interrupted (browser crash, laptop sleep, accidental close), I can reopen the admin UI and resume from where I left off — no lost progress.

**Why this priority**: P3 because the wizard is designed to complete in under 5 minutes — interruption is unlikely but frustrating when it happens. Persisted state is cheap insurance.

**Independent Test**: Start the wizard, complete through the SSH step, close the browser. Reopen. Verify the wizard resumes at the domain-selection step with VPS data pre-filled.

**Acceptance Scenarios**:

1. **Given** the wizard is partially completed (SSH validated, domain not yet chosen), **When** the browser is closed and reopened to the admin UI, **Then** the wizard resumes at the domain-selection step with validated VPS data pre-filled.
2. **Given** the wizard was interrupted during VPS bootstrap (in-progress), **When** the browser is reopened, **Then** the wizard shows the bootstrap progress and resumes monitoring — or shows a clear "bootstrap was interrupted, retry?" prompt.
3. **Given** the wizard completed successfully before the interruption, **When** the browser is reopened, **Then** the admin UI shows the normal dashboard (no wizard).

---

### Edge Cases

- **VPS SSH succeeds but lacks sudo/docker permissions**: Preflight MUST detect missing `sudo` access or absent Docker daemon and show a specific remediation message ("User needs sudo access and Docker must be installed").
- **Domain DNS A-record points to wrong IP**: Wizard MUST perform a DNS A-record lookup and compare against the VPS IP. Mismatch → warning with the actual A-record value displayed, allowing user to proceed anyway (DNS propagation delay) or fix.
- **Cloudflare token has insufficient scopes**: Token validation step MUST test actual scope by attempting a harmless read operation (e.g., list zones). Missing `DNS:Edit` → specific error listing missing scopes, not a generic "unauthorized".
- **Mobile WG app not installed**: QR code MUST encode the raw `.conf` text (not a proprietary format). Camera scan produces readable text. Admin UI MUST also display the config as copyable text and a downloadable `.conf` file alongside the QR code.
- **nip.io subdomain conflict**: Unlikely (nip.io uses the IP itself as the subdomain — `10.8.0.1.nip.io` always resolves to `10.8.0.1`). No real conflict possible. [NEEDS CLARIFICATION: if we generate human-readable prefixes like `myapp.10.8.0.1.nip.io`, ensure the prefix doesn't collide with existing routes in Caddy config.]
- **Invite link recipient on incompatible OS / no WG client**: The invite landing page MUST detect OS and link to the appropriate WireGuard client download page. Config download still works regardless.
- **Wizard interrupted between SSH-validation and bootstrap commit**: VPS is in a clean state (wizard hasn't mutated it yet). On resume, wizard re-validates SSH and proceeds. No partial-mutation risk because bootstrap is a single atomic operation.
- **Port exposure subdomain conflict**: If the auto-generated subdomain (or user-chosen one) already exists in Caddy config, the system MUST reject with `409 route_conflict` and suggest an alternative. No silent overwrite.
- **VPS preflight fails on unsupported distro**: Wizard shows "Unsupported OS: <detected>. Supported: Ubuntu 22.04/24.04, Debian 12." with a link to documentation. Does NOT proceed with bootstrap.
- **Wizard started on already-provisioned VPS**: Wizard detects existing unet deployment (check for running Docker containers matching `unet-*`) and offers "Re-provision" or "Connect to existing" options. No silent overwrite.
- **SSH key vs password auth**: Wizard SHOULD support passwordless SSH keys for bootstrap step. Passphrase-protected SSH keys are NOT supported by wizard UI (passwordless keys via `ssh-keygen -N ''` or use the CLI `unet bootstrap` command with `ssh-agent` for passphrase-managed keys). Wizard MUST detect passphrase-protected key during SSH validation and present clear error message redirecting user to the CLI workflow with link to quickstart §SSH Keys & ssh-agent. Password authentication is fully supported.
- **Passphrase-protected SSH key detection**: If the user provides an SSH key that requires a passphrase, the SSH auth attempt fails with a distinguishable error. Wizard MUST display: "This SSH key is passphrase-protected. The wizard supports passwordless keys only. Options: (1) Generate a new key without passphrase: `ssh-keygen -t ed25519 -N ''` (2) Use CLI setup with ssh-agent — see [SSH Keys & ssh-agent](#TODO-link-to-quickstart)." Wizard blocks commit until user provides a passwordless key or switches to password auth.
- **nip.io DNS unreachable at wizard time**: If `nip.io` DNS resolution fails during wizard, show "nip.io DNS is currently unreachable. You can proceed without DNS verification or use your own domain." Fall through to manual DNS mode.

## Requirements *(mandatory)*

### Functional Requirements

**Wizard Flow (P1)**:

- **FR-001**: The admin UI MUST present a first-run wizard when no VPS is configured (`vps.isProvisioned == false`). The wizard is a multi-step state machine with the following steps in order: (1) Welcome + prerequisites check, (2) VPS SSH credentials, (3) SSH validation + VPS preflight, (4) Domain selection (BYO-domain or nip.io), (5) DNS configuration (if BYO-domain), (6) Bootstrap execution, (7) First peer creation, (8) First URL display. Each step MUST be independently validated before proceeding to the next.
- **FR-002**: The wizard MUST persist partial state to `~/.unet/wizard-state.json` after each completed step. On admin UI load, if `vps.isProvisioned == false` AND `wizard-state.json` exists, the wizard MUST resume from the last completed step. Completed wizard state is deleted on successful completion (no stale state after onboarding).
- **FR-003**: The wizard MUST perform SSH connection validation as a live test before committing to the bootstrap step. Validation includes: (a) TCP connect to `host:port` (default 22) within 10s timeout, (b) successful SSH auth (key or password), (c) `sudo docker ps` execution to verify Docker access. All three MUST pass. Failure shows a specific error for which step failed.

**VPS Preflight (P1)**:

- **FR-004**: After SSH validation succeeds, the wizard MUST run a preflight check on the VPS that verifies: (a) OS/distro is in the supported list, (b) Docker daemon is running, (c) VPS has ≥1GB RAM, (d) Ports 443, 80 (if manual DNS mode), and the configured AmneziaWG UDP port are available (not bound by other services). [NEEDS CLARIFICATION: minimum RAM requirement — 1GB sufficient for AmneziaWG+Caddy, or require 2GB?] Preflight results are displayed to the user before bootstrap. Blocking failures prevent progression; warnings allow progression with user confirmation.

**Domain Validation (P1)**:

- **FR-005**: For BYO-domain mode, the wizard MUST validate the domain by: (a) DNS lookup for A-record matching VPS IP (or wildcard A-record), (b) DNS nameserver check to detect Cloudflare management, (c) Caddy TLS feasibility precheck (attempt HTTP-01 challenge dry-run or verify DNS-01 plugin availability if Cloudflare detected). Mismatch → specific warning with remediation guidance.

**QR Code Generation (P1)**:

- **FR-006**: The admin UI MUST generate a QR code encoding the complete WireGuard client configuration file content (including all AmneziaWG obfuscation parameters: Jc, Jmin, Jmax, S1-S4, H1-H4, I1-I5, PersistentKeepalive, MTU) when creating a peer. The QR code MUST use the standard WireGuard config format (`[Interface]` / `[Peer]` sections) compatible with the official WireGuard apps on Android and iOS.
- **FR-007**: Alongside the QR code, the admin UI MUST display: (a) the peer config as copyable plain text, (b) a "Download .conf file" button, (c) platform-specific instructions ("Open WireGuard app → Scan QR code" for mobile, "Import tunnel from file" for desktop). This ensures onboarding works even without QR camera access.

**One-Click Port Exposure (P1)**:

- **FR-008**: The admin UI MUST provide a "Expose Port" action that, given a local port number and optional subdomain label, atomically: (a) validates the subdomain per FR-012 rules from spec 001-init, (b) allocates the subdomain (auto-generates if not provided — e.g., `svc-<random-4>` for nip.io mode, or user-chosen for BYO-domain), (c) creates the Caddy route via admin API, (d) if Cloudflare DNS mode, creates the DNS A-record, (e) returns the full public URL to the UI. If any step fails, all preceding steps MUST be rolled back.
- **FR-009**: For nip.io mode, the system MUST auto-generate subdomains as `<label>.<wg-client-ip>.nip.io` where `<label>` is user-chosen or auto-generated (e.g., `app.10.8.0.2.nip.io`). No DNS record creation needed — nip.io resolves automatically. Caddy uses HTTP-01 TLS challenge per-subdomain (or no TLS if nip.io is used purely for HTTP testing). **Decision: Let's Encrypt HTTP-01** — Caddy auto-issues per-subdomain; rate limits managed by Caddy's built-in certificate management (resolved Clarifications 2026-05-27 round 1).

**Cloudflare DNS-01 Integration (P2)**:

- **FR-010**: During wizard domain setup, if Cloudflare is detected (nameserver check), the wizard MUST offer to configure automated DNS via Cloudflare API token. The wizard MUST validate the token by: (a) calling Cloudflare API to list zones and verifying the domain exists, (b) attempting a DNS record read to verify `DNS:Edit` scope, (c) storing the token in `~/.unet/config.json` (file mode 0600, value redacted in logs per FR-011 from spec 001-init).

**Peer Creation Flow (P1)**:

- **FR-011**: The admin UI peer creation flow MUST hide all cryptographic details from the user. The user provides only: (a) peer name (validated per FR-012 from spec 001-init — `json.Marshal` for all user values), (b) optional label/tags. Key generation, IP allocation, server-side `awg0.conf` update, and `awg syncconf` are performed automatically. The user never sees private keys, public keys, or IP addresses unless they opt into "advanced" view.

**Shareable Peer Invite Links (P2)**:

- **FR-012**: The admin UI MUST generate shareable peer-invite links with the following properties: (a) the URL contains an encrypted peer-config blob (symmetric encryption with a key held by the daemon — NOT the raw WG config in the URL), (b) an HMAC signature over the blob + timestamp, (c) a configurable TTL (default: 24h, max: 72h), (d) a server-side one-time-use flag. The daemon tracks consumed invite links in `~/.unet/config.json` (consumed IDs, garbage-collected after TTL expiry).
- **FR-013**: The invite-link landing page (served by the daemon at `GET /invite/<id>`) MUST: (a) validate the link signature and TTL, (b) check one-time-use flag, (c) if valid, display the WireGuard config as QR code + downloadable .conf + copyable text, (d) detect the visitor's OS and link to the appropriate WireGuard client, (e) mark the link as consumed after the config is first displayed. [NEEDS CLARIFICATION: "consumed after first display" means the invite is single-view. Is this too aggressive? Alternative: consumed after N views (N=3) or after download. Recommendation: consumed after download or QR scan, not just page load. Track via client-side confirmation callback.]

**Wizard Undo/Redo (P3)**:

- **FR-014**: Before the bootstrap step commits, the user MUST be able to navigate back to any previous wizard step and modify inputs. Changed inputs re-trigger validation for that step and all dependent steps. After bootstrap commits, backward navigation is disabled — the user can only re-provision from the dashboard.

## Key Entities

- **WizardState**: Persisted partial state of the first-run wizard. Attributes: `currentStep` (enum: welcome/ssh/domain/dns/bootstrap/peer/done), `sshHost`, `sshUser`, `sshPort`, `sshAuthType` (key/password), `domainMode` (byo/nipio), `domainName`, `cloudflareToken` (redacted), `preflightResults`, `bootstrapStatus`, `createdPeerId`, `createdAt`, `updatedAt`.
- **InviteLink**: Shareable peer-config link. Attributes: `id` (UUID), `peerId`, `encryptedConfigBlob`, `hmacSignature`, `createdAt`, `expiresAt`, `consumedAt` (null until consumed), `consumedIp`, `maxUses` (default: 1), `useCount`.
- **PreflightResult`: VPS compatibility check result. Attributes: `osDistro`, `osVersion`, `dockerInstalled`, `dockerRunning`, `ramMb`, `portAvailability` (map: port → free/bound), `compatible` (bool), `warnings` (string array), `checkedAt`.

## Assumptions

- **Wizard runs in the embedded admin UI**: The first-run wizard is part of the existing React UI served at `localhost:8080`. No separate application or Electron window.
- **Bootstrap reuses 001-init FR-001 flow**: The wizard's bootstrap step invokes the same VPS provisioning logic defined in spec 001-init (FR-001: docker-compose deploy, iptables, UFW). No parallel bootstrap implementation.
- **Peer creation reuses 002-api-control-plane**: Peer creation during the wizard uses the same backend logic as `POST /api/v1/peers` from spec 002 (FR-005). No separate code path.
- **One admin user = one VPS**: The wizard is designed for the single-admin, single-VPS model established in specs 001 and 002. Multi-VPS wizard flows are out of scope.
- **nip.io availability is best-effort**: The system does not guarantee nip.io DNS uptime. If nip.io is unreachable, the wizard falls back to BYO-domain mode with a clear message.
- **Invite links are served by the local daemon**: The invite landing page is hosted by the daemon at `localhost:8080/invite/<id>`. This means the inviter's daemon must be running for the recipient to access the invite. [NEEDS CLARIFICATION: is this acceptable? Alternative: encode the WG config in a self-contained URL that works offline (security trade-off — config in URL). Recommendation: daemon-served for v0.1 (proper auth + one-time-use). Future: upload encrypted config blob to VPS for remote serving.]

## Out of Scope (for this spec)

- Cloud VPS auto-provisioning via provider APIs (DigitalOcean/Hetzner/Vultr — future spec)
- Team/org management, multi-user roles, permission tiers
- Billing-related onboarding flows, payment integration, trial periods
- Mobile companion app (wizard is browser-only)
- Desktop tray icon / system notification integration (spec 004-desktop-integration)
- Wizard localization / i18n (English only for v0.1)
- Automated upgrade path from manual SSH-based setup to wizard-managed setup

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Wizard zero-to-first-public-URL completes in under 5 minutes on a freshly provisioned Ubuntu 22.04 VPS with 2GB RAM and 1Gbps link (excluding AmneziaWG Docker image build time on first run).
- **SC-002**: QR code scan to peer connected in under 30 seconds on a consumer mobile device (Android 13+ or iOS 16+) with the official WireGuard app installed.
- **SC-003**: One-click "Expose Port" action updates Caddy configuration and serves the first request through the public URL in under 3 seconds (excluding DNS propagation for BYO-domain HTTP-01 mode; nip.io and Cloudflare DNS-01 modes are immediate).
- **SC-004**: 95% of users who start the wizard complete it without referencing external documentation. Measured via opt-in telemetry (anonymized step-completion timestamps). Baseline target for v0.1; may be adjusted based on opted-in user data.
- **SC-005**: Invite links are consumed exactly once and expire precisely at the configured TTL. Zero config leaks from consumed/expired links (verified by automated test: expired link returns no config, consumed link returns no config).
- **SC-006**: BYO-domain precheck correctly identifies DNS misconfiguration (wrong A-record IP, missing wildcard, Cloudflare scope issues) BEFORE bootstrap commits — zero silent failures that require manual VPS cleanup.
- **SC-007**: Wizard resume works correctly after interruption at any step — partial state is persisted and restored without data loss. Verified by interrupting at each of the 8 wizard steps and confirming correct resume.
- **SC-008**: The wizard adds zero new terminal/CLI requirements — everything is accomplishable via the browser UI alone. A user who has the unet binary and a VPS can complete onboarding without opening a terminal (SSH key setup is the only possible exception, and the wizard supports password auth as alternative).