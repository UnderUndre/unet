#!/usr/bin/env bash
# Add one device as a peer to the Option 0 AWG hub.
# Usage (root, on the VPS):  bash add-peer.sh <name>
# Writes client config to /root/unet-peers/<name>.conf and appends the peer to awg0.conf
# with a no-downtime apply (awg syncconf; falls back to unit restart).
set -euo pipefail

[[ ${EUID} -eq 0 ]] || { echo "ERROR: run as root"; exit 1; }
NAME="${1:-}"
[[ "${NAME}" =~ ^[a-zA-Z0-9_-]{1,32}$ ]] || { echo "usage: add-peer.sh <name>  (1-32 chars [a-zA-Z0-9_-])"; exit 1; }

CONF=/etc/amneziawg/awg0.conf
PUB_FILE=/etc/amneziawg/server.pub
END_FILE=/etc/amneziawg/endpoint.env
OUT_DIR=/root/unet-peers
[[ -f "${CONF}" && -f "${PUB_FILE}" && -f "${END_FILE}" ]] || { echo "ERROR: run server-install.sh first"; exit 1; }

grep -q "# peer ${NAME}\b" "${CONF}" && { echo "ERROR: peer '${NAME}' already exists"; exit 1; }

# --- next free tailnet IP (10.66.0.2 .. 10.66.0.254)
CLIENT_IP=""
for i in $(seq 2 254); do
  if ! grep -q "10\.66\.0\.${i}/32" "${CONF}"; then CLIENT_IP="10.66.0.${i}"; break; fi
done
[[ -n "${CLIENT_IP}" ]] || { echo "ERROR: tailnet is full"; exit 1; }

# --- keys
CLIENT_PRIV="$(awg genkey)"
CLIENT_PUB="$(printf '%s' "${CLIENT_PRIV}" | awg pubkey)"
PSK="$(awg genpsk)"

# --- copy session-shaping params from the server conf (S1/S2/H1-H4 MUST match, per README)
S1="$(awk '/^S1 =/{print $3}' "${CONF}")"
S2="$(awk '/^S2 =/{print $3}' "${CONF}")"
H1="$(awk '/^H1 =/{print $3}' "${CONF}")"
H2="$(awk '/^H2 =/{print $3}' "${CONF}")"
H3="$(awk '/^H3 =/{print $3}' "${CONF}")"
H4="$(awk '/^H4 =/{print $3}' "${CONF}")"
for v in S1 S2 H1 H2 H3 H4; do [[ -n "${!v}" ]] || { echo "ERROR: cannot read ${v} from server conf"; exit 1; }; done

# --- endpoint
# shellcheck disable=SC1090
source "${END_FILE}"

# --- register peer on the server
cat >> "${CONF}" <<EOF

[Peer]
# peer ${NAME} (${CLIENT_IP})
PublicKey = ${CLIENT_PUB}
PresharedKey = ${PSK}
AllowedIPs = ${CLIENT_IP}/32
EOF

# --- apply without dropping existing sessions [VERIFY 3: syncconf availability]
if ! awg syncconf awg0 <(awg-quick strip awg0) 2>/dev/null; then
  echo ">> syncconf unavailable — restarting unit (brief tunnel blip)"
  systemctl restart awg-quick@awg0.service
fi

# --- client config (import into AmneziaVPN; or awg-quick on Linux)
umask 077
CLIENT_CONF="${OUT_DIR}/${NAME}.conf"
cat > "${CLIENT_CONF}" <<EOF
[Interface]
Address = ${CLIENT_IP}/24
PrivateKey = ${CLIENT_PRIV}
# Junk packets / signature packets are client-side only (carry no data).
Jc = 6
Jmin = 50
Jmax = 1100
# Session-shaping params: MUST match the server (auto-copied from awg0.conf).
S1 = ${S1}
S2 = ${S2}
H1 = ${H1}
H2 = ${H2}
H3 = ${H3}
H4 = ${H4}
DNS = 10.66.0.1

[Peer]
PublicKey = $(cat "${PUB_FILE}")
PresharedKey = ${PSK}
Endpoint = ${VPS_PUBLIC_IP}:${AWG_PORT}
# Route the whole tailnet through the tunnel (hub-and-spoke).
AllowedIPs = 10.66.0.0/24
PersistentKeepalive = 25
EOF
chmod 0600 "${CLIENT_CONF}"

echo "=============================================================="
echo " peer '${NAME}' created"
echo "   tailnet IP : ${CLIENT_IP}"
echo "   client conf: ${CLIENT_CONF}  (mode 0600 — transfer to the device securely)"
echo "   verify     : awg show   (handshake appears when the device connects)"
echo "=============================================================="
