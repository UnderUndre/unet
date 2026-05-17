# Appendix: AmneziaWG Peer Add/Remove Flow

This appendix documents the concrete SSH + `docker exec` operations the unet Go daemon performs to manage peers on the AmneziaWG server. There is **no server-side REST API** for peer management — the entire control plane is out-of-band SSH.

**Audience**: Implementers of `internal/tunnel/peer.go` (T013b) and `internal/tunnel/server_config.go` (T013a).

**Conventions**:
- `<vps>` — SSH alias / host of the VPS (resolved from `vps.host`, `vps.sshPort`, `vps.username`).
- `<container>` — Docker container name (from `vps.containerName`, default: `unet-amnezia-awg`).
- `<iface>` — AmneziaWG interface name (Linux server: always `awg0`).
- `<conf>` — `/opt/amnezia/awg/awg0.conf`.

All commands are templates; the daemon MUST substitute via Go `text/template` with strict-escaping (no shell expansion of user input).

---

## 1. Read Server State (T013a)

### 1.1 Fetch the full `awg0.conf`

```bash
ssh <vps> "docker exec <container> cat /opt/amnezia/awg/awg0.conf"
```

**Parse target**: extract from `[Interface]` section:
- `Address` → `tunnel.subnet` + `tunnel.serverIp` (the IP-side of the CIDR)
- `ListenPort` → `tunnel.serverEndpoint.port`
- `Jc`, `Jmin`, `Jmax`, `S1`–`S4`, `H1`–`H4`, `I1`–`I5` → `tunnel.obfuscation.*`

Parser must tolerate INI-style with `# comment` lines (Amnezia uses `# I1 = ...` syntax when injection slots are populated).

### 1.2 Fetch server public key

```bash
ssh <vps> "docker exec <container> cat /opt/amnezia/awg/wireguard_server_public_key.key"
```

→ `tunnel.serverPublicKey` (base64, 44 chars trailing `=`).

### 1.3 Fetch pre-shared key

```bash
ssh <vps> "docker exec <container> cat /opt/amnezia/awg/wireguard_psk.key"
```

→ `tunnel.presharedKey` (base64). Note: PSK is shared across ALL peers in this deployment — Amnezia's design choice, not per-peer.

### 1.4 Fetch existing peers (to determine next free IP and avoid pubkey collision)

```bash
ssh <vps> "docker exec <container> awg show <iface> dump"
```

Output format (tab-separated):

```
<server-pubkey>	<server-private-key>	<listen-port>	off
<peer-pubkey>	<peer-psk>	<peer-endpoint>	<peer-allowed-ips>	<last-handshake>	<rx>	<tx>	<keepalive>
<peer-pubkey>	<peer-psk>	<peer-endpoint>	<peer-allowed-ips>	<last-handshake>	<rx>	<tx>	<keepalive>
```

The first line is the interface; subsequent lines are peers. Daemon extracts `<peer-allowed-ips>` from each to enumerate occupied IPs.

### 1.5 Compute drift hash

```bash
ssh <vps> "docker exec <container> sha256sum /opt/amnezia/awg/awg0.conf | awk '{print $1}'"
```

Compare against `serverMirror.awgConfRaw` SHA256. Mismatch → surface drift warning in UI; re-parse §1.1.

---

## 2. Add a New Peer (T013b)

### 2.1 Generate keypair locally on the client (Go side)

```go
// Equivalent to: awg genkey | tee privkey | awg pubkey > pubkey
priv, _ := exec.Command("awg", "genkey").Output()
pub, _  := exec.Command("awg", "pubkey").Input(priv).Output()
```

Store `priv` → `tunnel.privateKey`, `pub` → `tunnel.publicKey`.

### 2.2 Allocate next free IP in subnet

From §1.4's enumeration, find smallest `N` such that `<serverSubnet-prefix>.N/32` is not in any existing peer's `AllowedIPs`. Start at `.2` (server is `.1`). Reserve and write to `tunnel.localIp`.

