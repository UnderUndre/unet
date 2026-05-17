# Security Review: Unet Core Architecture

**Date**: 2026-05-17
**Reviewer**: Automated Security Subagent
**Scope**: `src/cmd/`, `src/internal/**`
**Codebase**: ~5,400 LOC across 36 Go source files

---

## Findings

### [CRITICAL] SEC-001: No API Authentication on Localhost Daemon

- **File**: `src/internal/daemon/server.go:52-70`
- **Description**: The HTTP daemon binds to `127.0.0.1:<port>` and exposes full administrative API without any authentication middleware. A `UIToken` is generated at first run (`config.go:defaultConfig`) and stored in `config.json`, but no handler checks for it. Any local process (or browser XSS on any origin) can call `POST /api/vps/configure`, `POST /api/tunnel/connect`, `POST /api/ports`, etc. This grants full control over SSH credentials, VPS provisioning, tunnel lifecycle, and DNS management.
- **Recommendation**: Add authentication middleware that validates `UIToken` (from `config.json`) via `Authorization: Bearer <token>` header or a session cookie. Reject all unauthenticated requests with 401.
- **Status**: OPEN

---

### [CRITICAL] SEC-002: Data Race on `Manager.status` — Unprotected Write Path

- **File**: `src/internal/tunnel/manager.go:476-480` (`setStatus`)
- **Description**: `setStatus()` sets `m.status = s` directly without acquiring `m.mu`. This is called from `Connect()` (line ~113: `m.setStatus("connecting")`) after releasing the mutex, and from `fail()` (line ~262). Meanwhile, `Status()` (line 277) reads `m.status` under `m.mu.Lock()`. This is a textbook data race — one goroutine writing without a lock while another reads under a lock. Go race detector will flag this.
- **Recommendation**: Add `m.mu.Lock()` / `defer m.mu.Unlock()` to `setStatus()`. If the caller already holds the lock, refactor to use a locked/unlocked variant pair.
- **Status**: OPEN

---

### [CRITICAL] SEC-003: Data Race on `VPSHandler.task` — Cross-Goroutine Access Without Synchronization

- **File**: `src/internal/daemon/api_vps.go:30-34` (`provisionTask`), `api_vps.go:93-95` (write in handler goroutine), `api_vps.go:128-135` (write in background goroutine)
- **Description**: `VPSHandler.task` is a pointer to `provisionTask`. It is set in the HTTP handler goroutine (`h.task = &provisionTask{...}`) and then mutated from the background `runProvision` goroutine (`h.task.Status = "failed"`). No mutex, channel, or atomic protects these accesses. This is a data race that can cause corrupted reads in `handleVPSStatus`.
- **Recommendation**: Protect `task` with a mutex, use `sync/atomic.Pointer`, or communicate status via a channel.
- **Status**: OPEN

---

### [HIGH] SEC-004: Mutex Unlock/Relock Pattern in `BootstrapMTLS` Creates Race Window

- **File**: `src/internal/proxy/caddy_mtls.go:175-181`
- **Description**: `BootstrapMTLS` holds `c.mu`, then releases it (`c.mu.Unlock()`) before calling `c.GenerateClientCert()`, then re-acquires it (`c.mu.Lock()`). During the unlock window, another goroutine can call `AddRoute`, `RemoveRoute`, `ToggleMTLS`, or a second `BootstrapMTLS`. This can lead to concurrent modification of the Caddy admin API state or the HTTP client.
- **Recommendation**: Refactor `GenerateClientCert` to not require the mutex (it only calls `c.cfgMgr.Update`, which has its own lock). Alternatively, use a separate lock for cert generation vs. route manipulation.
- **Status**: OPEN

---

### [HIGH] SEC-005: Server Private Key Exposed via Unmasked `ServerMirror` in API Responses

- **File**: `src/internal/config/config.go` (`cloneAndMask`), `src/internal/tunnel/server_config.go:241` (`ServerMirrorJSON.ServerPrivateKey`)
- **Description**: `cloneAndMask()` masks individual `SecretString` fields (VPS.Password, Tunnel.PrivateKey, etc.) but does NOT inspect or mask the contents of the `ServerMirror` JSON blob. `ServerMirrorJSON.ServerPrivateKey` contains the WireGuard server private key in base64. If `GetMasked()` is called and the serialized config is exposed (e.g., through a future `/api/config` endpoint), the server private key would be leaked. The current API endpoints do not expose the mirror directly, but the abstraction is broken.
- **Recommendation**: Parse the `ServerMirror` JSON in `cloneAndMask`, mask the `serverPrivateKeyB64` field, and re-serialize. Or mark the field as `SecretString` and handle it at the config level.
- **Status**: OPEN

