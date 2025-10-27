#!/usr/bin/env bash
# DHCP + (opcional) NAT numa bridge local "512rede", simples e direto.
# - Mantém um dnsmasq dedicado só para a 512rede (DHCP ativo; DNS opcional).
# - NÃO usa macvlan/macvtap.
# - Se a "512rede" não existir, cria uma bridge kernel com esse nome.
# - NAT automático para a WAN se quiseres saída para a Internet.

set -euo pipefail

info()  { printf '[INFO] %s\n' "$*"; }
warn()  { printf '[WARN] %s\n' "$*" >&2; }
fatal() { printf '[ERROR] %s\n' "$*" >&2; exit 1; }

[[ ${EUID:-0} -eq 0 ]] || fatal 'Run as root.'

# --- Defaults (ajusta se precisares) -----------------------------------------
LAN_IF="512rede"                    # bridge local onde as VMs ligam
SUBNET_CIDR="${SUBNET_CIDR:-192.168.76.0/24}"
GATEWAY_IP="${GATEWAY_IP:-192.168.76.1}"
DHCP_RANGE_START="${DHCP_RANGE_START:-192.168.76.50}"
DHCP_RANGE_END="${DHCP_RANGE_END:-192.168.76.200}"
DNSMASQ_CONF_DIR="${DNSMASQ_CONF_DIR:-/etc/dnsmasq.d}"
DNSMASQ_LEASE_DIR="${DNSMASQ_LEASE_DIR:-/var/lib/dnsmasq}"
DEDICATED_UNIT="dnsmasq-${LAN_IF}.service"
SYSCTL_CONF="/etc/sysctl.d/99-${LAN_IF}-ipforward.conf"
ENABLE_NAT="${ENABLE_NAT:-1}"       # 1=ativa NAT; 0=sem NAT
RESOLV_CONF="${RESOLV_CONF:-/etc/resolv.conf}"   # upstream DNS para o dnsmasq (se DNS ativo)

# --- Helpers ------------------------------------------------------------------
command -v ip >/dev/null || fatal 'iproute2 em falta.'
command -v dnsmasq >/dev/null || fatal 'dnsmasq em falta.'

