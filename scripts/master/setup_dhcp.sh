#!/usr/bin/env bash
# Hardened and persistent DHCP + NAT for 1 LAN segment with macvtap.
# - Disables global dnsmasq and runs dedicated per-interface instance.
# - Removes and replaces old conflicting configs (services, drop-ins, old NAT).
# - Ensures ip_forward, relaxed rp_filter, iptables and persistence.

set -euo pipefail

truthy(){ local v="${1:-}"; case "${v,,}" in 1|true|yes|on) return 0;; esac; return 1; }
info(){ printf '[INFO] %s\n' "$*"; }
warn(){ printf '[WARN] %s\n' "$*" >&2; }
fatal(){ printf '[ERROR] %s\n' "$*" >&2; exit 1; }

usage(){
cat <<'USAGE'
Usage: sudo ./setup_dhcp.sh [WAN_IFACE]

  - Default LAN parent: 512rede; macvtap child: 512rede-host
  - WAN_IFACE: outbound interface for NAT (auto-detected if omitted)

Override via environment variables: LAN_PARENT_IF, LAN_INTERFACE_NAME, SUBNET_CIDR,
GATEWAY_IP, DHCP_RANGE_START, DHCP_RANGE_END, DHCP_LEASE_TIME, WAN_IF, etc.
USAGE
exit 1; }

[[ ${EUID:-0} -eq 0 ]] || fatal 'Requires root.'
[[ -r /etc/os-release ]] || fatal 'Missing /etc/os-release.'
. /etc/os-release
if [[ "${ID,,}" != "fedora" && ! ${ID_LIKE:-} =~ fedora ]]; then
  fatal 'This script is made for Fedora-like systems.'
fi

# --- SELinux: force permissive mode ------------------------------------------
if command -v selinuxenabled >/dev/null 2>&1 && selinuxenabled; then
  if command -v setenforce >/dev/null 2>&1; then
    if ! setenforce 0 2>/dev/null; then
      warn "Failed setenforce 0 (SELinux may block dnsmasq)."
    else
      info "SELinux set to permissive mode (runtime)."
    fi
  else
    warn "setenforce unavailable; could not change SELinux runtime mode."
  fi
else
  warn "SELinux is not active or selinuxenabled unavailable; continuing."
fi

if [[ -w /etc/selinux/config ]]; then
  if grep -q '^SELINUX=enforcing' /etc/selinux/config; then
    if sed -i 's/^SELINUX=.*/SELINUX=permissive/' /etc/selinux/config; then
      info "Updated /etc/selinux/config to SELINUX=permissive."
    else
      warn "Could not update /etc/selinux/config (check permissions)."
    fi
  fi
else
  warn "No permissions to edit /etc/selinux/config; persistent mode not changed."
fi


case "${1:-}" in -h|--help) usage;; esac
CLI_WAN_IF="${1:-}"

# --- Settings (can override via env) ------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MACVTAP_HELPER="${SCRIPT_DIR}/create_macvtap.sh"
[[ -x "${MACVTAP_HELPER}" ]] || fatal "Missing helper: ${MACVTAP_HELPER}"

LAN_PARENT_IF="${LAN_PARENT_IF:-512rede}"
LAN_INTERFACE_NAME="${LAN_INTERFACE_NAME:-${LAN_PARENT_IF}-host}"
NETWORK_NAME="$LAN_INTERFACE_NAME"

SUBNET_CIDR="${SUBNET_CIDR:-192.168.76.0/24}"
GATEWAY_IP="${GATEWAY_IP:-192.168.76.1}"
DHCP_RANGE_START="${DHCP_RANGE_START:-192.168.76.50}"
DHCP_RANGE_END="${DHCP_RANGE_END:-192.168.76.200}"
DHCP_LEASE_TIME="${DHCP_LEASE_TIME:-12h}"

RESOLV_CONF="${RESOLV_CONF:-/etc/resolv.conf}"
DNSMASQ_CONF_DIR="${DNSMASQ_CONF_DIR:-/etc/dnsmasq.d}"
DNSMASQ_LEASE_DIR="${DNSMASQ_LEASE_DIR:-/var/lib/dnsmasq}"

DNSMASQ_RUN_USER="${DNSMASQ_RUN_USER:-}"
DNSMASQ_RUN_GROUP="${DNSMASQ_RUN_GROUP:-}"

