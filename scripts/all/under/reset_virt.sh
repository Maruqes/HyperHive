#!/usr/bin/env bash
set -euo pipefail

# ===========================
#  USER CONFIGURATION
# ===========================

# (Required reading) Define the exact versions you want here, or leave empty to install the latest one.
# Use keys that match the dnf package names exactly.
# Tip: list available versions with `dnf list --showduplicates <package>`
declare -A VERSAO=(
  ["qemu-kvm"]=""
  ["qemu-img"]=""
  ["libvirt"]=""
  ["libvirt-devel"]=""
  ["libvirt-daemon"]=""
  ["libvirt-daemon-kvm"]=""
  ["libvirt-daemon-driver-qemu"]=""
  ["libvirt-daemon-driver-network"]=""
  ["libvirt-daemon-driver-storage"]=""
  ["libvirt-daemon-config-network"]=""
  ["libvirt-daemon-config-nwfilter"]=""
  ["virt-install"]=""
  ["virt-manager"]=""
  ["virt-viewer"]=""
  ["edk2-ovmf"]=""
  ["bridge-utils"]=""
  ["dnsmasq"]=""
  ["pkgconf-pkg-config"]=""
  ["pkg-config"]=""   # not installed
  ["qemu-guest-agent"]=""
)


# Lock package versions after installation? (uses dnf versionlock)
ENABLE_VERSIONLOCK=true

# Remove *all* libvirt/system state (configs, logs, caches) and also user state
# files (for example ~/.config/virt-manager)? Keeps VM disks (.qcow2) intact.
NUKE_USER_STATE=false

# Create a backup of /etc/libvirt before wiping everything
DO_BACKUP_ETC_LIBVIRT=true

# Force destroy/undefine existing VMs and networks before removing packages
FORCE_KILL_VMS=true

# Add the current user to the 'libvirt' group
ADD_USER_TO_GROUP=true

# ===========================
#  LOGIC
# ===========================

need_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "Missing command: $1"; exit 1; }; }
need_cmd dnf
need_cmd systemctl

SUDO="${SUDO:-}"
if [[ $EUID -ne 0 ]]; then
  SUDO="sudo"
fi

info()  { echo -e "\033[1;34m[INFO]\033[0m $*"; }
warn()  { echo -e "\033[1;33m[WARN]\033[0m $*"; }
err()   { echo -e "\033[1;31m[ERROR]\033[0m $*" >&2; }

pkg_spec() {
  local name="$1"
  local ver="${VERSAO[$name]:-}"
  if [[ "$name" == @* ]]; then
    # groups (e.g. @virtualization) do not take an explicit version
    echo "$name"
  elif [[ -n "$ver" ]]; then
    echo "${name}-${ver}"
  else
    echo "$name"
  fi
}

# Base package list (includes the group)
BASE_PKGS=(
  "@virtualization"
  qemu-kvm
  qemu-img
  libvirt
  libvirt-daemon
  libvirt-daemon-kvm
  libvirt-daemon-driver-qemu
  libvirt-daemon-driver-network
  libvirt-daemon-driver-storage
  libvirt-daemon-config-network
  libvirt-daemon-config-nwfilter
  virt-install
  virt-manager
  virt-viewer
  edk2-ovmf
  bridge-utils
  dnsmasq
  pkgconf-pkg-config
  pkg-config
  libvirt-devel
  qemu-guest-agent
)

STOP_SOCKETS=(
  virtqemud.socket virtlogd.socket virtstoraged.socket virtnetworkd.socket
)
STOP_SERVICES=(
  libvirtd virtqemud virtlogd virtstoraged virtnetworkd
)

# 1) Stop services and terminate VMs/networks
info "Stopping virtualization services..."
for s in "${STOP_SOCKETS[@]}";   do $SUDO systemctl stop "$s" 2>/dev/null || true; done
for s in "${STOP_SERVICES[@]}";  do $SUDO systemctl stop "$s" 2>/dev/null || true; done
$SUDO systemctl disable libvirtd 2>/dev/null || true

if $FORCE_KILL_VMS && command -v virsh >/dev/null 2>&1; then
  warn "Destroying libvirt-defined VMs and networks..."
  # destroy running VMs
  mapfile -t RUNNING < <(virsh list --name | sed '/^$/d' || true)
  for vm in "${RUNNING[@]:-}"; do virsh destroy "$vm" || true; done
  # remove definitions (includes NVRAM)
  mapfile -t ALL < <(virsh list --all --name | sed '/^$/d' || true)
  for vm in "${ALL[@]:-}"; do virsh undefine --nvram "$vm" || virsh undefine "$vm" || true; done
  # destroy/undefine the default network if present
  if virsh net-info default >/dev/null 2>&1; then
    virsh net-destroy default || true
    virsh net-undefine default || true
  fi
