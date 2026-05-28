# Developer Quickstart: Peer Onboarding Wizard

**Spec**: `specs/006-peer-onboarding/spec.md`
**Created**: 2026-05-28

---

## Prerequisites

- Go 1.22+ (matches project go.mod)
- Node 20+ (for React frontend build)
- Docker (for testcontainers-based integration tests)
- A Cloudflare account with a test zone (for DNS-01 integration tests)

---

## SSH Keys & ssh-agent

### Recommended: Passwordless SSH Key

The wizard UI supports **passwordless SSH keys only**. If your VPS requires a passphrase-protected key, use the CLI workflow instead (see below).

Generate a passwordless key:

```bash
ssh-keygen -t ed25519 -N '' -f ~/.ssh/unet-wizard
```

Copy the public key to your VPS:

```bash
ssh-copy-id -i ~/.ssh/unet-wizard.pub root@your-vps-ip
```

Use the private key path (`~/.ssh/unet-wizard`) in the wizard's SSH step.

### When You Need a Passphrase Key: CLI Workflow

If you must use a passphrase-protected key (e.g., organizational policy), the wizard cannot handle the interactive passphrase prompt. Use the CLI instead:

```bash
# Start ssh-agent and add your key
eval $(ssh-agent -s)
ssh-add ~/.ssh/your-passphrase-key

# Run bootstrap via CLI (ssh-agent handles auth)
unet bootstrap root@your-vps-ip

# After CLI bootstrap completes, open admin UI
# VPS is provisioned → wizard is skipped, dashboard shown
```

### Why No Passphrase in Wizard?

The wizard runs in the browser and communicates with the daemon backend via HTTP. SSH passphrase prompts are interactive terminal operations — they cannot be relayed through the wizard's step-based model. The daemon's SSH pool can use ssh-agent, but ssh-agent setup requires terminal access, which defeats the wizard's zero-terminal goal.

Error message you'll see if you try a passphrase-protected key:

> **This SSH key is passphrase-protected.** The wizard supports passwordless keys only.
> Options:
> 1. Generate a new key without passphrase: `ssh-keygen -t ed25519 -N ''`
> 2. Use CLI setup with ssh-agent — see quickstart §SSH Keys & ssh-agent

---

## 1. Run Wizard Locally with Mock VPS

### Setup mock SSH server

```bash
# From project root
cd src/internal/wizard/

# Run mock VPS via testcontainers (Ubuntu 22.04 + Docker + sshd)
go test -run TestMockVPS -v ./...
```

The mock VPS test starts a Docker container with:
- OpenSSH server on port 2222
- Passwordless sudo for test user
- Docker binary (mock — `docker ps` returns empty)
- Pre-configured `/etc/os-release` with Ubuntu 22.04

### Start daemon with wizard enabled

```bash
# Build daemon
go build -o unet-daemon ./src/internal/daemon/

# Run with mock config (no real VPS needed)
./unet-daemon --config=testdata/wizard-mock-config.json
```

### Walk through wizard

1. Open `http://localhost:8080` in browser.
2. Daemon detects no VPS configured → redirects to `/wizard`.
3. Complete steps: Welcome → SSH (use mock coords) → Preflight → Domain mode → Create peer → Commit.
4. Mock bootstrap completes instantly. Success page shows first URL.

### Test wizard resume

```bash
# Start wizard, complete through SSH step
# Kill daemon (Ctrl+C)
# Restart daemon
# Open browser → auto-resumes from SSH step
```

---

## 2. Generate QR + Deeplink for Existing Peer

### Unit test

```bash
cd src/internal/qr/
go test -v ./...
```

Tests verify:
- PNG generation (decode + verify content matches config)
- Deeplink URI format (`wireguard://import?config=<base64url>`)
- Round-trip: config → QR content → matches original

### Manual test via API

```bash
# Create a peer (assuming daemon running with existing VPS)
curl -X POST http://localhost:8080/v1/peers \
  -H "Content-Type: application/json" \
  -d '{"name": "test-phone"}'

# Response includes peer ID. Generate QR:
curl -X POST http://localhost:8080/v1/peers/{peer-id}/qr \
  -H "Content-Type: application/json" \
  -d '{"size": 256}'

# Response includes qr_png_base64 + deeplink_uri + config_text
# Copy qr_png_base64 → paste in browser address bar:
# data:image/png;base64,{qr_png_base64}
```

### Verify deeplink

```bash
# Decode the base64url config from deeplink
echo "W0ludGVyZmFjZV0..." | base64 -d | head -5
# Should show [Interface] section with PrivateKey, Address, etc.
```

---

## 3. Test Invite Link Flow

### HMAC URL mode

```bash
# Create invite for existing peer
curl -X POST http://localhost:8080/v1/peers/{peer-id}/invite \
  -H "Content-Type: application/json" \
  -d '{"mode": "hmac_url", "ttl_seconds": 3600, "max_uses": 1}'

# Copy the returned URL. Open in browser (or curl):
curl "http://localhost:8080/invite/{peer-id}?t=...&e=...&s=..."

# First request: 200 + config displayed
# Second request: 410 invite_consumed
```

