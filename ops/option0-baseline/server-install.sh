#!/usr/bin/env bash
# unet Option 0 baseline: AmneziaWG hub-and-spoke on a cheap VPS (Debian 12 / Ubuntu 22.04+).
# Builds upstream amneziawg-go + amneziawg-tools from source, configures awg0 (10.66.0.0/24),
# IP forwarding, NAT, and the UNET-EXPOSE DNAT chain for public port exposure.
# Run as root:  bash server-install.sh
set -euo pipefail

[[ ${EUID} -eq 0 ]] || { echo "ERROR: run as root"; exit 1; }

TAILNET="10.66.0.0/24"
AWG_PORT="${AWG_PORT:-51820}"
WAN_IF="$(ip route show default | awk '{print $5; exit}')"
[[ -n "${WAN_IF}" ]] || { echo "ERROR: no default route found"; exit 1; }
WAN_IP="$(ip -4 addr show "${WAN_IF}" | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -1)"
[[ -n "${WAN_IP}" ]] || { echo "ERROR: no IPv4 address on ${WAN_IF}"; exit 1; }
echo ">> WAN: ${WAN_IF} (${WAN_IP})   tailnet: ${TAILNET}   udp/${AWG_PORT}"

# ---------------------------------------------------------------- deps
apt-get update -y
apt-get install -y git make gcc curl iptables ca-certificates dnsmasq

# ---------------------------------------------------------------- Go toolchain
# go.mod of amneziawg-go requires a modern Go; distro packages lag behind.
GO_LATEST="$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -1)"
[[ "${GO_LATEST}" =~ ^go[0-9]+ ]] || { echo "ERROR: cannot resolve Go version"; exit 1; }
if ! command -v go >/dev/null 2>&1 || ! go version | grep -Eq 'go1\.2[4-9]'; then
  echo ">> installing ${GO_LATEST} from go.dev"
  rm -rf /usr/local/go
  curl -fsSL "https://go.dev/dl/${GO_LATEST}.linux-amd64.tar.gz" | tar -C /usr/local -xz
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
fi
go version

# ---------------------------------------------------------------- build AWG from source
mkdir -p /opt/awg-src && cd /opt/awg-src
[[ -d amneziawg-go ]]    || git clone --depth 1 https://github.com/amnezia-vpn/amneziawg-go
[[ -d amneziawg-tools ]] || git clone --depth 1 https://github.com/amnezia-vpn/amneziawg-tools
echo ">> building amneziawg-go"
( cd amneziawg-go && make )
echo ">> building amneziawg-tools"   # [VERIFY 1 resolved 2026-11-16 on ufo-finland: builds wg/wg-quick]
( cd amneziawg-tools/src && make )
install -m 0755 amneziawg-go/amneziawg-go /usr/local/bin/
# Binary names vary by repo tag: pick whatever the build produced, install as awg/awg-quick.
if   [[ -f amneziawg-tools/src/awg ]];       then WGBIN=awg
elif [[ -f amneziawg-tools/src/wg ]];        then WGBIN=wg
else
  echo "ERROR: neither awg nor wg built in amneziawg-tools/src:"; ls amneziawg-tools/src; exit 1
fi
if   [[ -f amneziawg-tools/src/awg-quick ]]; then QKBIN=awg-quick
elif [[ -f amneziawg-tools/src/wg-quick ]]; then QKBIN=wg-quick
elif [[ -f amneziawg-tools/src/wg-quick/linux.bash ]]; then QKBIN="wg-quick/linux.bash" # [VERIFY 2 resolved 2026-11-16: linux.bash honours WG_QUICK_USERSPACE_IMPLEMENTATION, default amneziawg-go]
else
  echo "ERROR: no quick tool built (awg-quick / wg-quick / wg-quick/linux.bash):"; ls amneziawg-tools/src; exit 1
fi
echo ">> tools binaries detected: ${WGBIN} + ${QKBIN} -> installing as awg/awg-quick"
install -m 0755 "amneziawg-tools/src/${WGBIN}" /usr/local/bin/awg
install -m 0755 "amneziawg-tools/src/${QKBIN}" /usr/local/bin/awg-quick
command -v awg >/dev/null && command -v awg-quick >/dev/null && command -v amneziawg-go >/dev/null \
  || { echo "ERROR: expected binaries missing after install — check [VERIFY 1] in README.md"; exit 1; }

