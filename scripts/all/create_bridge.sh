#!/usr/bin/env bash
set -Eeuo pipefail

# ===============================
#  Safe bridge creator for libvirt
#  Host <-> VM on same LAN
#  Default NIC: 512rede
# ===============================

NIC_DEV="${1:-512rede}"   # interface física
BR_NAME="br512"           # nome do bridge
BR_CONN="${BR_NAME}"
SLAVE_CONN="${BR_NAME}-slave-${NIC_DEV}"

# --- helpers ---
log()  { printf "\033[1;32m[INFO]\033[0m %s\n" "$*"; }
warn() { printf "\033[1;33m[WARN]\033[0m %s\n" "$*"; }
err()  { printf "\033[1;31m[ERRO]\033[0m %s\n" "$*" >&2; }
die()  { err "$*"; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "Falta o comando '$1'"; }

need nmcli
need ip
need bridge

# Requer root
if [[ $EUID -ne 0 ]]; then
  die "Corre como root: sudo $0 [NIC]."
fi

# Verificações base
ip link show "$NIC_DEV" >/dev/null 2>&1 || die "Interface '$NIC_DEV' não existe."
log "NIC: $NIC_DEV"

# Obter ligação ativa que usa esta NIC
ACTIVE_CONN="$(nmcli -t -f NAME,DEVICE con show --active | awk -F: -v d="$NIC_DEV" '$2==d{print $1; exit}')"
if [[ -z "${ACTIVE_CONN:-}" ]]; then
  # tenta qualquer ligação associada
  ACTIVE_CONN="$(nmcli -t -f NAME,DEVICE con show | awk -F: -v d="$NIC_DEV" '$2==d{print $1; exit}')"
fi
[[ -n "${ACTIVE_CONN:-}" ]] || die "Não encontrei ligação do NetworkManager para '$NIC_DEV'."

log "Ligação atual da NIC: $ACTIVE_CONN"

ACTIVE_TYPE="$(nmcli -g connection.type con show "$ACTIVE_CONN" 2>/dev/null || true)"
if [[ "$ACTIVE_TYPE" == "bridge-slave" ]]; then
  log "A interface '$NIC_DEV' já está ligada a um bridge via '$ACTIVE_CONN'. Nada a fazer."
  exit 0
fi

# Helpers que dependem de ACTIVE_CONN/BR_CONN (definidos mais abaixo)
nm_get() {
  local key="$1"
  local out
  out="$(nmcli -g "$key" con show "$ACTIVE_CONN" 2>/dev/null || true)"
  [[ -n "${out:-}" ]] || return 0
  printf '%s\n' "$out" | sed '/^\s*$/d;/^--$/d'
}

nm_copy_single() {
  local key="$1"
  local value
  value="$(nm_get "$key")"
  if ! nmcli con mod "$BR_CONN" "$key" "" 2>/dev/null; then
    warn "Não consegui limpar '$key' no perfil '$BR_CONN' (ignorar se não suportado)."
    return
  fi
  if [[ -n "${value//[[:space:]]/}" ]]; then
    local first
    first="$(printf '%s\n' "$value" | head -n1)"
    if ! nmcli con mod "$BR_CONN" "$key" "$first" 2>/dev/null; then
      warn "Não consegui aplicar '$key=$first' no perfil '$BR_CONN'."
    fi
  fi
}

nm_copy_multi() {
  local key="$1"
  local value
  value="$(nm_get "$key")"
  if ! nmcli con mod "$BR_CONN" "$key" "" 2>/dev/null; then
    warn "Não consegui limpar '$key' no perfil '$BR_CONN' (ignorar se não suportado)."
    return
  fi
  if [[ -n "${value//[[:space:]]/}" ]]; then
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      if ! nmcli con mod "$BR_CONN" +${key} "$line" 2>/dev/null; then
        warn "Não consegui adicionar '$key=$line' no perfil '$BR_CONN'."
      fi
    done <<< "$value"
  fi
}

nm_slave_copy_single() {
  local key="$1"
  local value
  value="$(nm_get "$key")"
  if ! nmcli con mod "$SLAVE_CONN" "$key" "" 2>/dev/null; then
    warn "Não consegui limpar '$key' no perfil '$SLAVE_CONN' (ignorar se não suportado)."
    return
  fi
  if [[ -n "${value//[[:space:]]/}" ]]; then
    local first
    first="$(printf '%s\n' "$value" | head -n1)"
    if ! nmcli con mod "$SLAVE_CONN" "$key" "$first" 2>/dev/null; then
      warn "Não consegui aplicar '$key=$first' no perfil '$SLAVE_CONN'."
    fi
  fi
}