---

### [HIGH] SEC-006: Temp File with Sensitive AWG Config Leaked on SSH Failure

- **File**: `src/internal/tunnel/peer.go:239-252` (`syncConf`)
- **Description**: `syncConf` writes the stripped AWG config to `/tmp/awg0-strip.conf` on the remote server, then runs `awg syncconf`, then `rm -f /tmp/awg0-strip.conf`. If the SSH session drops or `awg syncconf` fails mid-execution, the cleanup `rm -f` never runs. The temp file contains the full server WireGuard configuration (including private key and PSK) in plaintext. Any user on the VPS could read `/tmp/awg0-strip.conf` before cleanup.
- **Recommendation**: Use `mktemp` with restrictive permissions (`mktemp /tmp/awg0.XXXXXX && chmod 600`), or pipe the config via stdin to `awg syncconf` if supported. Add a trap/finally pattern: execute `rm -f` as a separate SSH command on reconnect.
- **Status**: OPEN

---

### [HIGH] SEC-007: `isPrivileged()` Always Returns `false` on Windows

- **File**: `src/internal/daemon/api_vps.go:239-244` (`isPrivileged`)
- **Description**: `isPrivileged()` uses `os.Getuid() == 0` which is a POSIX-only check. On Windows, `os.Getuid()` always returns `-1`, so the function always returns `false`. This means `POST /api/tunnel/connect` will always reject with 503 on Windows ("Administrator/root privileges required"), blocking a core use case. Conversely, the lack of a real Windows privilege check means other Windows-only paths may be unprotected.
- **Recommendation**: Add a Windows-specific check using `golang.org/x/sys/windows` to test for admin privileges (e.g., `OpenCurrentProcessToken` + `GetTokenInformation` with `TokenElevation`). Use build tags like `isPrivileged()` in the config package pattern.
- **Status**: OPEN

---

### [HIGH] SEC-008: VPS Host Not Validated for Format — SSRF Vector

- **File**: `src/internal/daemon/api_vps.go:62-64` (`handleVPSConfigure`)
- **Description**: The VPS host field is checked only for emptiness. There is no validation that it is a valid IP address, hostname, or FQDN. A user could supply `127.0.0.1`, `169.254.169.254` (AWS metadata), `0.0.0.0`, or any RFC 1918 address, causing the daemon to SSH to an unintended target. While `validateHost` in `provisioner/ssh.go` blocks shell metacharacters, it does not block private IPs or localhost.
- **Recommendation**: Add hostname/IP format validation in the API handler (regex for hostname, `net.ParseIP` for IPs). Optionally warn on private/reserved IP ranges. Consider blocking link-local and loopback addresses.
- **Status**: OPEN

---

### [HIGH] SEC-009: SSHPort Not Range-Validated

- **File**: `src/internal/daemon/api_vps.go:68-70`
- **Description**: `req.SSHPort` is defaulted to 22 if `<= 0`, but no upper bound check exists. Values like `99999` or `65536` (out of valid TCP port range) are accepted and stored, causing connection failures or unexpected behavior later.
- **Recommendation**: Add `req.SSHPort > 65535` check and reject with 400 Bad Request.
- **Status**: OPEN

---

### [MEDIUM] SEC-010: TOCTOU Race in Port Creation — Duplicate Subdomain

- **File**: `src/internal/daemon/api_ports.go:119-130` (duplicate check), `api_ports.go:152-165` (persist)
- **Description**: `handleCreate` reads the config to check for duplicate subdomains (`for _, ep := range cfg.ExposedPorts`), then later calls `cfgMgr.Update()` to append the new port. Between the `Get()` and `Update()`, another concurrent `handleCreate` request for the same subdomain would pass the duplicate check. `cfgMgr.Update()` holds an internal lock, but the read-then-write is not atomic.
- **Recommendation**: Move the duplicate check inside the `Update` callback so it runs atomically under the config manager's write lock.
- **Status**: OPEN

---

### [MEDIUM] SEC-011: No CORS Policy on API Endpoints

- **File**: `src/internal/daemon/server.go:52-70`
- **Description**: The HTTP server sets no CORS headers. Since the SPA frontend and the API are served from the same origin (127.0.0.1), same-origin policy prevents cross-origin issues. However, without explicit CORS denial headers, a malicious website could potentially issue requests to the unauthenticated API via `<form>` POSTs or `fetch()` in simple-request mode.
- **Recommendation**: Add `Access-Control-Allow-Origin: null` or same-origin-only policy on API routes. Block cross-origin requests in the root handler.
- **Status**: OPEN

