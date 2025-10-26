#!/usr/bin/env bash
set -Eeuo pipefail

# Reverte o bridge br512 e volta a usar a NIC física diretamente
# Uso: sudo ./revert_br512.sh [NIC]
# NIC padrão: 512rede

NIC_DEV="${1:-512rede}"
BR_IF="br512"
BR_CONN="$BR_IF"
SLAVE_FALLBACK="${BR_IF}-slave-${NIC_DEV}"

log()  { printf "\033[1;32m[INFO]\033[0m %s\n" "$*"; }
warn() { printf "\033[1;33m[WARN]\033[0m %s\n" "$*"; }
err()  { printf "\033[1;31m[ERRO]\033[0m %s\n" "$*" >&2; }
die()  { err "$*"; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "Falta o comando '$1'"; }

need nmcli
need ip
need bridge

(( EUID == 0 )) || die "Corre como root: sudo $0 [NIC]"

ip link show "$NIC_DEV" >/dev/null 2>&1 || die "Interface '$NIC_DEV' não existe."

if ! nmcli -t -f NAME con show | grep -Fxq "$BR_CONN"; then
  if ip link show "$BR_IF" >/dev/null 2>&1; then
    die "Interface '$BR_IF' existe mas não encontrei o perfil do NetworkManager ('$BR_CONN'). Verifica manualmente."
  fi
  log "Perfil '$BR_CONN' não existe — parece já revertido. Nada a fazer."
  exit 0
fi

BR_CONN_TYPE="$(nmcli -g connection.type con show "$BR_CONN" 2>/dev/null || true)"
[[ "$BR_CONN_TYPE" == "bridge" ]] || die "Perfil '$BR_CONN' não é do tipo bridge (atual: '$BR_CONN_TYPE')."

BR_ACTIVE_CONN="$(nmcli -t -f NAME,DEVICE con show --active | awk -F: -v b="$BR_IF" '$2==b{print $1; exit}')"
if [[ -n "${BR_ACTIVE_CONN:-}" && "$BR_ACTIVE_CONN" != "$BR_CONN" ]]; then
  log "Perfil ativo do bridge: $BR_ACTIVE_CONN"
fi

SLAVE_CONN="$(nmcli -t -f NAME,TYPE,DEVICE con show --active | awk -F: -v d="$NIC_DEV" '$2=="bridge-slave" && $3==d{print $1; exit}')"
if [[ -z "${SLAVE_CONN:-}" ]]; then
  SLAVE_CONN="$(nmcli -t -f NAME,TYPE,DEVICE con show | awk -F: -v d="$NIC_DEV" '$2=="bridge-slave" && $3==d{print $1; exit}')"
fi

if [[ -z "${SLAVE_CONN:-}" ]]; then
  if nmcli -t -f NAME con show | grep -Fxq "$SLAVE_FALLBACK"; then
    SLAVE_CONN="$SLAVE_FALLBACK"
  fi
fi

if [[ -n "${SLAVE_CONN:-}" ]]; then
  SLAVE_TYPE="$(nmcli -g connection.type con show "$SLAVE_CONN" 2>/dev/null || true)"
  if [[ "$SLAVE_TYPE" != "bridge-slave" ]]; then
    warn "Perfil '$SLAVE_CONN' não é do tipo bridge-slave (tipo: '$SLAVE_TYPE'). Ignorar."
    SLAVE_CONN=""
  fi
fi

log "NIC alvo: $NIC_DEV"
log "Bridge: $BR_IF (perfil: $BR_CONN)"
[[ -n "${SLAVE_CONN:-}" ]] && log "Ligação bridge-slave: $SLAVE_CONN" || warn "Sem ligação bridge-slave detetada para $NIC_DEV."

nm_conn_get() {
  local conn="$1" key="$2" out
  out="$(nmcli -g "$key" con show "$conn" 2>/dev/null || true)"
  [[ -n "${out:-}" ]] || return 0
  printf '%s\n' "$out" | sed '/^\s*$/d;/^--$/d'
}

nm_conn_copy_single() {
  local src="$1" dst="$2" key="$3" value first
  value="$(nm_conn_get "$src" "$key")"
  if ! nmcli con mod "$dst" "$key" "" 2>/dev/null; then
    warn "Não consegui limpar '$key' em '$dst' (ignorar se não suportado)."
    return
  fi
  if [[ -n "${value//[[:space:]]/}" ]]; then
    first="$(printf '%s\n' "$value" | head -n1)"
    if ! nmcli con mod "$dst" "$key" "$first" 2>/dev/null; then
      warn "Falha ao aplicar '$key=$first' em '$dst'."
    fi
  fi
}

