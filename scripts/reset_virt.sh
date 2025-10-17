#!/usr/bin/env bash
set -euo pipefail

# ===========================
#  CONFIGURAÇÃO PELO UTILIZADOR
# ===========================

# (Obrigatório ler) Define aqui as versões exatas que queres — ou deixa vazio para instalar a última.
# Usa as chaves exatamente como os nomes dos pacotes dnf.
# Dica: vê versões disponíveis com: dnf list --showduplicates <pacote>
declare -A VERSAO=(
  ["qemu-kvm"]="9.2.4-2.fc42"
  ["qemu-img"]="9.2.4-2.fc42"
  ["libvirt"]="11.0.0-4.fc42"
  ["libvirt-devel"]="11.0.0-4.fc42"
  ["libvirt-daemon"]="11.0.0-4.fc42"
  ["libvirt-daemon-kvm"]="11.0.0-4.fc42"
  ["libvirt-daemon-driver-qemu"]="11.0.0-4.fc42"
  ["libvirt-daemon-driver-network"]="11.0.0-4.fc42"
  ["libvirt-daemon-driver-storage"]="11.0.0-4.fc42"
  ["libvirt-daemon-config-network"]="11.0.0-4.fc42"
  ["libvirt-daemon-config-nwfilter"]="11.0.0-4.fc42"
  ["virt-install"]="5.0.0-2.fc42"
  ["virt-manager"]="5.0.0-2.fc42"
  ["virt-viewer"]="11.0-15.fc42"
  ["edk2-ovmf"]="20250523-16.fc42"
  ["bridge-utils"]="1.7.1-12.fc42"
  ["dnsmasq"]="2.90-6.fc42"
  ["pkgconf-pkg-config"]="2.3.0-2.fc42"
  ["pkg-config"]=""   # não instalado
)


# Trancar versões após instalar? (usa dnf versionlock)
ENABLE_VERSIONLOCK=true

# Apagar *todo* o estado do sistema/libvirt (configs, logs, caches) e também
# os ficheiros de estado dos utilizadores (ex.: ~/.config/virt-manager)?
# ⚠️ NÃO apaga discos de VMs (.qcow2) — isso tens de apagar à parte.
NUKE_USER_STATE=false

# Fazer backup de /etc/libvirt antes de limpar
DO_BACKUP_ETC_LIBVIRT=true

# Forçar a destruição/undefine de VMs e redes existentes antes de remover
FORCE_KILL_VMS=true

# Adicionar o utilizador atual ao grupo 'libvirt'
ADD_USER_TO_GROUP=true

# ===========================
#  LÓGICA
# ===========================

need_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "Falta o comando: $1"; exit 1; }; }
need_cmd dnf
need_cmd systemctl

SUDO="${SUDO:-}"
if [[ $EUID -ne 0 ]]; then
  SUDO="sudo"
fi

info()  { echo -e "\033[1;34m[INFO]\033[0m $*"; }
warn()  { echo -e "\033[1;33m[WARN]\033[0m $*"; }
err()   { echo -e "\033[1;31m[ERRO]\033[0m $*" >&2; }

pkg_spec() {
  local name="$1"
  local ver="${VERSAO[$name]:-}"
  if [[ "$name" == @* ]]; then
    # grupos (ex.: @virtualization) não têm versão
    echo "$name"
  elif [[ -n "$ver" ]]; then
    echo "${name}-${ver}"
  else
    echo "$name"
  fi
}

# Lista base (inclui grupo)
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
)

STOP_SOCKETS=(
  virtqemud.socket virtlogd.socket virtstoraged.socket virtnetworkd.socket
)
STOP_SERVICES=(
  libvirtd virtqemud virtlogd virtstoraged virtnetworkd
)

# 1) Parar serviços e matar VMs/rede
info "A parar serviços de virtualização…"
for s in "${STOP_SOCKETS[@]}";   do $SUDO systemctl stop "$s" 2>/dev/null || true; done
for s in "${STOP_SERVICES[@]}";  do $SUDO systemctl stop "$s" 2>/dev/null || true; done
$SUDO systemctl disable libvirtd 2>/dev/null || true

if $FORCE_KILL_VMS && command -v virsh >/dev/null 2>&1; then
  warn "A destruir VMs e redes definidas no libvirt…"
  # destruir VMs a correr
  mapfile -t RUNNING < <(virsh list --name | sed '/^$/d' || true)
  for vm in "${RUNNING[@]:-}"; do virsh destroy "$vm" || true; done
  # remover definições (inclui NVRAM)
  mapfile -t ALL < <(virsh list --all --name | sed '/^$/d' || true)
  for vm in "${ALL[@]:-}"; do virsh undefine --nvram "$vm" || virsh undefine "$vm" || true; done
  # destruir/undef default network se existir
  if virsh net-info default >/dev/null 2>&1; then
    virsh net-destroy default || true
    virsh net-undefine default || true
  fi