fi

# 2) Optional backup
if $DO_BACKUP_ETC_LIBVIRT && [[ -d /etc/libvirt ]]; then
  TS="$(date +%Y%m%d_%H%M%S)"
  BK="/root/libvirt-backup-${TS}.tar.gz"
  info "Saving /etc/libvirt backup to ${BK}"
  $SUDO tar -czf "$BK" /etc/libvirt || warn "Backup failed (continuing without it)."
fi

# 3) Full package removal
info "Removing virtualization packages..."
# remove group first (if available)
$SUDO dnf -y group remove virtualization || true
# remove specific packages
$SUDO dnf -y remove \
  qemu-kvm qemu-img qemu-system-* \
  libvirt libvirt-* \
  virt-install virt-manager virt-viewer \
  edk2-ovmf* bridge-utils dnsmasq || true

# remove orphaned dependencies
$SUDO dnf -y autoremove || true

# 4) Clean state/config directories
info "Cleaning libvirt directories..."
$SUDO systemctl daemon-reload || true

# Keep /var/lib/libvirt/images so VM disks remain untouched.
$SUDO rm -rf /etc/libvirt /var/cache/libvirt /var/log/libvirt || true
$SUDO find /var/lib/libvirt -maxdepth 1 -mindepth 1 ! -name images -exec rm -rf {} + 2>/dev/null || true

if $NUKE_USER_STATE; then
  warn "Clearing user state (~/.config/virt-manager, etc.)"
  while IFS= read -r -d '' home; do
    $SUDO rm -rf "${home}/.config/virt-manager" "${home}/.cache/virt-manager" 2>/dev/null || true
  done < <(find /home -maxdepth 1 -mindepth 1 -type d -print0 2>/dev/null || true)
fi

# 5) Reinstall with versions
info "Refreshing dnf metadata..."
$SUDO dnf -y clean all
$SUDO dnf -y makecache

INSTALL_SPECS=()
for p in "${BASE_PKGS[@]}"; do
  INSTALL_SPECS+=("$(pkg_spec "$p")")
done

info "Installing packages: ${INSTALL_SPECS[*]}"
$SUDO dnf install -y "${INSTALL_SPECS[@]}"

# 6) (Optional) Enable version locks
if $ENABLE_VERSIONLOCK; then
  info "Enabling versionlock..."
  $SUDO dnf -y install dnf-plugins-core
  # Clear previous locks for these packages
  $SUDO dnf versionlock delete '*' >/dev/null 2>&1 || true
  # Lock each installed package
  for p in "${BASE_PKGS[@]}"; do
    [[ "$p" == @* ]] && continue
    if rpm -q "$p" >/dev/null 2>&1; then
      nvra="$(rpm -q --qf '%{name}-%{version}-%{release}.%{arch}\n' "$p" | head -n1)"
      $SUDO dnf versionlock add "$nvra" || true
    fi
  done
fi

# 7) Enable sockets/daemons (split + monolithic)
info "Re-enabling libvirt services..."
$SUDO systemctl enable --now virtqemud.socket virtlogd.socket virtstoraged.socket virtnetworkd.socket
$SUDO systemctl enable --now libvirtd || true

# 8) Default network (if config package was installed)
if command -v virsh >/dev/null 2>&1; then
  if virsh net-info default >/dev/null 2>&1; then
    virsh net-autostart default || true
    virsh net-start default || true
  fi
fi

# 9) Add user to the libvirt group
if $ADD_USER_TO_GROUP; then
  USER_TO_ADD="${SUDO_USER:-$(id -un)}"
  info "Adding ${USER_TO_ADD} to the 'libvirt' group..."
  $SUDO usermod -aG libvirt "$USER_TO_ADD" || true
  warn "You may need to log out and back in for permissions to take effect."
fi

# 10) Verification
info "Final versions:"
command -v virsh >/dev/null 2>&1 && virsh --version
command -v qemu-system-x86_64 >/dev/null 2>&1 && qemu-system-x86_64 --version || true
rpm -q libvirt || true
rpm -q qemu-kvm || true

info "All done."