nm_conn_copy_multi() {
  local src="$1" dst="$2" key="$3" value
  value="$(nm_conn_get "$src" "$key")"
  if ! nmcli con mod "$dst" "$key" "" 2>/dev/null; then
    warn "Não consegui limpar '$key' em '$dst' (ignorar se não suportado)."
    return
  fi
  if [[ -n "${value//[[:space:]]/}" ]]; then
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      if ! nmcli con mod "$dst" +${key} "$line" 2>/dev/null; then
        warn "Falha ao adicionar '$key=$line' em '$dst'."
      fi
    done <<< "$value"
  fi
}

nm_slave_copy_single() {
  [[ -n "${SLAVE_CONN:-}" ]] || return 0
  nm_conn_copy_single "$SLAVE_CONN" "$ETH_CONN" "$1"
}

pretty_list() {
  local input="$1"
  if [[ -z "${input//[[:space:]]/}" ]]; then
    printf ''
    return 0
  fi
  printf '%s' "$input" | sed ':a;N;$!ba;s/\n/, /g'
}

have_all_addrs() {
  local expected="$1" actual="$2"
  while IFS= read -r addr; do
    [[ -z "$addr" ]] && continue
    grep -Fxq "$addr" <<< "$actual" || return 1
  done <<< "$expected"
  return 0
}

BR_PROFILE_AUTOCONNECT_ORIG="$(nmcli -g connection.autoconnect con show "$BR_CONN" 2>/dev/null || echo yes)"
BR_PROFILE_AUTOCONNECT_MOD=false

ORIG_SLAVE_AUTOCONNECT=""
SLAVE_AUTOCONNECT_MOD=false
if [[ -n "${SLAVE_CONN:-}" ]]; then
  ORIG_SLAVE_AUTOCONNECT="$(nmcli -g connection.autoconnect con show "$SLAVE_CONN" 2>/dev/null || echo yes)"
  if [[ "$ORIG_SLAVE_AUTOCONNECT" != "no" ]]; then
    if nmcli con mod "$SLAVE_CONN" connection.autoconnect no 2>/dev/null; then
      SLAVE_AUTOCONNECT_MOD=true
    else
      warn "Falha ao desativar autoconnect de '$SLAVE_CONN'."
    fi
  fi
fi

if [[ "$BR_PROFILE_AUTOCONNECT_ORIG" != "no" ]]; then
  if nmcli con mod "$BR_CONN" connection.autoconnect no 2>/dev/null; then
    BR_PROFILE_AUTOCONNECT_MOD=true
  else
    warn "Falha ao desativar autoconnect de '$BR_CONN'."
  fi
fi

# Guardar estado atual para validação posterior
BRIDGE_IPV4_ACTIVE="$(ip -o -4 addr show dev "$BR_IF" | awk '{print $4}')"
BRIDGE_IPV6_ACTIVE="$(ip -o -6 addr show dev "$BR_IF" scope global | awk '{print $4}')"

IPV4_METHOD="$(nm_conn_get "$BR_CONN" "ipv4.method")"
IPV4_ADDRS="$(nm_conn_get "$BR_CONN" "ipv4.addresses")"
IPV4_GW="$(nm_conn_get "$BR_CONN" "ipv4.gateway")"
IPV6_METHOD="$(nm_conn_get "$BR_CONN" "ipv6.method")"
IPV6_ADDRS="$(nm_conn_get "$BR_CONN" "ipv6.addresses")"
IPV6_GW="$(nm_conn_get "$BR_CONN" "ipv6.gateway")"

[[ -n "${IPV4_METHOD:-}" ]] || IPV4_METHOD=auto
[[ -n "${IPV6_METHOD:-}" ]] || IPV6_METHOD=auto

log "Config bridge atual:"
log "  IPv4 método=$IPV4_METHOD | addr=$(pretty_list "$BRIDGE_IPV4_ACTIVE") | gw='${IPV4_GW:-}'"
log "  IPv6 método=$IPV6_METHOD | addr=$(pretty_list "$BRIDGE_IPV6_ACTIVE") | gw='${IPV6_GW:-}'"

# Escolher ligação Ethernet direta a reutilizar/criar
ETH_CONN="$(nmcli -t -f NAME,TYPE,DEVICE con show | awk -F: -v d="$NIC_DEV" '$2!="bridge-slave" && $2=="802-3-ethernet" && $3==d{print $1; exit}')"
ETH_PROFILE_CREATED=false
ETH_AUTOCONNECT_MOD=false
ETH_CONN_UP=false

