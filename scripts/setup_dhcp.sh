#!/usr/bin/env bash
set -euo pipefail

# ------------------------------------------------------------
# setup-dhcp-master.sh
# Instala e configura o DHCP (dhcpd) para uma rede específica.
# Testado em Fedora/RHEL-like (pacote: dhcp-server; serviço: dhcpd)
# NÃO usa pipes no conteúdo (para teclado sem '|').
# ------------------------------------------------------------

# -------- Defaults (podes alterar já aqui) ------------------
IFACE_DEFAULT="512rede"
CIDR_DEFAULT="10.42.0.0/24"
GATEWAY_DEFAULT="10.42.0.1"
RANGE_START_DEFAULT="10.42.0.20"
RANGE_END_DEFAULT="10.42.0.250"
DNS_DEFAULT="1.1.1.1, 8.8.8.8"
LEASE_DEFAULT=86400
MAX_LEASE_DEFAULT=604800
SET_STATIC_IP_DEFAULT="yes"      # yes/no -> define IP fixo no IFACE com o GATEWAY_DEFAULT/CIDR
DISABLE_FIREWALL_DEFAULT="yes"   # yes/no -> desativa firewalld para evitar bloqueios DHCP
ENABLE_NAT_DEFAULT="yes"         # yes/no -> ativa NAT para partilhar internet via interface WAN
WAN_IFACE_DEFAULT=""             # nome da interface WAN; vazio detecta via rota default

# -------------- Helpers -------------------------------------
usage() {
  cat <<'USAGE'
Uso:
  sudo ./setup-dhcp-master.sh \
    --iface 512rede \
    --cidr 10.42.0.0/24 \
    --gateway 10.42.0.1 \
    --range 10.42.0.20 10.42.0.250 \
    --dns "1.1.1.1, 8.8.8.8" \
    --lease 86400 \
    --max-lease 604800 \
    --set-static-ip yes \
    --disable-firewall yes \
    --enable-nat yes \
    --wan-iface enp6s0

Notas:
- --range precisa de dois valores: início e fim.
- --set-static-ip yes define IP fixo no IFACE com o gateway e máscara do --cidr.
- --enable-nat yes ativa encaminhamento/NAT para partilhar internet pela interface WAN.
- --wan-iface pode ser omitido se quiseres detetar pela rota default.
- O script instala 'dhcp-server', escreve /etc/dhcp/dhcpd.conf e /etc/sysconfig/dhcpd,
  ativa e arranca o serviço, e (opcional) desativa o firewalld / configura NAT.
USAGE
}

require_root() {
  if [ "${EUID}" -ne 0 ]; then
    echo "Este script precisa de sudo/root."
    exit 1
  fi
}

cidr_to_netmask() {
  local prefix="$1"

  if ! [[ "$prefix" =~ ^[0-9]+$ ]] || [ "$prefix" -lt 0 ] || [ "$prefix" -gt 32 ]; then
    echo "ERRO: prefixo CIDR inválido: /$prefix" >&2
    exit 1
  fi

  if [ "$prefix" -eq 0 ]; then
    echo "0.0.0.0"
    return
  fi

  local mask=$(( (0xFFFFFFFF << (32 - prefix)) & 0xFFFFFFFF ))
  printf "%d.%d.%d.%d\n" \
    $(( (mask >> 24) & 0xFF )) \
    $(( (mask >> 16) & 0xFF )) \
    $(( (mask >> 8) & 0xFF )) \
    $(( mask & 0xFF ))
}

