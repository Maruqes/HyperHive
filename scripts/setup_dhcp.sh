#!/usr/bin/env bash
# Provisiona um servidor DHCP ISC para a rede 512rede com NAT e leases estáticas.
set -euo pipefail

LAN_INTERFACE="${LAN_INTERFACE:-512rede}"
WAN_INTERFACE="${WAN_INTERFACE:-$(ip route get 1.1.1.1 2>/dev/null | awk '/dev/ {print $5; exit}')}"
LAN_GATEWAY_IP="${LAN_GATEWAY_IP:-10.51.2.1}"
LAN_NETWORK="${LAN_NETWORK:-10.51.2.0}"
LAN_NETMASK="${LAN_NETMASK:-255.255.255.0}"
LAN_BROADCAST="${LAN_BROADCAST:-}"
DOMAIN_NAME="${DOMAIN_NAME:-512rede.local}"
DNS_SERVERS="${DNS_SERVERS:-1.1.1.1, 8.8.8.8}"
DHCP_HOSTS_FILE="${DHCP_HOSTS_FILE:-/etc/dhcp/512rede-hosts.conf}"
DHCP_CONF_FILE=/etc/dhcp/dhcpd.conf
DHCP_DEFAULTS_FILE=/etc/default/isc-dhcp-server
SYSCTL_FORWARD_FILE=/etc/sysctl.d/90-512rede-ipforward.conf
IPTABLES_RULES_FILE=/etc/iptables/rules.v4
PACKAGES=(isc-dhcp-server iptables-persistent)

require_root() {
    if [ "$(id -u)" -ne 0 ]; then
        echo "Este script precisa ser executado como root." >&2
        exit 1
    fi
}

ensure_nonempty() {
    local value=$1
    local label=$2
    if [ -z "$value" ]; then
        echo "${label} não pode ser vazio." >&2
        exit 1
    fi
}

ensure_command() {
    local cmd=$1
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "Comando obrigatório não encontrado: $cmd" >&2
        exit 1
    fi
}

backup_file() {
    local file=$1
    if [ -f "$file" ]; then
        local stamp
        stamp=$(date +%Y%m%d%H%M%S)
        cp "$file" "${file}.bak.${stamp}"
    fi
}

install_packages() {
    local missing=()
    for pkg in "${PACKAGES[@]}"; do
        if ! dpkg -s "$pkg" >/dev/null 2>&1; then
            missing+=("$pkg")
        fi
    done

    if [ "${#missing[@]}" -gt 0 ]; then
        export DEBIAN_FRONTEND=noninteractive
        apt-get update
        apt-get install -y "${missing[@]}"
    fi
}

netmask_to_prefix() {
    local mask=$1
    local IFS=.
    read -r o1 o2 o3 o4 <<<"$mask"
    local -a octets=($o1 $o2 $o3 $o4)
    local prefix=0
    for oct in "${octets[@]}"; do
        case $oct in
            255) prefix=$((prefix + 8));;
            254) prefix=$((prefix + 7));;
            252) prefix=$((prefix + 6));;
            248) prefix=$((prefix + 5));;
            240) prefix=$((prefix + 4));;
            224) prefix=$((prefix + 3));;
            192) prefix=$((prefix + 2));;
            128) prefix=$((prefix + 1));;
            0) ;;
            *)
                echo "Máscara de rede inválida: $mask" >&2
                exit 1
                ;;
        esac
    done
    echo "$prefix"
}

ip_to_int() {
    local ip=$1
    local IFS=.
    local o1 o2 o3 o4
    read -r o1 o2 o3 o4 <<<"$ip"
    printf '%u\n' "$(((10#$o1 << 24) | (10#$o2 << 16) | (10#$o3 << 8) | 10#$o4))"
}

int_to_ip() {
    local value=$1
    printf '%d.%d.%d.%d\n' \
        $(((value >> 24) & 0xFF)) \
        $(((value >> 16) & 0xFF)) \
        $(((value >> 8) & 0xFF)) \
        $((value & 0xFF))
}

network_address() {
    local ip=$1
    local mask=$2
    local ip_int mask_int
    ip_int=$(ip_to_int "$ip")
    mask_int=$(ip_to_int "$mask")
    local network_int=$((ip_int & mask_int))
    int_to_ip "$network_int"
}

calculate_broadcast() {
    local network=$1
    local mask=$2
    local network_int mask_int
    network_int=$(ip_to_int "$network")
    mask_int=$(ip_to_int "$mask")
    local broadcast=$((network_int | (0xFFFFFFFF ^ mask_int)))
    int_to_ip "$broadcast"
}

ensure_interface_up() {
    local iface=$1
    if ! ip link show "$iface" >/dev/null 2>&1; then
        echo "Interface '${iface}' não encontrada." >&2
        exit 1
    fi
    ip link set dev "$iface" up
}

