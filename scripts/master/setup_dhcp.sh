#!/usr/bin/env bash
# DHCP + NAT endurecido e persistente para 1 segmento LAN com macvtap.
# - Desativa dnsmasq global e corre instância dedicada por-Interface.
# - Remove e substitui configs antigas conflituosas (serviços, drop-ins, NAT antigo).
# - Garante ip_forward, rp_filter relaxado, iptables e persistência.

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
GATEWAY_IP, DHCP_RANGE_START, DHCP_RANGE_END, DHCP_LEASE_TIME, WAN_IF, etc.
USAGE
exit 1; }

[[ ${EUID:-0} -eq 0 ]] || fatal 'Requer root.'
[[ -r /etc/os-release ]] || fatal 'Sem /etc/os-release.'
. /etc/os-release
if [[ "${ID,,}" != "fedora" && ! ${ID_LIKE:-} =~ fedora ]]; then
  fatal 'Este script foi feito para Fedora-like.'
fi

# --- SELinux: força modo permissive ------------------------------------------
if command -v selinuxenabled >/dev/null 2>&1 && selinuxenabled; then
  if command -v setenforce >/dev/null 2>&1; then
    if ! setenforce 0 2>/dev/null; then
      warn "Falhou setenforce 0 (SELinux poderá bloquear dnsmasq)."
    else
      info "SELinux colocado em modo permissive (runtime)."
    fi
  else
    warn "setenforce indisponível; não foi possível alterar modo runtime de SELinux."
  fi
else
  warn "SELinux não está ativo ou selinuxenabled indisponível; a continuar."
fi

if [[ -w /etc/selinux/config ]]; then
  if grep -q '^SELINUX=enforcing' /etc/selinux/config; then
    if sed -i 's/^SELINUX=.*/SELINUX=permissive/' /etc/selinux/config; then
      info "Atualizado /etc/selinux/config para SELINUX=permissive."
    else
      warn "Não foi possível atualizar /etc/selinux/config (ver permissões)."
    fi
  fi
else
  warn "Sem permissões para editar /etc/selinux/config; modo persistente não alterado."
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
DHCP_LEASE_TIME="${DHCP_LEASE_TIME:-12h}"

RESOLV_CONF="${RESOLV_CONF:-/etc/resolv.conf}"
DNSMASQ_CONF_DIR="${DNSMASQ_CONF_DIR:-/etc/dnsmasq.d}"
DNSMASQ_LEASE_DIR="${DNSMASQ_LEASE_DIR:-/var/lib/dnsmasq}"

DNSMASQ_RUN_USER="${DNSMASQ_RUN_USER:-}"
DNSMASQ_RUN_GROUP="${DNSMASQ_RUN_GROUP:-}"

SYSCTL_CONF="/etc/sysctl.d/99-${NETWORK_NAME}-ipfwd-rpf.conf"
DEDICATED_UNIT="dnsmasq-${NETWORK_NAME}.service"
NAT_UNIT="${NETWORK_NAME}-nat.service"
MACVTAP_PERSIST="${MACVTAP_PERSIST:-1}"

command -v ip >/dev/null || fatal 'Falta iproute2.'
command -v dnsmasq >/dev/null || fatal 'Falta dnsmasq.'
command -v nmcli >/dev/null 2>&1 || warn 'nmcli ausente (persistência NM limitada).'

if [[ -z ${DNSMASQ_RUN_USER} ]]; then
  if getent passwd dnsmasq >/dev/null; then
    DNSMASQ_RUN_USER="dnsmasq"
  else
    DNSMASQ_RUN_USER="nobody"
  fi
fi
if [[ -z ${DNSMASQ_RUN_GROUP} ]]; then
  if getent group "${DNSMASQ_RUN_USER}" >/dev/null; then
    DNSMASQ_RUN_GROUP="${DNSMASQ_RUN_USER}"
  elif getent group dnsmasq >/dev/null; then
    DNSMASQ_RUN_GROUP="dnsmasq"
  elif getent group nogroup >/dev/null; then
    DNSMASQ_RUN_GROUP="nogroup"
  else
    DNSMASQ_RUN_GROUP="nobody"
  fi
