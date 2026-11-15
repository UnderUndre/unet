# Option 0 Baseline: AmneziaWG hub-and-spoke on a cheap VPS (zero custom code)

**Purpose:** evening-length baseline that solves the stated task without a single line of unet code:

1. **Expose localhost apps** from a home computer to the internet through the VPS (public port DNAT to the device's tailnet IP).
2. **Unified isolated network** for all devices: every device is a WireGuard-style peer on `10.66.0.0/24`, the VPS routes between them (hub-and-spoke, not a P2P mesh).

This kit is also the **UX acceptance baseline** for `specs/001-init-netstack-tunnel`: unet must beat this setup's ergonomics, otherwise unet is not worth building.

> ⚠️ Run these scripts **on your own VPS**. The agent session has no SSH access to it. Everything here runs as root on Debian 12 / Ubuntu 22.04+.

---

## What gets installed

| Component | Source | Role |
|---|---|---|
| `amneziawg-go` | github.com/amnezia-vpn/amneziawg-go (built from source) | userspace AWG daemon (no kernel module needed) |
| `awg`, `awg-quick` | github.com/amnezia-vpn/amneziawg-tools (built from source) | config/CLI tooling (`awg genkey`, `awg-quick up`) |
| `dnsmasq` (apt) | distro | tunnel-side DNS resolver on `10.66.0.1` — client configs use `DNS = 10.66.0.1`, so names resolve through the tunnel, never via the client's ISP resolver (zcode F2) |
| Go toolchain | go.dev official tarball (apt's Go is too old for go.mod ≥1.24) | build dependency only |

Network shape:

```
            INTERNET
               │ udp/51820 (AWG, obfuscated)      public tcp/PORT (DNAT)
   ┌───────────▼───────────────────────────┐
   │ VPS: awg0 = 10.66.0.1/24              │◄──── Caddy/public ingress later (unet T3a)
   │ + ip_forward + MASQUERADE             │
   └───┬──────────┬──────────┬─────────────┘
 10.66.0.2  10.66.0.3  10.66.0.4
   laptop     phone      home server
```

---

## 15-minute path (as root, on the VPS)

```bash
# 1. Install + build + configure (writes /etc/amneziawg/awg0.conf, enables systemd unit)
bash server-install.sh

# 2. Create a peer per device (prints/ writes client .conf at /root/unet-peers/<name>.conf)
bash add-peer.sh laptop
bash add-peer.sh phone

# 3. Expose a service from a device to the public internet
bash expose-port.sh 8080 10.66.0.2 3000 tcp
#    public :8080  ->  laptop 10.66.0.2:3000   (your localhost app)
```

Each `add-peer.sh` call writes `/root/unet-peers/<name>.conf` — import it into the client (see below).

## Client setup

- **Windows / macOS / Linux desktop, Android, iOS:** install **AmneziaVPN** (supports the AWG protocol natively), choose "import config file", feed it `<name>.conf`.
- **Linux laptop, manual:** `awg-quick up <name>.conf` with `WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go` (rootless unet replaces this later — that is spec 001's whole point).
- **Do NOT use vanilla WireGuard clients:** the AWG header/padding parameters (`H1-H4`, `S1-S2`) make the traffic incompatible with stock WireGuard by design.

## Verify

```bash
# on the VPS
awg show                                # peers, handshakes, transfer counters
ping 10.66.0.2                          # from a client: reach the laptop over the tailnet
curl -s http://<VPS_PUBLIC_IP>:8080/    # from anywhere: hits the laptop's localhost:3000
```

Success looks like: handshakes counted in `awg show`, tailnet IPs pingable across devices, public port lands on the laptop service. That is the full stated task, working tonight.

---

## AWG parameter rules (critical, encoded in the scripts)

- `S1`, `S2`, `H1-H4` are **session-shaping** parameters: they MUST be **identical** on the server and every client. `server-install.sh` generates them once and `add-peer.sh` copies them into every client config from the server conf.
- `Jc`, `Jmin`, `Jmax` (junk packets) and `I1-I5` (signature packets) carry no data: client-side only, harmless if they differ.
- Junk/signature packets larger than the system MTU get IP-fragmented, which looks suspicious to censors — `Jmax` default here stays below 1200.

## Rollback

```bash
awg-quick down awg0
systemctl disable awg-quick@awg0
rm -rf /etc/amneziawg /etc/systemd/system/awg-quick@awg0.service /opt/awg-src
sysctl -w net.ipv4.ip_forward=0
```

## [VERIFY] markers — honest unknowns, check on first run

These are the places where the upstream repos may differ from what this kit assumes. Nothing below is guessed about the protocol; they are build/packaging details:

1. **amneziawg-tools binary names** — kit assumes `src/` builds `awg` + `awg-quick`. If the build outputs differ, adjust the two `install -m 0755` lines in `server-install.sh`.
2. **`WG_QUICK_USERSPACE_IMPLEMENTATION` env in awg-quick** — the kit assumes awg-quick inherited wireguard-tools' userspace-launch env. If `awg-quick up` tries to create a kernel `amnezia` interface instead, use the fallback unit documented in `server-install.sh` (runs `amneziawg-go -f awg0` + `awg setconf`).
3. **`awg syncconf`** — used by `add-peer.sh` for no-downtime peer adds. If missing, the script falls back to a unit restart (brief tunnel blip, acceptable for baseline).

## Out of scope for this kit (by design)

- No key rotation, no multi-VPS, no magic DNS, no coordination API, no billing — that is exactly what 001/002/003 specs own later.
- The VPS public SSH port and the AWG port must be open; everything else stays closed. Exposed app ports are opened only by `expose-port.sh`, one at a time, and are public by definition.
