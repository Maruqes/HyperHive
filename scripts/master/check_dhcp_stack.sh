#!/usr/bin/env bash
# Quick diagnostic for macvtap + dnsmasq + NAT stack configured by setup_dhcp.sh.

set -uo pipefail

info(){ printf '[INFO] %s\n' "$*"; }
ok(){ printf '[ OK ] %s\n' "$*"; }
warn(){ printf '[WARN] %s\n' "$*" >&2; }
fail(){ printf '[FAIL] %s\n' "$*" >&2; EXITCODE=1; }

EXITCODE=0

[[ ${EUID:-0} -eq 0 ]] || { fail 'Run as root to check firewall/iptables.'; echo; exit "${EXITCODE}"; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

LAN_PARENT_IF="${LAN_PARENT_IF:-512rede}"
LAN_INTERFACE_NAME="${LAN_INTERFACE_NAME:-${LAN_PARENT_IF}-host}"
NETWORK_NAME="${LAN_INTERFACE_NAME}"

SUBNET_CIDR="${SUBNET_CIDR:-192.168.76.0/24}"
GATEWAY_IP="${GATEWAY_IP:-192.168.76.1}"
DHCP_RANGE_START="${DHCP_RANGE_START:-192.168.76.50}"
DHCP_RANGE_END="${DHCP_RANGE_END:-192.168.76.200}"

DNSMASQ_CONF_DIR="${DNSMASQ_CONF_DIR:-/etc/dnsmasq.d}"
DNSMASQ_LEASE_DIR="${DNSMASQ_LEASE_DIR:-/var/lib/dnsmasq}"
DEDICATED_UNIT="dnsmasq-${NETWORK_NAME}.service"

find_wan_iface(){ ip route show default 0.0.0.0/0 | awk '/default/ {print $5; exit}'; }
WAN_IF_INPUT="${CLI_WAN_IF:-${WAN_IF:-}}"
WAN_IF="${WAN_IF_INPUT:-$(find_wan_iface)}"

IFS=/ read -r SUBNET_BASE SUBNET_PREFIX <<<"${SUBNET_CIDR}"
prefix_to_mask(){
  local p=$1
  ((p==0)) && { printf '0.0.0.0'; return; }
  local m=$((0xffffffff^((1<<(32-p))-1)))
  printf '%d.%d.%d.%d' $(((m>>24)&255)) $(((m>>16)&255)) $(((m>>8)&255)) $((m&255))
}
NETMASK="$(prefix_to_mask "${SUBNET_PREFIX}")"
SUBNET_NETWORK="${SUBNET_BASE}/${SUBNET_PREFIX}"

info "Checking configuration for ${NETWORK_NAME} (gateway ${GATEWAY_IP}, range ${DHCP_RANGE_START}-${DHCP_RANGE_END})."

if [[ -z ${WAN_IF} ]]; then
  fail "WAN interface was not detected."
else
  ok "WAN interface detected: ${WAN_IF}"
fi

# macvtap interface
if ip link show "${NETWORK_NAME}" >/dev/null 2>&1; then
  ok "Interface ${NETWORK_NAME} exists."
  if ip -4 addr show "${NETWORK_NAME}" | grep -q "${GATEWAY_IP}/"; then
    ok "Interface ${NETWORK_NAME} has IPv4 ${GATEWAY_IP}/${SUBNET_PREFIX}."
  else
    fail "Interface ${NETWORK_NAME} does not have IPv4 ${GATEWAY_IP}/${SUBNET_PREFIX}."
  fi
  link_state=$(ip -o link show "${NETWORK_NAME}" | awk '{for(i=1;i<=NF;i++) if ($i=="state") {print $(i+1); exit}}')
  [[ ${link_state:-UNKNOWN} == "UP" ]] && ok "Interface ${NETWORK_NAME} is UP." || warn "Interface ${NETWORK_NAME} is in state ${link_state:-unknown}."
else
  fail "Interface ${NETWORK_NAME} does not exist."
fi

# Parent in promisc
if ip link show "${LAN_PARENT_IF}" >/dev/null 2>&1; then
  ok "Parent interface ${LAN_PARENT_IF} found."
  if ip link show "${LAN_PARENT_IF}" | grep -q "PROMISC"; then
    ok "Parent interface ${LAN_PARENT_IF} in promiscuous mode."
  else
    warn "Parent interface ${LAN_PARENT_IF} NOT in promiscuous mode."
  fi
else
  warn "Parent interface ${LAN_PARENT_IF} not found."
fi

# dnsmasq service
if systemctl cat "${DEDICATED_UNIT}" >/dev/null 2>&1; then
  if systemctl is-active --quiet "${DEDICATED_UNIT}"; then
    ok "Service ${DEDICATED_UNIT} active."
  else
    fail "Service ${DEDICATED_UNIT} is not active."
    systemctl --no-pager --lines=20 status "${DEDICATED_UNIT}" || true
  fi
else
  fail "Service ${DEDICATED_UNIT} does not exist."
fi

# Ports
if command -v ss >/dev/null 2>&1; then
  if ss -H -lnp 'sport = :53' 2>/dev/null | grep -q "dnsmasq"; then
    ok "dnsmasq listening on port 53."
  else
    fail "No dnsmasq listening on port 53."
  fi
  if ss -H -lnp 'sport = :67' 2>/dev/null | grep -q "dnsmasq"; then
    ok "dnsmasq listening on port 67."
  else
    fail "No dnsmasq listening on port 67."
  fi
else
  warn "Command 'ss' unavailable; skipping port checks."
fi

# Lease file
LEASE_FILE="${DNSMASQ_LEASE_DIR}/${NETWORK_NAME}.leases"
if [[ -w ${LEASE_FILE} ]]; then
  ok "Lease file ${LEASE_FILE} with write permissions."
  lease_count=$(wc -l <"${LEASE_FILE}")
  info "Current lease count: ${lease_count}"
else
  fail "Lease file ${LEASE_FILE} not writable."
fi

# Sysctl checks
if command -v sysctl >/dev/null 2>&1; then
  if [[ $(sysctl -n net.ipv4.ip_forward 2>/dev/null || echo 0) -eq 1 ]]; then
    ok "net.ipv4.ip_forward active."
  else
    fail "net.ipv4.ip_forward NOT active."
  fi
  if [[ $(sysctl -n "net.ipv4.conf.${NETWORK_NAME}.rp_filter" 2>/dev/null || echo 1) -eq 0 ]]; then
    ok "rp_filter relaxed on ${NETWORK_NAME}."
  else
    warn "rp_filter not relaxed for ${NETWORK_NAME}."
  fi
else
  warn "sysctl unavailable; could not validate ip_forward/rp_filter."
fi

# iptables / NAT
if [[ -n ${WAN_IF} ]]; then
  if command -v iptables >/dev/null 2>&1; then
    if iptables -t nat -C POSTROUTING -s "${SUBNET_NETWORK}" -o "${WAN_IF}" -j MASQUERADE >/dev/null 2>&1; then
      ok "iptables: MASQUERADE rule present (${SUBNET_NETWORK} -> ${WAN_IF})."
    else
      fail "iptables: missing MASQUERADE (${SUBNET_NETWORK} -> ${WAN_IF})."
    fi
    if iptables -C FORWARD -i "${WAN_IF}" -o "${NETWORK_NAME}" -m state --state RELATED,ESTABLISHED -j ACCEPT >/dev/null 2>&1 && \
       iptables -C FORWARD -i "${NETWORK_NAME}" -o "${WAN_IF}" -j ACCEPT >/dev/null 2>&1; then
      ok "iptables: forward rules present."
    else
      fail "iptables: forward rules missing."
    fi
  else
    warn "iptables unavailable; check NAT manually."
  fi
fi

# Basic connectivity (optional)
if command -v ping >/dev/null 2>&1; then
  if ping -I "${NETWORK_NAME}" -c1 -W1 "${GATEWAY_IP}" >/dev/null 2>&1; then
    ok "Loopback ping to gateway ${GATEWAY_IP} successful."
  else
    warn "Failed ping to ${GATEWAY_IP} from interface ${NETWORK_NAME}."
  fi
else
  warn "ping unavailable; could not test basic connectivity."
fi

echo
if (( EXITCODE == 0 )); then
  info "All checks passed."
else
  warn "Problems detected (EXIT=${EXITCODE})."
fi

exit "${EXITCODE}"
if [[ -z ${WAN_IF} ]]; then
  warn "No WAN interface defined/detected; NAT was not validated."
fi
