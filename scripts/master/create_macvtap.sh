#!/usr/bin/env bash
set -euo pipefail

# Creates (and persists) a macvtap in bridge mode anchored to a parent NIC.
# Usage:
#   sudo ./create_macvtap.sh [--persist] <parent_iface> <macvtap_iface> [ipv4_cidr]
# Example:
#   sudo ./create_macvtap.sh --persist enp3s0 512rede-host 192.168.76.1/24
#
# With --persist, installs a systemd service that (re)creates the interface on boot.

usage() {
  cat <<'USAGE'
Usage: create_macvtap.sh [--persist] <parent_iface> <macvtap_iface> [ipv4_cidr]

  --persist      Install systemd service to recreate interface on boot
  parent_iface   Existing physical NIC (e.g., enp3s0) or your '512rede'
  macvtap_iface  Name of macvtap to create (e.g., 512rede-host)
  ipv4_cidr      Optional IPv4/prefix to assign (e.g., 192.168.76.1/24)

Notes:
  - Requires root and iproute2.
  - Uses macvtap in bridge mode (for VMs <interface type="direct" mode="bridge">).
  - Sets parent to promisc for reliable forwarding.
USAGE
  exit 1
}

fatal(){ printf '[ERROR] %s\n' "$*" >&2; exit 1; }
warn(){  printf '[WARN] %s\n' "$*" >&2; }
info(){  printf '[INFO] %s\n' "$*"; }

ip_to_int(){ local IFS=.; read -r a b c d <<<"$1"; printf '%u' $(( (a<<24)|(b<<16)|(c<<8)|d )); }

