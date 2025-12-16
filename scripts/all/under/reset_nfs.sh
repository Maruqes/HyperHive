#!/usr/bin/env bash
# reset_nfs.sh - Wipes all NFS configuration/state and optionally reinstalls specific versions
# Usage:
#   ./reset_nfs.sh --force                  # wipe everything and DO NOT reinstall (if REMOVE_PACKAGES=1)
#   ./reset_nfs.sh --force --reinstall      # wipe and reinstall according to VERSAO defined below
# Optional flags:
#   --keep-packages   Do not remove packages (only clean config/state)
#   --with-rpcbind    Start rpcbind (for NFSv3)
#   --no-firewall     Leave iptables rules untouched
#   --no-selinux      Leave SELinux booleans untouched
# Safety net: backups stored in /root/nfs-reset-YYYYmmdd-HHMMSS/

set -euo pipefail

# ==================== SET VERSIONS HERE ====================
# Leave "" to install the latest available version.
declare -A VERSAO=(
  ["nfs-utils"]=""
  ["libnfsidmap"]=""
  ["rpcbind"]=""
  ["nfs4-acl-tools"]=""
)
# ================================================================

NFS_PORTS_TCP=(2049 20048 111)
NFS_PORTS_UDP=(2049 20048 111)

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
    *) echo "Usage: $0 --force [--reinstall] [--keep-packages] [--with-rpcbind] [--no-firewall] [--no-selinux]"; exit 2;;
  esac
done

# auto-escalate with sudo
if [[ $EUID -ne 0 ]]; then exec sudo -E bash "$0" "$@"; fi

if [[ $FORCE -ne 1 ]]; then
  echo "This script is DESTRUCTIVE for existing NFS configuration."
  echo "If you are sure, rerun with --force."
  exit 1
fi

log(){ echo -e "[reset-nfs] $*"; }
have(){ command -v "$1" &>/dev/null; }

iptables_accept(){
  local proto=$1 port=$2
  iptables -C INPUT -p "$proto" --dport "$port" -j ACCEPT 2>/dev/null || \
    iptables -A INPUT -p "$proto" --dport "$port" -j ACCEPT
}

iptables_remove(){
  local proto=$1 port=$2
  iptables -D INPUT -p "$proto" --dport "$port" -j ACCEPT 2>/dev/null || true
}

TS=$(date +%Y%m%d-%H%M%S)
BK="/root/nfs-reset-$TS"
mkdir -p "$BK"/{etc,var_lib_nfs,iptables}

log "Backing up configuration/state to $BK"
if [[ $TOUCH_FIREWALL -eq 1 ]] && have iptables; then
  iptables-save > "$BK/iptables/iptables.save" 2>/dev/null || true
fi

# 1) Stop and disable services
log "Stopping NFS/RPC services..."
systemctl stop nfs-server nfs-mountd nfs-idmapd rpc-statd rpcbind 2>/dev/null || true
systemctl disable nfs-server nfs-mountd nfs-idmapd rpc-statd rpcbind 2>/dev/null || true
systemctl reset-failed nfs-server nfs-mountd nfs-idmapd rpc-statd rpcbind 2>/dev/null || true

# 2) Unmount client-side NFS mounts
log "Unmounting NFS mounts (if any)..."
mapfile -t NFS_MPTS < <(awk '$3 ~ /^nfs/ {print $2}' /proc/mounts)
for m in "${NFS_MPTS[@]:-}"; do
  log " - umount -fl $m"
  umount -fl "$m" 2>/dev/null || true
done

# 3) Unexport everything (server)
if have exportfs; then
  log "Running exportfs -ua to drop all exports..."
  exportfs -ua 2>/dev/null || true
fi

# 4) Backup and wipe config files
log "Saving and cleaning /etc/exports, /etc/exports.d, /etc/nfs.conf, /var/lib/nfs..."
test -f /etc/exports   && cp -a /etc/exports   "$BK/etc/exports"
test -d /etc/exports.d && cp -a /etc/exports.d "$BK/etc/exports.d"
test -f /etc/nfs.conf  && cp -a /etc/nfs.conf  "$BK/etc/nfs.conf"
test -d /var/lib/nfs   && cp -a /var/lib/nfs   "$BK/var_lib_nfs"