### Shell conventions (read before §2.3+)

The daemon's SSH-side commands cross three shell environments. Each has different syntax constraints — getting this wrong is finding **F3** from the antigravity review:

| Layer | Shell | Supports `<(…)`? | Supports `$(…)`? | Heredoc quoting matters? |
|-------|-------|------------------|------------------|--------------------------|
| Daemon local (Go `exec.Command`) | none — direct exec, no shell | N/A | N/A | N/A |
| SSH remote host (Ubuntu VPS) | `/bin/bash` | ✅ yes | ✅ yes | YES — `<<EOF` interpolates, `<<'EOF'` does not |
| Inside container (`docker exec <c> sh -c '…'`) | BusyBox `ash` (Alpine default) | ❌ **NO** | ✅ yes | YES — same rules |
| Inside container (`docker exec <c> bash -c '…'`) | `bash` (installed by reference Dockerfile) | ✅ yes | ✅ yes | YES |

**Rules the daemon MUST follow** (each enforced by the patterns below):

1. **Never use `<(…)` process substitution inside `docker exec <c> sh -c '…'`.** It's bash-specific; Alpine's default `sh` is `ash` which silently fails parsing. Replacement pattern: write to a temp file inside the container, then read.
2. **All values flowing into commands MUST be Go-rendered with strict escaping** (via `text/template` with HTML-unsafe escapers OR explicit Go-side base64/JSON marshalling). NEVER use `$(…)` command substitution from the SSH-host shell to pull data from inside the container — it requires an extra round-trip AND can mis-render if the value contains shell-special chars.
3. **Use `<<'EOF'` (quoted heredoc)** when the body must travel literally to the remote without local interpolation. Use unquoted `<<EOF` only when you intentionally want local-side variable expansion.

### 2.3 Append [Peer] block to server's `awg0.conf`

The unet daemon uses the **temp-file hot-reload pattern**. `awg set` (live-only, non-persistent) is mentioned as Option A for completeness but is NOT used.

**Option A — Live config via `awg set` (NOT used by unet; for reference only)**:

```bash
ssh <vps> "docker exec <container> awg set <iface> peer <CLIENT_PUBKEY> \
    preshared-key /opt/amnezia/awg/wireguard_psk.key \
    allowed-ips <CLIENT_IP>/32 \
    persistent-keepalive 25"
```

Updates the running interface immediately. Does NOT persist — peer is lost on container restart.

**Option B — Persistent file edit + hot-reload (used by unet daemon)**:

The daemon pre-renders the `[Peer]` block in Go (with the PSK already read from local mirror, no in-container `cat`) and pushes via `docker exec -i … sh -c 'cat >> …'` with a **quoted** heredoc so the body travels literally:

```bash
# Daemon constructs PEER_BLOCK string in Go:
#   PEER_BLOCK = "\n[Peer]\nPublicKey = " + clientPub + "\n" + \
#                "PresharedKey = " + psk + "\n" + \
#                "AllowedIPs = " + clientIP + "/32\n" + \
#                "PersistentKeepalive = 25\n"
# (clientPub, psk, clientIP all validated as base64/CIDR before interpolation)

# Step 1: append peer block to disk (quoted heredoc, no interpolation, no <( ))
ssh <vps> "docker exec -i <container> sh -c 'cat >> /opt/amnezia/awg/awg0.conf'" <<'PEER'
<PEER_BLOCK rendered by Go — literal bytes via stdin>
PEER

# Step 2: hot-reload — temp-file pattern (NO process substitution; works in ash)
ssh <vps> "docker exec <container> sh -c '
  awg-quick strip /opt/amnezia/awg/awg0.conf > /tmp/awg0-strip.conf \
    && awg syncconf <iface> /tmp/awg0-strip.conf \
    && rm -f /tmp/awg0-strip.conf
'"
```