SYSCTL_CONF="/etc/sysctl.d/99-${NETWORK_NAME}-ipfwd-rpf.conf"
DEDICATED_UNIT="dnsmasq-${NETWORK_NAME}.service"
NAT_UNIT="${NETWORK_NAME}-nat.service"
MACVTAP_PERSIST="${MACVTAP_PERSIST:-1}"

command -v ip >/dev/null || fatal 'Missing iproute2.'
command -v dnsmasq >/dev/null || fatal 'Missing dnsmasq.'
command -v nmcli >/dev/null 2>&1 || warn 'nmcli missing (limited NM persistence).'

if [[ -z ${DNSMASQ_RUN_USER} ]]; then
  if getent passwd dnsmasq >/dev/null; then
    DNSMASQ_RUN_USER="dnsmasq"
  else
    DNSMASQ_RUN_USER="nobody"
  fi
fi
if [[ -z ${DNSMASQ_RUN_GROUP} ]]; then
  if getent group "${DNSMASQ_RUN_USER}" >/dev/null; then
    DNSMASQ_RUN_GROUP="${DNSMASQ_RUN_USER}"
  elif getent group dnsmasq >/dev/null; then
    DNSMASQ_RUN_GROUP="dnsmasq"
  elif getent group nogroup >/dev/null; then
    DNSMASQ_RUN_GROUP="nogroup"
  else
    DNSMASQ_RUN_GROUP="nobody"
  fi
fi

