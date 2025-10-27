#!/usr/bin/env bash
# Linux bridge setup for KVM/libvirt VMs on the same LAN as host (Fedora)
# - Creates a Linux bridge (br-lan) and enslaves the physical NIC
# - Moves the host IP to the bridge for transparent L2 connectivity
# - Configures dnsmasq to serve DHCP on the bridge
# - VMs attach to the bridge and get IPs from the same DHCP pool
# - NO NAT, NO macvlan/macvtap - simple L2 bridging

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: setup_dhcp.sh [--rollback]

  --rollback : Remove bridge and restore IP to physical interface

Environment variables override defaults (PHYS_IF, SUBNET_CIDR, GATEWAY_IP, ...).

This script creates a Linux bridge (br-lan) that allows KVM VMs to be on the
same LAN as the host and other nodes without NAT or macvlan.
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

# --- CLI parsing -------------------------------------------------------------
ROLLBACK=0
case "${1:-}" in
  -h|--help) usage ;;
  --rollback) ROLLBACK=1 ;;
esac

# --- Tunables (override via env) ---------------------------------------------
PHYS_IF="${PHYS_IF:-512rede}"                      # Physical NIC
BRIDGE_NAME="${BRIDGE_NAME:-br-lan}"               # Linux bridge name
SUBNET_CIDR="${SUBNET_CIDR:-192.168.76.0/24}"     # LAN subnet
GATEWAY_IP="${GATEWAY_IP:-192.168.76.1}"          # Host IP (will move to bridge)
DHCP_RANGE_START="${DHCP_RANGE_START:-192.168.76.50}"
DHCP_RANGE_END="${DHCP_RANGE_END:-192.168.76.200}"
RESOLV_CONF="${RESOLV_CONF:-/etc/resolv.conf}"
DNSMASQ_CONF_DIR="${DNSMASQ_CONF_DIR:-/etc/dnsmasq.d}"
DNSMASQ_LEASE_DIR="${DNSMASQ_LEASE_DIR:-/var/lib/dnsmasq}"
BRIDGE_CONN_NAME="bridge-${BRIDGE_NAME}"
SLAVE_CONN_NAME="bridge-slave-${PHYS_IF}"
DEDICATED_UNIT="dnsmasq-${BRIDGE_NAME}.service"

# --- Pre-reqs -----------------------------------------------------------------
command -v ip        >/dev/null 2>&1 || fatal 'iproute2 tools are required (missing `ip`).'
command -v dnsmasq   >/dev/null 2>&1 || fatal 'dnsmasq must be installed before running this script.'
command -v nmcli     >/dev/null 2>&1 || fatal 'nmcli (NetworkManager) is required for bridge management.'
command -v virsh     >/dev/null 2>&1 || warn 'virsh not found: libvirt integration will not be verified.'

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

# --- Verify physical interface exists -----------------------------------------
ip link show "${PHYS_IF}" >/dev/null 2>&1 || fatal "Physical interface '${PHYS_IF}' not found."

# =============================================================================
# ROLLBACK: Remove bridge and restore IP to physical interface
# =============================================================================
if [[ ${ROLLBACK} -eq 1 ]]; then
  info "═══ ROLLBACK MODE: Removing bridge ${BRIDGE_NAME} and restoring ${PHYS_IF} ═══"
  
  # Stop dnsmasq service
  systemctl disable --now "${DEDICATED_UNIT}" >/dev/null 2>&1 || true
  rm -f "/etc/systemd/system/${DEDICATED_UNIT}"
  systemctl daemon-reload
  
  # Remove dnsmasq config
  rm -f "${DNSMASQ_CONF_DIR}/${BRIDGE_NAME}.conf"
  rm -f "${DNSMASQ_LEASE_DIR}/${BRIDGE_NAME}.leases"
  
  # Delete NetworkManager connections
  nmcli connection delete "${BRIDGE_CONN_NAME}" >/dev/null 2>&1 || true
  nmcli connection delete "${SLAVE_CONN_NAME}" >/dev/null 2>&1 || true
  
  # Restore physical interface with IP
  info "Restoring ${PHYS_IF} with IP ${GATEWAY_IP}/${cidr_prefix}"
  nmcli connection add type ethernet ifname "${PHYS_IF}" con-name "${PHYS_IF}-direct" \
    ipv4.addresses "${GATEWAY_IP}/${cidr_prefix}" ipv4.method manual \
    ipv6.method ignore autoconnect yes >/dev/null
  nmcli connection up "${PHYS_IF}-direct" >/dev/null
  
  info "✓ Rollback complete. ${PHYS_IF} now has IP ${GATEWAY_IP}"
  info "Verification:"
  ip addr show "${PHYS_IF}" | grep inet
  exit 0