fi

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

  install -d -m 755 "${DNSMASQ_CONF_DIR}"
  install -d -m 775 "${DNSMASQ_LEASE_DIR}"
  chown "${DNSMASQ_RUN_USER}:${DNSMASQ_RUN_GROUP}" "${DNSMASQ_LEASE_DIR}" || warn "Não foi possível ajustar owner de ${DNSMASQ_LEASE_DIR}"
  chmod 775 "${DNSMASQ_LEASE_DIR}" || warn "Não foi possível ajustar permissões de ${DNSMASQ_LEASE_DIR}"
  if command -v restorecon >/dev/null 2>&1; then
    restorecon -R "${DNSMASQ_LEASE_DIR}" >/dev/null 2>&1 || warn "restorecon falhou para ${DNSMASQ_LEASE_DIR}"
  fi
  rm -f "${DNSMASQ_CONF_DIR}/${NETWORK_NAME}.conf"
  local lease_file="${DNSMASQ_LEASE_DIR}/${NETWORK_NAME}.leases"
  rm -f "${lease_file}"
  install -m 664 -o "${DNSMASQ_RUN_USER}" -g "${DNSMASQ_RUN_GROUP}" /dev/null "${lease_file}" 2>/dev/null || {
    touch "${lease_file}"
    chown "${DNSMASQ_RUN_USER}:${DNSMASQ_RUN_GROUP}" "${lease_file}" || warn "Não foi possível ajustar owner de ${lease_file}"
    chmod 664 "${lease_file}" || warn "Não foi possível ajustar permissões de ${lease_file}"
  }

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

    while read -r uuid name; do
      [[ -z ${uuid} ]] && continue
      local current_method
      current_method=$(nmcli -g ipv4.method connection show "${uuid}" 2>/dev/null || echo "")
      if [[ "${current_method}" != "disabled" ]]; then
        info "NM: a desativar IPv4 no parent (${LAN_PARENT_IF}) via perfil '${name}'"
        nmcli connection modify "${uuid}" ipv4.method disabled ipv4.addresses "" ipv4.gateway "" ipv4.never-default yes >/dev/null 2>&1 || warn "NM: falhou a definir IPv4 disabled para '${name}'"
        nmcli connection modify "${uuid}" ipv6.method ignore >/dev/null 2>&1 || true
        nmcli connection down "${uuid}" >/dev/null 2>&1 || true
        nmcli connection up "${uuid}" >/dev/null 2>&1 || true
      fi
    done < <(nmcli -t -f UUID,NAME,DEVICE connection show | awk -F: -v dev="${LAN_PARENT_IF}" '$3==dev{print $1" "$2}')
  fi

  # Força estado e IPv4 do child
  ip addr flush dev "${NETWORK_NAME}" || true
  ip link set "${NETWORK_NAME}" up
  ip addr add "${GATEWAY_IP}/${cidr_prefix}" dev "${NETWORK_NAME}" valid_lft forever preferred_lft forever

  # Parent em promisc para encaminhamento estável
  ip link set "${LAN_PARENT_IF}" promisc on || true
}
cleanup_for_network

stop_conflicting_dnsmasq_units(){
  command -v systemctl >/dev/null 2>&1 || return
  info "A verificar serviços dnsmasq conflitantes"
  while read -r unit; do
    [[ -z ${unit} ]] && continue
    [[ "${unit}" == "${DEDICATED_UNIT}" ]] && continue
    info "A parar unidade '${unit}' que usa dnsmasq"
    systemctl stop "${unit}" >/dev/null 2>&1 || true
    systemctl disable "${unit}" >/dev/null 2>&1 || true
  done < <(systemctl list-units --all 'dnsmasq*.service' --plain --no-legend 2>/dev/null | awk '{print $1}' | sort -u)
}
stop_conflicting_dnsmasq_units

