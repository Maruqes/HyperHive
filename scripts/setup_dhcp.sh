#!/usr/bin/env bash
# Hardened, isolated DHCP + NAT for a single interface on Fedora.
# - Wipes old per-network artifacts and conflicting settings for this network.
# - Disables the global dnsmasq.service (to avoid any 10.42.* leftovers).
# - Runs a dedicated dnsmasq-<network>.service that reads ONLY /etc/dnsmasq.d/<network>.conf
# - firewalld NAT (iptables fallback) + persistent IPv4 forwarding.

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: setup_dhcp.sh [LAN_IFACE] [WAN_IFACE]

  LAN_IFACE : Interface that should serve DHCP (defaults to NETWORK_NAME env or 512rede)
  WAN_IFACE : Upstream interface used for NAT (auto-detected if omitted)

Environment variables still override defaults (NETWORK_NAME, WAN_IF, SUBNET_CIDR, ...).
Command-line arguments take precedence over the environment.
USAGE
  exit 1
}

info()  { printf '[INFO] %s\n' "$*"; }
warn()  { printf '[WARN] %s\n' "$*" >&2; }
fatal() { printf '[ERROR] %s\n' "$*" >&2; exit 1; }

# --- Root & distro guard ------------------------------------------------------
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

# --- CLI overrides -----------------------------------------------------------
CLI_NETWORK_NAME=""
CLI_WAN_IF=""

case "${1:-}" in
  -h|--help) usage ;;
esac

