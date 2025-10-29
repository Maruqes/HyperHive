#!/usr/bin/env bash
set -euo pipefail

# Create a macvtap interface anchored to an existing NIC.
# Usage: create_macvtap.sh <parent_iface> <macvtap_iface> [ipv4_cidr]
# Example: sudo ./create_macvtap.sh 512rede 512rede-host 192.168.76.1/24

usage() {
  cat <<'USAGE'
Usage: create_macvtap.sh <parent_iface> <macvtap_iface> [ipv4_cidr]

  parent_iface   Existing physical NIC (e.g. 512rede)
  macvtap_iface  New macvtap name to create (e.g. 512rede-host)
  ipv4_cidr      Optional IPv4 address/prefix to assign (e.g. 192.168.76.1/24)

Notes:
  - Requires root and iproute2 (ip command).
  - Uses macvtap mode bridge so VMs with type='direct' can reach the host.
  - Sets the parent interface to promiscuous mode for reliable forwarding.
USAGE
  exit 1
}

fatal() {
  printf '[ERROR] %s\n' "$*" >&2
  exit 1
}

warn() {
  printf '[WARN] %s\n' "$*" >&2
}

info() {
  printf '[INFO] %s\n' "$*"
}

[[ ${EUID:-0} -eq 0 ]] || fatal 'This script must run as root.'
command -v ip >/dev/null 2>&1 || fatal 'ip command not found (install iproute2).'

if [[ $# -lt 2 || $# -gt 3 ]]; then
  usage
fi

PARENT_IF=$1
MACVTAP_IF=$2
IPV4_CIDR=${3:-}

[[ -n $PARENT_IF ]]  || fatal 'Parent interface name cannot be empty.'
[[ -n $MACVTAP_IF ]] || fatal 'macvtap interface name cannot be empty.'

ip link show "$PARENT_IF" >/dev/null 2>&1 || fatal "Parent interface '$PARENT_IF' not found."
if ip link show "$MACVTAP_IF" >/dev/null 2>&1; then
  fatal "Interface '$MACVTAP_IF' already exists."
fi

info "Enabling promiscuous mode on ${PARENT_IF}"
ip link set "$PARENT_IF" promisc on

info "Creating macvtap '${MACVTAP_IF}' on parent '${PARENT_IF}' (mode=bridge)"
ip link add link "$PARENT_IF" name "$MACVTAP_IF" type macvtap mode bridge

trap 'ip link delete "$MACVTAP_IF" 2>/dev/null || true' ERR

info "Bringing '${MACVTAP_IF}' up"
ip link set "$MACVTAP_IF" up

if [[ -n $IPV4_CIDR ]]; then
  info "Assigning IPv4 ${IPV4_CIDR} to ${MACVTAP_IF}"
  ip addr flush dev "$MACVTAP_IF" || true
  ip addr add "$IPV4_CIDR" dev "$MACVTAP_IF"
fi

# Allow the host to answer ARP for peers behind other macvtap clients
sysctl_key="net.ipv4.conf.${MACVTAP_IF}.proxy_arp"
if sysctl -a 2>/dev/null | grep -q "^${sysctl_key}"; then
  info "Enabling proxy_arp on ${MACVTAP_IF}"
  sysctl -q -w "${sysctl_key}=1"
else
  warn "Could not locate ${sysctl_key}; proxy_arp not enabled."
fi

trap - ERR

info "macvtap '${MACVTAP_IF}' ready."
if [[ -n $IPV4_CIDR ]]; then
  info "Host address ${IPV4_CIDR} now reachable by VMs using type='direct'."
else
  info "Remember to assign an address if the host must provide services or DHCP."
fi