fi

# =============================================================================
# MAIN SETUP: Create bridge, enslave physical NIC, move IP, configure dnsmasq
# =============================================================================

# =============================================================================
# MAIN SETUP: Create bridge, enslave physical NIC, move IP, configure dnsmasq
# =============================================================================

# --- Cleanup: remove old artifacts -------------------------------------------
cleanup_old_setup() {
  info "Cleaning old configurations..."
  
  # Stop old dnsmasq services
  systemctl disable --now dnsmasq.service >/dev/null 2>&1 || true
  systemctl disable --now "dnsmasq-${PHYS_IF}.service" >/dev/null 2>&1 || true
  rm -f "/etc/systemd/system/dnsmasq-${PHYS_IF}.service"
  
  # Remove old NAT-related units
  systemctl disable --now "${PHYS_IF}-nat.service" >/dev/null 2>&1 || true
  rm -f "/etc/systemd/system/${PHYS_IF}-nat.service"
  
  # Clean old dnsmasq configs
  rm -f "${DNSMASQ_CONF_DIR}/${PHYS_IF}.conf"
  
  # Ensure directories exist
  install -d -m 755 "${DNSMASQ_CONF_DIR}" "${DNSMASQ_LEASE_DIR}"
  
  systemctl daemon-reload
}
cleanup_old_setup

# --- Step 1: Create Linux bridge with NetworkManager ------------------------
info "═══ Step 1: Creating Linux bridge ${BRIDGE_NAME} ═══"

# Delete existing bridge and slave connections if they exist
nmcli connection delete "${BRIDGE_CONN_NAME}" >/dev/null 2>&1 || true
nmcli connection delete "${SLAVE_CONN_NAME}" >/dev/null 2>&1 || true

# Delete any connection currently bound to PHYS_IF
while read -r uuid name; do
  [[ -z ${uuid} ]] && continue
  info "Removing old connection '${name}' from ${PHYS_IF}"
  nmcli connection delete uuid "${uuid}" >/dev/null 2>&1 || true
done < <(nmcli -t -f UUID,NAME,DEVICE connection show | awk -F: -v dev="${PHYS_IF}" '$3==dev{print $1" "$2}')

info "Creating bridge connection ${BRIDGE_CONN_NAME}..."
nmcli connection add type bridge ifname "${BRIDGE_NAME}" con-name "${BRIDGE_CONN_NAME}" \
  bridge.stp no \
  ipv4.addresses "${GATEWAY_IP}/${cidr_prefix}" \
  ipv4.method manual \
  ipv4.never-default no \
  ipv6.method ignore \
  autoconnect yes >/dev/null

info "Adding ${PHYS_IF} as bridge slave (IP will move to bridge)..."
nmcli connection add type ethernet ifname "${PHYS_IF}" con-name "${SLAVE_CONN_NAME}" \
  master "${BRIDGE_NAME}" \
  slave-type bridge \
  autoconnect yes >/dev/null

info "⚠️  DOWNTIME: Activating bridge (network will briefly disconnect)..."
nmcli connection up "${BRIDGE_CONN_NAME}" >/dev/null
sleep 2
nmcli connection up "${SLAVE_CONN_NAME}" >/dev/null
sleep 1

