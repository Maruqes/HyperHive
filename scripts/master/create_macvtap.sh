#!/usr/bin/env bash
set -euo pipefail

# Create (and optionally persist) a macvtap interface anchored to a parent NIC.
# Usage:
#   sudo ./create_macvtap.sh [--persist] <parent_iface> <macvtap_iface> [ipv4_cidr]
# Example:
#   sudo ./create_macvtap.sh --persist 512rede 512rede-host 192.168.76.1/24
#
# When --persist is supplied the script installs a systemd oneshot unit that
# recreates the macvtap and reapplies the address on boot.

usage() {
  cat <<'USAGE'
Usage: create_macvtap.sh [--persist] <parent_iface> <macvtap_iface> [ipv4_cidr]

  --persist      Install a systemd unit to recreate the interface at boot
  parent_iface   Existing physical NIC (e.g. 512rede)
  macvtap_iface  macvtap name to create (e.g. 512rede-host)
  ipv4_cidr      Optional IPv4/prefix to assign (e.g. 192.168.76.1/24)

Notes:
  - Requires root and iproute2 (ip command).
  - Uses macvtap mode bridge so VMs with type='direct' can reach the host.
  - Puts the parent interface in promiscuous mode for reliable forwarding.
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

install_persistence() {
  local parent=$1
  local child=$2
  local ipv4=$3
  local helper="/usr/local/sbin/macvtap-${child}.sh"
  local unit="/etc/systemd/system/macvtap-${child}.service"
  local sysctl_conf="/etc/sysctl.d/99-${child}-proxy-arp.conf"

  info "Cleaning previous persistence artifacts for ${child}"
  systemctl disable --now "macvtap-${child}.service" >/dev/null 2>&1 || true
  rm -f "${helper}" "${unit}" "${sysctl_conf}"
  install -d -m 755 "$(dirname "${helper}")"
  install -d -m 755 "$(dirname "${unit}")"

  info "Installing persistence helper ${helper}"
  cat >"${helper}" <<SCRIPT
#!/usr/bin/env bash
set -euo pipefail

if ip link show ${child} >/dev/null 2>&1; then
  ip link set ${child} down || true
  ip link delete ${child} || true
fi

ip link show ${parent} >/dev/null 2>&1 || exit 1
ip link set ${parent} promisc on
ip link add link ${parent} name ${child} type macvtap mode bridge
ip link set ${child} up
$(if [[ -n ${ipv4} ]]; then
  cat <<EOF
ip addr add ${ipv4} dev ${child}
EOF
fi)
if sysctl -a 2>/dev/null | grep -q '^net.ipv4.conf.${child}.proxy_arp'; then
  sysctl -q -w net.ipv4.conf.${child}.proxy_arp=1
fi
SCRIPT
  chmod 0755 "${helper}"

  info "Creating systemd unit ${unit}"
  cat >"${unit}" <<UNIT
[Unit]
Description=macvtap ${child} on ${parent}
After=network.target

[Service]
Type=oneshot
ExecStart=${helper}
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
UNIT

  if [[ -n ${ipv4} ]]; then
    info "Persisting proxy_arp enablement for ${child}"
    cat >"${sysctl_conf}" <<CONF
net.ipv4.conf.${child}.proxy_arp = 1
CONF
  fi

  systemctl daemon-reload
  systemctl enable --now "macvtap-${child}.service"
  info "Persistence enabled for ${child}"
}

[[ ${EUID:-0} -eq 0 ]] || fatal 'This script must run as root.'
command -v ip >/dev/null 2>&1 || fatal 'ip command not found (install iproute2).'

PERSIST=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --persist)
      PERSIST=1
      shift
      ;;
    -h|--help)
      usage
      ;;
    --)
      shift
      break
      ;;
    -*)
      fatal "Unknown option '$1'"
      ;;
    *)
      break
      ;;
  esac
done

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
  info "Removing existing interface '${MACVTAP_IF}' before recreation"
  ip link set "$MACVTAP_IF" down 2>/dev/null || true
  ip link delete "$MACVTAP_IF" 2>/dev/null || true
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

if (( PERSIST )); then
  install_persistence "$PARENT_IF" "$MACVTAP_IF" "$IPV4_CIDR"
fi

info "macvtap '${MACVTAP_IF}' ready."
if [[ -n $IPV4_CIDR ]]; then
  info "Host address ${IPV4_CIDR} now reachable by VMs using type='direct'."
else
  info "Remember to assign an address if the host must provide services or DHCP."
fi