---

### [MEDIUM] SEC-012: Self-Signed mTLS Certificate — No Revocation Mechanism

- **File**: `src/internal/proxy/caddy_mtls.go:51-95` (`GenerateClientCert`)
- **Description**: Client certificates for Caddy admin API mTLS are self-signed with a 365-day validity period. There is no certificate revocation mechanism (no CRL, no OCSP). If a certificate is compromised, there is no way to invalidate it without regenerating a new certificate and re-bootstrapping the entire mTLS flow. The Caddy `access_control` array supports public key registration but not deletion.
- **Recommendation**: Implement a public key removal endpoint/function in the Caddy access_control flow. Add a `RotateMTLS` function that generates a new cert and removes the old key from Caddy.
- **Status**: OPEN

---

### [MEDIUM] SEC-013: Cloudflare API Token Stored in Plaintext in Config

- **File**: `src/internal/config/config.go` (`DNSConfig.Token`), `src/internal/daemon/api_dns.go:44`
- **Description**: The Cloudflare API token is stored as `SecretString` and masked in `GetMasked()` — this is correct. However, the token is still stored in plaintext in `~/.unet/config.json`. On disk, it is protected only by file permissions (0600). No encryption at rest.
- **Recommendation**: Consider using OS-specific secret storage (macOS Keychain, Windows Credential Manager, Linux libsecret) for long-lived API tokens. At minimum, document the threat model clearly.
- **Status**: OPEN

---

### [MEDIUM] SEC-014: `setFilePerm` Failure Silently Ignored

- **File**: `src/internal/config/config.go:248` (`saveLocked`)
- **Description**: After the atomic write (temp file + rename), `setFilePerm` is called on the temp file before rename. If it fails (e.g., on a system with unusual ACL setup), the error is only logged as a warning (`slog.Warn`). The config file containing all secrets (SSH passwords, WireGuard keys, API tokens) is then renamed into place with potentially permissive permissions.
- **Recommendation**: Return the error from `setFilePerm` as a hard failure, not a warning. The config file containing secrets should never exist with wrong permissions.
- **Status**: OPEN

---

### [MEDIUM] SEC-015: Known Hosts File — First-Connection Trust-On-First-Use Without User Confirmation

- **File**: `src/internal/provisioner/ssh.go:78-104` (`hostKeyCallback`)
- **Description**: The SSH host key callback implements Trust-On-First-Use (TOFU): on first connection, the host key is automatically stored without user confirmation. This means a MITM attack during the first SSH connection to the VPS would go undetected. The daemon runs unattended, so interactive confirmation is impractical, but the risk should be documented.
- **Recommendation**: Document the TOFU model and its MITM risk in user-facing docs. Consider providing an option to pre-seed known_hosts via CLI flag.
- **Status**: OPEN

---

### [LOW] SEC-016: `dockerExecCmd` vs `dockerExec` — Inconsistent Shell Escaping

- **File**: `src/internal/tunnel/server_config.go:231` (`dockerExecCmd`), `src/internal/tunnel/peer.go:391` (`dockerExec`)
- **Description**: `dockerExec` in `peer.go` wraps both the container name and the command with `shellArg()`: `docker exec %s sh -c %s`. `dockerExecCmd` in `server_config.go` wraps only the container: `docker exec %s %s`. While all current call sites use hardcoded command strings (no user input flows through the cmd parameter), this inconsistency is fragile. A future developer adding a new call to `dockerExecCmd` with user-derived input would create an injection vulnerability.
- **Recommendation**: Unify both functions to always use `shellArg()` for both parameters. Add a linter rule or comment warning about the injection risk.
- **Status**: OPEN

---

### [LOW] SEC-017: AWG Config File Written with 0600 But Config Dir is 0700

- **File**: `src/internal/tunnel/manager.go:442` (`writeClientConf`), `src/internal/config/config.go:143`
- **Description**: The client AWG config file (`awg0.conf`) is written with mode `0o600` — correct. The config directory (`~/.unet/`) is created with `0o700`. Both are appropriate. However, on Windows, `os.WriteFile` with `0o600` does not set restrictive ACLs — the Windows-specific `setFilePerm` is only called for `config.json`, not for `awg0.conf`.
- **Recommendation**: Call `setFilePerm` (or a similar Windows ACL function) on the AWG config file after writing, or use `os.WriteFile` with a wrapper that applies platform-appropriate permissions.
- **Status**: OPEN

---

### [LOW] SEC-018: Config Temp File Pattern Predictable

