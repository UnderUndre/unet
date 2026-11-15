#!/usr/bin/env bash
# Expose a service running on a tailnet device to the public internet via DNAT on the VPS.
# Usage (root, on the VPS):  bash expose-port.sh <public_port> <peer_ip> <target_port> [tcp|udp|both]
# Example:  bash expose-port.sh 8080 10.66.0.2 3000 tcp
#           -> http://<VPS_PUBLIC_IP>:8080  ==  laptop's 127.0.0.1:3000
# Rules persist in /etc/amneziawg/expose.rules and are replayed by firewall.sh on every tunnel up.
set -euo pipefail

[[ ${EUID} -eq 0 ]] || { echo "ERROR: run as root"; exit 1; }
PUB="${1:-}"; PEER_IP="${2:-}"; TPORT="${3:-}"; PROTO="${4:-tcp}"
CONF=/etc/amneziawg/awg0.conf
RULES_FILE=/etc/amneziawg/expose.rules

[[ "${PUB}" =~ ^[0-9]+$ ]] && [[ "${PUB}" -ge 1 && "${PUB}" -le 65535 ]] || { echo "usage: expose-port.sh <public_port 1-65535> <peer_ip> <target_port> [tcp|udp|both]"; exit 1; }
[[ "${TPORT}" =~ ^[0-9]+$ ]] && [[ "${TPORT}" -ge 1 && "${TPORT}" -le 65535 ]] || { echo "ERROR: target_port must be 1-65535"; exit 1; }
[[ "${PEER_IP}" =~ ^10\.66\.0\.[0-9]{1,3}$ ]] || { echo "ERROR: peer_ip must be in 10.66.0.0/24"; exit 1; }
[[ "${PEER_IP}" == "10.66.0.1" ]] && { echo "ERROR: 10.66.0.1 is the VPS itself — publish its ports directly, no DNAT needed"; exit 1; }
[[ "${PROTO}" =~ ^(tcp|udp|both)$ ]] || { echo "ERROR: proto must be tcp|udp|both"; exit 1; }
grep -q "AllowedIPs = ${PEER_IP}/32" "${CONF}" || { echo "ERROR: peer ${PEER_IP} not found in awg0.conf (run add-peer.sh first)"; exit 1; }

[[ "${PROTO}" == "both" ]] && PROTOS=(tcp udp) || PROTOS=("${PROTO}")

for p in "${PROTOS[@]}"; do
  RULE="-p ${p} --dport ${PUB} -j DNAT --to-destination ${PEER_IP}:${TPORT}"
  if iptables -t nat -C UNET-EXPOSE ${RULE} 2>/dev/null; then
    echo ">> already live: ${p}/${PUB} -> ${PEER_IP}:${TPORT}"
  else
    iptables -t nat -A UNET-EXPOSE ${RULE}
    echo ">> DNAT live: ${p}/${PUB} -> ${PEER_IP}:${TPORT}"
  fi
  grep -qxF "${RULE}" "${RULES_FILE}" 2>/dev/null || echo "${RULE}" >> "${RULES_FILE}"
done

# --- reminder about the local service binding
echo
echo " NOTE: the target service on the device must listen on an interface reachable from its"
echo "       tailnet IP (${PEER_IP}) — i.e. 0.0.0.0 or the tailnet address, not only 127.0.0.1 —"
echo "       OR use the unet reverse-forward (spec 001) which tunnels loopback safely."
echo " Persisted to ${RULES_FILE}; replayed automatically on every 'awg-quick up awg0'."
echo " Test from outside:  curl -v http://$(source /etc/amneziawg/endpoint.env; echo $VPS_PUBLIC_IP):${PUB}/"
