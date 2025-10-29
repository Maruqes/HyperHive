#!/usr/bin/env bash
# Configure a local bridge (br512rede) on a slave host so VMs can join the master DHCP network.

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: setup_bridge.sh

Environment variables:
  LAN_INTERFACE_NAME   Physical interface plugged into the master network (default: 512rede)
  BRIDGE_NAME          Bridge name to create/ensure (default: br512rede)
  NM_CONNECTION_NAME   NetworkManager bridge profile name (default: <BRIDGE_NAME>-dhcp)
  NM_SLAVE_CONNECTION_NAME  NetworkManager slave profile name (default: <LAN_INTERFACE_NAME>-slave)
  DHCP_TIMEOUT         Seconds to wait for DHCP when NetworkManager unavailable (default: 30)
USAGE
  exit 1
}

info()  { printf '[INFO] %s\n' "$*"; }
warn()  { printf '[WARN] %s\n' "$*" >&2; }
fatal() { printf '[ERROR] %s\n' "$*" >&2; exit 1; }

case "${1:-}" in
  -h|--help) usage ;;
  "") ;;
  *) usage ;;
esac

[[ ${EUID:-0} -eq 0 ]] || fatal 'This script must run as root.'
if [[ -r /etc/os-release ]]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  if [[ "${ID,,}" != "fedora" && ! ${ID_LIKE:-} =~ fedora ]]; then
    fatal 'This script currently targets Fedora-based hosts only.'
  fi
else
  fatal 'Unable to determine distribution (missing /etc/os-release).'
fi

LAN_INTERFACE_NAME="${LAN_INTERFACE_NAME:-512rede}"
BRIDGE_NAME="${BRIDGE_NAME:-br512rede}"
NM_CONNECTION_NAME="${NM_CONNECTION_NAME:-${BRIDGE_NAME}-dhcp}"
NM_SLAVE_CONNECTION_NAME="${NM_SLAVE_CONNECTION_NAME:-${LAN_INTERFACE_NAME}-slave}"
DHCP_TIMEOUT="${DHCP_TIMEOUT:-30}"

command -v ip >/dev/null 2>&1 || fatal 'iproute2 tools are required (missing `ip`).'
ip link show "${LAN_INTERFACE_NAME}" >/dev/null 2>&1 || fatal "Interface '${LAN_INTERFACE_NAME}' not found."

ensure_bridge_nm() {
  info "Configuring bridge ${BRIDGE_NAME} via NetworkManager"

  # Clean up conflicting profiles bound to the bridge or physical interface.
  for dev in "${BRIDGE_NAME}" "${LAN_INTERFACE_NAME}"; do
    while read -r uuid name; do
      [[ -z ${uuid} ]] && continue
      [[ ${name} == "${NM_CONNECTION_NAME}" || ${name} == "${NM_SLAVE_CONNECTION_NAME}" ]] && continue
      info "Deleting NM profile '${name}' on ${dev}"
      nmcli connection delete uuid "${uuid}" >/dev/null 2>&1 || true
    done < <(nmcli -t -f UUID,NAME,DEVICE connection show | awk -F: -v dev="${dev}" '$3==dev{print $1" "$2}')
  done

  nmcli device set "${LAN_INTERFACE_NAME}" managed yes >/dev/null 2>&1 || true
  nmcli device disconnect "${LAN_INTERFACE_NAME}" >/dev/null 2>&1 || true
  nmcli device disconnect "${BRIDGE_NAME}" >/dev/null 2>&1 || true

  nmcli connection delete "${NM_SLAVE_CONNECTION_NAME}" >/dev/null 2>&1 || true
  nmcli connection delete "${NM_CONNECTION_NAME}" >/dev/null 2>&1 || true

  info "Creating NM bridge profile ${NM_CONNECTION_NAME}"
  nmcli connection add type bridge ifname "${BRIDGE_NAME}" con-name "${NM_CONNECTION_NAME}" \
    ipv4.method auto ipv4.never-default yes ipv6.method ignore autoconnect yes >/dev/null
  nmcli connection modify "${NM_CONNECTION_NAME}" bridge.stp no ipv4.gateway "" ipv4.dns "" ipv4.dhcp-timeout "${DHCP_TIMEOUT}" >/dev/null

  info "Creating NM slave profile ${NM_SLAVE_CONNECTION_NAME} for ${LAN_INTERFACE_NAME}"
  nmcli connection add type bridge-slave ifname "${LAN_INTERFACE_NAME}" con-name "${NM_SLAVE_CONNECTION_NAME}" master "${NM_CONNECTION_NAME}" >/dev/null

  nmcli connection up "${NM_CONNECTION_NAME}" >/dev/null || warn "Failed to bring up ${NM_CONNECTION_NAME}"
  nmcli connection up "${NM_SLAVE_CONNECTION_NAME}" >/dev/null || warn "Failed to bring up ${NM_SLAVE_CONNECTION_NAME}"
}

ensure_bridge_manual() {
  warn "NetworkManager not found; falling back to iproute2 + dhclient."

  ip link set "${LAN_INTERFACE_NAME}" down >/dev/null 2>&1 || true
  ip link set "${LAN_INTERFACE_NAME}" nomaster >/dev/null 2>&1 || true

  if ! ip link show "${BRIDGE_NAME}" >/dev/null 2>&1; then
    info "Creating bridge ${BRIDGE_NAME}"
    ip link add name "${BRIDGE_NAME}" type bridge
  else
    info "Reusing existing bridge ${BRIDGE_NAME}"
  fi

  ip link set "${BRIDGE_NAME}" type bridge stp_state 0 2>/dev/null || true
  ip addr flush dev "${LAN_INTERFACE_NAME}" || true
  ip addr flush dev "${BRIDGE_NAME}" || true

  ip link set "${BRIDGE_NAME}" up
  ip link set "${LAN_INTERFACE_NAME}" master "${BRIDGE_NAME}"
  ip link set "${LAN_INTERFACE_NAME}" up

  if command -v dhclient >/dev/null 2>&1; then
    info "Requesting DHCP lease on ${BRIDGE_NAME}"
    dhclient -r "${LAN_INTERFACE_NAME}" >/dev/null 2>&1 || true
    dhclient -r "${BRIDGE_NAME}" >/dev/null 2>&1 || true
    dhclient -1 "${BRIDGE_NAME}" || warn "dhclient timed out obtaining lease on ${BRIDGE_NAME}"
  else
    warn 'dhclient not found; configure an IP on the bridge manually.'
  fi
}

if command -v nmcli >/dev/null 2>&1; then
  ensure_bridge_nm
else
  ensure_bridge_manual
fi

info "Bridge ${BRIDGE_NAME} ready. Attach VMs to this bridge to reach the master DHCP server."