rm -f /etc/exports 2>/dev/null || true
rm -f /etc/nfs.conf 2>/dev/null || true
if [[ -d /etc/exports.d ]]; then rm -f /etc/exports.d/* 2>/dev/null || true; fi
if [[ -d /var/lib/nfs ]]; then rm -rf /var/lib/nfs/* 2>/dev/null || true; fi

# 5) Firewall (remove rules and later re-open if reinstalling)
if [[ $TOUCH_FIREWALL -eq 1 ]] && have firewall-cmd && firewall-cmd --state &>/dev/null; then
  log "Cleaning NFS services from firewalld (permanent)..."
  firewall-cmd --permanent --remove-service=nfs       2>/dev/null || true
  firewall-cmd --permanent --remove-service=mountd    2>/dev/null || true
  firewall-cmd --permanent --remove-service=rpc-bind  2>/dev/null || true
  firewall-cmd --reload 2>/dev/null || true
fi

# 6) SELinux (booleans)
if [[ $TOUCH_SELINUX -eq 1 ]] && have getsebool; then
  if getsebool virt_use_nfs &>/dev/null; then
    log "Setting virt_use_nfs=off (you can enable it again later)..."
    setsebool -P virt_use_nfs off 2>/dev/null || true
  fi
  if getsebool use_nfs_home_dirs &>/dev/null; then
    setsebool -P use_nfs_home_dirs off 2>/dev/null || true
  fi
fi

# 7) Remove packages (optional)
if [[ $REMOVE_PACKAGES -eq 1 ]]; then
  log "Removing NFS packages..."
  dnf remove -y nfs-utils libnfsidmap rpcbind nfs4-acl-tools 2>/dev/null || true
else
  log "Keeping installed packages (--keep-packages)."
fi

# 8) Reinstall according to VERSAO (optional)
if [[ $DO_REINSTALL -eq 1 ]]; then
  log "Reinstalling packages as requested..."
  for pkg in "${!VERSAO[@]}"; do
    ver="${VERSAO[$pkg]}"
    if [[ -n "$ver" ]]; then
      log " - dnf install -y --allowerasing ${pkg}-${ver}"
      dnf install -y --allowerasing "${pkg}-${ver}"
    else
      log " - dnf install -y ${pkg} (latest available)"
      dnf install -y "${pkg}"
    fi
  done

  # 9) Re-enable core services
  log "Starting nfs-server, idmapd, statd..."
  systemctl enable --now nfs-server 2>/dev/null || true
  systemctl enable --now nfs-idmapd 2>/dev/null || true
  systemctl enable --now rpc-statd  2>/dev/null || true

  if [[ $WITH_RPCBIND -eq 1 ]]; then
    log "Starting rpcbind (NFSv3)..."
    systemctl enable --now rpcbind 2>/dev/null || true
  fi

  # 10) Re-open firewall rules
  if [[ $TOUCH_FIREWALL -eq 1 ]] && have firewall-cmd && firewall-cmd --state &>/dev/null; then
    log "Adding NFS services to firewalld (permanent)..."
    firewall-cmd --permanent --add-service=nfs       2>/dev/null || true
    firewall-cmd --permanent --add-service=mountd    2>/dev/null || true
    [[ $WITH_RPCBIND -eq 1 ]] && firewall-cmd --permanent --add-service=rpc-bind 2>/dev/null || true
    firewall-cmd --reload 2>/dev/null || true
  fi

  # 11) SELinux - enable boolean useful for VMs (optional)
  if [[ $TOUCH_SELINUX -eq 1 ]] && have getsebool && getsebool virt_use_nfs &>/dev/null; then
    log "Setting virt_use_nfs=on (needed for QEMU/libvirt over NFS)..."
    setsebool -P virt_use_nfs on 2>/dev/null || true
  fi
fi

echo
log "Done."
log "Backups stored at: $BK"
if [[ $DO_REINSTALL -eq 1 ]]; then
  log "Current service snapshot:"
  systemctl --no-pager --type=service | grep -E 'nfs-|rpc' || true
  echo
  echo "- Define your exports in /etc/exports (for example):"
  echo "    /mnt/vms *(rw,sync,no_subtree_check,no_root_squash,sec=sys)"
  echo "  Then run: sudo exportfs -ra"
fi
