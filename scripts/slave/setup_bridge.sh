#!/usr/bin/env bash
# Configure a local bridge (br512rede) on a slave host so VMs can join the master DHCP network.

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: setup_bridge.sh

This moves the LAN IP from the physical interface onto the bridge. After completion,
the bridge holds the IP; the physical interface will appear without an address.

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
STATE_DIR="${STATE_DIR:-/var/lib/hyperhive}"
STATE_FILE="${STATE_DIR}/${LAN_INTERFACE_NAME}.bridge_state"
STATE_CONN_FILE="${STATE_DIR}/${LAN_INTERFACE_NAME}.nmconnection"

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

  mkdir -p "${STATE_DIR}"

  if [[ ! -f ${STATE_CONN_FILE} ]]; then
    original_connection=$(nmcli -t -f NAME,DEVICE connection show --active | awk -F: -v dev="${LAN_INTERFACE_NAME}" '$2==dev{print $1; exit}')
    if [[ -z ${original_connection} ]]; then
      original_connection=$(nmcli -t -f NAME,DEVICE connection show | awk -F: -v dev="${LAN_INTERFACE_NAME}" '$2==dev{print $1; exit}')
    fi
    if [[ -n ${original_connection} && ${original_connection} != "${NM_CONNECTION_NAME}" && ${original_connection} != "${NM_SLAVE_CONNECTION_NAME}" ]]; then
      info "Saving original NM profile '${original_connection}' for restoration"
      if nmcli connection export "${original_connection}" "${STATE_CONN_FILE}" >/dev/null 2>&1; then
        printf '%s\n' "${original_connection}" > "${STATE_FILE}"
      else
        warn "Failed to export profile '${original_connection}'; bridge removal may not restore it."
      fi
    fi
  fi

  nmcli device set "${LAN_INTERFACE_NAME}" managed yes >/dev/null 2>&1 || true
  nmcli device set "${BRIDGE_NAME}" managed yes >/dev/null 2>&1 || true
  nmcli device disconnect "${LAN_INTERFACE_NAME}" >/dev/null 2>&1 || true
  nmcli device disconnect "${BRIDGE_NAME}" >/dev/null 2>&1 || true

  ip addr flush dev "${LAN_INTERFACE_NAME}" || true

  nmcli connection delete "${NM_SLAVE_CONNECTION_NAME}" >/dev/null 2>&1 || true
  nmcli connection delete "${NM_CONNECTION_NAME}" >/dev/null 2>&1 || true

  info "Creating NM bridge profile ${NM_CONNECTION_NAME}"
  nmcli connection add type bridge ifname "${BRIDGE_NAME}" con-name "${NM_CONNECTION_NAME}" \
    ipv4.method auto ipv6.method ignore autoconnect yes >/dev/null
  nmcli connection modify "${NM_CONNECTION_NAME}" bridge.stp no ipv4.never-default no ipv4.gateway "" ipv4.dns "" ipv4.dhcp-timeout "${DHCP_TIMEOUT}" >/dev/null

  info "Creating NM slave profile ${NM_SLAVE_CONNECTION_NAME} for ${LAN_INTERFACE_NAME}"
  nmcli connection add type bridge-slave ifname "${LAN_INTERFACE_NAME}" con-name "${NM_SLAVE_CONNECTION_NAME}" master "${NM_CONNECTION_NAME}" >/dev/null

  nmcli connection up "${NM_SLAVE_CONNECTION_NAME}" >/dev/null || warn "Failed to bring up ${NM_SLAVE_CONNECTION_NAME}"
  nmcli connection up "${NM_CONNECTION_NAME}" >/dev/null || warn "Failed to bring up ${NM_CONNECTION_NAME}"
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

wait_for_ipv4() {
  local iface=$1
  local timeout=$2
  for ((i=0; i<timeout; i++)); do
    if ip -4 addr show "${iface}" | grep -q 'inet '; then
      return 0
    fi
    sleep 1
  done
  return 1
}

if command -v nmcli >/dev/null 2>&1; then
  ensure_bridge_nm
  if ! wait_for_ipv4 "${BRIDGE_NAME}" "${DHCP_TIMEOUT}"; then
    warn "No IPv4 address detected on ${BRIDGE_NAME} after ${DHCP_TIMEOUT}s. Check cabling or master DHCP availability."
  fi
else
  ensure_bridge_manual
  if ! wait_for_ipv4 "${BRIDGE_NAME}" "${DHCP_TIMEOUT}"; then
    warn "Manual bridge setup did not obtain an IPv4 address on ${BRIDGE_NAME}."
  fi
fi

info "Bridge ${BRIDGE_NAME} ready. The bridge now carries the host IP; ${LAN_INTERFACE_NAME} stays enslaved. Attach VMs to ${BRIDGE_NAME} to reach the master DHCP server."
