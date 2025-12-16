#!/usr/bin/env bash
# Diagnóstico rápido para stack macvtap + dnsmasq + NAT configurados por setup_dhcp.sh.

set -uo pipefail

info(){ printf '[INFO] %s\n' "$*"; }
ok(){ printf '[ OK ] %s\n' "$*"; }
warn(){ printf '[WARN] %s\n' "$*" >&2; }
fail(){ printf '[FAIL] %s\n' "$*" >&2; EXITCODE=1; }

EXITCODE=0

[[ ${EUID:-0} -eq 0 ]] || { fail 'Executa como root para verificar firewall/iptables.'; echo; exit "${EXITCODE}"; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

LAN_PARENT_IF="${LAN_PARENT_IF:-512rede}"
LAN_INTERFACE_NAME="${LAN_INTERFACE_NAME:-${LAN_PARENT_IF}-host}"
NETWORK_NAME="${LAN_INTERFACE_NAME}"

SUBNET_CIDR="${SUBNET_CIDR:-192.168.76.0/24}"
GATEWAY_IP="${GATEWAY_IP:-192.168.76.1}"
DHCP_RANGE_START="${DHCP_RANGE_START:-192.168.76.50}"
DHCP_RANGE_END="${DHCP_RANGE_END:-192.168.76.200}"

DNSMASQ_CONF_DIR="${DNSMASQ_CONF_DIR:-/etc/dnsmasq.d}"
DNSMASQ_LEASE_DIR="${DNSMASQ_LEASE_DIR:-/var/lib/dnsmasq}"
DEDICATED_UNIT="dnsmasq-${NETWORK_NAME}.service"

find_wan_iface(){ ip route show default 0.0.0.0/0 | awk '/default/ {print $5; exit}'; }
WAN_IF_INPUT="${CLI_WAN_IF:-${WAN_IF:-}}"
WAN_IF="${WAN_IF_INPUT:-$(find_wan_iface)}"

IFS=/ read -r SUBNET_BASE SUBNET_PREFIX <<<"${SUBNET_CIDR}"
prefix_to_mask(){
  local p=$1
  ((p==0)) && { printf '0.0.0.0'; return; }
  local m=$((0xffffffff^((1<<(32-p))-1)))
  printf '%d.%d.%d.%d' $(((m>>24)&255)) $(((m>>16)&255)) $(((m>>8)&255)) $((m&255))
}
NETMASK="$(prefix_to_mask "${SUBNET_PREFIX}")"
SUBNET_NETWORK="${SUBNET_BASE}/${SUBNET_PREFIX}"

info "A verificar configuração para ${NETWORK_NAME} (gateway ${GATEWAY_IP}, range ${DHCP_RANGE_START}-${DHCP_RANGE_END})."

if [[ -z ${WAN_IF} ]]; then
  fail "Interface WAN não foi detetada."
else
  ok "Interface WAN detetada: ${WAN_IF}"
fi

# Interface macvtap
if ip link show "${NETWORK_NAME}" >/dev/null 2>&1; then
  ok "Interface ${NETWORK_NAME} existe."
  if ip -4 addr show "${NETWORK_NAME}" | grep -q "${GATEWAY_IP}/"; then
    ok "Interface ${NETWORK_NAME} tem IPv4 ${GATEWAY_IP}/${SUBNET_PREFIX}."
  else
    fail "Interface ${NETWORK_NAME} não tem IPv4 ${GATEWAY_IP}/${SUBNET_PREFIX}."
  fi
  link_state=$(ip -o link show "${NETWORK_NAME}" | awk '{for(i=1;i<=NF;i++) if ($i=="state") {print $(i+1); exit}}')
  [[ ${link_state:-UNKNOWN} == "UP" ]] && ok "Interface ${NETWORK_NAME} encontra-se UP." || warn "Interface ${NETWORK_NAME} está em estado ${link_state:-desconhecido}."
else
  fail "Interface ${NETWORK_NAME} não existe."
fi

# Parent em promisc
if ip link show "${LAN_PARENT_IF}" >/dev/null 2>&1; then
  ok "Interface parent ${LAN_PARENT_IF} encontrado."
  if ip link show "${LAN_PARENT_IF}" | grep -q "PROMISC"; then
    ok "Interface parent ${LAN_PARENT_IF} em modo promíscuo."
  else
    warn "Interface parent ${LAN_PARENT_IF} NÃO está em modo promíscuo."
  fi
else
  warn "Interface parent ${LAN_PARENT_IF} não encontrada."
fi

# dnsmasq service
if systemctl cat "${DEDICATED_UNIT}" >/dev/null 2>&1; then
  if systemctl is-active --quiet "${DEDICATED_UNIT}"; then
    ok "Serviço ${DEDICATED_UNIT} ativo."
  else
    fail "Serviço ${DEDICATED_UNIT} não está ativo."
    systemctl --no-pager --lines=20 status "${DEDICATED_UNIT}" || true
  fi
else
  fail "Serviço ${DEDICATED_UNIT} não existe."
fi

# Portas
if command -v ss >/dev/null 2>&1; then
  if ss -H -lnp 'sport = :53' 2>/dev/null | grep -q "dnsmasq"; then
    ok "dnsmasq a escutar na porta 53."
  else
    fail "Nenhum dnsmasq a escutar na porta 53."
  fi
  if ss -H -lnp 'sport = :67' 2>/dev/null | grep -q "dnsmasq"; then
    ok "dnsmasq a escutar na porta 67."
  else
    fail "Nenhum dnsmasq a escutar na porta 67."
  fi
else
  warn "Comando 'ss' indisponível; saltar verificação de portas."
fi

# Lease file
LEASE_FILE="${DNSMASQ_LEASE_DIR}/${NETWORK_NAME}.leases"
if [[ -w ${LEASE_FILE} ]]; then
  ok "Lease file ${LEASE_FILE} com permissões de escrita."
  lease_count=$(wc -l <"${LEASE_FILE}")
  info "Contagem de leases atuais: ${lease_count}"
else
  fail "Lease file ${LEASE_FILE} não escrevível."
fi

# Sysctl checks
if command -v sysctl >/dev/null 2>&1; then
  if [[ $(sysctl -n net.ipv4.ip_forward 2>/dev/null || echo 0) -eq 1 ]]; then
    ok "net.ipv4.ip_forward ativo."
  else
    fail "net.ipv4.ip_forward NÃO está ativo."
  fi
  if [[ $(sysctl -n "net.ipv4.conf.${NETWORK_NAME}.rp_filter" 2>/dev/null || echo 1) -eq 0 ]]; then
    ok "rp_filter relaxado em ${NETWORK_NAME}."
  else
    warn "rp_filter não relaxado para ${NETWORK_NAME}."
  fi
else
  warn "sysctl indisponível; não foi possível validar ip_forward/rp_filter."
fi

# iptables / NAT
if [[ -n ${WAN_IF} ]]; then
  if command -v iptables >/dev/null 2>&1; then
    if iptables -t nat -C POSTROUTING -s "${SUBNET_NETWORK}" -o "${WAN_IF}" -j MASQUERADE >/dev/null 2>&1; then
      ok "iptables: regra MASQUERADE presente (${SUBNET_NETWORK} -> ${WAN_IF})."
    else
      fail "iptables: falta MASQUERADE (${SUBNET_NETWORK} -> ${WAN_IF})."
    fi
    if iptables -C FORWARD -i "${WAN_IF}" -o "${NETWORK_NAME}" -m state --state RELATED,ESTABLISHED -j ACCEPT >/dev/null 2>&1 && \
       iptables -C FORWARD -i "${NETWORK_NAME}" -o "${WAN_IF}" -j ACCEPT >/dev/null 2>&1; then
      ok "iptables: regras de forward presentes."
    else
      fail "iptables: regras de forward ausentes."
    fi
  else
    warn "iptables indisponível; verifica NAT manualmente."
  fi
fi

# Conectividade básica (opcional)
if command -v ping >/dev/null 2>&1; then
  if ping -I "${NETWORK_NAME}" -c1 -W1 "${GATEWAY_IP}" >/dev/null 2>&1; then
    ok "Ping loopback no gateway ${GATEWAY_IP} bem-sucedido."
  else
    warn "Falha no ping ${GATEWAY_IP} a partir da interface ${NETWORK_NAME}."
  fi
else
  warn "ping indisponível; não foi possível testar conectividade básica."
fi

echo
if (( EXITCODE == 0 )); then
  info "Todas as verificações passaram."
else
  warn "Foram detetados problemas (EXIT=${EXITCODE})."
fi

exit "${EXITCODE}"
if [[ -z ${WAN_IF} ]]; then
  warn "Sem interface WAN definida/detetada; NAT não foi validado."
fi