if [[ -z "${ETH_CONN:-}" ]]; then
  ETH_CONN="${NIC_DEV}-direct"
  log "Nenhuma ligação 802-3-ethernet encontrada para $NIC_DEV. Será criada '$ETH_CONN'."
  nmcli con add type ethernet ifname "$NIC_DEV" con-name "$ETH_CONN"
  ETH_PROFILE_CREATED=true
fi

ETH_CONN_TYPE="$(nmcli -g connection.type con show "$ETH_CONN" 2>/dev/null || true)"
[[ "$ETH_CONN_TYPE" == "802-3-ethernet" ]] || die "Perfil '$ETH_CONN' existe mas não é 802-3-ethernet (tipo: '$ETH_CONN_TYPE')."

ORIG_ETH_AUTOCONNECT="$(nmcli -g connection.autoconnect con show "$ETH_CONN" 2>/dev/null || echo yes)"
[[ -n "${ORIG_ETH_AUTOCONNECT:-}" ]] || ORIG_ETH_AUTOCONNECT=yes

nmcli con mod "$ETH_CONN" connection.interface-name "$NIC_DEV" connection.master "" connection.slave-type "" connection.secondaries "" || true

if [[ "$ORIG_ETH_AUTOCONNECT" != "yes" ]]; then
  if nmcli con mod "$ETH_CONN" connection.autoconnect yes 2>/dev/null; then
    ETH_AUTOCONNECT_MOD=true
  else
    warn "Falha ao forçar autoconnect na ligação '$ETH_CONN'."
  fi
fi

log "A sincronizar parâmetros do bridge para '$ETH_CONN'..."
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "connection.metered"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "connection.zone"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "connection.mdns"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "connection.llmnr"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "connection.autoconnect-priority"

BRIDGE_MTU=""
if [[ -n "${SLAVE_CONN:-}" ]]; then
  BRIDGE_MTU="$(nm_conn_get "$SLAVE_CONN" "802-3-ethernet.mtu")"
fi
if [[ -n "${BRIDGE_MTU//[[:space:]]/}" ]]; then
  nmcli con mod "$ETH_CONN" 802-3-ethernet.mtu "$BRIDGE_MTU" 2>/dev/null || warn "Não consegui aplicar MTU '$BRIDGE_MTU' à ligação '$ETH_CONN'."
else
  nmcli con mod "$ETH_CONN" 802-3-ethernet.mtu "" 2>/dev/null || true
fi

nm_slave_copy_single "802-3-ethernet.cloned-mac-address"
nm_slave_copy_single "802-3-ethernet.wake-on-lan"
nm_slave_copy_single "802-3-ethernet.wake-on-lan-password"

nmcli con mod "$ETH_CONN" ipv4.method "$IPV4_METHOD"
if [[ "$IPV4_METHOD" == "manual" ]]; then
  if ! nmcli con mod "$ETH_CONN" ipv4.addresses "" 2>/dev/null; then
    warn "Falha ao limpar ipv4.addresses em '$ETH_CONN'."
  fi
  if [[ -n "${IPV4_ADDRS//[[:space:]]/}" ]]; then
    while IFS= read -r addr; do
      [[ -z "$addr" ]] && continue
      nmcli con mod "$ETH_CONN" +ipv4.addresses "$addr" 2>/dev/null || warn "Não consegui aplicar ipv4.addresses='$addr'."
    done <<< "$IPV4_ADDRS"
  else
    warn "Bridge configurado com IPv4 manual mas sem endereços definidos."
  fi
  if [[ -n "${IPV4_GW//[[:space:]]/}" ]]; then
    nmcli con mod "$ETH_CONN" ipv4.gateway "$IPV4_GW" 2>/dev/null || warn "Falha ao definir ipv4.gateway='$IPV4_GW'."
  else
    nmcli con mod "$ETH_CONN" ipv4.gateway "" 2>/dev/null || true
  fi
else
  nmcli con mod "$ETH_CONN" ipv4.addresses "" ipv4.gateway "" 2>/dev/null || true
fi