info "✓ Bridge ${BRIDGE_NAME} created with IP ${GATEWAY_IP}"
info "✓ ${PHYS_IF} enslaved to ${BRIDGE_NAME} (no IP on slave)"

info "✓ Bridge ${BRIDGE_NAME} created with IP ${GATEWAY_IP}"
info "✓ ${PHYS_IF} enslaved to ${BRIDGE_NAME} (no IP on slave)"

# --- Step 2: Configure dnsmasq to listen on bridge ---------------------------
info "═══ Step 2: Configuring dnsmasq on ${BRIDGE_NAME} ═══"

DNSMASQ_CONF="${DNSMASQ_CONF_DIR}/${BRIDGE_NAME}.conf"
cat >"${DNSMASQ_CONF}" <<CFG
# Auto-generated for bridge ${BRIDGE_NAME} - KVM VMs on same LAN
# Listens ONLY on ${BRIDGE_NAME}, isolated from other networks

interface=${BRIDGE_NAME}
bind-interfaces
domain-needed
bogus-priv

# DHCP configuration
dhcp-authoritative
dhcp-range=${DHCP_RANGE_START},${DHCP_RANGE_END},${NETMASK},infinite
dhcp-option=option:router,${GATEWAY_IP}
dhcp-option=option:dns-server,${GATEWAY_IP}
dhcp-leasefile=${DNSMASQ_LEASE_DIR}/${BRIDGE_NAME}.leases

# DNS forwarding
resolv-file=${RESOLV_CONF}
log-dhcp
log-queries

# Don't read /etc/hosts or /etc/dnsmasq.conf to avoid conflicts
no-hosts
CFG

# Test config
dnsmasq --test -C "${DNSMASQ_CONF}" >/dev/null || fatal "dnsmasq config test failed"

info "✓ dnsmasq config written to ${DNSMASQ_CONF}"

info "✓ dnsmasq config written to ${DNSMASQ_CONF}"

# --- Step 3: Create dedicated dnsmasq systemd service -------------------------
info "═══ Step 3: Creating systemd service ${DEDICATED_UNIT} ═══"

UNIT_PATH="/etc/systemd/system/${DEDICATED_UNIT}"
cat >"${UNIT_PATH}" <<EOF
[Unit]
Description=dnsmasq DHCP/DNS for bridge ${BRIDGE_NAME} (KVM VMs)
Documentation=man:dnsmasq(8)
Requires=sys-subsystem-net-devices-${BRIDGE_NAME}.device
BindsTo=sys-subsystem-net-devices-${BRIDGE_NAME}.device
After=sys-subsystem-net-devices-${BRIDGE_NAME}.device network-online.target NetworkManager-wait-online.service
Before=libvirtd.service

[Service]
Type=simple
# Wait for bridge to have an IP
ExecStartPre=/bin/bash -c 'for i in {1..60}; do ip -4 addr show ${BRIDGE_NAME} | grep -q "inet " && exit 0; sleep 1; done; echo "${BRIDGE_NAME} has no IPv4"; exit 1'
# Run with isolated config (ignore /etc/dnsmasq.conf to avoid conflicts)
ExecStart=/usr/sbin/dnsmasq -k --conf-file=${DNSMASQ_CONF} --bind-interfaces --except-interface=lo
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now "${DEDICATED_UNIT}"
sleep 2

info "✓ ${DEDICATED_UNIT} started"

info "✓ ${DEDICATED_UNIT} started"

# --- Step 4: Firewall configuration (allow DHCP/DNS, no NAT) -----------------
info "═══ Step 4: Configuring firewall (no NAT - bridged mode) ═══"

if command -v firewall-cmd >/dev/null 2>&1; then
  # Add bridge to trusted zone (VMs are on same LAN)
  firewall-cmd --permanent --zone=trusted --add-interface="${BRIDGE_NAME}" >/dev/null 2>&1 || true
  firewall-cmd --permanent --zone=trusted --add-service=dhcp >/dev/null 2>&1 || true
  firewall-cmd --permanent --zone=trusted --add-service=dns >/dev/null 2>&1 || true
  firewall-cmd --reload >/dev/null
  info "✓ firewalld: ${BRIDGE_NAME} in trusted zone (no NAT needed)"
