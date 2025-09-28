#!/usr/bin/env bash
# Configure a local DHCP server on Fedora for the 512rede network with NAT forwarding and infinite lease time.
set -euo pipefail

info() { printf '[INFO] %s\n' "$*"; }
warn() { printf '[WARN] %s\n' "$*" >&2; }
fatal() { printf '[ERROR] %s\n' "$*" >&2; exit 1; }

if [[ ${EUID:-0} -ne 0 ]]; then
    fatal 'This script must run as root.'
fi

if [[ -r /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    if [[ "${ID,,}" != "fedora" && ! ${ID_LIKE:-} =~ fedora ]]; then
        fatal 'This script currently targets Fedora-based hosts only.'
    fi
else
    fatal 'Unable to determine distribution (missing /etc/os-release).'
fi

NETWORK_NAME="${NETWORK_NAME:-512rede}"
SUBNET_CIDR="${SUBNET_CIDR:-192.168.76.0/24}"
GATEWAY_IP="${GATEWAY_IP:-192.168.76.1}"
DHCP_RANGE_START="${DHCP_RANGE_START:-192.168.76.50}"
DHCP_RANGE_END="${DHCP_RANGE_END:-192.168.76.200}"
DNSMASQ_CONF_DIR="${DNSMASQ_CONF_DIR:-/etc/dnsmasq.d}"
DNSMASQ_LEASE_DIR="${DNSMASQ_LEASE_DIR:-/var/lib/dnsmasq}"
SYSCTL_CONF="/etc/sysctl.d/99-${NETWORK_NAME}-ipforward.conf"

if ! command -v ip &>/dev/null; then
    fatal 'iproute2 tools are required (missing `ip`).'
fi
if ! command -v dnsmasq &>/dev/null; then
    fatal 'dnsmasq must be installed before running this script.'
fi

cidr_prefix=${SUBNET_CIDR#*/}
network_base=${SUBNET_CIDR%/*}
if [[ -z ${cidr_prefix} || ! ${cidr_prefix} =~ ^[0-9]+$ ]]; then
    fatal "Invalid SUBNET_CIDR '${SUBNET_CIDR}'"
fi
cidr_prefix=$((10#${cidr_prefix}))
if (( cidr_prefix < 0 || cidr_prefix > 32 )); then
    fatal "Invalid SUBNET_CIDR '${SUBNET_CIDR}'"
fi

prefix_to_mask() {
    local prefix=$1
    if (( prefix == 0 )); then
        printf '0.0.0.0'
        return
    fi
    local mask=$(( 0xffffffff ^ ((1 << (32 - prefix)) - 1) ))
    printf '%d.%d.%d.%d' \
        $(( (mask >> 24) & 255 )) \
        $(( (mask >> 16) & 255 )) \
        $(( (mask >> 8) & 255 )) \
        $(( mask & 255 ))
}

ip_to_int() {
    local IFS=.
    local a b c d
    read -r a b c d <<<"$1"
    printf '%u' $(( (a << 24) | (b << 16) | (c << 8) | d ))
}

int_to_ip() {
    local ip=$1
    printf '%d.%d.%d.%d' \
        $(( (ip >> 24) & 255 )) \
        $(( (ip >> 16) & 255 )) \
        $(( (ip >> 8) & 255 )) \
        $(( ip & 255 ))
}

mask_int=$(( cidr_prefix == 0 ? 0 : 0xffffffff ^ ((1 << (32 - cidr_prefix)) - 1) ))
network_int=$(( $(ip_to_int "${network_base}") & mask_int ))
network_address=$(int_to_ip "${network_int}")
SUBNET_NETWORK="${network_address}/${cidr_prefix}"
NETMASK=$(prefix_to_mask "${cidr_prefix}")

if ! ip link show "${NETWORK_NAME}" &>/dev/null; then
    fatal "Network interface '${NETWORK_NAME}' not found. Ensure the interface exists before running this script."
fi

info "Resetting IP configuration for ${NETWORK_NAME}"
ip addr flush dev "${NETWORK_NAME}" || warn "Unable to flush addresses on ${NETWORK_NAME}."
ip link set "${NETWORK_NAME}" down || warn "Unable to bring ${NETWORK_NAME} down."

info 'Removing previous dnsmasq state'
install -d -m 755 "${DNSMASQ_CONF_DIR}"
install -d -m 755 "${DNSMASQ_LEASE_DIR}"
DNSMASQ_CONF="${DNSMASQ_CONF_DIR}/${NETWORK_NAME}.conf"
rm -f "${DNSMASQ_CONF}"
rm -f "${DNSMASQ_LEASE_DIR}/${NETWORK_NAME}.leases"

info 'Preparing interface addressing'
ip link set "${NETWORK_NAME}" up
ip addr add "${GATEWAY_IP}/${cidr_prefix}" dev "${NETWORK_NAME}" valid_lft forever preferred_lft forever

info 'Writing dnsmasq configuration'
cat >"${DNSMASQ_CONF}" <<CFG
interface=${NETWORK_NAME}
bind-interfaces
no-resolv
domain-needed
bogus-priv
dhcp-authoritative
dhcp-range=${DHCP_RANGE_START},${DHCP_RANGE_END},${NETMASK},infinite
dhcp-option=option:router,${GATEWAY_IP}
dhcp-option=option:dns-server,${GATEWAY_IP}
dhcp-leasefile=${DNSMASQ_LEASE_DIR}/${NETWORK_NAME}.leases
log-dhcp
CFG

info 'Enabling IPv4 forwarding'
cat >"${SYSCTL_CONF}" <<SYSCTL
net.ipv4.ip_forward = 1
SYSCTL
sysctl -w net.ipv4.ip_forward=1 >/dev/null

find_wan_iface() {
    local iface
    iface=$(ip route show default 0.0.0.0/0 | awk '/default/ {print $5; exit}')
    if [[ -z ${iface} ]]; then
        fatal 'Unable to determine upstream (WAN) interface. Set WAN_IF before running.'
    fi
    printf '%s' "${iface}"
}

WAN_IF="${WAN_IF:-$(find_wan_iface)}"
if [[ -z ${WAN_IF} ]]; then
    fatal 'WAN interface detection failed.'
fi
if [[ "${WAN_IF}" == "${NETWORK_NAME}" ]]; then
    fatal 'WAN interface must differ from the DHCP-serving interface.'
fi

# Prefer firewalld for NAT configuration, fall back to iptables when unavailable.
apply_firewall_cmd() {
    if ! command -v firewall-cmd &>/dev/null; then
        return 1
    fi
    local default_zone
    default_zone=$(firewall-cmd --get-default-zone)
    info "Configuring firewalld (zone=${default_zone})"
    firewall-cmd --permanent --zone="${default_zone}" --remove-interface="${NETWORK_NAME}" >/dev/null 2>&1 || true
    firewall-cmd --permanent --zone=trusted --add-interface="${NETWORK_NAME}" >/dev/null
    firewall-cmd --permanent --zone="${default_zone}" --add-masquerade >/dev/null
    firewall-cmd --reload >/dev/null
    return 0
}

apply_iptables_rules() {
    if ! command -v iptables &>/dev/null; then
        warn 'iptables not available; skipping explicit NAT rules (ensure forwarding is configured).'
        return 1
    fi
    info "Configuring NAT via iptables (WAN=${WAN_IF}, LAN=${SUBNET_NETWORK})"
    iptables -t nat -D POSTROUTING -s "${SUBNET_NETWORK}" -o "${WAN_IF}" -j MASQUERADE 2>/dev/null || true
    iptables -D FORWARD -i "${WAN_IF}" -o "${NETWORK_NAME}" -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true
    iptables -D FORWARD -i "${NETWORK_NAME}" -o "${WAN_IF}" -j ACCEPT 2>/dev/null || true

    iptables -t nat -A POSTROUTING -s "${SUBNET_NETWORK}" -o "${WAN_IF}" -j MASQUERADE
    iptables -A FORWARD -i "${WAN_IF}" -o "${NETWORK_NAME}" -m state --state RELATED,ESTABLISHED -j ACCEPT
    iptables -A FORWARD -i "${NETWORK_NAME}" -o "${WAN_IF}" -j ACCEPT
    return 0
}

if ! apply_firewall_cmd; then
    apply_iptables_rules || warn 'Failed to configure NAT; manual intervention may be necessary.'
fi

dnsmasq_restart() {
    if command -v systemctl &>/dev/null && systemctl list-unit-files | grep -q '^dnsmasq\.service'; then
        info 'Restarting dnsmasq service'
        systemctl restart dnsmasq.service
        systemctl enable dnsmasq.service >/dev/null 2>&1 || true
        return
    fi
    warn 'dnsmasq.service not managed by systemd; launching standalone instance.'
    pkill -f "dnsmasq.*--conf-file=${DNSMASQ_CONF}" >/dev/null 2>&1 || true
    dnsmasq --conf-file="${DNSMASQ_CONF}" --keep-in-foreground --log-facility=- &
    disown || true
}

dnsmasq_restart

info "DHCP setup for ${NETWORK_NAME} complete."
