#!/usr/bin/env bash
# Tear down br512rede and restore the physical interface to standalone DHCP.

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: remove_bridge.sh

Environment variables:
  LAN_INTERFACE_NAME        Physical interface previously enslaved (default: 512rede)
  BRIDGE_NAME               Bridge name to remove (default: br512rede)
  NM_CONNECTION_NAME        NetworkManager bridge profile to delete (default: <BRIDGE_NAME>-dhcp)
  NM_SLAVE_CONNECTION_NAME  NetworkManager slave profile to delete (default: <LAN_INTERFACE_NAME>-slave)
  NM_RESTORE_CONNECTION_NAME NetworkManager connection to recreate for the physical NIC (default: <LAN_INTERFACE_NAME>-dhcp)
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
NM_RESTORE_CONNECTION_NAME="${NM_RESTORE_CONNECTION_NAME:-${LAN_INTERFACE_NAME}-dhcp}"

command -v ip >/dev/null 2>&1 || fatal 'iproute2 tools are required (missing `ip`).'
ip link show "${LAN_INTERFACE_NAME}" >/dev/null 2>&1 || fatal "Interface '${LAN_INTERFACE_NAME}' not found."

remove_bridge_nm() {
  info "Cleaning up NetworkManager bridge configuration for ${BRIDGE_NAME}"
  nmcli connection down "${NM_SLAVE_CONNECTION_NAME}" >/dev/null 2>&1 || true
  nmcli connection down "${NM_CONNECTION_NAME}" >/dev/null 2>&1 || true
  nmcli connection delete "${NM_SLAVE_CONNECTION_NAME}" >/dev/null 2>&1 || true
  nmcli connection delete "${NM_CONNECTION_NAME}" >/dev/null 2>&1 || true

  nmcli device disconnect "${BRIDGE_NAME}" >/dev/null 2>&1 || true
  nmcli device disconnect "${LAN_INTERFACE_NAME}" >/dev/null 2>&1 || true

  if nmcli -t -f NAME connection show | grep -Fxq "${NM_RESTORE_CONNECTION_NAME}"; then
    info "Bringing up existing ${NM_RESTORE_CONNECTION_NAME}"
    nmcli connection up "${NM_RESTORE_CONNECTION_NAME}" >/dev/null || warn "Failed to activate ${NM_RESTORE_CONNECTION_NAME}"
    return
  fi

  info "Creating restoration profile ${NM_RESTORE_CONNECTION_NAME} for ${LAN_INTERFACE_NAME}"
  nmcli connection add type ethernet ifname "${LAN_INTERFACE_NAME}" con-name "${NM_RESTORE_CONNECTION_NAME}" \
    ipv4.method auto ipv6.method ignore autoconnect yes >/dev/null
  nmcli connection up "${NM_RESTORE_CONNECTION_NAME}" >/dev/null || warn "Failed to activate ${NM_RESTORE_CONNECTION_NAME}"
}

remove_bridge_manual() {
  warn "NetworkManager not found; removing bridge via iproute2."
  ip link set "${LAN_INTERFACE_NAME}" down >/dev/null 2>&1 || true
  ip link set "${LAN_INTERFACE_NAME}" nomaster >/dev/null 2>&1 || true

  if ip link show "${BRIDGE_NAME}" >/dev/null 2>&1; then
    info "Deleting bridge ${BRIDGE_NAME}"
    ip link set "${BRIDGE_NAME}" down >/dev/null 2>&1 || true
    ip link delete "${BRIDGE_NAME}" type bridge >/dev/null 2>&1 || true
  fi

  ip addr flush dev "${LAN_INTERFACE_NAME}" || true
  ip link set "${LAN_INTERFACE_NAME}" up

  if command -v dhclient >/dev/null 2>&1; then
    info "Requesting DHCP lease on ${LAN_INTERFACE_NAME}"
    dhclient -r "${LAN_INTERFACE_NAME}" >/dev/null 2>&1 || true
    dhclient "${LAN_INTERFACE_NAME}" || warn "dhclient failed to obtain lease on ${LAN_INTERFACE_NAME}"
  else
    warn 'dhclient not found; configure an IP on the physical interface manually.'
  fi
}

if command -v nmcli >/dev/null 2>&1; then
  remove_bridge_nm
else
  remove_bridge_manual
fi

info "Bridge ${BRIDGE_NAME} removed. ${LAN_INTERFACE_NAME} operates standalone again."