else
  warn "firewalld not available. Ensure DHCP (67/68) and DNS (53) are allowed."
fi

# --- Step 5: libvirt network verification -------------------------------------
info "═══ Step 5: Verifying libvirt default network isolation ═══"

if command -v virsh >/dev/null 2>&1; then
  # Ensure libvirt's default NAT network (virbr0) doesn't conflict
  if virsh net-info default >/dev/null 2>&1; then
    local_default_net=$(virsh net-dumpxml default 2>/dev/null | grep -oP '(?<=<ip address=")[^"]+' || echo "")
    if [[ ${local_default_net} == "192.168.76."* ]]; then
      warn "libvirt default network uses ${local_default_net} - conflicts with your LAN!"
      warn "Consider: virsh net-destroy default && virsh net-undefine default"
    else
      info "✓ libvirt default network (${local_default_net:-virbr0}) is isolated"
    fi
  fi
fi

# --- Verification and instructions --------------------------------------------
info ""
info "═══════════════════════════════════════════════════════════════════════════"
info "✓ Bridge setup complete! ${BRIDGE_NAME} is serving DHCP ${DHCP_RANGE_START}-${DHCP_RANGE_END}"
info "═══════════════════════════════════════════════════════════════════════════"
info ""
info "VERIFICATION COMMANDS:"
info "  1. Check bridge status:"
info "     ip -d link show ${BRIDGE_NAME}"
info "     bridge link"
info ""
info "  2. Verify IP on bridge:"
info "     ip addr show ${BRIDGE_NAME}"
info ""
info "  3. Check dnsmasq service:"
info "     systemctl status ${DEDICATED_UNIT}"
info "     ss -ulpn | grep ':53\\|:67'"
info ""
info "  4. Monitor DHCP leases:"
info "     tail -f ${DNSMASQ_LEASE_DIR}/${BRIDGE_NAME}.leases"
info ""
info "  5. Test DHCP from another machine:"
info "     sudo dhclient -v ${PHYS_IF}"
info ""
info "  6. Ping test from VM:"
info "     ping ${GATEWAY_IP}"
info ""
info "══════════════════════════════════════════════════════════════════════════"
info "LIBVIRT VM CONFIGURATION:"
info "══════════════════════════════════════════════════════════════════════════"
info ""
info "Option A - Edit existing VM:"
info "  virsh edit <vm-name>"
info "  Replace network interface section with:"
info "    <interface type='bridge'>"
info "      <source bridge='${BRIDGE_NAME}'/>"
info "      <model type='virtio'/>"
info "    </interface>"
info ""
info "Option B - Create new VM with bridge:"
info "  virt-install \\"
info "    --name my-vm \\"
info "    --memory 2048 \\"
info "    --vcpus 2 \\"
info "    --disk size=20 \\"
info "    --network bridge=${BRIDGE_NAME},model=virtio \\"
info "    --cdrom /path/to/installer.iso"
info ""
info "Option C - Attach existing VM to bridge:"
info "  virsh attach-interface <vm-name> bridge ${BRIDGE_NAME} --model virtio --config"
info "  virsh reboot <vm-name>"
info ""
info "══════════════════════════════════════════════════════════════════════════"
info "ROLLBACK (if needed):"
info "══════════════════════════════════════════════════════════════════════════"
info "  sudo $0 --rollback"
info ""
info "This will:"
info "  - Remove bridge ${BRIDGE_NAME}"
info "  - Stop dnsmasq service"
info "  - Restore IP ${GATEWAY_IP} directly to ${PHYS_IF}"
info ""
info "═══════════════════════════════════════════════════════════════════════════"

# Final status check
sleep 1
systemctl --no-pager --lines=20 status "${DEDICATED_UNIT}" || true
echo ""
ip -br addr show "${BRIDGE_NAME}" || true
bridge link show | grep "${PHYS_IF}" || true