pretty_list() {
  local input="$1"
  if [[ -z "${input//[[:space:]]/}" ]]; then
    printf ''
    return 0
  fi
  printf '%s' "$input" | sed ':a;N;$!ba;s/\n/, /g'
}

BR_PROFILE_CREATED=false
SLAVE_PROFILE_CREATED=false
AUTOCONNECT_MODIFIED=false

ORIG_AUTOCONNECT="$(nmcli -g connection.autoconnect con show "$ACTIVE_CONN" 2>/dev/null || echo yes)"
[[ -n "${ORIG_AUTOCONNECT:-}" ]] || ORIG_AUTOCONNECT=yes

rollback() {
  warn "Erro detetado; a reverter alterações..."
  if [[ "$AUTOCONNECT_MODIFIED" == true ]]; then
    nmcli con mod "$ACTIVE_CONN" connection.autoconnect "$ORIG_AUTOCONNECT" || true
  fi
  if [[ "$SLAVE_PROFILE_CREATED" == true ]]; then
    nmcli con down "$SLAVE_CONN" || true
    nmcli con delete "$SLAVE_CONN" || true
  fi
  if [[ "$BR_PROFILE_CREATED" == true ]]; then
    nmcli con down "$BR_CONN" || true
    nmcli con delete "$BR_CONN" || true
  fi
  warn "A tentar reativar a ligação original '$ACTIVE_CONN'..."
  nmcli -w 10 con up "$ACTIVE_CONN" || true
}

trap rollback ERR

# Fotografar endereços ativos antes da migração (para validação depois)
PHY_IPV4_ACTIVE="$(ip -o -4 addr show dev "$NIC_DEV" | awk '{print $4}')"
PHY_IPV6_ACTIVE="$(ip -o -6 addr show dev "$NIC_DEV" scope global | awk '{print $4}')"

# Capturar configuração IPv4 atual
IPV4_METHOD="$(nmcli -g ipv4.method con show "$ACTIVE_CONN" 2>/dev/null || echo auto)"
IPV4_ADDRS="$(nmcli -g ipv4.addresses con show "$ACTIVE_CONN" 2>/dev/null || true)"
IPV4_GW="$(nmcli -g ipv4.gateway con show "$ACTIVE_CONN" 2>/dev/null || true)"
IPV4_DNS="$(nmcli -g ipv4.dns con show "$ACTIVE_CONN" 2>/dev/null || true)"

IPV6_METHOD="$(nmcli -g ipv6.method con show "$ACTIVE_CONN" 2>/dev/null || echo auto)"
IPV6_ADDRS="$(nmcli -g ipv6.addresses con show "$ACTIVE_CONN" 2>/dev/null || true)"
IPV6_GW="$(nmcli -g ipv6.gateway con show "$ACTIVE_CONN" 2>/dev/null || true)"
IPV6_DNS="$(nmcli -g ipv6.dns con show "$ACTIVE_CONN" 2>/dev/null || true)"

[[ -n "${IPV4_METHOD:-}" ]] || IPV4_METHOD=auto
[[ -n "${IPV6_METHOD:-}" ]] || IPV6_METHOD=auto

log "ipv4.method=$IPV4_METHOD addr='${IPV4_ADDRS:-}' gw='${IPV4_GW:-}' dns='${IPV4_DNS:-}'"
log "ipv6.method=$IPV6_METHOD addr='${IPV6_ADDRS:-}' gw='${IPV6_GW:-}' dns='${IPV6_DNS:-}'"

# Criar bridge se não existir
if nmcli -t -f NAME con show | grep -Fxq "$BR_CONN"; then
  log "Bridge '$BR_CONN' já existe (perfil NM)."
else
  log "A criar bridge '$BR_NAME'..."
  nmcli connection add type bridge ifname "$BR_NAME" con-name "$BR_CONN" \
    bridge.stp no bridge.forward-delay 0
  BR_PROFILE_CREATED=true
fi

log "A sincronizar parâmetros do bridge com a ligação atual..."
nmcli con mod "$BR_CONN" connection.autoconnect yes connection.autoconnect-slaves 1 \
  bridge.stp no bridge.forward-delay 0
