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
RANGE_START_DEFAULT="10.42.0.50"
RANGE_END_DEFAULT="10.42.0.200"
DNS_DEFAULT="1.1.1.1, 8.8.8.8"
LEASE_DEFAULT=600
MAX_LEASE_DEFAULT=7200
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
    --range 10.42.0.50 10.42.0.200 \
    --dns "1.1.1.1, 8.8.8.8" \
    --lease 600 \
    --max-lease 7200 \
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
  # Suporta prefixos comuns; expande se precisares
  local prefix="$1"
  case "$prefix" in
    8)  echo "255.0.0.0" ;;
    16) echo "255.255.0.0" ;;
    24) echo "255.255.255.0" ;;
    25) echo "255.255.255.128" ;;
    26) echo "255.255.255.192" ;;
    27) echo "255.255.255.224" ;;
    28) echo "255.255.255.240" ;;
    29) echo "255.255.255.248" ;;
    30) echo "255.255.255.252" ;;
    32) echo "255.255.255.255" ;;
    *)  echo "ERRO: prefixo CIDR não suportado: /$prefix" ; exit 1 ;;
  esac
}

split_cidr() {
  # Entrada: 10.42.0.0/24 -> define NET=10.42.0.0 e PREFIX=24
  local cidr="$1"
  NET="${cidr%/*}"
  PREFIX="${cidr#*/}"
  if [ -z "$NET" ] || [ -z "$PREFIX" ]; then
    echo "CIDR inválido: $cidr"
    exit 1
  fi
  NETMASK="$(cidr_to_netmask "$PREFIX")"
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
echo "    CIDR..........: $CIDR  (NET=$NET NETMASK=$NETMASK)"
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

subnet $NET netmask $NETMASK {
  range $RANGE_START $RANGE_END;
  option routers $GATEWAY;
  option broadcast-address $(echo "$NET" | awk -F. '{printf "%d.%d.%d.255", $1,$2,$3}');
  option domain-name-servers $DNS;
}
EOF

# NOTA: usamos awk só para formar o broadcast básico x.y.z.255. Se preferires sem awk,
# substitui manualmente por, por ex., 10.42.0.255 no bloco acima.

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