nm_conn_copy_multi "$BR_CONN" "$ETH_CONN" "ipv4.dns"
nm_conn_copy_multi "$BR_CONN" "$ETH_CONN" "ipv4.dns-search"
nm_conn_copy_multi "$BR_CONN" "$ETH_CONN" "ipv4.routes"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv4.route-metric"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv4.never-default"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv4.may-fail"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv4.ignore-auto-dns"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv4.ignore-auto-routes"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv4.dhcp-client-id"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv4.dhcp-hostname"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv4.dhcp-send-hostname"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv4.dhcp-timeout"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv4.dhcp-fqdn"

nmcli con mod "$ETH_CONN" ipv6.method "$IPV6_METHOD"
if [[ "$IPV6_METHOD" == "manual" ]]; then
  if ! nmcli con mod "$ETH_CONN" ipv6.addresses "" 2>/dev/null; then
    warn "Falha ao limpar ipv6.addresses em '$ETH_CONN'."
  fi
  if [[ -n "${IPV6_ADDRS//[[:space:]]/}" ]]; then
    while IFS= read -r addr; do
      [[ -z "$addr" ]] && continue
      nmcli con mod "$ETH_CONN" +ipv6.addresses "$addr" 2>/dev/null || warn "Não consegui aplicar ipv6.addresses='$addr'."
    done <<< "$IPV6_ADDRS"
  fi
  if [[ -n "${IPV6_GW//[[:space:]]/}" ]]; then
    nmcli con mod "$ETH_CONN" ipv6.gateway "$IPV6_GW" 2>/dev/null || warn "Falha ao definir ipv6.gateway='$IPV6_GW'."
  else
    nmcli con mod "$ETH_CONN" ipv6.gateway "" 2>/dev/null || true
  fi
else
  nmcli con mod "$ETH_CONN" ipv6.addresses "" ipv6.gateway "" 2>/dev/null || true
fi

nm_conn_copy_multi "$BR_CONN" "$ETH_CONN" "ipv6.dns"
nm_conn_copy_multi "$BR_CONN" "$ETH_CONN" "ipv6.dns-search"
nm_conn_copy_multi "$BR_CONN" "$ETH_CONN" "ipv6.routes"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv6.route-metric"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv6.never-default"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv6.may-fail"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv6.ignore-auto-dns"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv6.ignore-auto-routes"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv6.addr-gen-mode"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv6.ip6-privacy"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv6.ra-timeout"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv6.dhcp-duid"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv6.dhcp-hostname"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv6.dhcp-iaid"
nm_conn_copy_single "$BR_CONN" "$ETH_CONN" "ipv6.dhcp-send-hostname"

log "Ligação Ethernet alvo: $ETH_CONN"

rollback() {
  warn "Erro detetado; a reverter para o bridge '$BR_CONN'..."
  if [[ "$ETH_CONN_UP" == true ]]; then
    nmcli con down "$ETH_CONN" 2>/dev/null || true
  fi
  if [[ "$ETH_PROFILE_CREATED" == true ]]; then
    nmcli con delete "$ETH_CONN" 2>/dev/null || true
  else
    if [[ "$ETH_AUTOCONNECT_MOD" == true ]]; then
      nmcli con mod "$ETH_CONN" connection.autoconnect "$ORIG_ETH_AUTOCONNECT" 2>/dev/null || true
    fi
  fi
  if [[ "$BR_PROFILE_AUTOCONNECT_MOD" == true ]]; then
    nmcli con mod "$BR_CONN" connection.autoconnect "$BR_PROFILE_AUTOCONNECT_ORIG" 2>/dev/null || true
  fi
  if [[ -n "${SLAVE_CONN:-}" ]]; then
    if [[ "$SLAVE_AUTOCONNECT_MOD" == true ]]; then
      nmcli con mod "$SLAVE_CONN" connection.autoconnect "$ORIG_SLAVE_AUTOCONNECT" 2>/dev/null || true
    fi
    nmcli -w 20 con up "$SLAVE_CONN" 2>/dev/null || true
  fi
  nmcli -w 20 con up "$BR_CONN" 2>/dev/null || true
}

trap rollback ERR

echo
warn "Será feito failover para ligação direta. Pode haver 2–5s de quebra."
warn "A continuar em 5s... (Ctrl+C para abortar)"
sleep 5

if [[ -n "${SLAVE_CONN:-}" ]]; then
  log "A desligar bridge-slave '$SLAVE_CONN'..."
  nmcli -w 15 con down "$SLAVE_CONN" 2>/dev/null || warn "Bridge-slave '$SLAVE_CONN' já estava down."
fi

log "A subir ligação direta '$ETH_CONN' em $NIC_DEV..."
nmcli -w 30 con up "$ETH_CONN"
ETH_CONN_UP=true