nmcli con mod "$BR_CONN" connection.interface-name "$BR_NAME"
nm_copy_single "connection.metered"
nm_copy_single "connection.zone"
nm_copy_single "connection.mdns"
nm_copy_single "connection.llmnr"
nm_copy_single "connection.autoconnect-priority"

BRIDGE_MTU="$(nm_get "802-3-ethernet.mtu")"
if [[ -n "${BRIDGE_MTU//[[:space:]]/}" ]]; then
  if ! nmcli con mod "$BR_CONN" bridge.mtu "$BRIDGE_MTU" 2>/dev/null; then
    warn "Não consegui aplicar bridge.mtu='$BRIDGE_MTU' no perfil '$BR_CONN'."
  fi
else
  nmcli con mod "$BR_CONN" bridge.mtu "" 2>/dev/null || warn "Não consegui limpar bridge.mtu no perfil '$BR_CONN'."
fi

CLONED_MAC_VALUE="$(nm_get "802-3-ethernet.cloned-mac-address")"
if [[ -n "${CLONED_MAC_VALUE//[[:space:]]/}" ]]; then
  if ! nmcli con mod "$BR_CONN" bridge.mac-address "$CLONED_MAC_VALUE" 2>/dev/null; then
    warn "Não consegui aplicar bridge.mac-address='$CLONED_MAC_VALUE' no perfil '$BR_CONN'."
  fi
else
  nmcli con mod "$BR_CONN" bridge.mac-address "" 2>/dev/null || warn "Não consegui limpar bridge.mac-address no perfil '$BR_CONN'."
fi

# IPv4
nmcli con mod "$BR_CONN" ipv4.method "$IPV4_METHOD"
if [[ "$IPV4_METHOD" == "manual" ]]; then
  log "Clonagem IPv4 manual para o bridge..."
  if ! nmcli con mod "$BR_CONN" ipv4.addresses "" 2>/dev/null; then
    warn "Falha ao limpar ipv4.addresses no bridge."
  fi
  if [[ -n "${IPV4_ADDRS//[[:space:]]/}" ]]; then
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      if ! nmcli con mod "$BR_CONN" +ipv4.addresses "$line" 2>/dev/null; then
        warn "Falha ao aplicar ipv4.addresses='$line' no bridge."
      fi
    done <<< "$IPV4_ADDRS"
  else
    warn "Ligação original usa IPv4 manual mas sem endereços definidos."
  fi
  if [[ -n "${IPV4_GW//[[:space:]]/}" ]]; then
    nmcli con mod "$BR_CONN" ipv4.gateway "$IPV4_GW"
  else
    nmcli con mod "$BR_CONN" ipv4.gateway ""
  fi
else
  log "Bridge fica com método IPv4='$IPV4_METHOD' (sem endereços manuais)."
  nmcli con mod "$BR_CONN" ipv4.addresses "" ipv4.gateway ""
fi

nm_copy_multi "ipv4.dns"
nm_copy_multi "ipv4.dns-search"
nm_copy_multi "ipv4.routes"
nm_copy_single "ipv4.route-metric"
nm_copy_single "ipv4.never-default"
nm_copy_single "ipv4.may-fail"
nm_copy_single "ipv4.ignore-auto-dns"
nm_copy_single "ipv4.ignore-auto-routes"
nm_copy_single "ipv4.dhcp-client-id"
nm_copy_single "ipv4.dhcp-hostname"
nm_copy_single "ipv4.dhcp-send-hostname"
nm_copy_single "ipv4.dhcp-timeout"
nm_copy_single "ipv4.dhcp-fqdn"

# IPv6
nmcli con mod "$BR_CONN" ipv6.method "$IPV6_METHOD"
if [[ "$IPV6_METHOD" == "manual" ]]; then
  log "Clonagem IPv6 manual para o bridge..."
  if ! nmcli con mod "$BR_CONN" ipv6.addresses "" 2>/dev/null; then
    warn "Falha ao limpar ipv6.addresses no bridge."
  fi
  if [[ -n "${IPV6_ADDRS//[[:space:]]/}" ]]; then
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      if ! nmcli con mod "$BR_CONN" +ipv6.addresses "$line" 2>/dev/null; then
        warn "Falha ao aplicar ipv6.addresses='$line' no bridge."
      fi
    done <<< "$IPV6_ADDRS"
  fi
  if [[ -n "${IPV6_GW//[[:space:]]/}" ]]; then
    nmcli con mod "$BR_CONN" ipv6.gateway "$IPV6_GW"
  else
    nmcli con mod "$BR_CONN" ipv6.gateway ""
  fi
