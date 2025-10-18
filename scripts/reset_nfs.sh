#!/usr/bin/env bash
# reset_nfs.sh — Remove toda a configuração/estado do NFS e reinstala versões especificadas
# Uso:
#   ./reset_nfs.sh --force                  # limpa tudo e NÃO reinstala (se REMOVE_PACKAGES=1)
#   ./reset_nfs.sh --force --reinstall      # limpa e reinstala conforme VERSAO no topo
# Flags opcionais:
#   --keep-packages   Não desinstala pacotes (apenas limpa config/estado)
#   --with-rpcbind    Arranca rpcbind (para NFSv3)
#   --no-firewall     Não mexe no firewalld
#   --no-selinux      Não mexe em booleans do SELinux
# Saída segura: backups em /root/nfs-reset-YYYYmmdd-HHMMSS/

set -euo pipefail

# ==================== CONFIGURA VERSÕES AQUI ====================
# Deixa "" para instalar a última disponível.
declare -A VERSAO=(
  ["nfs-utils"]=""
  ["libnfsidmap"]=""
  ["rpcbind"]=""
  ["nfs4-acl-tools"]=""
)
# ================================================================

FORCE=0
DO_REINSTALL=0
REMOVE_PACKAGES=1
WITH_RPCBIND=0
TOUCH_FIREWALL=1
TOUCH_SELINUX=1

for a in "$@"; do
  case "$a" in
    --force)         FORCE=1 ;;
    --reinstall)     DO_REINSTALL=1 ;;
    --keep-packages) REMOVE_PACKAGES=0 ;;
    --with-rpcbind)  WITH_RPCBIND=1 ;;
    --no-firewall)   TOUCH_FIREWALL=0 ;;
    --no-selinux)    TOUCH_SELINUX=0 ;;
    *) echo "Uso: $0 --force [--reinstall] [--keep-packages] [--with-rpcbind] [--no-firewall] [--no-selinux]"; exit 2;;
  esac
done

# sudo automático
if [[ $EUID -ne 0 ]]; then exec sudo -E bash "$0" "$@"; fi

if [[ $FORCE -ne 1 ]]; then
  echo "Este script é DESTRUTIVO para a configuração do NFS."
  echo "Se tens a certeza, corre novamente com --force"
  exit 1
fi

log(){ echo -e "[reset-nfs] $*"; }
have(){ command -v "$1" &>/dev/null; }

TS=$(date +%Y%m%d-%H%M%S)
BK="/root/nfs-reset-$TS"
mkdir -p "$BK"/{etc,var_lib_nfs,firewalld}

log "Backup de configuração/estado para $BK"

# 1) Parar e desativar serviços
log "A parar serviços NFS/RPC…"
systemctl stop nfs-server nfs-mountd nfs-idmapd rpc-statd rpcbind 2>/dev/null || true
systemctl disable nfs-server nfs-mountd nfs-idmapd rpc-statd rpcbind 2>/dev/null || true
systemctl reset-failed nfs-server nfs-mountd nfs-idmapd rpc-statd rpcbind 2>/dev/null || true

# 2) Desmontar mounts NFS (cliente)
log "A desmontar mounts NFS (se existirem)…"
mapfile -t NFS_MPTS < <(awk '$3 ~ /^nfs/ {print $2}' /proc/mounts)
for m in "${NFS_MPTS[@]:-}"; do
  log " - umount -fl $m"
  umount -fl "$m" 2>/dev/null || true
done

# 3) Desexportar tudo (servidor)
if have exportfs; then
  log "A desexportar sistemas de ficheiros (exportfs -ua)…"
  exportfs -ua 2>/dev/null || true
fi

# 4) Backups e limpeza de ficheiros
log "A salvar e limpar /etc/exports, /etc/exports.d, /etc/nfs.conf, /var/lib/nfs…"
test -f /etc/exports   && cp -a /etc/exports   "$BK/etc/exports"
test -d /etc/exports.d && cp -a /etc/exports.d "$BK/etc/exports.d"
test -f /etc/nfs.conf  && cp -a /etc/nfs.conf  "$BK/etc/nfs.conf"
test -d /var/lib/nfs   && cp -a /var/lib/nfs   "$BK/var_lib_nfs"

