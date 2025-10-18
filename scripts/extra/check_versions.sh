#!/usr/bin/env bash
# check_virt_versions.sh
# Display installed package and binary versions for the virtualization stack

set -u
IFS=$'\n\t'

# RPM packages to inspect (feel free to adjust)
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

# Binaries to report via --version (first output line)
BINARIES=(
  virsh
  qemu-system-x86_64
  qemu-img
  virt-install
  virt-manager
  virt-viewer
  dnsmasq
)

line() { printf '%*s\n' "${COLUMNS:-80}" '' | tr ' ' '-'; }

echo "== Installed RPMs (NVRA) =="
{
  printf "%-40s %-30s\n" "Package" "Version-Release.Arch"
  printf "%-40s %-30s\n" "-------" "---------------------"
  for pkg in "${PACKAGES[@]}"; do
    if rpm -q "$pkg" &>/dev/null; then
      # package name plus version-release.arch
      vr=$(rpm -q --qf '%{VERSION}-%{RELEASE}.%{ARCH}\n' "$pkg" | head -n1)
      printf "%-40s %-30s\n" "$pkg" "$vr"
    else
      printf "%-40s %-30s\n" "$pkg" "NOT INSTALLED"
    fi
  done
} | column -t

echo
echo "== Runtime versions (command --version) =="
{
  printf "%-28s %s\n" "Command" "Version (first output line)"
  printf "%-28s %s\n" "-------" "----------------------------"
  for cmd in "${BINARIES[@]}"; do
    if command -v "$cmd" >/dev/null 2>&1; then
      v=$("$cmd" --version 2>&1 | head -n1 | tr -s ' ')
      printf "%-28s %s\n" "$cmd" "$v"
    else
      printf "%-28s %s\n" "$cmd" "NOT FOUND"
    fi
  done
} | column -t

echo
echo "== Libvirt (if available) =="
if command -v virsh >/dev/null 2>&1; then
  # This reports library, driver, and hypervisor versions
  if virsh -c qemu:///system version >/dev/null 2>&1; then
    virsh -c qemu:///system version
  else
    echo "virsh is installed but could not connect to libvirtd/virtqemud."
  fi
else
  echo "virsh is not installed."
fi

echo
echo "== DNF versionlocks (if any) =="
if command -v dnf >/dev/null 2>&1; then
  if dnf -q list installed dnf-plugins-core >/dev/null 2>&1; then
    dnf versionlock list || echo "No versionlocks defined."
  else
    echo "dnf-plugins-core is not installed (no versionlocks)."
  fi
else
  echo "DNF not found."
fi