else
  log "Bridge fica com método IPv6='$IPV6_METHOD'."
  nmcli con mod "$BR_CONN" ipv6.addresses "" ipv6.gateway ""
fi

nm_copy_multi "ipv6.dns"
nm_copy_multi "ipv6.dns-search"
nm_copy_multi "ipv6.routes"
nm_copy_single "ipv6.route-metric"
nm_copy_single "ipv6.never-default"
nm_copy_single "ipv6.may-fail"
nm_copy_single "ipv6.ignore-auto-dns"
nm_copy_single "ipv6.ignore-auto-routes"
nm_copy_single "ipv6.addr-gen-mode"
nm_copy_single "ipv6.ip6-privacy"
nm_copy_single "ipv6.ra-timeout"
nm_copy_single "ipv6.dhcp-duid"
nm_copy_single "ipv6.dhcp-hostname"
nm_copy_single "ipv6.dhcp-iaid"

# Criar/atualizar ligação bridge-slave para a NIC
if nmcli -t -f NAME con show | grep -Fxq "$SLAVE_CONN"; then
  log "Ligação slave '$SLAVE_CONN' já existe; a garantir que aponta para $BR_CONN..."
  SLAVE_TYPE="$(nmcli -g connection.type con show "$SLAVE_CONN" 2>/dev/null || true)"
  if [[ "$SLAVE_TYPE" != "bridge-slave" ]]; then
    die "Ligação '$SLAVE_CONN' já existe mas não é do tipo bridge-slave (tipo atual: '$SLAVE_TYPE')."
  fi
  nmcli con mod "$SLAVE_CONN" master "$BR_CONN" connection.autoconnect yes connection.interface-name "$NIC_DEV" connection.slave-type bridge
else
  log "A criar ligação slave '$SLAVE_CONN' para $NIC_DEV..."
  nmcli connection add type bridge-slave ifname "$NIC_DEV" master "$BR_CONN" con-name "$SLAVE_CONN"
  SLAVE_PROFILE_CREATED=true
fi

nm_slave_copy_single "connection.metered"
nm_slave_copy_single "connection.zone"
nm_slave_copy_single "connection.mdns"
nm_slave_copy_single "connection.llmnr"
nm_slave_copy_single "connection.autoconnect-priority"
nm_slave_copy_single "802-3-ethernet.mtu"
nm_slave_copy_single "802-3-ethernet.cloned-mac-address"
nm_slave_copy_single "802-3-ethernet.wake-on-lan"
nm_slave_copy_single "802-3-ethernet.wake-on-lan-password"

# Subir o bridge (NetworkManager faz a transição de forma atómica)
log "A ativar o bridge '$BR_CONN' (pode haver 1–3s de latência na rede)..."
nmcli -w 20 con up "$BR_CONN"

# Validar que os endereços migraram para o bridge antes de mexer mais
BR_CURRENT_V4=""
BR_CURRENT_V6=""

if [[ "$IPV4_METHOD" == "manual" && -n "${IPV4_ADDRS//[[:space:]]/}" ]]; then
  IPv4_OK=false
  for attempt in {1..10}; do
    BR_CURRENT_V4="$(ip -o -4 addr show dev "$BR_NAME" | awk '{print $4}')"
    MISSING_V4=false
    while IFS= read -r addr; do
      [[ -z "$addr" ]] && continue
      if ! grep -Fxq "$addr" <<< "$BR_CURRENT_V4"; then
        MISSING_V4=true
        break
      fi
    done <<< "$IPV4_ADDRS"
    if [[ "$MISSING_V4" == false ]]; then
      IPv4_OK=true
      break
    fi
    sleep 1
  done
  if [[ "$IPv4_OK" != true ]]; then
    die "Endereços IPv4 esperados não apareceram no bridge '$BR_NAME' — abortado para evitar perda de conectividade."
  fi
else
  for attempt in {1..10}; do
    BR_CURRENT_V4="$(ip -o -4 addr show dev "$BR_NAME" | awk '{print $4}')"
    if [[ -n "${BR_CURRENT_V4//[[:space:]]/}" || -z "${PHY_IPV4_ACTIVE//[[:space:]]/}" ]]; then
      break
    fi
    sleep 1
  done
  if [[ -n "${PHY_IPV4_ACTIVE//[[:space:]]/}" && -z "${BR_CURRENT_V4//[[:space:]]/}" ]]; then
    die "Bridge '$BR_NAME' não obteve nenhum IPv4 após migração (endereços antigos: $(pretty_list "$PHY_IPV4_ACTIVE"))."
  fi