ip_to_int() {
  local ip="$1"
  local IFS='.'
  local -a octets

  read -r -a octets <<<"$ip"
  if [ "${#octets[@]}" -ne 4 ]; then
    echo "ERRO: IP inválido: $ip" >&2
    exit 1
  fi

  local value=0 octet
  for octet in "${octets[@]}"; do
    if ! [[ "$octet" =~ ^[0-9]+$ ]] || [ "$octet" -lt 0 ] || [ "$octet" -gt 255 ]; then
      echo "ERRO: IP inválido: $ip" >&2
      exit 1
    fi
    value=$(( (value << 8) + 10#$octet ))
  done

  echo "$value"
}

int_to_ip() {
  local value="$1"
  printf "%d.%d.%d.%d\n" \
    $(( (value >> 24) & 0xFF )) \
    $(( (value >> 16) & 0xFF )) \
    $(( (value >> 8) & 0xFF )) \
    $(( value & 0xFF ))
}

network_address() {
  local ip="$1"
  local netmask="$2"
  local ip_int mask_int

  ip_int="$(ip_to_int "$ip")"
  mask_int="$(ip_to_int "$netmask")"

  int_to_ip $(( ip_int & mask_int ))
}

broadcast_address() {
  local ip="$1"
  local netmask="$2"
  local ip_int mask_int

  ip_int="$(ip_to_int "$ip")"
  mask_int="$(ip_to_int "$netmask")"

  int_to_ip $(( ip_int | ((~mask_int) & 0xFFFFFFFF) ))
}

ip_in_network() {
  local ip="$1"
  local network="$2"
  local prefix="$3"

  local netmask
  netmask="$(cidr_to_netmask "$prefix")"

  local ip_int net_int mask_int
  ip_int="$(ip_to_int "$ip")"
  mask_int="$(ip_to_int "$netmask")"
  net_int="$(ip_to_int "$network")"

  [ $(( ip_int & mask_int )) -eq $(( net_int & mask_int )) ]
}

compare_ip_order() {
  local left right
  left="$(ip_to_int "$1")"
  right="$(ip_to_int "$2")"

  if [ "$left" -lt "$right" ]; then
    echo "lt"
  elif [ "$left" -gt "$right" ]; then
    echo "gt"
  else
    echo "eq"
  fi
}

split_cidr() {
  local cidr="$1"
  local raw_ip

  raw_ip="${cidr%/*}"
  PREFIX="${cidr#*/}"
  if [ -z "$raw_ip" ] || [ -z "$PREFIX" ]; then
    echo "CIDR inválido: $cidr"
    exit 1
  fi
  NETMASK="$(cidr_to_netmask "$PREFIX")"
  NET="$(network_address "$raw_ip" "$NETMASK")"
  BROADCAST="$(broadcast_address "$NET" "$NETMASK")"
}

validate_network_inputs() {
  if ! ip_in_network "$GATEWAY" "$NET" "$PREFIX"; then
    echo "ERRO: gateway $GATEWAY não pertence à rede $NET/$PREFIX" >&2
    exit 1
  fi

  if ! ip_in_network "$RANGE_START" "$NET" "$PREFIX"; then
    echo "ERRO: início do range $RANGE_START não pertence à rede $NET/$PREFIX" >&2
    exit 1
  fi

  if ! ip_in_network "$RANGE_END" "$NET" "$PREFIX"; then
    echo "ERRO: fim do range $RANGE_END não pertence à rede $NET/$PREFIX" >&2
    exit 1
  fi

  if [ "$(compare_ip_order "$RANGE_START" "$RANGE_END")" = "gt" ]; then
    echo "ERRO: início do range ($RANGE_START) é maior que o fim ($RANGE_END)." >&2
    exit 1
  fi

  if [ "$(compare_ip_order "$RANGE_START" "$NET")" != "gt" ]; then
    echo "ERRO: início do range ($RANGE_START) não pode ser o endereço de rede ($NET)." >&2
    exit 1
  fi

  if [ "$(compare_ip_order "$RANGE_END" "$BROADCAST")" != "lt" ]; then
    echo "ERRO: fim do range ($RANGE_END) não pode ser o broadcast ($BROADCAST)." >&2
    exit 1
  fi

  local rel_gateway_range_start rel_gateway_range_end
  rel_gateway_range_start="$(compare_ip_order "$GATEWAY" "$RANGE_START")"
  rel_gateway_range_end="$(compare_ip_order "$GATEWAY" "$RANGE_END")"

  if [ "$rel_gateway_range_start" != "lt" ] && [ "$rel_gateway_range_end" != "gt" ]; then
    echo "Aviso: gateway $GATEWAY está dentro do range DHCP ($RANGE_START-$RANGE_END)." >&2
  fi

  if [ "$(compare_ip_order "$GATEWAY" "$NET")" != "gt" ] || [ "$(compare_ip_order "$GATEWAY" "$BROADCAST")" != "lt" ]; then
    echo "ERRO: gateway $GATEWAY não pode usar o endereço de rede ($NET) nem broadcast ($BROADCAST)." >&2
    exit 1
  fi
}

validate_lease_config() {
  if ! [[ "$LEASE" =~ ^[0-9]+$ ]] || [ "$LEASE" -le 0 ]; then
    echo "ERRO: --lease deve ser um inteiro positivo (atual: $LEASE)." >&2
    exit 1
  fi

  if ! [[ "$MAX_LEASE" =~ ^[0-9]+$ ]] || [ "$MAX_LEASE" -le 0 ]; then
    echo "ERRO: --max-lease deve ser um inteiro positivo (atual: $MAX_LEASE)." >&2
    exit 1
  fi

  if [ "$LEASE" -gt "$MAX_LEASE" ]; then
    echo "ERRO: --lease ($LEASE) não pode ser maior que --max-lease ($MAX_LEASE)." >&2
    exit 1
  fi

  if [ "$MAX_LEASE" -lt 3600 ]; then
    echo "Aviso: max-lease está inferior a 1h; os clientes podem renovar com muita frequência." >&2
  fi
}

ensure_iface_exists() {
  local iface="$1"
  if ! ip link show "$iface" >/dev/null 2>&1; then
    echo "ERRO: interface '$iface' não encontrada."
    exit 1
  fi
}

detect_wan_iface() {
  if [ -n "$WAN_IFACE" ]; then
    return
  fi

  local detected
  detected="$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<=NF;i++) if ($i == "dev") {print $(i+1); exit}}' || true)"
  if [ -z "$detected" ]; then
    detected="$(ip route show default 2>/dev/null | awk '/default/ {print $5; exit}')"
  fi

  if [ -z "$detected" ]; then
    echo "ERRO: não consegui detetar a interface WAN. Usa --wan-iface." >&2
    exit 1
  fi

  WAN_IFACE="$detected"
}

ensure_ip_forwarding() {
  local sysctl_file="/etc/sysctl.d/99-dhcp-nat.conf"
  /bin/mkdir -p /etc/sysctl.d
  echo "net.ipv4.ip_forward = 1" > "$sysctl_file"
  sysctl -w net.ipv4.ip_forward=1 >/dev/null
}

configure_nat() {
  local lan="$1"
  local wan="$2"
  local nft_bin

  nft_bin="$(command -v nft || true)"
  if [ -z "$nft_bin" ]; then
    echo "ERRO: utilitário 'nft' não encontrado; instala o pacote nftables." >&2
    exit 1
  fi

  local config_dir="/etc/512svman"
  local rules_file="$config_dir/dhcp-nat.nft"
  local service_file="/etc/systemd/system/dhcp-nat.service"

  /bin/mkdir -p "$config_dir"
  echo "  - Guardando regras NAT em $rules_file"
  cat > "$rules_file" <<EOF
table inet dhcp_nat {
  chain forward {
    type filter hook forward priority 0;
    policy drop;
    ct state established,related accept;
    iifname "$lan" oifname "$wan" accept;
  }

  chain postrouting {
    type nat hook postrouting priority srcnat;
    oifname "$wan" masquerade;
  }
}
EOF

  $nft_bin list table inet dhcp_nat >/dev/null 2>&1 && $nft_bin delete table inet dhcp_nat >/dev/null 2>&1 || true
  $nft_bin -f "$rules_file"

  cat > "$service_file" <<EOF
[Unit]
Description=Reaplicar regras NAT para DHCP
After=network.target
ConditionPathExists=$rules_file

[Service]
Type=oneshot
ExecStart=$nft_bin -f $rules_file
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF

  echo "  - Serviço systemd: dhcp-nat.service"
  systemctl daemon-reload
  systemctl enable --now dhcp-nat.service >/dev/null
}

connection_matches_iface() {
  local iface="$1"
  local name="$2"
  local uuid="$3"
  local device="$4"

  if [ "$device" = "$iface" ]; then
    return 0
  fi

  local value

  value="$(nmcli -g connection.interface-name connection show "$uuid" 2>/dev/null || true)"
  if [ -n "$value" ] && [ "$value" = "$iface" ]; then
    return 0
  fi

  value="$(nmcli -g 802-3-ethernet.interface-name connection show "$uuid" 2>/dev/null || true)"
  if [ -n "$value" ] && [ "$value" = "$iface" ]; then
    return 0
  fi

  value="$(nmcli -g GENERAL.DEVICES connection show "$uuid" 2>/dev/null || true)"
  if [ -n "$value" ]; then
    local IFS=','
    read -r -a devs <<<"$value"
    for dev in "${devs[@]}"; do
      dev="${dev// /}"
      if [ "$dev" = "$iface" ]; then
        return 0
      fi
    done
  fi

  if [ "$name" = "$iface" ]; then
    return 0
  fi

  return 1
}

nmcli_connections_for_iface() {
  local iface="$1"
  nmcli -t -f NAME,UUID,DEVICE connection show 2>/dev/null | while IFS=: read -r name uuid device; do
    [ -z "$uuid" ] && continue
    if connection_matches_iface "$iface" "$name" "$uuid" "$device"; then
      echo "$uuid"
    fi
  done
}

nmcli_delete_connections_for_iface() {
  local iface="$1"
  local -a targets=()

  while IFS= read -r uuid; do
    [ -z "$uuid" ] && continue
    targets+=("$uuid")
  done < <(nmcli_connections_for_iface "$iface")

  if [ "${#targets[@]}" -eq 0 ]; then
    return
  fi

  local uuid name
  for uuid in "${targets[@]}"; do
    name="$(nmcli -g connection.id connection show "$uuid" 2>/dev/null || echo "$uuid")"
    echo "  - Removendo ligação '$name' ($uuid) para $iface"
    nmcli connection delete "$uuid" >/dev/null || echo "Aviso: falhou remover ligação '$name' ($uuid)."
  done
}

# -------------- Parse Args ----------------------------------
IFACE="$IFACE_DEFAULT"
CIDR="$CIDR_DEFAULT"
GATEWAY="$GATEWAY_DEFAULT"
RANGE_START="$RANGE_START_DEFAULT"
RANGE_END="$RANGE_END_DEFAULT"
DNS="$DNS_DEFAULT"
LEASE="$LEASE_DEFAULT"
MAX_LEASE="$MAX_LEASE_DEFAULT"
SET_STATIC_IP="$SET_STATIC_IP_DEFAULT"
DISABLE_FIREWALL="$DISABLE_FIREWALL_DEFAULT"
ENABLE_NAT="$ENABLE_NAT_DEFAULT"
WAN_IFACE="$WAN_IFACE_DEFAULT"

if [ $# -gt 0 ]; then
  while [ $# -gt 0 ]; do
    case "$1" in
      -h|--help) usage; exit 0 ;;
      --iface) IFACE="$2"; shift 2 ;;
      --cidr) CIDR="$2"; shift 2 ;;
      --gateway) GATEWAY="$2"; shift 2 ;;
      --range) RANGE_START="$2"; RANGE_END="$3"; shift 3 ;;
      --dns) DNS="$2"; shift 2 ;;
      --lease) LEASE="$2"; shift 2 ;;
      --max-lease) MAX_LEASE="$2"; shift 2 ;;
      --set-static-ip) SET_STATIC_IP="$2"; shift 2 ;;
      --disable-firewall) DISABLE_FIREWALL="$2"; shift 2 ;;
      --enable-nat) ENABLE_NAT="$2"; shift 2 ;;
      --wan-iface) WAN_IFACE="$2"; shift 2 ;;
      *) echo "Arg desconhecido: $1"; usage; exit 1 ;;
    esac
  done