prefix=${SUBNET_CIDR#*/}
netbase=${SUBNET_CIDR%/*}
[[ $prefix =~ ^[0-9]+$ && $prefix -ge 0 && $prefix -le 32 ]] || fatal "SUBNET_CIDR inválido: $SUBNET_CIDR"

mask_from_prefix() { local p=$1 m; ((p==0)) && { echo 0.0.0.0; return; }; m=$((0xffffffff^((1<<(32-p))-1))); printf '%d.%d.%d.%d\n' $(((m>>24)&255)) $(((m>>16)&255)) $(((m>>8)&255)) $((m&255)); }
NETMASK=$(mask_from_prefix "$prefix")

find_wan_iface() {
  ip route show default 0.0.0.0/0 | awk '/default/ {print $5; exit}'
}

# --- 0) Garantir que a 512rede existe e está UP (cria bridge se não existir) --
if ! ip link show "$LAN_IF" >/dev/null 2>&1; then
  info "Interface '$LAN_IF' não existe. A criar bridge kernel '$LAN_IF'..."
  ip link add name "$LAN_IF" type bridge
  ip link set "$LAN_IF" up
else
  # Se já existe, só garantir que está UP
  ip link set "$LAN_IF" up || true
fi

# --- 1) Limpar instância antiga dedicada e preparar dirs ----------------------
install -d -m 755 "$DNSMASQ_CONF_DIR" "$DNSMASQ_LEASE_DIR"
rm -f "$DNSMASQ_CONF_DIR/${LAN_IF}.conf" "$DNSMASQ_LEASE_DIR/${LAN_IF}.leases"

# Não mexo no dnsmasq global que possas usar para outras coisas,
# apenas vou arrancar uma instância dedicada com --conf-file=<ficheiro>.
systemctl stop "$DEDICATED_UNIT" 2>/dev/null || true
systemctl disable "$DEDICATED_UNIT" 2>/dev/null || true

# Atribuir IP à bridge (via ip puro para ser imediato; opcionalmente poderias usar nmcli)
ip addr flush dev "$LAN_IF" || true
ip addr add "${GATEWAY_IP}/${prefix}" dev "$LAN_IF"
ip link set "$LAN_IF" up

# --- 2) Escrever config dedicada do dnsmasq -----------------------------------
cat >"$DNSMASQ_CONF_DIR/${LAN_IF}.conf" <<CFG
# Auto-generated for ${LAN_IF}
interface=${LAN_IF}
bind-interfaces
domain-needed
bogus-priv

# DHCP
dhcp-authoritative
dhcp-range=${DHCP_RANGE_START},${DHCP_RANGE_END},${NETMASK},infinite
dhcp-option=option:router,${GATEWAY_IP}
# DNS oferecido aos clientes: o próprio gateway (dnsmasq). Se não quiseres que o dnsmasq faça DNS,
# muda a linha para, por ex., 1.1.1.1 e 8.8.8.8, e adiciona 'port=0' abaixo.
dhcp-option=option:dns-server,${GATEWAY_IP}

# Se NÃO quiseres DNS neste dnsmasq (DHCP-only), descomenta a linha seguinte:
# port=0

# Upstream resolvers (quando DNS ativo)
resolv-file=${RESOLV_CONF}

# Leases file
dhcp-leasefile=${DNSMASQ_LEASE_DIR}/${LAN_IF}.leases

log-dhcp
CFG

# Testar config antes de arrancar
dnsmasq --test -C "$DNSMASQ_CONF_DIR/${LAN_IF}.conf" >/dev/null

# --- 3) Ativar IPv4 forwarding (necessário se tiveres NAT) --------------------
echo "net.ipv4.ip_forward = 1" > "$SYSCTL_CONF"
sysctl -w net.ipv4.ip_forward=1 >/dev/null

# --- 4) NAT opcional através da tua WAN --------------------------------------
WAN_IF="${WAN_IF:-$(find_wan_iface || true)}"
if [[ "${ENABLE_NAT}" == "1" && -n "${WAN_IF:-}" && "$WAN_IF" != "$LAN_IF" ]]; then
  info "NAT ativo: ${LAN_IF} -> ${WAN_IF}"
  if command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
    default_zone=$(firewall-cmd --get-default-zone)
    firewall-cmd --permanent --zone="$default_zone" --add-masquerade >/dev/null
    firewall-cmd --permanent --zone=trusted --add-interface="$LAN_IF" >/dev/null
    firewall-cmd --permanent --zone=trusted --add-service=dhcp >/dev/null 2>&1 || true
    firewall-cmd --permanent --zone=trusted --add-service=dns  >/dev/null 2>&1 || true
    firewall-cmd --reload >/dev/null
  else
    # iptables fallback mínimo
    iptables -t nat -D POSTROUTING -s "$SUBNET_CIDR" -o "$WAN_IF" -j MASQUERADE 2>/dev/null || true
    iptables -t nat -A POSTROUTING -s "$SUBNET_CIDR" -o "$WAN_IF" -j MASQUERADE
    iptables -D FORWARD -i "$WAN_IF" -o "$LAN_IF" -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true
    iptables -A FORWARD -i "$WAN_IF" -o "$LAN_IF" -m state --state RELATED,ESTABLISHED -j ACCEPT
    iptables -D FORWARD -i "$LAN_IF" -o "$WAN_IF" -j ACCEPT 2>/dev/null || true
    iptables -A FORWARD -i "$LAN_IF" -o "$WAN_IF" -j ACCEPT
  fi
else
  info "NAT desativado (ENABLE_NAT=${ENABLE_NAT})."
fi

# --- 5) Service unit dedicada para este dnsmasq -------------------------------
cat >/etc/systemd/system/"$DEDICATED_UNIT" <<EOF
[Unit]
Description=dnsmasq (dedicado) para ${LAN_IF}
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStartPre=/bin/bash -c 'for i in {1..30}; do ip -4 addr show ${LAN_IF} | grep -q "inet " && exit 0; sleep 1; done; echo "${LAN_IF} sem IPv4"; exit 1'
ExecStart=/usr/sbin/dnsmasq -k --conf-file=${DNSMASQ_CONF_DIR}/${LAN_IF}.conf --bind-interfaces
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now "$DEDICATED_UNIT"

sleep 1
systemctl --no-pager --lines=50 status "$DEDICATED_UNIT" || true
ss -lupn | egrep ':(53|67|68)\b' || true

info "OK: ${LAN_IF} ativo em ${GATEWAY_IP}/${prefix}, DHCP ${DHCP_RANGE_START}-${DHCP_RANGE_END}."
echo  "Liga as VMs com <interface type='bridge'><source bridge='${LAN_IF}'/></interface>."