fi

if [[ "$IPV6_METHOD" == "manual" && -n "${IPV6_ADDRS//[[:space:]]/}" ]]; then
  IPv6_OK=false
  for attempt in {1..10}; do
    BR_CURRENT_V6="$(ip -o -6 addr show dev "$BR_NAME" scope global | awk '{print $4}')"
    MISSING_V6=false
    while IFS= read -r addr; do
      [[ -z "$addr" ]] && continue
      if ! grep -Fxq "$addr" <<< "$BR_CURRENT_V6"; then
        MISSING_V6=true
        break
      fi
    done <<< "$IPV6_ADDRS"
    if [[ "$MISSING_V6" == false ]]; then
      IPv6_OK=true
      break
    fi
    sleep 1
  done
  if [[ "$IPv6_OK" != true ]]; then
    die "Endereços IPv6 esperados não apareceram no bridge '$BR_NAME'."
  fi
else
  for attempt in {1..10}; do
    BR_CURRENT_V6="$(ip -o -6 addr show dev "$BR_NAME" scope global | awk '{print $4}')"
    if [[ -n "${BR_CURRENT_V6//[[:space:]]/}" || -z "${PHY_IPV6_ACTIVE//[[:space:]]/}" ]]; then
      break
    fi
    sleep 1
  done
  if [[ -n "${PHY_IPV6_ACTIVE//[[:space:]]/}" && -z "${BR_CURRENT_V6//[[:space:]]/}" ]]; then
    die "Bridge '$BR_NAME' não obteve IPv6 global após migração (endereços antigos: $(pretty_list "$PHY_IPV6_ACTIVE"))."
  fi
fi

if [[ -n "${BR_CURRENT_V4//[[:space:]]/}" ]]; then
  log "IPv4 ativos no bridge: $(pretty_list "$BR_CURRENT_V4")"
else
  log "Bridge sem IPv4 ativos atualmente."
fi

if [[ -n "${BR_CURRENT_V6//[[:space:]]/}" ]]; then
  log "IPv6 ativos no bridge: $(pretty_list "$BR_CURRENT_V6")"
else
  log "Bridge sem IPv6 globais ativos atualmente."
fi

if [[ "$ACTIVE_CONN" != "$BR_CONN" && "$ORIG_AUTOCONNECT" == "yes" ]]; then
  log "A desativar autoconnect da ligação antiga '$ACTIVE_CONN' (somente após sucesso)."
  if nmcli con mod "$ACTIVE_CONN" connection.autoconnect no; then
    AUTOCONNECT_MODIFIED=true
  else
    warn "Falha ao desativar autoconnect da ligação '$ACTIVE_CONN'. Recomendo validar manualmente."
  fi
fi

# Garantir que a NIC física não mantém IP direto (o NM geralmente já trata disto)
log "A limpar IP direto na NIC física (se existir)..."
ip addr flush dev "$NIC_DEV" || true

# Verificação básica de rota/gateway (não falha o script se não houver ICMP)
GW="${IPV4_GW:-$(ip route | awk '/^default/ {print $3; exit}')}"
if [[ -n "${GW:-}" ]]; then
  log "A testar conectividade ao gateway $GW (2 pings)..."
  if ping -c 2 -W 2 "$GW" >/dev/null 2>&1; then
    log "Conectividade OK."
  else
    warn "Não obtive resposta ICMP do gateway (pode estar a bloquear ping). Continua."
  fi
else
  warn "Não consegui detetar gateway para teste — a continuar."
fi

GW6="${IPV6_GW:-$(ip -6 route | awk '/^default/ {print $3; exit}')}"
if [[ -n "${GW6//[[:space:]]/}" ]]; then
  log "A testar conectividade IPv6 ao gateway $GW6 (2 pings)..."
  if ping -6 -c 2 -W 2 "$GW6" >/dev/null 2>&1; then
    log "Conectividade IPv6 OK."
  else
    warn "Gateway IPv6 não respondeu a ping (pode ser esperado se não aceita ICMP)."
  fi
else
  warn "Não consegui detetar gateway IPv6 para teste — a continuar."
fi

# Mostrar estado final
echo
log "Estado final:"
ip addr show "$BR_NAME" | sed 's/^/  /'
bridge link | sed 's/^/  /'
