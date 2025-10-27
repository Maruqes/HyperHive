#!/usr/bin/env bash
set -euo pipefail

# ================================
#  macvlan-host em cima de 512rede
# ================================
# Por omissão:
# - Interface "pai": 512rede
# - Interface macvlan: macvlan-host
# - Modo: bridge
# - IP a atribuir: 192.168.76.250/24  (altera se precisares)
#
# NOTA: Não altera a tua 512rede nem o NetworkManager.

PARENT_IF="${PARENT_IF:-512rede}"
MACVLAN_IF="${MACVLAN_IF:-macvlan-host}"
MODE="${MODE:-bridge}"
HOST_IP_CIDR="${HOST_IP_CIDR:-192.168.76.250/24}"
ADD_FIREWALL_IF="${ADD_FIREWALL_IF:-1}"   # 1=adiciona a firewall, 0=não

need_root() {
  if [[ $EUID -ne 0 ]]; then
    echo "Este script tem de correr como root (sudo)." >&2
    exit 1
  fi
}

check_parent() {
  if ! ip link show "$PARENT_IF" >/dev/null 2>&1; then
    echo "A interface pai '$PARENT_IF' não existe." >&2
    exit 2
  fi
}

create_macvlan() {
  if ! ip link show "$MACVLAN_IF" >/dev/null 2>&1; then
    ip link add link "$PARENT_IF" name "$MACVLAN_IF" type macvlan mode "$MODE"
  fi
  ip link set "$MACVLAN_IF" up
}

assign_ip() {
  # Evita atribuir um IP que já esteja em uso noutra interface
  local ip_no_cidr="${HOST_IP_CIDR%/*}"
  if ! ip -o addr show dev "$MACVLAN_IF" | grep -qF "$HOST_IP_CIDR"; then
    if ip -o addr | awk '{print $4}' | grep -qwF "$ip_no_cidr"; then
      echo "ATENÇÃO: o IP $ip_no_cidr já está atribuído a outra interface." >&2
      echo "Altera a variável HOST_IP_CIDR e volta a correr." >&2
      exit 3
    fi
    ip addr add "$HOST_IP_CIDR" dev "$MACVLAN_IF"
  fi
}

tune_sysctls() {
  # Minimiza comportamentos estranhos de ARP quando há 2 ifaces na mesma sub-rede
  sysctl -w "net.ipv4.conf.${MACVLAN_IF}.arp_filter=1" >/dev/null
  sysctl -w "net.ipv4.conf.${PARENT_IF}.arp_filter=1"  >/dev/null
  sysctl -w "net.ipv4.conf.${MACVLAN_IF}.arp_announce=2" >/dev/null || true
}

add_firewall() {
  if [[ "$ADD_FIREWALL_IF" == "1" ]] && command -v firewall-cmd >/dev/null 2>&1; then
    if firewall-cmd --state >/dev/null 2>&1; then
      local zone
      zone="$(firewall-cmd --get-default-zone)"
      firewall-cmd --permanent --zone="$zone" --add-interface="$MACVLAN_IF" || true
      firewall-cmd --reload || true
    fi
  fi
}

main() {
  need_root
  check_parent
  create_macvlan
  assign_ip
  tune_sysctls
  add_firewall
  echo "✅ Criada '${MACVLAN_IF}' sobre '${PARENT_IF}' em modo '${MODE}' com IP ${HOST_IP_CIDR}."
  echo "   O host já consegue comunicar com as VMs ligadas via macvtap à '${PARENT_IF}'."
}

main "$@"