fi

# -------------- Execução ------------------------------------
require_root
split_cidr "$CIDR"
validate_network_inputs
validate_lease_config
ensure_iface_exists "$IFACE"

if [ "$ENABLE_NAT" = "yes" ]; then
  detect_wan_iface
  if [ -z "$WAN_IFACE" ]; then
    echo "ERRO: interface WAN não definida." >&2
    exit 1
  fi
  if [ "$WAN_IFACE" = "$IFACE" ]; then
    echo "ERRO: WAN_IFACE ($WAN_IFACE) não pode ser igual a IFACE ($IFACE)." >&2
    exit 1
  fi
  ensure_iface_exists "$WAN_IFACE"
fi

WAN_PRINT="(ignorado)"
if [ "$ENABLE_NAT" = "yes" ]; then
  WAN_PRINT="${WAN_IFACE:-auto}"
fi

echo "==> Parâmetros:"
echo "    IFACE.........: $IFACE"
echo "    CIDR..........: $CIDR  (NET=$NET BROADCAST=$BROADCAST NETMASK=$NETMASK)"
echo "    GATEWAY.......: $GATEWAY"
echo "    RANGE.........: $RANGE_START  ->  $RANGE_END"
echo "    DNS...........: $DNS"
echo "    LEASES........: default=$LEASE  max=$MAX_LEASE"
echo "    SET_STATIC_IP.: $SET_STATIC_IP"
echo "    DISABLE_FW....: $DISABLE_FIREWALL"
echo "    ENABLE_NAT....: $ENABLE_NAT"
echo "    WAN_IFACE.....: $WAN_PRINT"
echo

