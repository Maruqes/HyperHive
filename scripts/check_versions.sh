#!/usr/bin/env bash
# check_virt_versions.sh
# Mostra versões instaladas de pacotes e binários do stack de virtualização

set -u
IFS=$'\n\t'

# Pacotes RPM a inspecionar (podes adicionar/remover aqui)
PACKAGES=(
  qemu-kvm
  qemu-img
  libvirt
  libvirt-devel
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
)

# Binários para mostrar runtime --version (a 1ª linha)
BINARIES=(
  virsh
  qemu-system-x86_64
  qemu-img
  virt-install
  virt-manager
  virt-viewer
  dnsmasq
)

line() { printf '%*s\n' "${COLUMNS:-80}" '' | tr ' ' '─'; }

echo "== RPM instalados (NVRA) =="
{
  printf "%-40s %-30s\n" "Pacote" "Versão-Release.Arquitetura"
  printf "%-40s %-30s\n" "------" "---------------------------"
  for pkg in "${PACKAGES[@]}"; do
    if rpm -q "$pkg" &>/dev/null; then
      # nome + versão-release.arquitetura
      vr=$(rpm -q --qf '%{VERSION}-%{RELEASE}.%{ARCH}\n' "$pkg" | head -n1)
      printf "%-40s %-30s\n" "$pkg" "$vr"
    else
      printf "%-40s %-30s\n" "$pkg" "NÃO INSTALADO"
    fi
  done
} | column -t

echo
echo "== Versões em runtime (comando --version) =="
{
  printf "%-28s %s\n" "Comando" "Versão (1ª linha do output)"
  printf "%-28s %s\n" "-------" "-----------------------------"
  for cmd in "${BINARIES[@]}"; do
    if command -v "$cmd" >/dev/null 2>&1; then
      v=$("$cmd" --version 2>&1 | head -n1 | tr -s ' ')
      printf "%-28s %s\n" "$cmd" "$v"
    else
      printf "%-28s %s\n" "$cmd" "NÃO ENCONTRADO"
    fi
  done
} | column -t

echo
echo "== Libvirt (se disponível) =="
if command -v virsh >/dev/null 2>&1; then
  # Isto mostra versões de biblioteca, driver e hypervisor
  if virsh -c qemu:///system version >/dev/null 2>&1; then
    virsh -c qemu:///system version
  else
    echo "virsh existe, mas não conseguiu ligar ao daemon (libvirtd/virtqemud)."
  fi
else
  echo "virsh não instalado."
fi

echo
echo "== Versionlocks do DNF (se existirem) =="
if command -v dnf >/dev/null 2>&1; then
  if dnf -q list installed dnf-plugins-core >/dev/null 2>&1; then
    dnf versionlock list || echo "Sem versionlocks definidos."
  else
    echo "dnf-plugins-core não instalado (sem versionlocks)."
  fi
else
  echo "DNF não encontrado."
fi