remove_conflicting_ipv4(){
  local parent=$1 ip_cidr=$2
  [[ -z "${ip_cidr}" ]] && return
  local cidr_prefix=${ip_cidr#*/}
  local base_ip=${ip_cidr%/*}
  [[ ${cidr_prefix} =~ ^[0-9]+$ ]] || return
  local prefix=$((10#${cidr_prefix}))
  (( prefix>=0 && prefix<=32 )) || return
  local mask=$(( prefix==0 ? 0 : 0xffffffff ^ ((1<<(32-prefix))-1) ))
  local network=$(( $(ip_to_int "${base_ip}") & mask ))

  local addr
  local -a addrs=()
  mapfile -t addrs < <(ip -4 -o addr show dev "${parent}" 2>/dev/null | awk '{print $4}' || true)
  (( ${#addrs[@]} == 0 )) && return
  for addr in "${addrs[@]}"; do
    [[ -z "${addr:-}" ]] && continue
    local candidate_ip=${addr%/*}
    local candidate_int=$(ip_to_int "${candidate_ip}")
    if (( (candidate_int & mask) == network )); then
      ip -4 addr del "${addr}" dev "${parent}" || true
      warn "Removed IPv4 ${addr} from ${parent} due to conflict with ${ip_cidr}"
    fi
  done
}

install_persistence() {
  local parent=$1 child=$2 ipv4=${3:-}
  local helper="/usr/local/sbin/macvtap-${child}.sh"
  local unit="/etc/systemd/system/macvtap-${child}.service"
  local devunit="sys-subsystem-net-devices-${parent}.device"

  info "Cleaning up old persistence artifacts for ${child}"
  systemctl disable --now "macvtap-${child}.service" >/dev/null 2>&1 || true
  rm -f "${helper}" "${unit}"

  install -d -m 755 "$(dirname "${helper}")" "$(dirname "${unit}")"

  info "Installing helper ${helper}"
  cat >"${helper}" <<SCRIPT
#!/usr/bin/env bash
set -euo pipefail

ensure_ipv4_forwarding(){
  local conf="/etc/sysctl.d/99-macvtap-ipforward.conf"
  cat >"\${conf}" <<'CONF'
net.ipv4.ip_forward = 1
net.ipv4.conf.all.forwarding = 1
CONF
  sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1 || true
  sysctl -w net.ipv4.conf.all.forwarding=1 >/dev/null 2>&1 || true
  sysctl -p "\${conf}" >/dev/null 2>&1 || sysctl --system >/dev/null 2>&1 || true
}

ensure_ipv4_forwarding

modprobe macvtap >/dev/null 2>&1 || true

# Remove if already exists
ip link show ${child} >/dev/null 2>&1 && { ip link set ${child} down || true; ip link delete ${child} || true; }

# Wait for parent to exist and be up
for i in {1..20}; do
  ip link show ${parent} >/dev/null 2>&1 && break
  sleep 1
done
ip link show ${parent} >/dev/null 2>&1 || exit 1

# Remove conflicting IPv4 on parent
remove_conflicting_ipv4(){
  local parent=\$1 ip_cidr=\$2
  [[ -z "\${ip_cidr}" ]] && return
  IFS=/ read -r base prefix <<<"\${ip_cidr}"
  [[ \${prefix} =~ ^[0-9]+$ ]] || return
  prefix=\$((10#\${prefix}))
  (( prefix>=0 && prefix<=32 )) || return
  ip_to_int(){ local IFS=.; read -r a b c d <<<"\$1"; printf '%u' \$(( (a<<24)|(b<<16)|(c<<8)|d )); }
  local mask=\$(( prefix==0 ? 0 : 0xffffffff ^ ((1<<(32-prefix))-1) ))
  local network=\$(( \$(ip_to_int "\${base}") & mask ))
  local -a addrs=()
  mapfile -t addrs < <(ip -4 -o addr show "\${parent}" 2>/dev/null | awk '{print \$4}' || true)
  (( \${#addrs[@]} == 0 )) && return
  for addr in "\${addrs[@]}"; do
    [[ -z "\${addr:-}" ]] && continue
    local cand_ip=\${addr%/*}
    local cand_int=\$(ip_to_int "\${cand_ip}")
    if (( (cand_int & mask) == network )); then
      ip -4 addr del "\${addr}" dev "\${parent}" || true
    fi
  done
}
remove_conflicting_ipv4 ${parent} "${ipv4}"

# Ensure promisc on parent
ip link set ${parent} promisc on || true

# Create macvtap bridge and bring up
ip link add link ${parent} name ${child} type macvtap mode bridge
ip link set ${child} up

# Assign IPv4 if provided
if [[ -n "${ipv4}" ]]; then
  ip addr flush dev ${child} || true
  ip addr add ${ipv4} dev ${child}
fi

# Disable rp_filter on child (avoids silent drops)
sysctl -w net.ipv4.conf.${child}.rp_filter=0 >/dev/null 2>&1 || true
SCRIPT
  chmod 0755 "${helper}"

  info "Creating systemd service ${unit}"
  cat >"${unit}" <<UNIT
[Unit]
Description=macvtap ${child} on ${parent}
After=network-online.target NetworkManager-wait-online.service ${devunit}
Wants=network-online.target NetworkManager-wait-online.service
BindsTo=${devunit}

[Service]
Type=oneshot
ExecStart=${helper}
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
UNIT

  systemctl daemon-reload
  systemctl enable --now "macvtap-${child}.service"
  info "Persistence active for ${child}"
}

ensure_ipv4_forwarding(){
  local conf="/etc/sysctl.d/99-macvtap-ipforward.conf"
  info "Ensuring ip_forward active and persistent (${conf})"
  cat >"${conf}" <<'CONF'
net.ipv4.ip_forward = 1
net.ipv4.conf.all.forwarding = 1
CONF
  sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1 || warn "Failed to set net.ipv4.ip_forward"
  sysctl -w net.ipv4.conf.all.forwarding=1 >/dev/null 2>&1 || warn "Failed to set net.ipv4.conf.all.forwarding"
  sysctl -p "${conf}" >/dev/null 2>&1 || sysctl --system >/dev/null 2>&1 || warn "Could not reload sysctl, check manually"
}

[[ ${EUID:-0} -eq 0 ]] || fatal 'This script requires root.'
command -v ip >/dev/null 2>&1 || fatal 'Missing ip command (iproute2).'

PERSIST=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --persist) PERSIST=1; shift;;
    -h|--help) usage;;
    --) shift; break;;
    -*) fatal "Unknown option: $1";;
    *) break;;
  esac
done

[[ $# -lt 2 || $# -gt 3 ]] && usage

PARENT_IF=$1
MACVTAP_IF=$2
IPV4_CIDR=${3:-}

ip link show "$PARENT_IF" >/dev/null 2>&1 || fatal "Parent '$PARENT_IF' does not exist."

modprobe macvtap >/dev/null 2>&1 || true

ensure_ipv4_forwarding

# Clean up if already exists
if ip link show "$MACVTAP_IF" >/dev/null 2>&1; then
  info "Removing existing interface '${MACVTAP_IF}'"
  ip link set "$MACVTAP_IF" down 2>/dev/null || true
  ip link delete "$MACVTAP_IF" 2>/dev/null || true
fi

info "Parent ${PARENT_IF} -> promisc on"
ip link set "$PARENT_IF" promisc on || true

remove_conflicting_ipv4 "$PARENT_IF" "$IPV4_CIDR"

info "Creating macvtap '${MACVTAP_IF}' (mode=bridge)"
ip link add link "$PARENT_IF" name "$MACVTAP_IF" type macvtap mode bridge
trap 'ip link delete "$MACVTAP_IF" 2>/dev/null || true' ERR
ip link set "$MACVTAP_IF" up

if [[ -n $IPV4_CIDR ]]; then
  info "Assigning IPv4 ${IPV4_CIDR} to ${MACVTAP_IF}"
  ip addr flush dev "$MACVTAP_IF" || true
  ip addr add "$IPV4_CIDR" dev "$MACVTAP_IF"
fi

# Relaxed rp_filter (avoids drops on return path)
sysctl -w "net.ipv4.conf.${MACVTAP_IF}.rp_filter=0" >/dev/null 2>&1 || true

trap - ERR

if (( PERSIST )); then
  install_persistence "$PARENT_IF" "$MACVTAP_IF" "$IPV4_CIDR"
fi

info "macvtap '${MACVTAP_IF}' ready."
[[ -n $IPV4_CIDR ]] && info "Address ${IPV4_CIDR} active on host."