The temp file `/tmp/awg0-strip.conf` lives inside the container's `/tmp` (ephemeral tmpfs) and is removed immediately after `syncconf` consumes it. If the daemon crashes between write and remove, the file is cleaned on next container restart (tmpfs is wiped). This pattern is shell-portable (works in `sh`, `ash`, `bash`, `zsh`) and avoids the bashism that broke the previous design.

### 2.4 Update `clientsTable` (metadata for Amnezia Desktop compatibility)

The clientsTable is a JSON array of `{clientId, userData}` objects. **`clientName` is user-supplied** and may contain quotes, backslashes, newlines, or other JSON-special characters — building the JSON via shell-string interpolation is an injection vector (antigravity F4).

The daemon serialises the full updated array in Go with `encoding/json.Marshal` (which handles all JSON-escaping rules correctly), then pushes the byte payload via stdin to `cat >`:

```bash
# Daemon's Go code:
#   updated := append(existingClients, ClientEntry{
#     ClientID: clientPubBase64,
#     UserData: UserData{
#       ClientName: clientName,           // raw user input — Marshal handles escaping
#       CreationDate: time.Now().Format(time.RFC3339),
#     },
#   })
#   payload, _ := json.MarshalIndent(updated, "", "  ")
#   // payload is now a fully-escaped JSON byte slice safe to ship

# Atomic write: temp-file + rename
ssh <vps> "docker exec -i <container> sh -c 'cat > /opt/amnezia/awg/clientsTable.new'" <<< "$payload"
ssh <vps> "docker exec <container> mv /opt/amnezia/awg/clientsTable.new /opt/amnezia/awg/clientsTable"
```

(In real Go: `cmd := exec.Command("ssh", vps, "docker", "exec", "-i", container, "sh", "-c", "cat > /opt/amnezia/awg/clientsTable.new"); cmd.Stdin = bytes.NewReader(payload); cmd.Run()` — no shell interpolation of `clientName` anywhere along the path.)

### 2.5 Verify peer is active

```bash
ssh <vps> "docker exec <container> awg show <iface> peers" | grep -Fxq -- "<CLIENT_PUBKEY>"
```

`-x` matches whole line, `-F` matches literal string (no regex special-char hazard), `-q` silences output. Exit code 0 → success; non-zero → peer didn't take, abort and roll back §2.3 (re-issue with the same peer removed via §3.1).

### 2.6 Register peer's mTLS public key with Caddy (when `caddyApi.authMode == "mtls"`)

Co-located with peer-add to avoid the multi-peer lockout (antigravity F2). Skipped when `authMode == "ip-only"`.

```bash
# Daemon's Go code constructs the updated Caddy admin config:
#   1. Read existing /config/caddy/autosave.json via SSH (or use cached copy)
#   2. Append the new peer's base64-DER pubkey into
#      admin.remote.access_control[0].public_keys[]
#   3. Marshal back to JSON

# Push the merged config + reload Caddy
ssh <vps> "docker exec -i unet-caddy sh -c 'cat > /config/caddy/autosave.json'" <<< "$caddy_config"
ssh <vps> "docker exec unet-caddy caddy reload --config /config/caddy/autosave.json --adapter json"
```

`caddy reload` is in-process — no listener restart, no in-flight HTTPS interruption.

**First-peer special-case**: when the *first* mTLS peer is being registered, the merged config additionally flips `admin.listen` to a TLS-wrapped form. After that one `caddy reload`, all subsequent admin calls require mTLS. Subsequent peers append to `public_keys[]` without flipping (already TLS).

**Stale-entry pruning**: see `contracts/caddy-api.md` "Recovery from Client Cert Loss" — periodic peer-rotate ops should remove `public_keys[]` entries that no longer match any active peer's certificate (binding tracked in `clientsTable` metadata).

---

## 3. Remove a Peer

### 3.1 Live removal via `awg set`

```bash
ssh <vps> "docker exec <container> awg set <iface> peer <CLIENT_PUBKEY> remove"
```

