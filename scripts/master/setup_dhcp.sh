#!/usr/bin/env bash
# DHCP + NAT endurecido e persistente para 1 segmento LAN com macvtap.
# - Desativa dnsmasq global e corre instância dedicada por-Interface.
# - Remove e substitui configs antigas conflituosas (serviços, drop-ins, NAT antigo).
# - Garante ip_forward, rp_filter relaxado, firewalld (ou iptables) e persistência.

set -euo pipefail

truthy(){ local v="${1:-}"; case "${v,,}" in 1|true|yes|on) return 0;; esac; return 1; }
info(){ printf '[INFO] %s\n' "$*"; }
warn(){ printf '[WARN] %s\n' "$*" >&2; }
fatal(){ printf '[ERROR] %s\n' "$*" >&2; exit 1; }

usage(){
cat <<'USAGE'
Uso: sudo ./setup_dhcp.sh [WAN_IFACE]

  - LAN parent por defeito: 512rede; macvtap child: 512rede-host
  - WAN_IFACE: interface de saída para NAT (autodetecta se omitido)

Override por variáveis de ambiente: LAN_PARENT_IF, LAN_INTERFACE_NAME, SUBNET_CIDR,
GATEWAY_IP, DHCP_RANGE_START, DHCP_RANGE_END, WAN_IF, etc.
USAGE
exit 1; }

[[ ${EUID:-0} -eq 0 ]] || fatal 'Requer root.'
[[ -r /etc/os-release ]] || fatal 'Sem /etc/os-release.'
. /etc/os-release
if [[ "${ID,,}" != "fedora" && ! ${ID_LIKE:-} =~ fedora ]]; then
  fatal 'Este script foi feito para Fedora-like.'
fi

case "${1:-}" in -h|--help) usage;; esac
CLI_WAN_IF="${1:-}"

# --- Definições (podes overriding por env) ------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MACVTAP_HELPER="${SCRIPT_DIR}/create_macvtap.sh"
[[ -x "${MACVTAP_HELPER}" ]] || fatal "Helper em falta: ${MACVTAP_HELPER}"

LAN_PARENT_IF="${LAN_PARENT_IF:-512rede}"
LAN_INTERFACE_NAME="${LAN_INTERFACE_NAME:-${LAN_PARENT_IF}-host}"
NETWORK_NAME="$LAN_INTERFACE_NAME"

SUBNET_CIDR="${SUBNET_CIDR:-192.168.76.0/24}"
GATEWAY_IP="${GATEWAY_IP:-192.168.76.1}"
DHCP_RANGE_START="${DHCP_RANGE_START:-192.168.76.50}"
DHCP_RANGE_END="${DHCP_RANGE_END:-192.168.76.200}"

RESOLV_CONF="${RESOLV_CONF:-/etc/resolv.conf}"
DNSMASQ_CONF_DIR="${DNSMASQ_CONF_DIR:-/etc/dnsmasq.d}"
DNSMASQ_LEASE_DIR="${DNSMASQ_LEASE_DIR:-/var/lib/dnsmasq}"

SYSCTL_CONF="/etc/sysctl.d/99-${NETWORK_NAME}-ipfwd-rpf.conf"
DEDICATED_UNIT="dnsmasq-${NETWORK_NAME}.service"
NAT_UNIT="${NETWORK_NAME}-nat.service"
MACVTAP_PERSIST="${MACVTAP_PERSIST:-1}"

command -v ip >/dev/null || fatal 'Falta iproute2.'
command -v dnsmasq >/dev/null || fatal 'Falta dnsmasq.'
command -v nmcli >/dev/null 2>&1 || warn 'nmcli ausente (persistência NM limitada).'