- **File**: `src/internal/config/config.go:225` (`saveLocked`)
- **Description**: `os.CreateTemp(dir, "config.*.tmp")` generates a temp file with a predictable prefix. While `os.CreateTemp` uses random suffixes (using `os.Temp`'s internal randomness), the prefix leaks the purpose. On shared systems, an attacker could race the rename with a symlink attack. The window is extremely narrow (between `CreateTemp` and `atomicRename`) and further mitigated by the `0o700` directory permissions.
- **Recommendation**: No action needed for current threat model. Document that `~/.unet/` must not be on a world-writable directory or NFS share.
- **Status**: OPEN

---

### [LOW] SEC-019: No Rate Limiting on API Endpoints

- **File**: `src/internal/daemon/server.go`
- **Description**: The HTTP server has no rate limiting middleware. An attacker with localhost access could flood `POST /api/vps/configure` to trigger rapid provisioning cycles, or brute-force the UIToken (once authentication is added) without rate limiting.
- **Recommendation**: Add per-IP rate limiting (e.g., `golang.org/x/time/rate`) to sensitive mutation endpoints. Since the server is localhost-only, this is a defense-in-depth measure.
- **Status**: OPEN

---

### [INFO] SEC-020: `InsecureSkipVerify: true` in mTLS Client — Intentional and Documented

- **File**: `src/internal/proxy/caddy_mtls.go:340-343`
- **Description**: `tls.Config{InsecureSkipVerify: true}` is used when connecting to the Caddy admin API. The `#nolint:gosec` annotation and inline comment explain that this is because the Caddy admin API is accessed via tunnel IP (not a hostname with matching cert). The mTLS client certificate provides authentication; hostname verification is irrelevant for IP-based endpoints.
- **Recommendation**: No action needed. The justification is sound for tunnel-internal communication.
- **Status**: OPEN

---

### [INFO] SEC-021: `shellEscape` / `shellArg` — Correct Single-Quote Escaping

- **File**: `src/internal/provisioner/ssh.go:356-358` (`shellEscape`), `src/internal/tunnel/server_config.go:236-238` (`shellArg`)
- **Description**: Both functions implement the standard POSIX single-quote escaping pattern: wrap in `'...'` and replace embedded `'` with `'\''`. This is the correct defense against shell injection. Duplicated in two packages but identical logic.
- **Recommendation**: Consider extracting to a shared `internal/shell` package to reduce duplication and ensure consistency.
- **Status**: OPEN

---

### [INFO] SEC-022: SSH Password Auth Sends Cleartext Over Encrypted Channel

- **File**: `src/internal/provisioner/ssh.go:139-157` (`connect`)
- **Description**: Password-based SSH authentication sends the password over the SSH encrypted channel. This is standard SSH behavior and not a vulnerability. The password is stored as `SecretString` in config and masked in all API responses.
- **Recommendation**: No action needed. Consider recommending key-based auth as default in documentation.
- **Status**: OPEN

---

### [INFO] SEC-023: Atomic Config Write Pattern — Correctly Implemented

- **File**: `src/internal/config/config.go:222-255` (`saveLocked`), `config_posix.go`, `config_windows.go`
- **Description**: The atomic write pattern (temp file → write → fsync → chmod → rename) is correctly implemented. POSIX uses `os.Rename` (atomic). Windows uses `MoveFileEx` with `MOVEFILE_REPLACE_EXISTING`. The `Update()` method works on a copy and only replaces the in-memory config on successful save, preventing corruption.
- **Recommendation**: No action needed. Well implemented.
- **Status**: OPEN

---

## Summary

| Severity | Count |
|----------|-------|
| Critical | 3     |
| High     | 6     |
| Medium   | 6     |
| Low      | 4     |
| Info     | 4     |
| **Total** | **23** |

### Critical Issues (Immediate Action Required)
1. **SEC-001**: No API authentication — full admin access from any local process
2. **SEC-002**: Data race on `Manager.status` — undefined behavior under concurrency
3. **SEC-003**: Data race on `VPSHandler.task` — cross-goroutine mutation without synchronization

### Priority Remediation Order
1. SEC-001 (API auth) — blocks production deployment
2. SEC-002 + SEC-003 (data races) — enable Go race detector in CI
3. SEC-004 (BootstrapMTLS race window) — refactor lock pattern
4. SEC-006 (temp file leak with secrets) — cleanup hardening
5. SEC-008 (host validation) — input validation pass
6. SEC-005 (unmasked server private key) — defense in depth
7. SEC-007 (Windows privilege check) — platform parity
8. SEC-009 (port range validation) — quick fix
9. SEC-010–SEC-019 — medium/low priority hardening