kill_conflicting_dns(){
  command -v ss >/dev/null 2>&1 || { warn "Sem utilitário 'ss' para detetar conflitos de portas."; return; }
  local ports=(53 67)
  declare -A handled=()
  for port in "${ports[@]}"; do
    while IFS=' ' read -r pid exe addr proto; do
      [[ -z "${pid}" ]] && continue
      local key="${pid}-${proto}"
      [[ -n "${handled[${key}]:-}" ]] && continue
      handled["${key}"]=1
      case "${exe}" in
        dnsmasq)
          info "A terminar dnsmasq pré-existente (PID ${pid}) no endereço ${addr} (${proto})"
          kill "${pid}" >/dev/null 2>&1 || true
          sleep 0.5
          kill -9 "${pid}" >/dev/null 2>&1 || true
          ;;
        *)
          fatal "Porta ${port}/${addr} ocupada por PID ${pid} (${exe}). Liberta-a antes de continuar."
          ;;
      esac
    done < <(ss -H -lnp "sport = :${port}" 2>/dev/null | awk -v ip="${GATEWAY_IP}" -v p="${port}" '
      {
        local_addr=$5
        proto=$1
        gsub(/^\[|\]$/, "", local_addr)
        if (match(local_addr, /:([0-9]+)$/, mport)) {
          portnum=mport[1]
          addr=substr(local_addr, 1, length(local_addr)-length(mport[0]))
        } else {
          next
        }
        if (portnum != p) next
        if (addr == "" || addr == "*" || addr == "0.0.0.0" || addr == "::" || addr == ip) {
          if (match($0, /pid=([0-9]+)/, m) && match($0, /"([^"]+)"/, c)) {
            printf "%s %s %s:%s %s\n", m[1], c[1], (addr==""?"*":addr), portnum, proto
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
listen-address=${GATEWAY_IP}
except-interface=lo
bind-interfaces
domain-needed
bogus-priv
# DHCP
dhcp-authoritative
dhcp-range=${DHCP_RANGE_START},${DHCP_RANGE_END},${NETMASK},${DHCP_LEASE_TIME}
dhcp-option=option:router,${GATEWAY_IP}
dhcp-option=option:dns-server,${GATEWAY_IP}
dhcp-leasefile=${DNSMASQ_LEASE_DIR}/${NETWORK_NAME}.leases
# DNS forwarders
resolv-file=${RESOLV_CONF}
log-dhcp
CFG

dnsmasq --test -C "${DNSMASQ_CONF}" >/dev/null || fatal "Teste de configuração falhou: ${DNSMASQ_CONF}"

# --- WAN detection ------------------------------------------------------------
find_wan_iface(){ ip route show default 0.0.0.0/0 | awk '/default/ {print $5; exit}'; }
WAN_IF_INPUT="${CLI_WAN_IF:-${WAN_IF:-}}"
WAN_IF="${WAN_IF_INPUT:-$(find_wan_iface)}"
[[ -n ${WAN_IF} ]] || fatal 'Não foi possível detetar a interface WAN.'
[[ "${WAN_IF}" != "${NETWORK_NAME}" ]] || fatal 'WAN não pode ser a mesma que a interface DHCP.'
ip link show "${WAN_IF}" >/dev/null 2>&1 || fatal "WAN '${WAN_IF}' não existe."

# --- ip_forward + rp_filter relaxado -----------------------------------------
info "A ativar ip_forward e a relaxar rp_filter"
cat >"${SYSCTL_CONF}" <<SYSCTL
net.ipv4.ip_forward = 1
net.ipv4.conf.all.rp_filter = 0
net.ipv4.conf.default.rp_filter = 0
net.ipv4.conf.${NETWORK_NAME}.rp_filter = 0
net.ipv4.conf.${LAN_PARENT_IF}.rp_filter = 0
SYSCTL
if [[ "${LAN_PARENT_IF}" != "${WAN_IF}" && "${WAN_IF}" != "${NETWORK_NAME}" ]]; then
  cat >>"${SYSCTL_CONF}" <<SYSCTL
net.ipv4.conf.${WAN_IF}.rp_filter = 0
SYSCTL
fi
sysctl --system >/dev/null

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
apply_iptables || warn 'NAT não ficou persistente — verifica manualmente.'

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
ExecStart=/usr/sbin/dnsmasq -k --conf-file=${DNSMASQ_CONF} --bind-interfaces --user=${DNSMASQ_RUN_USER} --group=${DNSMASQ_RUN_GROUP}
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

info "Pronto: ${NETWORK_NAME} a servir DHCP ${DHCP_RANGE_START}-${DHCP_RANGE_END} via ${GATEWAY_IP} (leases ${DHCP_LEASE_TIME}) e NAT a sair por ${WAN_IF}."
echo "Dica: tcpdump -ni ${NETWORK_NAME} 'port 67 or 68' durante um pedido DHCP."