# --- Helpers CIDR -------------------------------------------------------------
cidr_prefix=${SUBNET_CIDR#*/}; network_base=${SUBNET_CIDR%/*}
[[ $cidr_prefix =~ ^[0-9]+$ ]] || fatal "SUBNET_CIDR inválido: ${SUBNET_CIDR}"
cidr_prefix=$((10#${cidr_prefix})); (( cidr_prefix>=0 && cidr_prefix<=32 )) || fatal "SUBNET_CIDR inválido."

prefix_to_mask(){ local p=$1; ((p==0)) && { printf '0.0.0.0'; return; }; local m=$((0xffffffff^((1<<(32-p))-1))); printf '%d.%d.%d.%d' $(((m>>24)&255)) $(((m>>16)&255)) $(((m>>8)&255)) $((m&255)); }
ip_to_int(){ local IFS=.; read -r a b c d <<<"$1"; printf '%u' $(( (a<<24)|(b<<16)|(c<<8)|d )); }
int_to_ip(){ local v=$1; printf '%d.%d.%d.%d' $(((v>>24)&255)) $(((v>>16)&255)) $(((v>>8)&255)) $((v&255)); }
mask_int=$(( cidr_prefix==0 ? 0 : 0xffffffff ^ ((1<<(32-cidr_prefix))-1) ))
network_int=$(( $(ip_to_int "${network_base}") & mask_int ))
network_address=$(int_to_ip "${network_int}")
NETMASK=$(prefix_to_mask "${cidr_prefix}")
SUBNET_NETWORK="${network_address}/${cidr_prefix}"

# --- Cria/Recria macvtap (e remove IP duplicado no parent dentro desta subnet)
ensure_macvtap(){
  local ip_cidr="${GATEWAY_IP}/${cidr_prefix}"
  local args=()
  truthy "${MACVTAP_PERSIST}" && args+=(--persist)

  info "A (re)criar macvtap ${LAN_INTERFACE_NAME} em ${LAN_PARENT_IF}"
  ip link show "${LAN_PARENT_IF}" >/dev/null 2>&1 || fatal "Parent '${LAN_PARENT_IF}' não existe."

  # Remove QUALQUER IPv4 do parent que pertença à nossa SUBNET (evita duplicação com o child)
  while read -r addr; do
    [[ -z "${addr}" ]] && continue
    ip -4 addr del "${addr}" dev "${LAN_PARENT_IF}" || true
    warn "Removido IPv4 ${addr} do parent ${LAN_PARENT_IF} (pertencia à ${SUBNET_NETWORK})"
  done < <(ip -4 -o addr show dev "${LAN_PARENT_IF}" | awk -v net="${SUBNET_NETWORK}" '
    {print $4}
    ' | while read -r a; do
          # filtro simples por prefixo igual ao da subnet (ex.: 192.168.76.)
          base="${a%/*}"; echo "$base/${a#*/}"
        done | awk -v n="${network_base}" -v p="${cidr_prefix}" '
            BEGIN{
              split(n,b,"."); net=(b[1]*256*256*256)+(b[2]*256*256)+(b[3]*256)+b[4];
              mask=(p==0?0:(2^32-1) - (2^(32-p)-1));
            }
            {
              split($0,ipm,"/"); split(ipm[1],q,".");
              ip=(q[1]*256*256*256)+(q[2]*256*256)+(q[3]*256)+q[4];
              if ((and(ip,mask))==net) print ipm[1]"/"p;
            }')

  "${MACVTAP_HELPER}" "${args[@]}" "${LAN_PARENT_IF}" "${LAN_INTERFACE_NAME}" "${ip_cidr}"
}
ensure_macvtap

ip link show "${NETWORK_NAME}" >/dev/null 2>&1 || fatal "Interface '${NETWORK_NAME}' não encontrada."

# --- Limpeza agressiva de artefactos antigos ---------------------------------
cleanup_for_network(){
  info "A limpar artefactos antigos para ${NETWORK_NAME}"

  install -d -m 755 "${DNSMASQ_CONF_DIR}" "${DNSMASQ_LEASE_DIR}"
  rm -f "${DNSMASQ_CONF_DIR}/${NETWORK_NAME}.conf" "${DNSMASQ_LEASE_DIR}/${NETWORK_NAME}.leases"

  systemctl disable --now "${DEDICATED_UNIT}" >/dev/null 2>&1 || true

  rm -f "/etc/systemd/system/dnsmasq.service.d/${NETWORK_NAME}-wait.conf"
  rmdir --ignore-fail-on-non-empty "/etc/systemd/system/dnsmasq.service.d" 2>/dev/null || true

  if systemctl list-unit-files | grep -q '^dnsmasq\.service'; then
    systemctl disable --now dnsmasq.service >/dev/null 2>&1 || true
  fi

  pkill -f "dnsmasq.*${NETWORK_NAME}" >/dev/null 2>&1 || true

  systemctl disable --now "${NAT_UNIT}" >/dev/null 2>&1 || true
  rm -f "/etc/systemd/system/${NAT_UNIT}"

  # NM: apaga perfis do child; parent mantém-se (apenas limpamos IPs já feito acima)
  if command -v nmcli >/dev/null 2>&1; then
    while read -r uuid name; do
      [[ -z ${uuid} ]] && continue
      info "NM: a remover perfil '${name}' do device ${NETWORK_NAME}"
      nmcli connection delete uuid "${uuid}" >/dev/null 2>&1 || true
    done < <(nmcli -t -f UUID,NAME,DEVICE connection show | awk -F: -v dev="${NETWORK_NAME}" '$3==dev{print $1" "$2}')
  fi

  # Força estado e IPv4 do child
  ip addr flush dev "${NETWORK_NAME}" || true
  ip link set "${NETWORK_NAME}" up
  ip addr add "${GATEWAY_IP}/${cidr_prefix}" dev "${NETWORK_NAME}" valid_lft forever preferred_lft forever

  # Parent em promisc para encaminhamento estável
  ip link set "${LAN_PARENT_IF}" promisc on || true
}
cleanup_for_network

kill_conflicting_dns(){
  command -v ss >/dev/null 2>&1 || { warn "Sem utilitário 'ss' para detetar conflitos de portas."; return; }
  local ports=(53 67)
  for port in "${ports[@]}"; do
    while IFS=' ' read -r pid exe addr; do
      [[ -z "${pid}" ]] && continue
      case "${exe}" in
        dnsmasq)
          info "A terminar dnsmasq pré-existente (PID ${pid}) que ocupava ${addr}"
          kill "${pid}" >/dev/null 2>&1 || true
          sleep 0.5
          kill -9 "${pid}" >/dev/null 2>&1 || true
          ;;
        *)
          fatal "Porta ${port}/${addr} ocupada por PID ${pid} (${exe}). Liberta-a antes de continuar."
          ;;
      esac
    done < <(ss -H -lunp "sport = :${port}" 2>/dev/null | awk -v ip="${GATEWAY_IP}" -v p="${port}" '
      {
        local_addr=$5
        match(local_addr, /:([0-9]+)$/, mport)
        if (!mport[1] || mport[1] != p) next
        if (local_addr ~ ("^"ip":") || (p == 53 && local_addr ~ "^0\\.0\\.0\\.0:")) {
          if (match($0, /pid=([0-9]+)/, m) && match($0, /\"([^\"]+)\"/, c)) {
            printf "%s %s %s\n", m[1], c[1], local_addr
          }
        }
      }')
  done
}
kill_conflicting_dns

# --- dnsmasq dedicado (só lê o ficheiro deste network) -----------------------
DNSMASQ_CONF="${DNSMASQ_CONF_DIR}/${NETWORK_NAME}.conf"
info "A escrever ${DNSMASQ_CONF}"
cat >"${DNSMASQ_CONF}" <<CFG
# Auto-gerado para ${NETWORK_NAME} — NÃO EDITAR À MÃO
interface=${NETWORK_NAME}
bind-interfaces
domain-needed
bogus-priv
# DHCP
dhcp-authoritative
dhcp-range=${DHCP_RANGE_START},${DHCP_RANGE_END},${NETMASK},infinite
dhcp-option=option:router,${GATEWAY_IP}
dhcp-option=option:dns-server,${GATEWAY_IP}
dhcp-leasefile=${DNSMASQ_LEASE_DIR}/${NETWORK_NAME}.leases
# DNS forwarders
resolv-file=${RESOLV_CONF}
log-dhcp
CFG

dnsmasq --test -C "${DNSMASQ_CONF}" >/dev/null || fatal "Teste de configuração falhou: ${DNSMASQ_CONF}"

# --- ip_forward + rp_filter relaxado -----------------------------------------
info "A ativar ip_forward e a relaxar rp_filter"
cat >"${SYSCTL_CONF}" <<SYSCTL
net.ipv4.ip_forward = 1
net.ipv4.conf.all.rp_filter = 0
net.ipv4.conf.default.rp_filter = 0
net.ipv4.conf.${NETWORK_NAME}.rp_filter = 0
SYSCTL
sysctl --system >/dev/null

# --- WAN detection ------------------------------------------------------------
find_wan_iface(){ ip route show default 0.0.0.0/0 | awk '/default/ {print $5; exit}'; }
WAN_IF_INPUT="${CLI_WAN_IF:-${WAN_IF:-}}"
WAN_IF="${WAN_IF_INPUT:-$(find_wan_iface)}"
[[ -n ${WAN_IF} ]] || fatal 'Não foi possível detetar a interface WAN.'
[[ "${WAN_IF}" != "${NETWORK_NAME}" ]] || fatal 'WAN não pode ser a mesma que a interface DHCP.'
ip link show "${WAN_IF}" >/dev/null 2>&1 || fatal "WAN '${WAN_IF}' não existe."

# --- NAT via firewalld (preferido) ou iptables (fallback) --------------------
apply_firewalld(){
  command -v firewall-cmd >/dev/null 2>&1 || return 1
  local default_zone
  default_zone=$(firewall-cmd --get-default-zone) || { warn "firewalld: não foi possível obter a zona por defeito."; return 1; }
  info "A configurar firewalld (zona por defeito=${default_zone}, LAN=${NETWORK_NAME})"

  firewall-cmd --permanent --zone="${default_zone}" --remove-interface="${NETWORK_NAME}" >/dev/null 2>&1 || true
  if firewall-cmd --permanent --zone=trusted --add-interface="${NETWORK_NAME}" >/dev/null 2>&1; then
    firewall-cmd --zone=trusted --add-interface="${NETWORK_NAME}" >/dev/null 2>&1 || warn "firewalld: interface runtime '${NETWORK_NAME}' na zona trusted falhou."
  else
    warn "firewalld: não conseguiu associar interface '${NETWORK_NAME}' à zona trusted; a usar fallback por subnet."
    firewall-cmd --permanent --zone=trusted --add-source="${SUBNET_NETWORK}" >/dev/null || warn "firewalld: fallback --permanent --add-source falhou."
    firewall-cmd --zone=trusted --add-source="${SUBNET_NETWORK}" >/dev/null 2>&1 || warn "firewalld: fallback runtime --add-source falhou."
  fi
  if ! firewall-cmd --permanent --zone="${default_zone}" --add-masquerade >/dev/null 2>&1; then
    warn "firewalld: não conseguiu ativar masquerade na zona ${default_zone}."
    return 1
  fi
  firewall-cmd --zone="${default_zone}" --add-masquerade >/dev/null 2>&1 || warn "firewalld: masquerade runtime na zona ${default_zone} falhou."

  firewall-cmd --permanent --zone=trusted --add-service=dhcp >/dev/null 2>&1 || warn "firewalld: não conseguiu adicionar serviço DHCP (permanent)."
  firewall-cmd --permanent --zone=trusted --add-service=dns  >/dev/null 2>&1 || warn "firewalld: não conseguiu adicionar serviço DNS (permanent)."
  firewall-cmd --zone=trusted --add-service=dhcp >/dev/null 2>&1 || true
  firewall-cmd --zone=trusted --add-service=dns  >/dev/null 2>&1 || true

  firewall-cmd --reload >/dev/null 2>&1 || { warn "firewalld: reload falhou."; return 1; }
}

apply_iptables(){
  command -v iptables >/dev/null 2>&1 || { warn 'Sem iptables; NAT não configurado.'; return 1; }

  info "A configurar NAT via iptables (WAN=${WAN_IF}, LAN=${SUBNET_NETWORK})"
  iptables -t nat -D POSTROUTING -s "${SUBNET_NETWORK}" -o "${WAN_IF}" -j MASQUERADE 2>/dev/null || true
  iptables -D FORWARD -i "${WAN_IF}" -o "${NETWORK_NAME}" -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true
  iptables -D FORWARD -i "${NETWORK_NAME}" -o "${WAN_IF}" -j ACCEPT 2>/dev/null || true

  iptables -t nat -A POSTROUTING -s "${SUBNET_NETWORK}" -o "${WAN_IF}" -j MASQUERADE
  iptables -A FORWARD -i "${WAN_IF}" -o "${NETWORK_NAME}" -m state --state RELATED,ESTABLISHED -j ACCEPT
  iptables -A FORWARD -i "${NETWORK_NAME}" -o "${WAN_IF}" -j ACCEPT

  cat >"/etc/systemd/system/${NAT_UNIT}" <<UNIT
[Unit]
Description=Persist NAT rules for ${NETWORK_NAME}
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/sbin/iptables -t nat -C POSTROUTING -s ${SUBNET_NETWORK} -o ${WAN_IF} -j MASQUERADE || /usr/sbin/iptables -t nat -A POSTROUTING -s ${SUBNET_NETWORK} -o ${WAN_IF} -j MASQUERADE
ExecStart=/usr/sbin/iptables -C FORWARD -i ${WAN_IF} -o ${NETWORK_NAME} -m state --state RELATED,ESTABLISHED -j ACCEPT || /usr/sbin/iptables -A FORWARD -i ${WAN_IF} -o ${NETWORK_NAME} -m state --state RELATED,ESTABLISHED -j ACCEPT
ExecStart=/usr/sbin/iptables -C FORWARD -i ${NETWORK_NAME} -o ${WAN_IF} -j ACCEPT || /usr/sbin/iptables -A FORWARD -i ${NETWORK_NAME} -o ${WAN_IF} -j ACCEPT
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
UNIT

  systemctl daemon-reload
  systemctl enable --now "${NAT_UNIT}" >/dev/null 2>&1 || true
}

if apply_firewalld; then
  info "firewalld configurado."
else
  warn "firewalld indisponível; a cair para iptables."
  apply_iptables || warn 'NAT não ficou persistente — verifica manualmente.'
fi

# --- Serviço dedicado do dnsmasq ---------------------------------------------
UNIT_PATH="/etc/systemd/system/${DEDICATED_UNIT}"
info "A criar serviço dedicado ${DEDICATED_UNIT}"
cat >"${UNIT_PATH}" <<EOF
[Unit]
Description=dnsmasq para ${NETWORK_NAME}
Wants=network-online.target NetworkManager-wait-online.service
After=macvtap-${NETWORK_NAME}.service network-online.target NetworkManager-wait-online.service

[Service]
Type=simple
# Espera até a interface ter IPv4
ExecStartPre=/bin/bash -c 'for i in {1..20}; do ip -4 addr show ${NETWORK_NAME} | grep -q "inet " && exit 0; sleep 1; done; echo "${NETWORK_NAME} sem IPv4"; exit 1'
ExecStart=/usr/sbin/dnsmasq -k --conf-file=${DNSMASQ_CONF} --bind-interfaces
Restart=on-failure
RestartSec=2
# Endurecer um pouco
AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_ADMIN CAP_NET_RAW
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
EOF

info "A ativar NetworkManager-wait-online"
systemctl enable NetworkManager-wait-online.service >/dev/null 2>&1 || true

systemctl daemon-reload
systemctl reset-failed "${DEDICATED_UNIT}" >/dev/null 2>&1 || true
systemctl enable "${DEDICATED_UNIT}" >/dev/null
systemctl start "${DEDICATED_UNIT}" --no-block >/dev/null

info "A aguardar até 20s por estado 'active' de ${DEDICATED_UNIT}"
for i in {1..20}; do
  systemctl is-active --quiet "${DEDICATED_UNIT}" && break
  sleep 1
done
systemctl is-active --quiet "${DEDICATED_UNIT}" || { systemctl --no-pager --lines=80 status "${DEDICATED_UNIT}" || true; fatal "${DEDICATED_UNIT} não arrancou."; }

# --- Verificação --------------------------------------------------------------
systemctl --no-pager --lines=20 status "${DEDICATED_UNIT}" || true
ss -lupn | egrep ':(53|67|68)\b' || true

info "Pronto: ${NETWORK_NAME} a servir DHCP ${DHCP_RANGE_START}-${DHCP_RANGE_END} via ${GATEWAY_IP} e NAT a sair por ${WAN_IF}."
echo "Dica: tcpdump -ni ${NETWORK_NAME} 'port 67 or 68' durante um pedido DHCP."