# ---------------------------------------------------------------- keys + obfuscation params
SERVER_PRIV="$(awg genkey)"
SERVER_PUB="$(printf '%s' "${SERVER_PRIV}" | awg pubkey)"
# S1/S2/H1-H4 must be IDENTICAL on server and every client (session shaping).
# Jc/Jmin/Jmax and I1-I5 are client-side only and NOT set on the server.
S1=$(( (RANDOM % 60) + 30 ))
S2=$(( (RANDOM % 40) + 15 ))
gen_h() { echo $(( (RANDOM % 900000) + 100000 )); }
H1="$(gen_h)"; H2="$(gen_h)"; H3="$(gen_h)"; H4="$(gen_h)"
# H values must be pairwise distinct (AWG spec): regenerate collisions cheaply.
while [[ "${H2}" == "${H1}" ]]; do H2="$(gen_h)"; done
while [[ "${H3}" == "${H1}" || "${H3}" == "${H2}" ]]; do H3="$(gen_h)"; done
while [[ "${H4}" == "${H1}" || "${H4}" == "${H2}" || "${H4}" == "${H3}" ]]; do H4="$(gen_h)"; done

# ---------------------------------------------------------------- IP forward
echo 'net.ipv4.ip_forward = 1' > /etc/sysctl.d/99-awg-forward.conf
sysctl --system >/dev/null


mkdir -p /etc/amnezia/amneziawg /root/unet-peers
# ---------------------------------------------------------------- firewall helper (idempotent, replay-safe)
cat > /etc/amnezia/amneziawg/firewall.sh <<'FWEOF'
#!/usr/bin/env bash
# Called by awg-quick PostUp/PostDown with a single action arg (up|down).
# WAN_IF/TAILNET/IFACE are baked at install time (the awg-quick fork does not
# substitute shell vars or %i in PostUp — verified on ufo-finland 2026-11-16).
set -u
WAN_IF="__WAN_IF__"
TAILNET="__TAILNET__"
IFACE="awg0"
RULES_FILE="/etc/amnezia/amneziawg/expose.rules"
up() {
  iptables -t nat -N UNET-EXPOSE 2>/dev/null || iptables -t nat -F UNET-EXPOSE
  iptables -t nat -C PREROUTING -i "${WAN_IF}" -j UNET-EXPOSE 2>/dev/null ||     iptables -t nat -A PREROUTING -i "${WAN_IF}" -j UNET-EXPOSE
  iptables -C FORWARD -i "${IFACE}" -j ACCEPT 2>/dev/null || iptables -A FORWARD -i "${IFACE}" -j ACCEPT
  iptables -C FORWARD -o "${IFACE}" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT 2>/dev/null ||     iptables -A FORWARD -o "${IFACE}" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
  iptables -C FORWARD -o "${IFACE}" -m conntrack --ctstate DNAT -j ACCEPT 2>/dev/null ||     iptables -A FORWARD -o "${IFACE}" -m conntrack --ctstate DNAT -j ACCEPT
  iptables -t nat -C POSTROUTING -s "${TAILNET}" -o "${WAN_IF}" -j MASQUERADE 2>/dev/null ||     iptables -t nat -A POSTROUTING -s "${TAILNET}" -o "${WAN_IF}" -j MASQUERADE
  if [[ -f "${RULES_FILE}" ]]; then
    while IFS= read -r rule; do
      [[ -z "${rule}" ]] && continue
      iptables -t nat -C UNET-EXPOSE ${rule} 2>/dev/null || iptables -t nat -A UNET-EXPOSE ${rule}
    done < "${RULES_FILE}"
  fi
}
down() {
  iptables -t nat -D PREROUTING -i "${WAN_IF}" -j UNET-EXPOSE 2>/dev/null || true
  iptables -t nat -F UNET-EXPOSE 2>/dev/null || true
}
case "${1:-}" in
  up)   up ;;
  down) down ;;
  *) echo "usage: firewall.sh <up|down>"; exit 1 ;;
esac
FWEOF
sed -i "s|__WAN_IF__|${WAN_IF}|;s|__TAILNET__|${TAILNET}|" /etc/amnezia/amneziawg/firewall.sh
chmod 0700 /etc/amnezia/amneziawg/firewall.sh
chmod 0700 /etc/amnezia/amneziawg/firewall.sh

