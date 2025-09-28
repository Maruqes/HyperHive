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
IGNORE_CLIENT_UIDS_DEFAULT="yes" # yes/no -> ignora client-ids para garantir leases por MAC
STATIC_LEASE_SECONDS_DEFAULT=31536000 # ~1 ano; usado em reservas estáticas ("forever")

STATIC_RESERVATIONS_RAW=()
STATIC_RESERVATIONS=()

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
    --wan-iface enp6s0 \
    --ignore-client-uids yes \
    --static-host "pc1,aa:bb:cc:dd:ee:ff,10.42.0.10" \
    --static-host "aa:bb:cc:dd:ee:11,10.42.0.11" \
    --static-lease 31536000

Notas:
- --range precisa de dois valores: início e fim.
- --set-static-ip yes define IP fixo no IFACE com o gateway e máscara do --cidr.
- --enable-nat yes ativa encaminhamento/NAT para partilhar internet pela interface WAN.
- --wan-iface pode ser omitido se quiseres detetar pela rota default.
- --ignore-client-uids yes/no controla se o servidor ignora client-ids e usa só o MAC.
- --static-host aceita várias ocorrências. Formatos válidos: "host,mac,ip" ou "mac,ip".
- --static-lease define o lease (segundos) aplicado às reservas estáticas.
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

require_command() {
  local cmd="$1"
  local hint="${2:-instala o pacote correspondente.}"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "ERRO: comando '$cmd' não encontrado. $hint" >&2
    exit 1
  fi
}

trim() {
  local value="$*"
  # awk remove espaços em branco de início/fim
  echo "$value" | awk '{gsub(/^\s+|\s+$/, ""); print}'
}

ensure_yes_no() {
  local value="$1"
  local label="$2"
  case "$value" in
    yes|no) ;;
    *)
      echo "ERRO: $label deve ser 'yes' ou 'no'." >&2
      exit 1
      ;;
  esac
}

ensure_positive_integer() {
  local value="$1"
  local label="$2"
  if ! [[ "$value" =~ ^[0-9]+$ ]] || [ "$value" -le 0 ]; then
    echo "ERRO: $label deve ser um inteiro positivo." >&2
    exit 1
  fi
}

ensure_ipv4_address() {
  local ip="$1"
  local context="${2:-IPv4}"
  if ! python3 - "$ip" <<'PY' >/dev/null 2>&1; then
import ipaddress
import sys
try:
    ipaddress.IPv4Address(sys.argv[1])
except ValueError:
    sys.exit(1)
PY
    echo "ERRO: valor '$ip' inválido para $context." >&2
    exit 1
  fi
}

normalize_dns_list() {
  local raw="$1"
  raw="${raw//,/ }"
  local result=""
  local item
  for item in $raw; do
    item="$(trim "$item")"
    [ -z "$item" ] && continue
    ensure_ipv4_address "$item" "DNS"
    if [ -n "$result" ]; then
      result+=", "
    fi
    result+="$item"
  done
  if [ -z "$result" ]; then
    echo "ERRO: lista de DNS vazia." >&2
    exit 1
  fi
  echo "$result"
}

split_cidr() {
  local cidr="$1"
  local out
  if ! out="$(python3 - "$cidr" <<'PY' 2>/dev/null
import ipaddress
import sys
try:
    net = ipaddress.ip_network(sys.argv[1], strict=False)
except ValueError as exc:
    sys.stderr.write(str(exc))
    sys.exit(1)
print(f"{net.network_address}|{net.prefixlen}|{net.netmask}|{net.broadcast_address}")
PY
)"; then
    echo "ERRO: CIDR inválido: $cidr" >&2
    exit 1
  fi
  IFS='|' read -r NET PREFIX NETMASK BROADCAST <<<"$out"
}

ensure_ip_in_network() {
  local ip="$1"
  local cidr="$2"
  local label="${3:-IP}"
  if ! python3 - "$ip" "$cidr" <<'PY' >/dev/null 2>&1; then
import ipaddress
import sys
ip = ipaddress.IPv4Address(sys.argv[1])
net = ipaddress.ip_network(sys.argv[2], strict=False)
sys.exit(0 if ip in net else 1)
PY
    echo "ERRO: $label '$ip' não pertence à rede $cidr." >&2
    exit 1
  fi
}

ensure_ip_order() {
  local start="$1"
  local end="$2"
  if ! python3 - "$start" "$end" <<'PY' >/dev/null 2>&1; then
import ipaddress
import sys
inicio = ipaddress.IPv4Address(sys.argv[1])
fim = ipaddress.IPv4Address(sys.argv[2])
sys.exit(0 if inicio <= fim else 1)
PY
    echo "ERRO: IP inicial '$start' é maior do que o final '$end'." >&2
    exit 1
  fi
}