rm -f /etc/exports 2>/dev/null || true
rm -f /etc/nfs.conf 2>/dev/null || true
if [[ -d /etc/exports.d ]]; then rm -f /etc/exports.d/* 2>/dev/null || true; fi
if [[ -d /var/lib/nfs ]]; then rm -rf /var/lib/nfs/* 2>/dev/null || true; fi

# 5) Firewall (remover regras e depois, se reinstalar, voltar a abrir)
if [[ $TOUCH_FIREWALL -eq 1 && $(command -v firewall-cmd) && $(firewall-cmd --state 2>/dev/null) ]]; then
  log "A limpar serviços NFS do firewalld (permanent)…"
  firewall-cmd --permanent --remove-service=nfs     2>/dev/null || true
  firewall-cmd --permanent --remove-service=mountd  2>/dev/null || true
  firewall-cmd --permanent --remove-service=rpc-bind 2>/dev/null || true
  firewall-cmd --reload 2>/dev/null || true
fi

# 6) SELinux (booleans)
if [[ $TOUCH_SELINUX -eq 1 && $(command -v getsebool) ]]; then
  if getsebool virt_use_nfs &>/dev/null; then
    log "A repor virt_use_nfs=off (podes voltar a ligar no fim)…"
    setsebool -P virt_use_nfs off 2>/dev/null || true
  fi
  if getsebool use_nfs_home_dirs &>/dev/null; then
    setsebool -P use_nfs_home_dirs off 2>/dev/null || true
  fi
fi

# 7) Desinstalar pacotes (opcional)
if [[ $REMOVE_PACKAGES -eq 1 ]]; then
  log "A desinstalar pacotes NFS…"
  dnf remove -y nfs-utils libnfsidmap rpcbind nfs4-acl-tools 2>/dev/null || true
else
  log "A manter pacotes instalados (--keep-packages)."
fi

# 8) Reinstalar conforme VERSAO (opcional)
if [[ $DO_REINSTALL -eq 1 ]]; then
  log "A reinstalar pacotes conforme versões definidas…"
  for pkg in "${!VERSAO[@]}"; do
    ver="${VERSAO[$pkg]}"
    if [[ -n "$ver" ]]; then
      log " - dnf install -y --allowerasing ${pkg}-${ver}"
      dnf install -y --allowerasing "${pkg}-${ver}"
    else
      log " - dnf install -y ${pkg} (última)"
      dnf install -y "${pkg}"
    fi
  done

  # 9) Re-ativar serviços principais
  log "A activar/arrancar nfs-server, idmapd, statd…"
  systemctl enable --now nfs-server 2>/dev/null || true
  systemctl enable --now nfs-idmapd 2>/dev/null || true
  systemctl enable --now rpc-statd  2>/dev/null || true

  if [[ $WITH_RPCBIND -eq 1 ]]; then
    log "A activar rpcbind (NFSv3)…"
    systemctl enable --now rpcbind 2>/dev/null || true
  fi

  # 10) Abrir firewall novamente
  if [[ $TOUCH_FIREWALL -eq 1 && $(command -v firewall-cmd) && $(firewall-cmd --state 2>/dev/null) ]]; then
    log "A abrir serviços NFS no firewalld (permanent)…"
    firewall-cmd --permanent --add-service=nfs     2>/dev/null || true
    firewall-cmd --permanent --add-service=mountd  2>/dev/null || true
    [[ $WITH_RPCBIND -eq 1 ]] && firewall-cmd --permanent --add-service=rpc-bind 2>/dev/null || true
    firewall-cmd --reload 2>/dev/null || true
  fi

  # 11) SELinux — ligar boolean útil para VMs (opcional)
  if [[ $TOUCH_SELINUX -eq 1 && $(command -v getsebool) && getsebool virt_use_nfs &>/dev/null ]]; then
    log "A definir virt_use_nfs=on (útil para QEMU/libvirt usar NFS)…"
    setsebool -P virt_use_nfs on 2>/dev/null || true
  fi
fi

echo
log "Concluído."
log "Backups em: $BK"
if [[ $DO_REINSTALL -eq 1 ]]; then
  log "Estado actual:"
  systemctl --no-pager --type=service | grep -E 'nfs-|rpc' || true
  echo
  echo "• Define os exports em /etc/exports (ex.):"
  echo "    /mnt/vms *(rw,sync,no_subtree_check,no_root_squash,sec=sys)"
  echo "  Depois:  sudo exportfs -ra"
fi