# ---------------------------------------------------------------- server conf
mkdir -p /etc/amnezia/amneziawg /root/unet-peers
: > /etc/amnezia/amneziawg/expose.rules
umask 077
cat > /etc/amnezia/amneziawg/awg0.conf <<EOF
[Interface]
Address = 10.66.0.1/24
ListenPort = ${AWG_PORT}
PrivateKey = ${SERVER_PRIV}
# AWG obfuscation. S1/S2/H1-H4 MUST be identical in every client config (copied by add-peer.sh).
S1 = ${S1}
S2 = ${S2}
H1 = ${H1}
H2 = ${H2}
H3 = ${H3}
H4 = ${H4}

PostUp = /etc/amnezia/amneziawg/firewall.sh up
PostDown = /etc/amnezia/amneziawg/firewall.sh down
EOF

printf '%s\n' "${SERVER_PUB}" > /etc/amnezia/amneziawg/server.pub
printf 'VPS_PUBLIC_IP=%s\nAWG_PORT=%s\n' "${WAN_IP}" "${AWG_PORT}" > /etc/amnezia/amneziawg/endpoint.env

# ---------------------------------------------------------------- systemd unit
# [VERIFY 2] relies on awg-quick inheriting wireguard-tools' userspace-launch env.
cat > /etc/systemd/system/awg-quick@awg0.service <<'UNITEOF'
[Unit]
Description=AmneziaWG baseline hub (awg0) via awg-quick (userspace amneziawg-go)
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
Environment=WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go
ExecStart=/usr/local/bin/awg-quick up awg0
ExecStop=/usr/local/bin/awg-quick down awg0

[Install]
WantedBy=multi-user.target
UNITEOF
systemctl daemon-reload
systemctl enable awg-quick@awg0.service
systemctl start awg-quick@awg0.service

# ---------------------------------------------------------------- in-tunnel DNS resolver (FR-009 / zcode F2)
# bind-dynamic: attaches to 10.66.0.1 as soon as awg0 exists (no chicken-egg).
cat > /etc/dnsmasq.d/unet-tailnet.conf <<'DNSEOF'
# unet Option 0: tunnel-side resolver bound to the AWG tailnet address ONLY
listen-address=10.66.0.1
bind-dynamic
no-resolv
server=1.1.1.1
server=8.8.8.8
cache-size=1000
no-hosts
DNSEOF
systemctl restart dnsmasq || echo "WARN: dnsmasq did not start — DNS=10.66.0.1 on clients will fail until it does"

# ---------------------------------------------------------------- fallback unit (only if [VERIFY 2] fails)
cat > /etc/amnezia/amneziawg/awg0.userspace-fallback.service <<'FBEOF'
# If "awg-quick up" fails with a kernel-interface error (no WG_QUICK_USERSPACE_IMPLEMENTATION
# support in your awg-tools build), disable the unit above and use this one instead:
#   1) systemctl disable --now awg-quick@awg0
#   2) cp awg0.userspace-fallback.service /etc/systemd/system/awg0.service && systemctl daemon-reload
#   3) systemctl enable --now awg0
#   4) apply config manually:  awg setconf awg0 /etc/amnezia/amneziawg/awg0.conf && ip addr add 10.66.0.1/24 dev awg0 && ip link set awg0 up
#      (firewall: /etc/amnezia/amneziawg/firewall.sh up)
[Unit]
Description=AmneziaWG userspace daemon (amneziawg-go -f awg0)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/amneziawg-go -f awg0
Restart=on-failure

[Install]
WantedBy=multi-user.target
FBEOF

# ---------------------------------------------------------------- summary
echo
echo "=============================================================="
echo " DONE. Server peer summary:"
echo "   endpoint      : ${WAN_IP}:${AWG_PORT} (udp, open it in the VPS firewall!)"
echo "   server pubkey : ${SERVER_PUB}"
echo "   tailnet       : ${TAILNET} (VPS = 10.66.0.1)"
echo
echo " Next:"
echo "   bash add-peer.sh laptop"
echo "   bash expose-port.sh 8080 10.66.0.2 3000 tcp"
echo "=============================================================="