### Short-code mode

```bash
# Create invite with short code
curl -X POST http://localhost:8080/v1/peers/{peer-id}/invite \
  -H "Content-Type: application/json" \
  -d '{"mode": "short_code", "ttl_seconds": 3600}'

# Copy the returned code. Enter at invite landing page:
curl "http://localhost:8080/invite?code=84736291"

# First request: 200 + config displayed
# Second request: 410 invite_consumed
```

### Test rate limiting

```bash
# Rapid invalid attempts (wrong code)
for i in {1..6}; do
  curl -s -o /dev/null -w "%{http_code}" "http://localhost:8080/invite?code=00000001"
  echo ""
done
# Expected: 403, 403, 403, 403, 403, 429

# After 20 total failures → code invalidated (410 on next valid attempt)
```

### Test expiry

```bash
# Create invite with 5-second TTL
curl -X POST http://localhost:8080/v1/peers/{peer-id}/invite \
  -d '{"mode": "hmac_url", "ttl_seconds": 5}'
# Wait 6 seconds
sleep 6
# Attempt to consume → 410 invite_expired
```

### Verify invite store

```bash
# Check JSONL file
cat ~/.unet/invites.jsonl | jq '.'
# Should show invite records with token_hash, encrypted_config_blob, consumed_at, etc.
```

---

## 4. Test Cloudflare DNS-01 Integration

### Prerequisites

- Cloudflare API token with Zone:Read + DNS:Edit on a test zone.
- Set `CF_TEST_TOKEN` and `CF_TEST_ZONE` env vars.

### Unit test (mock CF API)

```bash
cd src/internal/wizard/dnscheck/
go test -run TestCloudflare -v ./...
```

Uses `httptest.NewServer` to mock Cloudflare API responses. Tests:
- Token validation (valid/invalid/missing scopes)
- Zone lookup (found/not found)
- DNS record creation (success/failure)

### Integration test (real CF API)

```bash
# Only runs if CF_TEST_TOKEN and CF_TEST_ZONE are set
CF_TEST_TOKEN=op:xxx CF_TEST_ZONE=example.com \
  go test -run TestCloudflareIntegration -tags=integration -v ./...
```

Tests:
- List zones → find test zone
- Create DNS A-record → verify via DNS lookup
- Delete DNS A-record (cleanup)

**Note**: Integration tests create real DNS records in your test zone. Cleanup runs in test teardown. If test is interrupted, manual cleanup may be needed.

---

## 5. Test nip.io Fallback

### Unit test

```bash
cd src/internal/wizard/dnscheck/
go test -run TestNipIo -v ./...
```

Tests:
- DNS resolution: `10-8-0-2.nip.io` resolves to `10.8.0.2`
- Subdomain: `app.10-8-0-2.nip.io` resolves to `10.8.0.2`
- HTTP-01 feasibility: port 80 check (mock)

### Manual test

```bash
# Verify nip.io resolution works from your machine
dig +short app.10-8-0-2.nip.io
# Expected: 10.8.0.2

# In wizard: select "Use nip.io" → skip all DNS steps → commit
# Daemon registers route with nip.io subdomain → Caddy auto-issues cert
# Verify: curl -v https://app.10-8-0-2.nip.io (should show LE cert)
```

---

## 6. Test Wizard State Machine (Full E2E)

### Playwright test

```bash
cd src/web/
npx playwright test --grep "wizard"
```

Test scenarios:
1. **Happy path**: Complete all steps in order. Verify success page shows URL + QR.
2. **Interrupt + resume**: Complete SSH step → close browser → reopen → verify resume from SSH.
3. **Back navigation**: Complete through domain check → go back to SSH → re-enter → verify state updated.
4. **Preflight failure**: Provide SSH coords to mock VPS with unsupported distro → verify blocking failure message.
5. **Domain check failure**: Enter non-resolving domain → verify error → switch to nip.io → continue.

### Run specific test

```bash
npx playwright test --grep "wizard happy path"
```

---

## Test Matrix Summary

| Test type | Command | What it covers |
|-----------|---------|---------------|
| Unit (wizard orchestrator) | `go test ./src/internal/wizard/ -v` | State transitions, persistence, validation |
| Unit (preflight) | `go test ./src/internal/wizard/preflight/ -v` | OS detection, disk/sudo/docker checks |
| Unit (dnscheck) | `go test ./src/internal/wizard/dnscheck/ -v` | A-record lookup, CF detection |
| Unit (QR) | `go test ./src/internal/qr/ -v` | PNG generation, deeplink URI |
| Unit (invite) | `go test ./src/internal/invite/ -v` | HMAC signing, short-code, consumption |
| Integration (mock SSH) | `go test -run TestMockVPS ./src/internal/wizard/ -v` | Full wizard flow with testcontainers |
| Integration (Cloudflare) | `go test -tags=integration ./src/internal/wizard/dnscheck/ -v` | Real CF API (requires env vars) |
| E2E (Playwright) | `npx playwright test --grep "wizard"` | Full browser-based wizard walkthrough |