# --- CIDR helpers -------------------------------------------------------------
cidr_prefix=${SUBNET_CIDR#*/}; network_base=${SUBNET_CIDR%/*}
[[ $cidr_prefix =~ ^[0-9]+$ ]] || fatal "Invalid SUBNET_CIDR: ${SUBNET_CIDR}"
cidr_prefix=$((10#${cidr_prefix})); (( cidr_prefix>=0 && cidr_prefix<=32 )) || fatal "Invalid SUBNET_CIDR."

prefix_to_mask(){ local p=$1; ((p==0)) && { printf '0.0.0.0'; return; }; local m=$((0xffffffff^((1<<(32-p))-1))); printf '%d.%d.%d.%d' $(((m>>24)&255)) $(((m>>16)&255)) $(((m>>8)&255)) $((m&255)); }
ip_to_int(){ local IFS=.; read -r a b c d <<<"$1"; printf '%u' $(( (a<<24)|(b<<16)|(c<<8)|d )); }
int_to_ip(){ local v=$1; printf '%d.%d.%d.%d' $(((v>>24)&255)) $(((v>>16)&255)) $(((v>>8)&255)) $((v&255)); }
mask_int=$(( cidr_prefix==0 ? 0 : 0xffffffff ^ ((1<<(32-cidr_prefix))-1) ))
network_int=$(( $(ip_to_int "${network_base}") & mask_int ))
network_address=$(int_to_ip "${network_int}")
NETMASK=$(prefix_to_mask "${cidr_prefix}")
SUBNET_NETWORK="${network_address}/${cidr_prefix}"

# --- Create/Recreate macvtap (and remove duplicate IP on parent within this subnet)
ensure_macvtap(){
  local ip_cidr="${GATEWAY_IP}/${cidr_prefix}"
  local args=()
  truthy "${MACVTAP_PERSIST}" && args+=(--persist)

  info "(Re)creating macvtap ${LAN_INTERFACE_NAME} on ${LAN_PARENT_IF}"
  ip link show "${LAN_PARENT_IF}" >/dev/null 2>&1 || fatal "Parent '${LAN_PARENT_IF}' does not exist."

  # Remove ANY IPv4 from parent that belongs to our SUBNET (avoids duplication with child)
  while read -r addr; do
    [[ -z "${addr}" ]] && continue
    ip -4 addr del "${addr}" dev "${LAN_PARENT_IF}" || true
    warn "Removed IPv4 ${addr} from parent ${LAN_PARENT_IF} (belonged to ${SUBNET_NETWORK})"
  done < <(ip -4 -o addr show dev "${LAN_PARENT_IF}" | awk -v net="${SUBNET_NETWORK}" '
    {print $4}
    ' | while read -r a; do
          # simple filter by prefix matching subnet (e.g., 192.168.76.)
          base="${a%/*}"; echo "$base/${a#*/}"
        done | awk -v n="${network_base}" -v p="${cidr_prefix}" '
            BEGIN{
              split(n,b,"."); net=(b[1]*256*256*256)+(b[2]*256*256)+(b[3]*256)+b[4];
              mask=(p==0?0:(2^32-1) - (2^(32-p)-1));
            }
            {
              split($0,ipm,"/"); split(ipm[1],q,".");
              ip=(q[1]*256*256*256)+(q[2]*256*256)+(q[3]*256)+q[4];
              if ((and(ip,mask))==net) print ipm[1]"/"p;
            }')

  "${MACVTAP_HELPER}" "${args[@]}" "${LAN_PARENT_IF}" "${LAN_INTERFACE_NAME}" "${ip_cidr}"
}
ensure_macvtap

ip link show "${NETWORK_NAME}" >/dev/null 2>&1 || fatal "Interface '${NETWORK_NAME}' not found."

# --- Aggressive cleanup of old artifacts --------------------------------------
cleanup_for_network(){
  info "Cleaning up old artifacts for ${NETWORK_NAME}"

  install -d -m 755 "${DNSMASQ_CONF_DIR}"
  install -d -m 775 "${DNSMASQ_LEASE_DIR}"
  chown "${DNSMASQ_RUN_USER}:${DNSMASQ_RUN_GROUP}" "${DNSMASQ_LEASE_DIR}" || warn "Could not adjust owner of ${DNSMASQ_LEASE_DIR}"
  chmod 775 "${DNSMASQ_LEASE_DIR}" || warn "Could not adjust permissions of ${DNSMASQ_LEASE_DIR}"
  if command -v restorecon >/dev/null 2>&1; then
    restorecon -R "${DNSMASQ_LEASE_DIR}" >/dev/null 2>&1 || warn "restorecon failed for ${DNSMASQ_LEASE_DIR}"
  fi
  rm -f "${DNSMASQ_CONF_DIR}/${NETWORK_NAME}.conf"
  local lease_file="${DNSMASQ_LEASE_DIR}/${NETWORK_NAME}.leases"
  rm -f "${lease_file}"
  install -m 664 -o "${DNSMASQ_RUN_USER}" -g "${DNSMASQ_RUN_GROUP}" /dev/null "${lease_file}" 2>/dev/null || {
    touch "${lease_file}"
    chown "${DNSMASQ_RUN_USER}:${DNSMASQ_RUN_GROUP}" "${lease_file}" || warn "Could not adjust owner of ${lease_file}"
    chmod 664 "${lease_file}" || warn "Could not adjust permissions of ${lease_file}"
  }

  systemctl disable --now "${DEDICATED_UNIT}" >/dev/null 2>&1 || true

  rm -f "/etc/systemd/system/dnsmasq.service.d/${NETWORK_NAME}-wait.conf"
  rmdir --ignore-fail-on-non-empty "/etc/systemd/system/dnsmasq.service.d" 2>/dev/null || true

  if systemctl list-unit-files | grep -q '^dnsmasq\.service'; then
    systemctl disable --now dnsmasq.service >/dev/null 2>&1 || true
  fi

  pkill -f "dnsmasq.*${NETWORK_NAME}" >/dev/null 2>&1 || true

  systemctl disable --now "${NAT_UNIT}" >/dev/null 2>&1 || true
  rm -f "/etc/systemd/system/${NAT_UNIT}"

  # NM: delete child profiles; parent is kept (we already cleaned IPs above)
  if command -v nmcli >/dev/null 2>&1; then
    while read -r uuid name; do
      [[ -z ${uuid} ]] && continue
      info "NM: removing profile '${name}' from device ${NETWORK_NAME}"
      nmcli connection delete uuid "${uuid}" >/dev/null 2>&1 || true
    done < <(nmcli -t -f UUID,NAME,DEVICE connection show | awk -F: -v dev="${NETWORK_NAME}" '$3==dev{print $1" "$2}')

    while read -r uuid name; do
      [[ -z ${uuid} ]] && continue
      local current_method
      current_method=$(nmcli -g ipv4.method connection show "${uuid}" 2>/dev/null || echo "")
      if [[ "${current_method}" != "disabled" ]]; then
        info "NM: disabling IPv4 on parent (${LAN_PARENT_IF}) via profile '${name}'"
        nmcli connection modify "${uuid}" ipv4.method disabled ipv4.addresses "" ipv4.gateway "" ipv4.never-default yes >/dev/null 2>&1 || warn "NM: failed to set IPv4 disabled for '${name}'"
        nmcli connection modify "${uuid}" ipv6.method ignore >/dev/null 2>&1 || true
        nmcli connection down "${uuid}" >/dev/null 2>&1 || true
        nmcli connection up "${uuid}" >/dev/null 2>&1 || true
      fi
    done < <(nmcli -t -f UUID,NAME,DEVICE connection show | awk -F: -v dev="${LAN_PARENT_IF}" '$3==dev{print $1" "$2}')
  fi

  # Force child state and IPv4
  ip addr flush dev "${NETWORK_NAME}" || true
  ip link set "${NETWORK_NAME}" up
  ip addr add "${GATEWAY_IP}/${cidr_prefix}" dev "${NETWORK_NAME}" valid_lft forever preferred_lft forever

  # Parent in promisc for stable forwarding
  ip link set "${LAN_PARENT_IF}" promisc on || true
}
cleanup_for_network

stop_conflicting_dnsmasq_units(){
  command -v systemctl >/dev/null 2>&1 || return
  info "Checking for conflicting dnsmasq services"
  while read -r unit; do
    [[ -z ${unit} ]] && continue
    [[ "${unit}" == "${DEDICATED_UNIT}" ]] && continue
    info "Stopping unit '${unit}' that uses dnsmasq"
    systemctl stop "${unit}" >/dev/null 2>&1 || true
    systemctl disable "${unit}" >/dev/null 2>&1 || true
  done < <(systemctl list-units --all 'dnsmasq*.service' --plain --no-legend 2>/dev/null | awk '{print $1}' | sort -u)
}
stop_conflicting_dnsmasq_units

kill_conflicting_dns(){
  command -v ss >/dev/null 2>&1 || { warn "Missing 'ss' utility to detect port conflicts."; return; }
  local ports=(53 67)
  declare -A handled=()
  for port in "${ports[@]}"; do
    while IFS=' ' read -r pid exe addr proto; do
      [[ -z "${pid}" ]] && continue
      local key="${pid}-${proto}"
      [[ -n "${handled[${key}]:-}" ]] && continue
      handled["${key}"]=1
      case "${exe}" in
        dnsmasq)
          info "Terminating pre-existing dnsmasq (PID ${pid}) on address ${addr} (${proto})"
          kill "${pid}" >/dev/null 2>&1 || true
          sleep 0.5
          kill -9 "${pid}" >/dev/null 2>&1 || true
          ;;
        *)
          fatal "Port ${port}/${addr} occupied by PID ${pid} (${exe}). Free it before continuing."
          ;;
      esac
    done < <(ss -H -lnp "sport = :${port}" 2>/dev/null | awk -v ip="${GATEWAY_IP}" -v p="${port}" '
      {
        local_addr=$5
        proto=$1
        gsub(/^\[|\]$/, "", local_addr)
        if (match(local_addr, /:([0-9]+)$/, mport)) {
          portnum=mport[1]
          addr=substr(local_addr, 1, length(local_addr)-length(mport[0]))
        } else {
          next
        }
        if (portnum != p) next
        if (addr == "" || addr == "*" || addr == "0.0.0.0" || addr == "::" || addr == ip) {
          if (match($0, /pid=([0-9]+)/, m) && match($0, /"([^"]+)"/, c)) {
            printf "%s %s %s:%s %s\n", m[1], c[1], (addr==""?"*":addr), portnum, proto
          }
        }
      }')
  done
}
kill_conflicting_dns

# --- Dedicated dnsmasq (only reads this network's file) ----------------------
DNSMASQ_CONF="${DNSMASQ_CONF_DIR}/${NETWORK_NAME}.conf"
info "Writing ${DNSMASQ_CONF}"
cat >"${DNSMASQ_CONF}" <<CFG
# Auto-generated for ${NETWORK_NAME} — DO NOT EDIT MANUALLY
interface=${NETWORK_NAME}
listen-address=${GATEWAY_IP}
except-interface=lo
bind-interfaces
domain-needed
bogus-priv
# DHCP
dhcp-authoritative
dhcp-range=${DHCP_RANGE_START},${DHCP_RANGE_END},${NETMASK},${DHCP_LEASE_TIME}
dhcp-option=option:router,${GATEWAY_IP}
dhcp-option=option:dns-server,${GATEWAY_IP}
dhcp-leasefile=${DNSMASQ_LEASE_DIR}/${NETWORK_NAME}.leases
# DNS forwarders
resolv-file=${RESOLV_CONF}
log-dhcp
CFG

dnsmasq --test -C "${DNSMASQ_CONF}" >/dev/null || fatal "Configuration test failed: ${DNSMASQ_CONF}"

# --- WAN detection ------------------------------------------------------------
find_wan_iface(){ ip route show default 0.0.0.0/0 | awk '/default/ {print $5; exit}'; }
WAN_IF_INPUT="${CLI_WAN_IF:-${WAN_IF:-}}"
WAN_IF="${WAN_IF_INPUT:-$(find_wan_iface)}"
[[ -n ${WAN_IF} ]] || fatal 'Could not detect WAN interface.'
[[ "${WAN_IF}" != "${NETWORK_NAME}" ]] || fatal 'WAN cannot be the same as DHCP interface.'
ip link show "${WAN_IF}" >/dev/null 2>&1 || fatal "WAN '${WAN_IF}' does not exist."

# --- ip_forward + relaxed rp_filter ------------------------------------------
info "Enabling ip_forward and relaxing rp_filter"
cat >"${SYSCTL_CONF}" <<SYSCTL
net.ipv4.ip_forward = 1
net.ipv4.conf.all.forwarding = 1
net.ipv4.conf.all.rp_filter = 0
net.ipv4.conf.default.rp_filter = 0
net.ipv4.conf.${NETWORK_NAME}.rp_filter = 0
net.ipv4.conf.${LAN_PARENT_IF}.rp_filter = 0
SYSCTL
if [[ "${LAN_PARENT_IF}" != "${WAN_IF}" && "${WAN_IF}" != "${NETWORK_NAME}" ]]; then
  cat >>"${SYSCTL_CONF}" <<SYSCTL
net.ipv4.conf.${WAN_IF}.rp_filter = 0
SYSCTL
fi
sysctl --system >/dev/null

apply_iptables(){
  command -v iptables >/dev/null 2>&1 || { warn 'Missing iptables; NAT not configured.'; return 1; }

  # Ensure ip_forward at runtime (in addition to persistent sysctl)
  sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1 || true
  sysctl -w net.ipv4.conf.all.forwarding=1 >/dev/null 2>&1 || true

  info "Configuring NAT via iptables (WAN=${WAN_IF}, LAN=${SUBNET_NETWORK})"
  iptables -t nat -D POSTROUTING -s "${SUBNET_NETWORK}" -o "${WAN_IF}" -j MASQUERADE 2>/dev/null || true
  iptables -D FORWARD -i "${WAN_IF}" -o "${NETWORK_NAME}" -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true
  iptables -D FORWARD -i "${NETWORK_NAME}" -o "${WAN_IF}" -j ACCEPT 2>/dev/null || true

  # Insert at the top to avoid existing DROP rules
  iptables -t nat -I POSTROUTING 1 -s "${SUBNET_NETWORK}" -o "${WAN_IF}" -j MASQUERADE
  iptables -I FORWARD 1 -i "${WAN_IF}" -o "${NETWORK_NAME}" -m state --state RELATED,ESTABLISHED -j ACCEPT
  iptables -I FORWARD 1 -i "${NETWORK_NAME}" -o "${WAN_IF}" -j ACCEPT

  cat >"/etc/systemd/system/${NAT_UNIT}" <<UNIT
[Unit]
Description=Persist NAT rules for ${NETWORK_NAME}
After=network-online.target NetworkManager-wait-online.service sys-subsystem-net-devices-${WAN_IF}.device
Wants=network-online.target NetworkManager-wait-online.service sys-subsystem-net-devices-${WAN_IF}.device
BindsTo=sys-subsystem-net-devices-${WAN_IF}.device

[Service]
Type=oneshot
ExecStartPre=/bin/bash -c 'for i in {1..30}; do ip link show ${WAN_IF} >/dev/null 2>&1 && ip link show ${NETWORK_NAME} >/dev/null 2>&1 && exit 0; sleep 1; done; echo "Interfaces ${WAN_IF}/${NETWORK_NAME} unavailable"; exit 1'
ExecStartPre=/bin/bash -c 'for i in {1..30}; do ip route show default 0.0.0.0/0 | grep -q "dev ${WAN_IF}" && exit 0; sleep 1; done; echo "Default route via ${WAN_IF} not present"; exit 1'
ExecStart=/bin/bash -c '/usr/sbin/iptables -t nat -C POSTROUTING -s ${SUBNET_NETWORK} -o ${WAN_IF} -j MASQUERADE || /usr/sbin/iptables -t nat -I POSTROUTING 1 -s ${SUBNET_NETWORK} -o ${WAN_IF} -j MASQUERADE'
ExecStart=/bin/bash -c '/usr/sbin/iptables -C FORWARD -i ${WAN_IF} -o ${NETWORK_NAME} -m state --state RELATED,ESTABLISHED -j ACCEPT || /usr/sbin/iptables -I FORWARD 1 -i ${WAN_IF} -o ${NETWORK_NAME} -m state --state RELATED,ESTABLISHED -j ACCEPT'
ExecStart=/bin/bash -c '/usr/sbin/iptables -C FORWARD -i ${NETWORK_NAME} -o ${WAN_IF} -j ACCEPT || /usr/sbin/iptables -I FORWARD 1 -i ${NETWORK_NAME} -o ${WAN_IF} -j ACCEPT'
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
UNIT

  systemctl daemon-reload
  systemctl enable --now "${NAT_UNIT}" >/dev/null 2>&1 || true
}
apply_iptables || warn 'NAT did not become persistent — check manually.'

# --- Dedicated dnsmasq service ------------------------------------------------
UNIT_PATH="/etc/systemd/system/${DEDICATED_UNIT}"
info "Creating dedicated service ${DEDICATED_UNIT}"
cat >"${UNIT_PATH}" <<EOF
[Unit]
Description=dnsmasq for ${NETWORK_NAME}
Wants=network-online.target NetworkManager-wait-online.service
After=macvtap-${NETWORK_NAME}.service network-online.target NetworkManager-wait-online.service

[Service]
Type=simple
# Wait until interface has IPv4
ExecStartPre=/bin/bash -c 'for i in {1..20}; do ip -4 addr show ${NETWORK_NAME} | grep -q "inet " && exit 0; sleep 1; done; echo "${NETWORK_NAME} without IPv4"; exit 1'
ExecStart=/usr/sbin/dnsmasq -k --conf-file=${DNSMASQ_CONF} --bind-interfaces --user=${DNSMASQ_RUN_USER} --group=${DNSMASQ_RUN_GROUP}
Restart=on-failure
RestartSec=2
# Harden a bit
AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_ADMIN CAP_NET_RAW
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
EOF

info "Enabling NetworkManager-wait-online"
systemctl enable NetworkManager-wait-online.service >/dev/null 2>&1 || true

systemctl daemon-reload
systemctl reset-failed "${DEDICATED_UNIT}" >/dev/null 2>&1 || true
systemctl enable "${DEDICATED_UNIT}" >/dev/null
systemctl start "${DEDICATED_UNIT}" --no-block >/dev/null

info "Waiting up to 20s for 'active' state of ${DEDICATED_UNIT}"
for i in {1..20}; do
  systemctl is-active --quiet "${DEDICATED_UNIT}" && break
  sleep 1
done
systemctl is-active --quiet "${DEDICATED_UNIT}" || { systemctl --no-pager --lines=80 status "${DEDICATED_UNIT}" || true; fatal "${DEDICATED_UNIT} did not start."; }

# --- Verification -------------------------------------------------------------
systemctl --no-pager --lines=20 status "${DEDICATED_UNIT}" || true
ss -lupn | egrep ':(53|67|68)\b' || true

info "Ready: ${NETWORK_NAME} serving DHCP ${DHCP_RANGE_START}-${DHCP_RANGE_END} via ${GATEWAY_IP} (leases ${DHCP_LEASE_TIME}) and NAT exiting through ${WAN_IF}."
echo "Tip: tcpdump -ni ${NETWORK_NAME} 'port 67 or 68' during a DHCP request."
