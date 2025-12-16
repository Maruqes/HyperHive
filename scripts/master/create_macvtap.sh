#!/usr/bin/env bash
set -euo pipefail

# Cria (e persiste) um macvtap em modo bridge ancorado a uma NIC parent.
# Uso:
#   sudo ./create_macvtap.sh [--persist] <parent_iface> <macvtap_iface> [ipv4_cidr]
# Ex.:
#   sudo ./create_macvtap.sh --persist enp3s0 512rede-host 192.168.76.1/24
#
# Com --persist, instala um serviço systemd que (re)cria a interface no arranque.

usage() {
  cat <<'USAGE'
Usage: create_macvtap.sh [--persist] <parent_iface> <macvtap_iface> [ipv4_cidr]

  --persist      Instala serviço systemd para recriar a interface no boot
  parent_iface   NIC física existente (ex.: enp3s0) ou a tua '512rede'
  macvtap_iface  nome do macvtap a criar (ex.: 512rede-host)
  ipv4_cidr      IPv4/prefix opcional a atribuir (ex.: 192.168.76.1/24)

Notas:
  - Requer root e iproute2.
  - Usa macvtap em modo bridge (para VMs <interface type="direct" mode="bridge">).
  - Coloca o parent em promisc para encaminhamento fiável.
USAGE
  exit 1
}

fatal(){ printf '[ERROR] %s\n' "$*" >&2; exit 1; }
warn(){  printf '[WARN] %s\n' "$*" >&2; }
info(){  printf '[INFO] %s\n' "$*"; }

ip_to_int(){ local IFS=.; read -r a b c d <<<"$1"; printf '%u' $(( (a<<24)|(b<<16)|(c<<8)|d )); }