echo "==> 1/7 Instalar dhcp-server (se necessário)…"
dnf -qy install dhcp-server

echo "==> 2/7 Configurar interface no serviço dhcpd…"
/bin/mkdir -p /etc/sysconfig
cat > /etc/sysconfig/dhcpd <<EOF
DHCPDARGS="$IFACE"
EOF

echo "==> 3/7 Criar /etc/dhcp/dhcpd.conf…"
/bin/mkdir -p /etc/dhcp
cat > /etc/dhcp/dhcpd.conf <<EOF
authoritative;
default-lease-time $LEASE;
max-lease-time $MAX_LEASE;
one-lease-per-client true;
deny duplicates;
ignore client-uids true;
reuse-lease-on-expiry 1;
log-facility local7;

subnet $NET netmask $NETMASK {
  range $RANGE_START $RANGE_END;
  option routers $GATEWAY;
  option subnet-mask $NETMASK;
  option broadcast-address $BROADCAST;
  option domain-name-servers $DNS;
}
EOF

# O broadcast é calculado automaticamente para a rede fornecida.

echo "==> 4/7 (Opcional) Definir IP fixo $GATEWAY/$PREFIX em $IFACE…"
if [ "$SET_STATIC_IP" = "yes" ]; then
  echo "A remover ligações NetworkManager existentes para $IFACE (se houver)…"
  nmcli_delete_connections_for_iface "$IFACE"
  echo "A criar ligação manual '$IFACE' com $GATEWAY/$PREFIX…"
  nmcli connection add type ethernet ifname "$IFACE" con-name "$IFACE" \
    ipv4.method manual ipv4.addresses "$GATEWAY/$PREFIX" \
    ipv6.method ignore connection.autoconnect yes >/dev/null
  nmcli connection up "$IFACE" >/dev/null
