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

modprobe macvtap >/dev/null 2>&1 || true

# Remove se já existir
ip link show ${child} >/dev/null 2>&1 && { ip link set ${child} down || true; ip link delete ${child} || true; }

# Espera que a parent exista e suba
for i in {1..20}; do
  ip link show ${parent} >/dev/null 2>&1 && break
  sleep 1
done
ip link show ${parent} >/dev/null 2>&1 || exit 1

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

# Limpa se já existir
if ip link show "$MACVTAP_IF" >/dev/null 2>&1; then
  info "A remover interface existente '${MACVTAP_IF}'"
  ip link set "$MACVTAP_IF" down 2>/dev/null || true
  ip link delete "$MACVTAP_IF" 2>/dev/null || true
fi

info "Parent ${PARENT_IF} -> promisc on"
ip link set "$PARENT_IF" promisc on || true

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