remove_conflicting_ipv4(){
  local parent=$1 ip_cidr=$2
  [[ -z "${ip_cidr}" ]] && return
  local cidr_prefix=${ip_cidr#*/}
  local base_ip=${ip_cidr%/*}
  [[ ${cidr_prefix} =~ ^[0-9]+$ ]] || return
  local prefix=$((10#${cidr_prefix}))
  (( prefix>=0 && prefix<=32 )) || return
  local mask=$(( prefix==0 ? 0 : 0xffffffff ^ ((1<<(32-prefix))-1) ))
  local network=$(( $(ip_to_int "${base_ip}") & mask ))

  local addr
  local -a addrs=()
  mapfile -t addrs < <(ip -4 -o addr show dev "${parent}" 2>/dev/null | awk '{print $4}' || true)
  (( ${#addrs[@]} == 0 )) && return
  for addr in "${addrs[@]}"; do
    [[ -z "${addr:-}" ]] && continue
    local candidate_ip=${addr%/*}
    local candidate_int=$(ip_to_int "${candidate_ip}")
    if (( (candidate_int & mask) == network )); then
      ip -4 addr del "${addr}" dev "${parent}" || true
      warn "Removido IPv4 ${addr} de ${parent} por conflito com ${ip_cidr}"
    fi
  done
}

install_persistence() {
  local parent=$1 child=$2 ipv4=${3:-}
  local helper="/usr/local/sbin/macvtap-${child}.sh"
  local unit="/etc/systemd/system/macvtap-${child}.service"
  local devunit="sys-subsystem-net-devices-${parent}.device"

  info "A limpar artefactos antigos de persistência para ${child}"
  systemctl disable --now "macvtap-${child}.service" >/dev/null 2>&1 || true
  rm -f "${helper}" "${unit}"

  install -d -m 755 "$(dirname "${helper}")" "$(dirname "${unit}")"

  info "A instalar helper ${helper}"
  cat >"${helper}" <<SCRIPT
#!/usr/bin/env bash
set -euo pipefail

ensure_ipv4_forwarding(){
  local conf="/etc/sysctl.d/99-macvtap-ipforward.conf"
  cat >"\${conf}" <<'CONF'
net.ipv4.ip_forward = 1
net.ipv4.conf.all.forwarding = 1
CONF
  sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1 || true
  sysctl -w net.ipv4.conf.all.forwarding=1 >/dev/null 2>&1 || true
  sysctl -p "\${conf}" >/dev/null 2>&1 || sysctl --system >/dev/null 2>&1 || true
}

ensure_ipv4_forwarding

modprobe macvtap >/dev/null 2>&1 || true

# Remove se já existir
ip link show ${child} >/dev/null 2>&1 && { ip link set ${child} down || true; ip link delete ${child} || true; }

# Espera que a parent exista e suba
for i in {1..20}; do
  ip link show ${parent} >/dev/null 2>&1 && break
  sleep 1
done
ip link show ${parent} >/dev/null 2>&1 || exit 1

# Remove IPv4 conflitantes no parent
remove_conflicting_ipv4(){
  local parent=\$1 ip_cidr=\$2
  [[ -z "\${ip_cidr}" ]] && return
  IFS=/ read -r base prefix <<<"\${ip_cidr}"
  [[ \${prefix} =~ ^[0-9]+$ ]] || return
  prefix=\$((10#\${prefix}))
  (( prefix>=0 && prefix<=32 )) || return
  ip_to_int(){ local IFS=.; read -r a b c d <<<"\$1"; printf '%u' \$(( (a<<24)|(b<<16)|(c<<8)|d )); }
  local mask=\$(( prefix==0 ? 0 : 0xffffffff ^ ((1<<(32-prefix))-1) ))
  local network=\$(( \$(ip_to_int "\${base}") & mask ))
  local -a addrs=()
  mapfile -t addrs < <(ip -4 -o addr show "\${parent}" 2>/dev/null | awk '{print \$4}' || true)
  (( \${#addrs[@]} == 0 )) && return
  for addr in "\${addrs[@]}"; do
    [[ -z "\${addr:-}" ]] && continue
    local cand_ip=\${addr%/*}
    local cand_int=\$(ip_to_int "\${cand_ip}")
    if (( (cand_int & mask) == network )); then
      ip -4 addr del "\${addr}" dev "\${parent}" || true
    fi
  done
}
remove_conflicting_ipv4 ${parent} "${ipv4}"

# Garante promisc no parent
ip link set ${parent} promisc on || true

# Cria macvtap bridge e sobe
ip link add link ${parent} name ${child} type macvtap mode bridge
ip link set ${child} up

# Atribui IPv4 se fornecido
if [[ -n "${ipv4}" ]]; then
  ip addr flush dev ${child} || true
  ip addr add ${ipv4} dev ${child}
fi

# Desarma rp_filter no child (evita drops silenciosos)
sysctl -w net.ipv4.conf.${child}.rp_filter=0 >/dev/null 2>&1 || true
SCRIPT
  chmod 0755 "${helper}"

  info "A criar serviço systemd ${unit}"
  cat >"${unit}" <<UNIT
[Unit]
Description=macvtap ${child} on ${parent}
After=network-online.target NetworkManager-wait-online.service ${devunit}
Wants=network-online.target NetworkManager-wait-online.service
BindsTo=${devunit}

[Service]
Type=oneshot
ExecStart=${helper}
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
UNIT

  systemctl daemon-reload
  systemctl enable --now "macvtap-${child}.service"
  info "Persistência ativa para ${child}"
}

ensure_ipv4_forwarding(){
  local conf="/etc/sysctl.d/99-macvtap-ipforward.conf"
  info "A garantir ip_forward ativo e persistente (${conf})"
  cat >"${conf}" <<'CONF'
net.ipv4.ip_forward = 1
net.ipv4.conf.all.forwarding = 1
CONF
  sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1 || warn "Falha a definir net.ipv4.ip_forward"
  sysctl -w net.ipv4.conf.all.forwarding=1 >/dev/null 2>&1 || warn "Falha a definir net.ipv4.conf.all.forwarding"
  sysctl -p "${conf}" >/dev/null 2>&1 || sysctl --system >/dev/null 2>&1 || warn "Não consegui recarregar sysctl, verifica manualmente"
}

[[ ${EUID:-0} -eq 0 ]] || fatal 'Este script requer root.'
command -v ip >/dev/null 2>&1 || fatal 'Falta o comando ip (iproute2).'

PERSIST=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --persist) PERSIST=1; shift;;
    -h|--help) usage;;
    --) shift; break;;
    -*) fatal "Opção desconhecida: $1";;
    *) break;;
  esac
done

[[ $# -lt 2 || $# -gt 3 ]] && usage

PARENT_IF=$1
MACVTAP_IF=$2
IPV4_CIDR=${3:-}

ip link show "$PARENT_IF" >/dev/null 2>&1 || fatal "Parent '$PARENT_IF' não existe."

modprobe macvtap >/dev/null 2>&1 || true

ensure_ipv4_forwarding

# Limpa se já existir
if ip link show "$MACVTAP_IF" >/dev/null 2>&1; then
  info "A remover interface existente '${MACVTAP_IF}'"
  ip link set "$MACVTAP_IF" down 2>/dev/null || true
  ip link delete "$MACVTAP_IF" 2>/dev/null || true
fi

info "Parent ${PARENT_IF} -> promisc on"
ip link set "$PARENT_IF" promisc on || true

remove_conflicting_ipv4 "$PARENT_IF" "$IPV4_CIDR"

info "A criar macvtap '${MACVTAP_IF}' (mode=bridge)"
ip link add link "$PARENT_IF" name "$MACVTAP_IF" type macvtap mode bridge
trap 'ip link delete "$MACVTAP_IF" 2>/dev/null || true' ERR
ip link set "$MACVTAP_IF" up

if [[ -n $IPV4_CIDR ]]; then
  info "Atribuir IPv4 ${IPV4_CIDR} a ${MACVTAP_IF}"
  ip addr flush dev "$MACVTAP_IF" || true
  ip addr add "$IPV4_CIDR" dev "$MACVTAP_IF"
fi

# rp_filter relaxado (evita drops no caminho de retorno)
sysctl -w "net.ipv4.conf.${MACVTAP_IF}.rp_filter=0" >/dev/null 2>&1 || true

trap - ERR

if (( PERSIST )); then
  install_persistence "$PARENT_IF" "$MACVTAP_IF" "$IPV4_CIDR"
fi

info "macvtap '${MACVTAP_IF}' pronto."
[[ -n $IPV4_CIDR ]] && info "Endereço ${IPV4_CIDR} ativo no host."