### 3.2 Remove from on-disk `awg0.conf`

`awg-quick strip` doesn't remove specific peers — we need to edit the file ourselves. Use `awg-quick save` if `SaveConfig = true` (writes live state to file), or surgical edit:

```bash
ssh <vps> bash -s <<EOF
docker exec <container> sh -c "awg-quick save <iface>"
EOF
```

`awg-quick save` writes the current runtime state to `<conf>`, removing the peer atomically. **Requires** `SaveConfig = true` in the `[Interface]` section — unet must set this when generating the initial config (T009b).

### 3.3 Remove from `clientsTable`

Same pattern as §2.4 — rewrite the JSON minus the removed entry, atomically.

---

## 4. Rotate Server Obfuscation Parameters

User-triggered "rotate keys" flow. **Drops all clients** — this is the trade-off.

### 4.1 Generate fresh parameter set client-side

Random `Jc ∈ [4,12]`, `Jmin ∈ [10,100]`, `Jmax ∈ [Jmin+10, MTU-200]`, `S1-S4 ∈ [0,200]`, `H1-H4` random non-overlapping `uint32` ranges. `I*` left unchanged (mimicry presets are user-curated).

### 4.2 Push new config to server

```bash
ssh <vps> bash -s <<EOF
# Backup existing config
docker exec <container> cp /opt/amnezia/awg/awg0.conf /opt/amnezia/awg/awg0.conf.bak

# Write new conf (preserving [Peer] blocks)
docker exec -i <container> sh -c 'cat > /opt/amnezia/awg/awg0.conf.new' <<CONF
<rendered new awg0.conf with same peers>
CONF
docker exec <container> mv /opt/amnezia/awg/awg0.conf.new /opt/amnezia/awg/awg0.conf

# Apply with full restart (syncconf can't change obfuscation params live)
docker exec <container> awg-quick down <iface> || true
docker exec <container> awg-quick up <iface>
EOF
```

**Critical**: obfuscation parameters (`J*/S*/H*/I*`) require interface DOWN/UP — they cannot be hot-reloaded. All connected peers will drop and must reconnect with the new parameters.

### 4.3 Re-fetch params on each client (drift detection)

After step 4.2, every other unet client of this VPS will see a hash mismatch on its next connect attempt (FR-010) and re-pull the obfuscation set automatically.

---

## 5. Error Recovery

### 5.1 Volume lost (container recreated without persistent volume)

Symptoms: client connect fails with handshake timeout; SSH reveals empty `/opt/amnezia/awg/` (and, in mTLS mode, empty `/config/caddy/` as well if the `caddy-config` volume was also lost).