ip_in_range() {
  python3 - "$1" "$2" "$3" <<'PY' >/dev/null 2>&1
import ipaddress
import sys
ip = ipaddress.IPv4Address(sys.argv[1])
inicio = ipaddress.IPv4Address(sys.argv[2])
fim = ipaddress.IPv4Address(sys.argv[3])
sys.exit(0 if inicio <= ip <= fim else 1)
PY
}

process_static_hosts() {
  STATIC_RESERVATIONS=()
  if [ "${#STATIC_RESERVATIONS_RAW[@]}" -eq 0 ]; then
    return
  fi

  local -A seen_macs=()
  local -A seen_ips=()
  local -A seen_hosts=()
  local entry index=0
  for entry in "${STATIC_RESERVATIONS_RAW[@]}"; do
    index=$((index + 1))
    local first second third
    IFS=',' read -r first second third <<<"$entry"
    local host mac ip
    if [ -n "$third" ]; then
      host="$first"
      mac="$second"
      ip="$third"
    else
      host=""
      mac="$first"
      ip="$second"
    fi

    host="$(trim "$host")"
    mac="$(trim "$mac")"
    ip="$(trim "$ip")"

    if [ -z "$mac" ] || [ -z "$ip" ]; then
      echo "ERRO: --static-host '$entry' requer pelo menos MAC e IP (formato host,mac,ip ou mac,ip)." >&2
      exit 1
    fi

    if [ -z "$host" ]; then
      host="static-${index}"
    fi

    local mac_lower="${mac,,}"
    if [[ ! "$mac_lower" =~ ^([0-9a-f]{2}:){5}[0-9a-f]{2}$ ]]; then
      echo "ERRO: MAC '$mac' inválido em --static-host '$entry'. Usa formato aa:bb:cc:dd:ee:ff." >&2
      exit 1
    fi

    ensure_ipv4_address "$ip" "IP estático"
    ensure_ip_in_network "$ip" "$CIDR" "IP estático"

    if [ "$ip" = "$NET" ] || [ "$ip" = "$BROADCAST" ]; then
      echo "ERRO: IP estático '$ip' em --static-host '$entry' coincide com o endereço reservado da rede." >&2
      exit 1
    fi

    if [ "$ip" = "$GATEWAY" ]; then
      echo "ERRO: IP estático '$ip' em --static-host '$entry' coincide com o gateway ($GATEWAY)." >&2
      exit 1
    fi

    if ip_in_range "$ip" "$RANGE_START" "$RANGE_END"; then
      echo "ERRO: IP estático '$ip' em --static-host '$entry' está dentro do range dinâmico ($RANGE_START-$RANGE_END)." >&2
      echo "      Ajusta o --range ou escolhe outro IP fixo." >&2
      exit 1
    fi

    if [ -n "${seen_macs[$mac_lower]:-}" ]; then
      echo "ERRO: MAC '$mac' duplicado nas reservas estáticas." >&2
      exit 1
    fi

    if [ -n "${seen_ips[$ip]:-}" ]; then
      echo "ERRO: IP '$ip' duplicado nas reservas estáticas." >&2
      exit 1
    fi

    local safe_host
    safe_host="$(echo "$host" | tr '[:upper:]' '[:lower:]')"
    safe_host="$(echo "$safe_host" | sed 's/[^a-z0-9_-]/-/g')"
    safe_host="$(echo "$safe_host" | sed 's/^-*//; s/-*$//')"
    if [ -z "$safe_host" ]; then
      safe_host="static-${index}"
    fi

    if [ -n "${seen_hosts[$safe_host]:-}" ]; then
      echo "ERRO: Nome de host '$host' (normalizado: $safe_host) duplicado nas reservas estáticas." >&2
      exit 1
    fi

    seen_macs[$mac_lower]=1
    seen_ips[$ip]=1
    seen_hosts[$safe_host]=1
    STATIC_RESERVATIONS+=("$safe_host|$mac_lower|$ip")
  done
}

render_static_hosts() {
  local indent="${1:-  }"
  local entry
  if [ "${#STATIC_RESERVATIONS[@]}" -eq 0 ]; then
    return
  fi

  echo "${indent}# Reservas estáticas geradas pelo 512SvMan"
  for entry in "${STATIC_RESERVATIONS[@]}"; do
    IFS='|' read -r host mac ip <<<"$entry"
    cat <<EOF
${indent}host $host {
${indent}  hardware ethernet $mac;
${indent}  fixed-address $ip;
${indent}  option host-name "$host";
${indent}  default-lease-time $STATIC_LEASE_SECONDS;
${indent}  max-lease-time $STATIC_LEASE_SECONDS;
${indent}  deny duplicates;
${indent}}
EOF
  done
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
IGNORE_CLIENT_UIDS="$IGNORE_CLIENT_UIDS_DEFAULT"
STATIC_LEASE_SECONDS="$STATIC_LEASE_SECONDS_DEFAULT"
STATIC_RESERVATIONS_RAW=()

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
      --ignore-client-uids) IGNORE_CLIENT_UIDS="$2"; shift 2 ;;
      --static-host) STATIC_RESERVATIONS_RAW+=("$2"); shift 2 ;;
      --static-lease) STATIC_LEASE_SECONDS="$2"; shift 2 ;;
      *) echo "Arg desconhecido: $1"; usage; exit 1 ;;
    esac
  done