assign_gateway_ip() {
    local iface=$1
    local ip_addr=$2
    local netmask=$3
    local prefix
    prefix=$(netmask_to_prefix "$netmask")
    if ! ip addr show dev "$iface" | awk '/inet / {print $2}' | grep -Fxq "${ip_addr}/${prefix}"; then
        ip addr add "${ip_addr}/${prefix}" dev "$iface"
    fi
}

configure_dhcp_defaults() {
    local file=$1
    backup_file "$file"
    touch "$file"
    if grep -q '^INTERFACESv4=' "$file"; then
        sed -i "s/^INTERFACESv4=.*/INTERFACESv4=\"${LAN_INTERFACE}\"/" "$file"
    else
        printf '\nINTERFACESv4=\"%s\"\n' "$LAN_INTERFACE" >>"$file"
    fi
    if grep -q '^INTERFACESv6=' "$file"; then
        sed -i 's/^INTERFACESv6=.*/INTERFACESv6=""/' "$file"
    else
        printf 'INTERFACESv6=""\n' >>"$file"
    fi
}

ensure_hosts_file() {
    local file=$1
    if [ ! -f "$file" ]; then
        mkdir -p "$(dirname "$file")"
        cat >"$file" <<'EOS'
# Inclua aqui as reservas estáticas no formato:
# host nome_maquina {
#     hardware ethernet 00:11:22:33:44:55;
#     fixed-address 10.51.2.10;
#     option host-name "nome_maquina";
#     default-lease-time infinite;
#     max-lease-time infinite;
# }
EOS
    fi
}

render_dhcp_conf() {
    local file=$1
    backup_file "$file"
    cat >"$file" <<EOF
option domain-name "${DOMAIN_NAME}";
option domain-name-servers ${DNS_SERVERS};
default-lease-time infinite;
max-lease-time infinite;
authoritative;
one-lease-per-client true;
ddns-update-style none;
subnet ${LAN_NETWORK} netmask ${LAN_NETMASK} {
    option routers ${LAN_GATEWAY_IP};
    option broadcast-address ${LAN_BROADCAST};
    option subnet-mask ${LAN_NETMASK};
    include "${DHCP_HOSTS_FILE}";
}
EOF
}

configure_ip_forwarding() {
    sysctl -w net.ipv4.ip_forward=1 >/dev/null
    cat >"$SYSCTL_FORWARD_FILE" <<'EOF'
net.ipv4.ip_forward = 1
EOF
}

configure_nat() {
    local lan_cidr=${LAN_NETWORK}/$(netmask_to_prefix "$LAN_NETMASK")
    if ! iptables -t nat -C POSTROUTING -s "$lan_cidr" -o "$WAN_INTERFACE" -j MASQUERADE >/dev/null 2>&1; then
        iptables -t nat -A POSTROUTING -s "$lan_cidr" -o "$WAN_INTERFACE" -j MASQUERADE
    fi
    mkdir -p "$(dirname "$IPTABLES_RULES_FILE")"
    iptables-save >"$IPTABLES_RULES_FILE"
}

restart_dhcp_service() {
    systemctl enable isc-dhcp-server.service
    systemctl restart isc-dhcp-server.service
}

validate_dhcp_conf() {
    if ! dhcpd -t -cf "$DHCP_CONF_FILE" >/dev/null 2>&1; then
        echo "Falha na validação do arquivo ${DHCP_CONF_FILE}." >&2
        exit 1
    fi
}

main() {
    require_root
    ensure_nonempty "$LAN_INTERFACE" "LAN_INTERFACE"
    ensure_nonempty "$LAN_GATEWAY_IP" "LAN_GATEWAY_IP"
    ensure_nonempty "$LAN_NETWORK" "LAN_NETWORK"
    ensure_nonempty "$LAN_NETMASK" "LAN_NETMASK"
    ensure_nonempty "$WAN_INTERFACE" "WAN_INTERFACE"

    LAN_NETWORK=$(network_address "$LAN_NETWORK" "$LAN_NETMASK")
    if [ -z "$LAN_BROADCAST" ]; then
        LAN_BROADCAST=$(calculate_broadcast "$LAN_NETWORK" "$LAN_NETMASK")
    fi

    ensure_command ip
    ensure_command iptables
    install_packages
    ensure_command dhcpd
    ensure_command iptables-save

    ensure_interface_up "$LAN_INTERFACE"
    assign_gateway_ip "$LAN_INTERFACE" "$LAN_GATEWAY_IP" "$LAN_NETMASK"

    configure_dhcp_defaults "$DHCP_DEFAULTS_FILE"
    ensure_hosts_file "$DHCP_HOSTS_FILE"
    render_dhcp_conf "$DHCP_CONF_FILE"
    validate_dhcp_conf
    configure_ip_forwarding
    configure_nat
    restart_dhcp_service

    echo "Servidor DHCP configurado para a rede '${LAN_INTERFACE}' com leases estáticas."
}

main "$@"