Recovery: daemon reconstructs full server-side state from `serverMirror` (FR-010 mirror), preserving existing client keys (they're in local config, not lost). Each file is restored via a **quoted heredoc** so the (potentially key-material-bearing) body is NOT interpreted by the host's bash.

```bash
# Step 1 — restore AmneziaWG files (one ssh-per-file for atomicity per write)
ssh <vps> "docker exec -i <container> sh -c 'cat > /opt/amnezia/awg/awg0.conf'" <<< "$awg0_conf_mirror"
ssh <vps> "docker exec -i <container> sh -c 'cat > /opt/amnezia/awg/wireguard_psk.key'" <<< "$psk_mirror"
ssh <vps> "docker exec -i <container> sh -c 'cat > /opt/amnezia/awg/wireguard_server_private_key.key'" <<< "$srv_priv_mirror"
ssh <vps> "docker exec -i <container> sh -c 'cat > /opt/amnezia/awg/wireguard_server_public_key.key'" <<< "$srv_pub_mirror"

# Step 2 — restore clientsTable (regenerated in Go from local peer registry, see §2.4)
ssh <vps> "docker exec -i <container> sh -c 'cat > /opt/amnezia/awg/clientsTable'" <<< "$clientsTable_payload"

# Step 3 — restart AmneziaWG interface
ssh <vps> "docker exec <container> awg-quick down awg0 2>/dev/null || true"
ssh <vps> "docker exec <container> awg-quick up awg0"

# Step 4 (mTLS-only) — restore Caddy admin config including ALL peers' mTLS pubkeys
# The daemon reconstructs the Caddy admin block from local mTLS state across all peers
# (this peer's own cert is in ~/.unet/config.json; OTHER peers' pubkeys are mirrored
#  alongside the AmneziaWG mirror — daemon stores both AWG and Caddy mirrors). Then
# follows §2.6's push pattern.
if [ "$auth_mode" = "mtls" ]; then
    ssh <vps> "docker exec -i unet-caddy sh -c 'cat > /config/caddy/autosave.json'" <<< "$caddy_admin_config_mirror"
    ssh <vps> "docker exec unet-caddy caddy reload --config /config/caddy/autosave.json --adapter json"
fi
```

**Implication for `serverMirror` schema**: the `serverMirror` blob in `data-model.md` MUST include a `caddyAdminConfig` field alongside `awgConfRaw` + `clientsTable`, persisted on every successful peer-add. Without this, mTLS state is unrecoverable on volume loss and the user is locked out of the Caddy admin endpoint with no in-band recovery (only manual SSH-edit).

### 5.2 SSH connection drops mid-write

The peer-add flow §2 has three failure windows:
1. After conf-append, before syncconf → peer is on disk but not in running interface → harmless, next connect succeeds
2. During syncconf → may partially apply → daemon retries syncconf idempotently (it's a diff operation)
3. After syncconf, before clientsTable update → peer works but metadata is stale → daemon detects on next §1.4 enumeration and self-heals

In all three cases the daemon's reconciliation loop converges. The conf-append (step §2.3) is the only step that's hard to undo — daemon must `flock`-style retry or detect duplicate pubkey before re-adding.

### 5.3 Hot-reload (`syncconf`) fails

Typical cause: a parameter that requires full restart was changed (e.g., `ListenPort` or any `J*/S*/H*/I*`). Daemon falls back to `awg-quick down && up` — all peers drop briefly.

```bash
ssh <vps> "docker exec <container> sh -c 'awg-quick down <iface> && awg-quick up <iface>'"
```

---

## 6. Why Not Use Amnezia's Own Add-Client Script?

The Amnezia Desktop client uses scripts in `/opt/amnezia/awg/add-client.sh` (if present) which it `scp`'s onto the server during initial provisioning. We chose to inline the SSH commands instead because:

1. **Auditability**: every command is visible in the unet codebase, not hidden in shell scripts on the VPS.
2. **Idempotency**: Go-side logic can branch on existing state more cleanly than shell.
3. **Atomicity**: we control transaction boundaries (mirror update only after successful syncconf + clientsTable update).
4. **No shell injection surface**: by using `bash -s <<EOF` with parameter substitution via Go templating, we avoid passing user-controlled strings through interactive shell parsing.

---

## 7. Testing

Manual test from a clean VPS:

```bash
# Initial state: empty /opt/amnezia/awg/
ssh <vps> "docker exec <container> ls /opt/amnezia/awg/"
# (expected: empty or just the .keep file)

# Run unet provisioner (T009)
unet vps configure --host <vps> --user root --key ~/.ssh/id_rsa
# → should write awg0.conf + keys + clientsTable

# Connect first client
unet tunnel connect
# → should add [Peer] block + syncconf + clientsTable upsert

# Verify
ssh <vps> "docker exec <container> awg show awg0"
# → should list peer with last-handshake within last minute

# Drift test (manual edit)
ssh <vps> "docker exec <container> awg set awg0 peer <fake-pubkey> allowed-ips 10.8.1.99/32"
# Within 30s, unet UI should display drift warning banner
```

Automated E2E coverage lives in T026, T027, T028.