fi

# 2) Backup opcional
if $DO_BACKUP_ETC_LIBVIRT && [[ -d /etc/libvirt ]]; then
  TS="$(date +%Y%m%d_%H%M%S)"
  BK="/root/libvirt-backup-${TS}.tar.gz"
  info "A guardar backup de /etc/libvirt em ${BK}"
  $SUDO tar -czf "$BK" /etc/libvirt || warn "Falhou backup (segue sem backup)."
fi

# 3) Remoção total de pacotes
info "A remover pacotes de virtualização…"
# remover grupo primeiro (se existir)
$SUDO dnf -y group remove virtualization || true
# remover pacotes específicos
$SUDO dnf -y remove \
  qemu-kvm qemu-img qemu-system-* \
  libvirt libvirt-* \
  virt-install virt-manager virt-viewer \
  edk2-ovmf* bridge-utils dnsmasq || true

# limpar dependências órfãs
$SUDO dnf -y autoremove || true

# 4) Limpeza de estado/configuração
info "A limpar diretórios do libvirt…"
$SUDO systemctl daemon-reload || true

# Mantemos /var/lib/libvirt/images para não tocar nos discos das VMs.
$SUDO rm -rf /etc/libvirt /var/cache/libvirt /var/log/libvirt || true
$SUDO find /var/lib/libvirt -maxdepth 1 -mindepth 1 ! -name images -exec rm -rf {} + 2>/dev/null || true

if $NUKE_USER_STATE; then
  warn "A limpar estado de utilizadores (~/.config/virt-manager, etc.)"
  while IFS= read -r -d '' home; do
    $SUDO rm -rf "${home}/.config/virt-manager" "${home}/.cache/virt-manager" 2>/dev/null || true
  done < <(find /home -maxdepth 1 -mindepth 1 -type d -print0 2>/dev/null || true)
fi

# 5) Reinstalação com versões
info "A atualizar índices do dnf…"
$SUDO dnf -y clean all
$SUDO dnf -y makecache

INSTALL_SPECS=()
for p in "${BASE_PKGS[@]}"; do
  INSTALL_SPECS+=("$(pkg_spec "$p")")
done

info "A instalar pacotes: ${INSTALL_SPECS[*]}"
$SUDO dnf install -y "${INSTALL_SPECS[@]}"

# 6) (Opcional) Trancar versões
if $ENABLE_VERSIONLOCK; then
  info "A ativar versionlock…"
  $SUDO dnf -y install dnf-plugins-core
  # Limpar locks antigos destes pacotes
  $SUDO dnf versionlock delete '*' >/dev/null 2>&1 || true
  # Lock por pacote instalado
  for p in "${BASE_PKGS[@]}"; do
    [[ "$p" == @* ]] && continue
    if rpm -q "$p" >/dev/null 2>&1; then
      nvra="$(rpm -q --qf '%{name}-%{version}-%{release}.%{arch}\n' "$p" | head -n1)"
      $SUDO dnf versionlock add "$nvra" || true
    fi
  done
fi

# 7) Ativar sockets/daemons (split + monolítico)
info "A (re)ativar serviços libvirt…"
$SUDO systemctl enable --now virtqemud.socket virtlogd.socket virtstoraged.socket virtnetworkd.socket
$SUDO systemctl enable --now libvirtd || true

# 8) Rede default (se pacote de config instalou)
if command -v virsh >/dev/null 2>&1; then
  if virsh net-info default >/dev/null 2>&1; then
    virsh net-autostart default || true
    virsh net-start default || true
  fi
fi

# 9) Adicionar utilizador ao grupo libvirt
if $ADD_USER_TO_GROUP; then
  USER_TO_ADD="${SUDO_USER:-$(id -un)}"
  info "A adicionar ${USER_TO_ADD} ao grupo 'libvirt'…"
  $SUDO usermod -aG libvirt "$USER_TO_ADD" || true
  warn "Poderás ter de fazer logout/login para aplicar permissões."
fi

# 10) Verificações
info "Versões finais:"
command -v virsh >/dev/null 2>&1 && virsh --version
command -v qemu-system-x86_64 >/dev/null 2>&1 && qemu-system-x86_64 --version || true
rpm -q libvirt || true
rpm -q qemu-kvm || true

info "Concluído ✅"