fi

echo "==> 5/7 Configurar IP forwarding + NAT…"
if [ "$ENABLE_NAT" = "yes" ]; then
  echo "  - net.ipv4.ip_forward -> 1"
  ensure_ip_forwarding
  echo "  - NAT: $IFACE -> $WAN_IFACE"
  configure_nat "$IFACE" "$WAN_IFACE"
else
  echo "  - NAT desativado (--enable-nat no)."
fi

echo "==> 6/7 (Opcional) Desativar firewalld…"
if [ "$DISABLE_FIREWALL" = "yes" ]; then
  if systemctl list-unit-files firewalld.service >/dev/null 2>&1; then
    echo "Desativando firewalld (stop + disable)…"
    systemctl disable --now firewalld >/dev/null || echo "Aviso: falha ao desativar firewalld."
  else
    echo "firewalld não encontrado; nada a desativar."
  fi
fi

echo "==> 7/7 Ativar e arrancar serviço dhcpd…"
systemctl enable --now dhcpd

sleep 1
systemctl --no-pager --full status dhcpd || true

echo
echo "=== PRONTO! ===================================================="
echo "Interface: $IFACE   Rede: $CIDR"
echo "Range: $RANGE_START - $RANGE_END  Gateway: $GATEWAY  DNS: $DNS"
echo "Segue logs ao vivo com:  journalctl -fu dhcpd"
echo "==============================================================="
