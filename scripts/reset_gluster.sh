#!/bin/bash
# reset_gluster.sh - full GlusterFS reset (DANGEROUS: destroys all Gluster data/config)

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "Este script tem de ser executado como root." >&2
  exit 1
fi

log() {
  echo "[reset_gluster] $*"
}

PKG_MGR=""

detect_pkg_manager() {
  if command -v dnf >/dev/null 2>&1; then
    PKG_MGR="dnf"
  elif command -v yum >/dev/null 2>&1; then
    PKG_MGR="yum"
  elif command -v apt-get >/dev/null 2>&1; then
    PKG_MGR="apt"
  elif command -v zypper >/dev/null 2>&1; then
    PKG_MGR="zypper"
  else
    echo "Nenhum gestor de pacotes suportado encontrado (dnf, yum, apt-get, zypper)." >&2
    exit 1
  fi
}

remove_gluster_packages() {
  log "A remover pacotes Gluster (${PKG_MGR})..."
  case "$PKG_MGR" in
    dnf)
      if ! dnf remove -y glusterfs-server glusterfs glusterfs-fuse glusterfs-cli glusterfs-libs; then
        log "Pacotes Gluster rpm já não estavam instalados."
      fi
      ;;
    yum)
      if ! yum remove -y glusterfs-server glusterfs glusterfs-fuse glusterfs-cli glusterfs-libs; then
        log "Pacotes Gluster rpm já não estavam instalados."
      fi
      ;;
    apt)
      if ! apt-get purge -y glusterfs-server glusterfs-client glusterfs-common; then
        log "Pacotes Gluster deb já não estavam instalados."
      fi
      if ! apt-get autoremove -y --purge; then
        log "Sem pacotes órfãos para remover."
      fi
      ;;
    zypper)
      if ! zypper --non-interactive remove --force glusterfs glusterfs-server glusterfs-fuse; then
        log "Pacotes Gluster zypper já não estavam instalados."
      fi
      ;;
  esac
}

install_gluster_packages() {
  log "A instalar pacotes Gluster (${PKG_MGR})..."
  case "$PKG_MGR" in
    dnf)
      dnf install -y glusterfs glusterfs-server glusterfs-fuse glusterfs-cli
      ;;
    yum)
      yum install -y glusterfs glusterfs-server glusterfs-fuse glusterfs-cli
      ;;
    apt)
      apt-get update
      apt-get install -y glusterfs-server glusterfs-client
      ;;
    zypper)
      zypper --non-interactive install --force glusterfs glusterfs-server glusterfs-fuse
      ;;
  esac
}

export DEBIAN_FRONTEND=noninteractive

detect_pkg_manager
log "Gestor de pacotes detectado: ${PKG_MGR}."

log "A parar serviços GlusterFS..."
if systemctl list-unit-files | grep -q '^glusterd\.service'; then
  systemctl stop glusterd glusterfsd glusterfs 2>/dev/null || true
  systemctl disable glusterd 2>/dev/null || true
  systemctl reset-failed glusterd 2>/dev/null || true
else
  log "Serviços Gluster não encontrados no systemd (a continuar)."
fi

log "A matar processos remanescentes..."
pkill -9 glusterfsd 2>/dev/null || true
pkill -9 glusterd 2>/dev/null || true
pkill -9 glusterfs 2>/dev/null || true

log "A desmontar volumes Gluster montados..."
while read -r mnt; do
  [[ -z "$mnt" ]] && continue
  umount -f "$mnt" 2>/dev/null || true
  log "  desmontado $mnt"
done < <(mount | awk '$5 ~ /glusterfs/ {print $3}')

log "A remover diretórios de configuração e runtime..."
rm -rf /var/lib/glusterd /var/log/glusterfs /etc/glusterfs /var/run/gluster

if [[ -d /gluster_bricks ]]; then
  log "A remover conteúdo de /gluster_bricks..."
  find /gluster_bricks -mindepth 1 -maxdepth 1 -exec rm -rf {} + 2>/dev/null || true
fi

log "A limpar diretórios gluster_bricks espalhados..."
find / -path '*/gluster_bricks/*' -type d -prune -exec rm -rf {} + 2>/dev/null || true

remove_gluster_packages

log "A reinstalar pacotes GlusterFS..."
install_gluster_packages

log "A reativar e iniciar glusterd..."
systemctl enable --now glusterd

log "Reset concluído em $(hostname)."