if [[ $# -gt 0 ]]; then
  CLI_NETWORK_NAME=$1
  shift
fi

if [[ $# -gt 0 ]]; then
  CLI_WAN_IF=$1
  shift
fi

if [[ $# -gt 0 ]]; then
  usage
fi

# --- Tunables (override via env) ---------------------------------------------
NETWORK_NAME_DEFAULT="${NETWORK_NAME:-512rede}"   # your LAN interface name
NETWORK_NAME="${CLI_NETWORK_NAME:-${NETWORK_NAME_DEFAULT}}"
SUBNET_CIDR="${SUBNET_CIDR:-192.168.76.0/24}"     # LAN subnet
GATEWAY_IP="${GATEWAY_IP:-192.168.76.1}"          # LAN gateway (this host)
DHCP_RANGE_START="${DHCP_RANGE_START:-192.168.76.50}"
DHCP_RANGE_END="${DHCP_RANGE_END:-192.168.76.200}"
RESOLV_CONF="${RESOLV_CONF:-/etc/resolv.conf}"    # upstream resolvers to forward to
DNSMASQ_CONF_DIR="${DNSMASQ_CONF_DIR:-/etc/dnsmasq.d}"
DNSMASQ_LEASE_DIR="${DNSMASQ_LEASE_DIR:-/var/lib/dnsmasq}"
NM_CONNECTION_NAME="${NM_CONNECTION_NAME:-${NETWORK_NAME}-static}"
SYSCTL_CONF="/etc/sysctl.d/99-${NETWORK_NAME}-ipforward.conf"
DEDICATED_UNIT="dnsmasq-${NETWORK_NAME}.service"
NAT_UNIT="${NETWORK_NAME}-nat.service"
NAT_HELPER="/usr/local/sbin/${NETWORK_NAME}-nat.sh"

# --- Pre-reqs -----------------------------------------------------------------
command -v ip        >/dev/null 2>&1 || fatal 'iproute2 tools are required (missing `ip`).'
command -v dnsmasq   >/dev/null 2>&1 || fatal 'dnsmasq must be installed before running this script.'
command -v nmcli     >/dev/null 2>&1 || warn 'nmcli not found: persistence for interface may be weaker.'

# --- CIDR helpers -------------------------------------------------------------
cidr_prefix=${SUBNET_CIDR#*/}
network_base=${SUBNET_CIDR%/*}
[[ -n ${cidr_prefix} && ${cidr_prefix} =~ ^[0-9]+$ ]] || fatal "Invalid SUBNET_CIDR '${SUBNET_CIDR}'"
cidr_prefix=$((10#${cidr_prefix}))
(( cidr_prefix >= 0 && cidr_prefix <= 32 )) || fatal "Invalid SUBNET_CIDR '${SUBNET_CIDR}'"

prefix_to_mask() {
  local p=$1; (( p==0 )) && { printf '0.0.0.0'; return; }
  local m=$(( 0xffffffff ^ ((1 << (32 - p)) - 1) ))
  printf '%d.%d.%d.%d' $(((m>>24)&255)) $(((m>>16)&255)) $(((m>>8)&255)) $((m&255))
}
ip_to_int() { local IFS=.; read -r a b c d <<<"$1"; printf '%u' $(( (a<<24)|(b<<16)|(c<<8)|d )); }
int_to_ip() { local v=$1; printf '%d.%d.%d.%d' $(((v>>24)&255)) $(((v>>16)&255)) $(((v>>8)&255)) $((v&255)); }

mask_int=$(( cidr_prefix == 0 ? 0 : 0xffffffff ^ ((1 << (32 - cidr_prefix)) - 1) ))
network_int=$(( $(ip_to_int "${network_base}") & mask_int ))
network_address=$(int_to_ip "${network_int}")
SUBNET_NETWORK="${network_address}/${cidr_prefix}"
NETMASK=$(prefix_to_mask "${cidr_prefix}")

# --- Verify interface exists --------------------------------------------------
ip link show "${NETWORK_NAME}" >/dev/null 2>&1 || fatal "Interface '${NETWORK_NAME}' not found."

# --- Cleanup: remove anything for THIS network that could interfere ----------
cleanup_for_network() {
  info "Cleaning prior artifacts for ${NETWORK_NAME}"

  # 1) Old per-network dnsmasq conf and leases (we'll recreate)
  install -d -m 755 "${DNSMASQ_CONF_DIR}" "${DNSMASQ_LEASE_DIR}"
  rm -f "${DNSMASQ_CONF_DIR}/${NETWORK_NAME}.conf"
  rm -f "${DNSMASQ_LEASE_DIR}/${NETWORK_NAME}.leases"

  # 2) Remove old drop-ins we might have created on the global dnsmasq.service
  rm -f "/etc/systemd/system/dnsmasq.service.d/${NETWORK_NAME}-wait.conf"
  rmdir --ignore-fail-on-non-empty "/etc/systemd/system/dnsmasq.service.d" 2>/dev/null || true

  # 3) Stop & disable the global dnsmasq (we'll run a dedicated instance)
  if systemctl list-unit-files | grep -q '^dnsmasq\.service'; then
    systemctl disable --now dnsmasq.service >/dev/null 2>&1 || true
  fi

  # 4) Kill any stray dnsmasq bound to our iface/IP
  pkill -f "dnsmasq.*${NETWORK_NAME}" >/dev/null 2>&1 || true

  # 5) Remove legacy iptables NAT unit
  systemctl disable --now "${NAT_UNIT}" >/dev/null 2>&1 || true
  rm -f "/etc/systemd/system/${NAT_UNIT}" "${NAT_HELPER}"

  # 6) Ensure NM connection for this iface is NOT in 'shared' mode and is clean
  if command -v nmcli >/dev/null 2>&1; then
    # Delete every NM profile bound to this DEVICE except our target name
    while read -r uuid name; do
      [[ -z ${uuid} ]] && continue
      [[ ${name} == "${NM_CONNECTION_NAME}" ]] && continue
      info "Deleting NM profile '${name}' on ${NETWORK_NAME}"
      nmcli connection delete uuid "${uuid}" >/dev/null 2>&1 || true
    done < <(nmcli -t -f UUID,NAME,DEVICE connection show | awk -F: -v dev="${NETWORK_NAME}" '$3==dev{print $1" "$2}')

    # Recreate our clean static profile
    nmcli connection delete "${NM_CONNECTION_NAME}" >/dev/null 2>&1 || true
    info "Creating NM static profile ${NM_CONNECTION_NAME} on ${NETWORK_NAME} (${GATEWAY_IP}/${cidr_prefix})"
    nmcli connection add type ethernet ifname "${NETWORK_NAME}" con-name "${NM_CONNECTION_NAME}" \
      ipv4.addresses "${GATEWAY_IP}/${cidr_prefix}" ipv4.method manual ipv4.never-default yes \
      ipv6.method ignore autoconnect yes >/dev/null
    nmcli connection modify "${NM_CONNECTION_NAME}" ipv4.gateway "" ipv4.dns "" ipv4.may-fail no >/dev/null
    nmcli connection up "${NM_CONNECTION_NAME}" >/dev/null || warn "Failed to activate ${NM_CONNECTION_NAME}"
  else
    # Fallback if NM missing: set IP directly
    ip addr flush dev "${NETWORK_NAME}" || true
    ip link set "${NETWORK_NAME}" up
    ip addr add "${GATEWAY_IP}/${cidr_prefix}" dev "${NETWORK_NAME}" valid_lft forever preferred_lft forever
  fi
}
cleanup_for_network

# --- Write a per-network dnsmasq config (isolated) ---------------------------
DNSMASQ_CONF="${DNSMASQ_CONF_DIR}/${NETWORK_NAME}.conf"
info "Writing ${DNSMASQ_CONF}"
cat >"${DNSMASQ_CONF}" <<CFG
# Auto-generated for ${NETWORK_NAME} -- DO NOT EDIT BY HAND
interface=${NETWORK_NAME}
bind-interfaces
domain-needed
bogus-priv
# DHCP
dhcp-authoritative
dhcp-range=${DHCP_RANGE_START},${DHCP_RANGE_END},${NETMASK},infinite
dhcp-option=option:router,${GATEWAY_IP}
dhcp-option=option:dns-server,${GATEWAY_IP}
dhcp-leasefile=${DNSMASQ_LEASE_DIR}/${NETWORK_NAME}.leases
# DNS forwarders
resolv-file=${RESOLV_CONF}
log-dhcp
CFG

# Sanity check config before starting a service with it
dnsmasq --test -C "${DNSMASQ_CONF}" >/dev/null || fatal "dnsmasq config test failed for ${DNSMASQ_CONF}"

# --- IPv4 forwarding (persist) -----------------------------------------------
info 'Enabling IPv4 forwarding'
cat >"${SYSCTL_CONF}" <<SYSCTL
net.ipv4.ip_forward = 1
SYSCTL
sysctl -w net.ipv4.ip_forward=1 >/dev/null

# --- WAN detection ------------------------------------------------------------
find_wan_iface() {
  local iface
  iface=$(ip route show default 0.0.0.0/0 | awk '/default/ {print $5; exit}')
  [[ -n ${iface} ]] || fatal 'Unable to determine upstream (WAN) interface. Set WAN_IF env before running.'
  printf '%s' "${iface}"
}
WAN_IF_INPUT="${CLI_WAN_IF:-${WAN_IF:-}}"
WAN_IF="${WAN_IF_INPUT:-$(find_wan_iface)}"
[[ -n ${WAN_IF} ]] || fatal 'WAN interface detection failed.'
[[ "${WAN_IF}" != "${NETWORK_NAME}" ]] || fatal 'WAN interface must differ from the DHCP-serving interface.'
ip link show "${WAN_IF}" >/dev/null 2>&1 || fatal "WAN interface '${WAN_IF}' not found."

# --- NAT via firewalld (preferred) or iptables fallback ----------------------
apply_firewall_cmd() {
  if ! command -v firewall-cmd >/dev/null 2>&1; then
    return 1
  fi
  local default_zone
  default_zone=$(firewall-cmd --get-default-zone)
  info "Configuring firewalld (default zone=${default_zone}, LAN=${NETWORK_NAME})"
  firewall-cmd --permanent --zone="${default_zone}" --remove-interface="${NETWORK_NAME}" >/dev/null 2>&1 || true
  firewall-cmd --permanent --zone=trusted --add-interface="${NETWORK_NAME}" >/dev/null
  firewall-cmd --permanent --zone="${default_zone}" --add-masquerade >/dev/null
  # Allow DHCP/DNS explicitly in trusted (usually open, but be explicit)
  firewall-cmd --permanent --zone=trusted --add-service=dhcp >/dev/null 2>&1 || true
  firewall-cmd --permanent --zone=trusted --add-service=dns  >/dev/null 2>&1 || true
  firewall-cmd --reload >/dev/null
  return 0
}
apply_iptables_rules() {
  if ! command -v iptables >/dev/null 2>&1; then
    warn 'iptables not available; skipping explicit NAT rules.'
    return 1
  fi
  info "Configuring NAT via iptables (WAN=${WAN_IF}, LAN=${SUBNET_NETWORK})"
  iptables -t nat -D POSTROUTING -s "${SUBNET_NETWORK}" -o "${WAN_IF}" -j MASQUERADE 2>/dev/null || true
  iptables -D FORWARD -i "${WAN_IF}" -o "${NETWORK_NAME}" -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true
  iptables -D FORWARD -i "${NETWORK_NAME}" -o "${WAN_IF}" -j ACCEPT 2>/dev/null || true

  iptables -t nat -A POSTROUTING -s "${SUBNET_NETWORK}" -o "${WAN_IF}" -j MASQUERADE
  iptables -A FORWARD -i "${WAN_IF}" -o "${NETWORK_NAME}" -m state --state RELATED,ESTABLISHED -j ACCEPT
  iptables -A FORWARD -i "${NETWORK_NAME}" -o "${WAN_IF}" -j ACCEPT

  # Persist with a simple oneshot unit
  cat >"/etc/systemd/system/${NAT_UNIT}" <<UNIT
[Unit]
Description=Persist NAT rules for ${NETWORK_NAME}
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/sbin/iptables -t nat -C POSTROUTING -s ${SUBNET_NETWORK} -o ${WAN_IF} -j MASQUERADE || /usr/sbin/iptables -t nat -A POSTROUTING -s ${SUBNET_NETWORK} -o ${WAN_IF} -j MASQUERADE
ExecStart=/usr/sbin/iptables -C FORWARD -i ${WAN_IF} -o ${NETWORK_NAME} -m state --state RELATED,ESTABLISHED -j ACCEPT || /usr/sbin/iptables -A FORWARD -i ${WAN_IF} -o ${NETWORK_NAME} -m state --state RELATED,ESTABLISHED -j ACCEPT
ExecStart=/usr/sbin/iptables -C FORWARD -i ${NETWORK_NAME} -o ${WAN_IF} -j ACCEPT || /usr/sbin/iptables -A FORWARD -i ${NETWORK_NAME} -o ${WAN_IF} -j ACCEPT
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl enable --now "${NAT_UNIT}" >/dev/null 2>&1 || true
  return 0
}
firewall_configured=0
if apply_firewall_cmd; then
  firewall_configured=1
else
  warn 'firewalld not configured; attempting iptables fallback.'
  apply_iptables_rules || warn 'iptables NAT not configured; you must add forwarding manually.'
fi

# --- Dedicated dnsmasq service bound to this interface ONLY -------------------
UNIT_PATH="/etc/systemd/system/${DEDICATED_UNIT}"
info "Creating dedicated service ${DEDICATED_UNIT}"
cat >"${UNIT_PATH}" <<EOF
[Unit]
Description=dnsmasq for ${NETWORK_NAME}
Requires=sys-subsystem-net-devices-${NETWORK_NAME}.device
BindsTo=sys-subsystem-net-devices-${NETWORK_NAME}.device
After=sys-subsystem-net-devices-${NETWORK_NAME}.device network-online.target NetworkManager-wait-online.service

[Service]
Type=simple
# Wait until ${NETWORK_NAME} has an IPv4 address
ExecStartPre=/bin/bash -c 'for i in {1..60}; do ip -4 addr show ${NETWORK_NAME} | grep -q "inet " && exit 0; sleep 1; done; echo "${NETWORK_NAME} has no IPv4"; exit 1'
# Run ONLY with the per-network config; ignore /etc/dnsmasq.conf to avoid 10.42.* or other leftovers
ExecStart=/usr/sbin/dnsmasq -k --conf-file=${DNSMASQ_CONF} --bind-interfaces
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
EOF

# --- Enable/Start services ----------------------------------------------------
info "Enabling NetworkManager-wait-online for better ordering"
systemctl enable NetworkManager-wait-online.service >/dev/null 2>&1 || true

systemctl daemon-reload
info "Starting ${DEDICATED_UNIT}"
systemctl enable --now "${DEDICATED_UNIT}"

# --- Verification -------------------------------------------------------------
sleep 1
systemctl --no-pager --lines=50 status "${DEDICATED_UNIT}" || true
ss -lupn | egrep ':(53|67|68)\b' || true

info "Done. ${NETWORK_NAME} is serving DHCP ${DHCP_RANGE_START}-${DHCP_RANGE_END} via ${GATEWAY_IP} and NATing out ${WAN_IF}."
echo "Tip: test with 'tcpdump -ni ${NETWORK_NAME} port 67 or 68' while a client requests DHCP."
