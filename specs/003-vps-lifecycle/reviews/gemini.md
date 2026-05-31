# SpecKit Review: 003-vps-lifecycle

**Reviewer**: gemini
**Reviewed at**: 2026-05-31T00:00:00Z
**Commit**: HEAD
**Artifacts reviewed**: spec.md, plan.md, tasks.md, data-model.md, contracts/

## Summary

The VPS Lifecycle feature introduces crucial automation for provisioning, backup, and migration. While the overall phasing and encryption strategy (using `age`) are sound, the migration protocol relies on a flawed assumption regarding WireGuard and DNS. Furthermore, automated SSH operations contain critical blind spots regarding interactive prompts and failure recovery that could leave the system in a broken, split-brain state.

## Findings

| ID | Severity | Area | Finding | Recommendation |
|---|---|---|---|---|
| F1 | CRITICAL | Logical Consistency | WireGuard vs DNS Cutover: The migration protocol relies on "DNS-TTL redirect" for cutover. However, standard WireGuard clients resolve the Endpoint DNS name *once* at startup and cache the IP indefinitely. A DNS change will NOT migrate active peers until they manually restart their tunnels. | The spec must address WireGuard's endpoint resolution behavior. Options: 1) Require dynamic endpoint update scripts on clients, 2) Use a floating IP (BGP/Anycast) instead of DNS, or 3) Explicitly document that clients must toggle their tunnels off/on during migration. |
| F2 | CRITICAL | Hidden Assumption | Sudo Password Prompts: The bootstrap script assumes `sudo` is passwordless or the user is `root`. If `sudo` requires a password, the automated SSH session will hang indefinitely waiting for stdin, deadlocking the API. | The preflight check MUST explicitly verify passwordless sudo (e.g., `sudo -n true`) and fail fast with a clear error if a password is required, prompting the user to configure `visudo`. |
| F3 | HIGH | Edge Case | Unbounded Backup Size: If the `audit.jsonl` (introduced in 002) is included in the state bundle, the backup size will grow indefinitely. Encrypting/compressing massive files will cause memory/CPU spikes and timeouts. | Explicitly exclude `audit.jsonl` from the lifecycle state bundle, or implement log rotation/truncation before export. The bundle should strictly be config/state necessary to restore the tunnel. |
| F4 | HIGH | Failure Modes | Split-Brain Migration: The 10-step migration protocol lacks a transactional rollback mechanism. If the process crashes at step 7 (DNS cutover) or 8 (Drain source), `MigrationPlan` tracks the phase, but it's unclear how the daemon resumes or reverts a half-migrated system. | Define a specific `resume` or `rollback` state machine for the migration task. If step 8 fails, does the user retry? Is the source kept alive indefinitely? |
| F5 | MEDIUM | Hidden Assumption | S3 Credentials Scope: The plan lists `aws-sdk-go-v2` for S3 sync, but the data model and API do not specify how AWS credentials (AK/SK, region, endpoint) are supplied to the daemon or securely stored in `config.json`. | Update `VPSProfile` or global config data model to include encrypted/secure storage fields for S3/R2 bucket credentials and endpoint URLs. |
| F6 | MEDIUM | Edge Case | OS Compatibility: The "idempotent curl" docker install script is generally tailored for Debian/Ubuntu/CentOS. Deploying to Alpine, Arch, or immutable OSes (Flatcar) will fail unpredictably. | Limit official support to specific Linux distributions and explicitly check `/etc/os-release` during preflight, rejecting unsupported OSes rather than failing mid-install. |

## Alternative approaches considered

- **Cloud-init / User-Data**: Instead of imperatively executing bash commands over SSH for the initial bootstrap (which is prone to network drops and timing issues), the API could generate a `cloud-init` user-data script. The user provides this script when creating the VPS in their cloud console, and the VPS automatically installs Docker, Compose, and dials back home to `unet`.
- **WireGuard Roaming**: Since the private key moves to the new VPS during migration, if the clients are behind NAT, the new VPS could theoretically send persistent keepalives to the clients' last known public IPs to "punch back" and update the endpoint dynamically, bypassing the DNS caching issue entirely.

## VERDICT

```yaml
verdict: CRITICAL
reviewer: gemini
reviewed_at: 2026-05-31T00:00:00Z
commit: HEAD
critical_count: 2
high_count: 2
medium_count: 2
low_count: 0
```