# Validar migração dos endereços IP para a NIC
NIC_IPV4_CURRENT=""
NIC_IPV6_CURRENT=""

if [[ -n "${BRIDGE_IPV4_ACTIVE//[[:space:]]/}" ]]; then
  for attempt in {1..20}; do
    NIC_IPV4_CURRENT="$(ip -o -4 addr show dev "$NIC_DEV" | awk '{print $4}')"
    if [[ "$IPV4_METHOD" == "manual" ]]; then
      have_all_addrs "$BRIDGE_IPV4_ACTIVE" "$NIC_IPV4_CURRENT" && break
    else
      [[ -n "${NIC_IPV4_CURRENT//[[:space:]]/}" ]] && break
    fi
    sleep 1
  done
  if [[ "$IPV4_METHOD" == "manual" ]]; then
    have_all_addrs "$BRIDGE_IPV4_ACTIVE" "$NIC_IPV4_CURRENT" || die "Os IPv4 do bridge não migraram para '$NIC_DEV'. Abortado para proteger a sessão."
  else
    if [[ -z "${NIC_IPV4_CURRENT//[[:space:]]/}" ]]; then
      die "Bridge tinha IPv4 ativo mas a NIC '$NIC_DEV' não recebeu nenhum endereço após a migração."
    fi
  fi
else
  NIC_IPV4_CURRENT="$(ip -o -4 addr show dev "$NIC_DEV" | awk '{print $4}')"
fi

if [[ -n "${BRIDGE_IPV6_ACTIVE//[[:space:]]/}" ]]; then
  for attempt in {1..30}; do
    NIC_IPV6_CURRENT="$(ip -o -6 addr show dev "$NIC_DEV" scope global | awk '{print $4}')"
    if [[ "$IPV6_METHOD" == "manual" ]]; then
      have_all_addrs "$BRIDGE_IPV6_ACTIVE" "$NIC_IPV6_CURRENT" && break
    else
      [[ -n "${NIC_IPV6_CURRENT//[[:space:]]/}" ]] && break
    fi
    sleep 1
  done
  if [[ "$IPV6_METHOD" == "manual" ]]; then
    have_all_addrs "$BRIDGE_IPV6_ACTIVE" "$NIC_IPV6_CURRENT" || die "Os IPv6 globais do bridge não migraram para '$NIC_DEV'."
  else
    if [[ -z "${NIC_IPV6_CURRENT//[[:space:]]/}" ]]; then
      die "Bridge tinha IPv6 global ativo mas a NIC '$NIC_DEV' não obteve nenhum após a migração."
    fi
  fi
else
  NIC_IPV6_CURRENT="$(ip -o -6 addr show dev "$NIC_DEV" scope global | awk '{print $4}')"
fi

log "IPv4 ativos na NIC: $(pretty_list "$NIC_IPV4_CURRENT")"
log "IPv6 ativos na NIC: $(pretty_list "$NIC_IPV6_CURRENT")"

trap - ERR

log "A desativar o bridge '$BR_CONN'..."
nmcli -w 20 con down "$BR_CONN" 2>/dev/null || warn "Bridge '$BR_CONN' já estava down."
if ! nmcli con delete "$BR_CONN" 2>/dev/null; then
  warn "Falha ao eliminar perfil '$BR_CONN'. Verifica manualmente."
else
  log "Perfil '$BR_CONN' eliminado."
fi

if [[ -n "${SLAVE_CONN:-}" ]]; then
  log "A remover ligação bridge-slave '$SLAVE_CONN'..."
  nmcli con delete "$SLAVE_CONN" 2>/dev/null || warn "Não foi possível remover '$SLAVE_CONN'."
fi

ip addr flush dev "$BR_IF" 2>/dev/null || true
ip link set "$BR_IF" down 2>/dev/null || true
ip link delete "$BR_IF" type bridge 2>/dev/null || true

ip link set "$NIC_DEV" promisc off 2>/dev/null || true

echo
log "Estado final:"
ip addr show "$NIC_DEV" | sed 's/^/  /'
ip route | sed 's/^/  /'
bridge link | sed 's/^/  /'

echo
cat <<EOF
========================================================
✅ Reversão concluída.

• NIC ativa: $NIC_DEV  (ligação: $ETH_CONN)
• Bridge $BR_IF desmontado.

Se precisares voltar ao bridge:
  - executa novamente o script de criação.

Dicas de verificação:
  nmcli -t -f NAME,TYPE,DEVICE con show
  nmcli dev status
========================================================
EOF