fi

# -------------- Execução ------------------------------------
require_root
require_command python3 "Instala python3 (pacote python3)."

ensure_yes_no "$SET_STATIC_IP" "--set-static-ip"
ensure_yes_no "$DISABLE_FIREWALL" "--disable-firewall"
ensure_yes_no "$ENABLE_NAT" "--enable-nat"
ensure_yes_no "$IGNORE_CLIENT_UIDS" "--ignore-client-uids"

ensure_positive_integer "$LEASE" "--lease"
ensure_positive_integer "$MAX_LEASE" "--max-lease"
ensure_positive_integer "$STATIC_LEASE_SECONDS" "--static-lease"

if [ "$MAX_LEASE" -lt "$LEASE" ]; then
  echo "ERRO: --max-lease ($MAX_LEASE) deve ser >= --lease ($LEASE)." >&2
  exit 1
fi

DNS_FORMATTED="$(normalize_dns_list "$DNS")"

split_cidr "$CIDR"
ensure_ipv4_address "$GATEWAY" "gateway"
ensure_ip_in_network "$GATEWAY" "$CIDR" "Gateway"
if [ "$GATEWAY" = "$NET" ] || [ "$GATEWAY" = "$BROADCAST" ]; then
  echo "ERRO: Gateway ($GATEWAY) coincide com endereço reservado (rede/broadcast)." >&2
  exit 1
fi

ensure_ipv4_address "$RANGE_START" "range (início)"
ensure_ipv4_address "$RANGE_END" "range (fim)"
ensure_ip_in_network "$RANGE_START" "$CIDR" "Início do range"
ensure_ip_in_network "$RANGE_END" "$CIDR" "Fim do range"
ensure_ip_order "$RANGE_START" "$RANGE_END"

if [ "$RANGE_START" = "$NET" ] || [ "$RANGE_START" = "$BROADCAST" ] || [ "$RANGE_START" = "$GATEWAY" ]; then
  echo "ERRO: Início do range ($RANGE_START) inválido (rede/gateway/broadcast)." >&2
  exit 1
fi

if [ "$RANGE_END" = "$NET" ] || [ "$RANGE_END" = "$BROADCAST" ] || [ "$RANGE_END" = "$GATEWAY" ]; then
  echo "ERRO: Fim do range ($RANGE_END) inválido (rede/gateway/broadcast)." >&2
  exit 1
fi

if ip_in_range "$GATEWAY" "$RANGE_START" "$RANGE_END"; then
  echo "ERRO: O gateway ($GATEWAY) não pode estar dentro do range dinâmico ($RANGE_START-$RANGE_END)." >&2
  exit 1
fi

ensure_iface_exists "$IFACE"

if [ "$SET_STATIC_IP" = "yes" ]; then
  require_command nmcli "Instala NetworkManager (nmcli) ou define --set-static-ip no."
fi

process_static_hosts
STATIC_HOST_COUNT=${#STATIC_RESERVATIONS[@]}

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
echo "    CIDR..........: $CIDR  (NET=$NET NETMASK=$NETMASK BROADCAST=$BROADCAST)"
echo "    GATEWAY.......: $GATEWAY"
echo "    RANGE.........: $RANGE_START  ->  $RANGE_END"
echo "    DNS...........: $DNS_FORMATTED"
echo "    LEASES........: default=$LEASE  max=$MAX_LEASE"
echo "    STATIC_LEASE..: $STATIC_LEASE_SECONDS (segundos)"
echo "    IGNORE_UIDS...: $IGNORE_CLIENT_UIDS"
echo "    STATIC_HOSTS..: $STATIC_HOST_COUNT"
if [ "$STATIC_HOST_COUNT" -gt 0 ]; then
  for entry in "${STATIC_RESERVATIONS[@]}"; do
    IFS='|' read -r host mac ip <<<"$entry"
    echo "      - $host -> $ip ($mac)"
  done
fi
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
# Arquivo gerado automaticamente por 512SvMan (scripts/setup_dhcp.sh)
authoritative;
ddns-update-style none;
log-facility local7;
one-lease-per-client on;
default-lease-time $LEASE;
max-lease-time $MAX_LEASE;
ignore client-updates;
$( [ "$IGNORE_CLIENT_UIDS" = "yes" ] && printf $'ignore client-uids;
' )

subnet $NET netmask $NETMASK {
  option routers $GATEWAY;
  option subnet-mask $NETMASK;
  option broadcast-address $BROADCAST;
  option domain-name-servers $DNS_FORMATTED;
  range $RANGE_START $RANGE_END;
$(render_static_hosts "  ")
}
EOF

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
