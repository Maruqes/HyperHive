#!/usr/bin/env bash
set -u -o pipefail

# ===========================
#  FULL SYSTEM RESET SCRIPT
# ===========================
# This script will reset both NFS and Virtualization systems
# causing potential data loss and system disruption.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/under" && pwd)"

SUDO_CMD=""
if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  if command -v sudo >/dev/null 2>&1; then
    SUDO_CMD="sudo"
  else
    echo "ERROR: precisa de root e não existe sudo."
    exit 1
  fi
fi

sudo_run() {
  if [[ -n "$SUDO_CMD" ]]; then
    "$SUDO_CMD" "$@"
  else
    "$@"
  fi
}

# Colors for output
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color
BOLD='\033[1m'

DEFAULT_LIBVIRT_NET="default"
LIBVIRT_DEFAULT_XML="/usr/share/libvirt/networks/default.xml"
LIBVIRT_BRIDGE="virbr0"
SYSCTL_DEFAULT_NET_CONF="/etc/sysctl.d/99-hyperhive-default-net.conf"
LIBVIRT_PORTS_TCP=(16509 16514)
NFS_PORTS_TCP=(2049 20048 111)
NFS_PORTS_UDP=(2049 20048 111)

# -----------------------------
# Error aggregation
# -----------------------------
ERRORS=()

run_step() {
  # run_step "Descrição" cmd arg1 arg2 ...
  local title="$1"; shift
  echo -e "${BOLD}${YELLOW}>> ${title}${NC}"
  if "$@"; then
    return 0
  else
    local rc=$?
    ERRORS+=("${title} (exit ${rc}): $*")
    echo -e "${RED}!! ERRO (exit ${rc}) em: ${title}${NC}"
    return 0  # <- importante: NÃO propagamos o erro, para continuar o script
  fi
}

run_step_sudo() {
  local title="$1"; shift
  run_step "$title" sudo_run "$@"
}

disable_firewalld() {
  if systemctl list-unit-files 2>/dev/null | grep -q '^firewalld\.service'; then
    sudo_run systemctl disable --now firewalld >/dev/null 2>&1 || return 1
    sudo_run systemctl mask firewalld >/dev/null 2>&1 || return 1
  fi
  return 0
}

ensure_iptables_accept() {
  local chain=$1 proto=$2 port=$3
  sudo_run iptables -C "$chain" -p "$proto" --dport "$port" -j ACCEPT 2>/dev/null || \
    sudo_run iptables -A "$chain" -p "$proto" --dport "$port" -j ACCEPT
}

ensure_iptables_rules() {
  command -v iptables >/dev/null 2>&1 || { echo -e "${YELLOW}iptables não encontrado; regras não aplicadas.${NC}"; return 0; }

  for p in "${NFS_PORTS_TCP[@]}"; do ensure_iptables_accept INPUT tcp "$p" || return 1; done
  for p in "${NFS_PORTS_UDP[@]}"; do ensure_iptables_accept INPUT udp "$p" || return 1; done
  for p in "${LIBVIRT_PORTS_TCP[@]}"; do ensure_iptables_accept INPUT tcp "$p" || return 1; done

  sudo_run iptables -C FORWARD -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || \
    sudo_run iptables -A FORWARD -m state --state RELATED,ESTABLISHED -j ACCEPT

  return 0
}

ensure_default_network_defined() {
  if sudo_run virsh net-info "$DEFAULT_LIBVIRT_NET" >/dev/null 2>&1; then
    return 0
  fi

  if [[ ! -f "$LIBVIRT_DEFAULT_XML" ]]; then
    echo -e "${RED}Default network XML not found at $LIBVIRT_DEFAULT_XML. Install libvirt-daemon-config-network.${NC}"
    return 1
  fi

  sudo_run virsh net-define "$LIBVIRT_DEFAULT_XML" >/dev/null 2>&1
}

default_network_is_active() {
  local info active
  if ! info="$(sudo_run virsh net-info "$DEFAULT_LIBVIRT_NET" 2>/dev/null)"; then
    return 1
  fi
  active=$(printf '%s\n' "$info" | awk -F ':' '/Active/ {gsub(/^[ \t]+/, "", $2); print tolower($2)}')
  [[ "$active" == "yes" ]]
}

ensure_default_network_running() {
  if ! default_network_is_active; then
    sudo_run virsh net-start "$DEFAULT_LIBVIRT_NET" >/dev/null 2>&1 || return 1
  fi
  sudo_run virsh net-autostart "$DEFAULT_LIBVIRT_NET" >/dev/null 2>&1
}

ensure_bridge_device() {
  if sudo_run ip link show "$LIBVIRT_BRIDGE" >/dev/null 2>&1; then
    return 0
  fi

  echo -e "${YELLOW}virbr0 missing; restarting libvirt networking...${NC}"
  for svc in virtnetworkd libvirtd; do
    sudo_run systemctl restart "$svc" >/dev/null 2>&1 || true
  done

  ensure_default_network_running || true

  sudo_run ip link show "$LIBVIRT_BRIDGE" >/dev/null 2>&1
}

ensure_ip_forwarding() {
  sudo_run bash -c "echo 'net.ipv4.ip_forward=1' > '$SYSCTL_DEFAULT_NET_CONF'" || return 1
  sudo_run sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1
}

ensure_default_network_connectivity() {
  echo ""
  echo -e "${BOLD}[4.b] Ensuring libvirt default network connectivity...${NC}"
  if ! command -v virsh >/dev/null 2>&1; then
    echo -e "${YELLOW}virsh not found; skipping default network configuration.${NC}"
    return 0
  fi

  ensure_default_network_defined || return 1
  ensure_default_network_running || return 1
  ensure_bridge_device || return 1
  ensure_ip_forwarding || return 1

  echo -e "${YELLOW}✓ Default libvirt network ready.${NC}"
  return 0
}

clear

echo -e "${RED}${BOLD}"
echo "╔════════════════════════════════════════════════════════════════════════════╗"
echo "║                                                                            ║"
echo "║                         ⚠️  CRITICAL WARNING  ⚠️                           ║"
echo "║                                                                            ║"
echo "╚════════════════════════════════════════════════════════════════════════════╝"
echo -e "${NC}"
echo ""
echo -e "${YELLOW}${BOLD}This script will perform a COMPLETE SYSTEM RESET of:${NC}"
echo ""
echo -e "${RED}  1. VIRTUALIZATION SYSTEM (reset_virt.sh)${NC}"
echo "     • All running VMs will be DESTROYED"
echo "     • All VM definitions will be UNDEFINED"
echo "     • All libvirt networks will be REMOVED"
echo "     • All libvirt configurations will be WIPED"
echo "     • VM disk images (.qcow2) will be PRESERVED"
echo "     • All virtualization packages will be REINSTALLED"
echo ""
echo -e "${RED}  2. NFS SYSTEM (reset_nfs.sh)${NC}"
echo "     • All NFS exports will be REMOVED"
echo "     • All NFS configurations will be RESET"
echo "     • NFS service will be RESTARTED"
echo "     • Exported directories may become INACCESSIBLE"
echo "     • Client connections will be DROPPED"
echo ""
echo -e "${RED}  3. EXTRA PACKAGES (extra.sh)${NC}"
echo "     • Additional useful packages will be installed"
echo "     • fio (disk I/O benchmarking tool)"
echo "     • stress-ng (system stress testing)"
echo "     • memtester (memory testing)"
echo ""
echo -e "${YELLOW}${BOLD}POTENTIAL DATA LOSS:${NC}"
echo "  • NFS shared data may become temporarily inaccessible"
echo "  • VM state and snapshots will be lost"
echo "  • Custom libvirt network configurations will be deleted"
echo "  • User virt-manager settings will be cleared"
echo ""
echo -e "${YELLOW}${BOLD}SYSTEM IMPACT:${NC}"
echo "  • Services will be stopped and restarted"
echo "  • System packages will be removed and reinstalled"
echo "  • Network connectivity may be briefly interrupted"
echo "  • You may need to log out and back in after completion"
echo ""
echo -e "${RED}${BOLD}════════════════════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${YELLOW}This operation is ${BOLD}IRREVERSIBLE${NC}${YELLOW} and should only be performed if you:"
echo "  • Understand the consequences"
echo "  • Have backed up important data"
echo "  • Are prepared to reconfigure services"
echo -e "${NC}"
echo ""

# First confirmation
ans1=""
read -r -p "Do you understand and wish to continue? (type 'YES' in capital letters): " ans1
echo ""
if [[ "$ans1" != "YES" ]]; then
  echo "Operation cancelled. No changes were made."
  exit 0
fi

# Second confirmation
echo -e "${RED}${BOLD}FINAL CONFIRMATION${NC}"
echo "This is your last chance to abort before destructive operations begin."
echo ""
ans2=""
read -r -p "Are you absolutely sure? (type 'I UNDERSTAND' to proceed): " ans2
echo ""
if [[ "$ans2" != "I UNDERSTAND" ]]; then
  echo "Operation cancelled. No changes were made."
  exit 0
fi

echo ""
echo -e "${YELLOW}Starting system reset in 5 seconds...${NC}"
echo "Press Ctrl+C NOW to abort!"
sleep 5

run_step_sudo "Disable/mask firewalld (best-effort)" disable_firewalld

echo ""
echo "═══════════════════════════════════════════════════════════════════════════"
echo ""

FORCE_FLAG="--force"

# Execute reset_virt.sh
if [[ -f "${SCRIPT_DIR}/reset_virt.sh" ]]; then
  echo -e "${BOLD}[1/3] Running Virtualization Reset...${NC}"
  echo ""
  run_step_sudo "reset_virt.sh ${FORCE_FLAG}" bash "${SCRIPT_DIR}/reset_virt.sh" "${FORCE_FLAG}"
  echo ""
  echo -e "${YELLOW}✓ Virtualization reset step finished (check summary at end).${NC}"
else
  ERRORS+=("reset_virt.sh not found at ${SCRIPT_DIR}/reset_virt.sh")
  echo -e "${RED}ERROR: reset_virt.sh not found at ${SCRIPT_DIR}/reset_virt.sh${NC}"
fi

echo ""
echo "═══════════════════════════════════════════════════════════════════════════"
echo ""

# Execute reset_nfs.sh
if [[ -f "${SCRIPT_DIR}/reset_nfs.sh" ]]; then
  echo -e "${BOLD}[2/3] Running NFS Reset...${NC}"
  echo ""
  run_step_sudo "reset_nfs.sh ${FORCE_FLAG}" bash "${SCRIPT_DIR}/reset_nfs.sh" "${FORCE_FLAG}"
  echo ""
  echo -e "${YELLOW}✓ NFS reset step finished (check summary at end).${NC}"
else
  ERRORS+=("reset_nfs.sh not found at ${SCRIPT_DIR}/reset_nfs.sh")
  echo -e "${RED}ERROR: reset_nfs.sh not found at ${SCRIPT_DIR}/reset_nfs.sh${NC}"
fi

echo ""
echo "═══════════════════════════════════════════════════════════════════════════"
echo ""

# Execute extra.sh
if [[ -f "${SCRIPT_DIR}/extra.sh" ]]; then
  echo -e "${BOLD}[3/3] Installing Extra Packages...${NC}"
  echo ""
  run_step_sudo "extra.sh" bash "${SCRIPT_DIR}/extra.sh"
  echo ""
  echo -e "${YELLOW}✓ Extra packages step finished (check summary at end).${NC}"
else
  echo -e "${YELLOW}WARNING: extra.sh not found at ${SCRIPT_DIR}/extra.sh${NC}"
fi

echo ""
echo "═══════════════════════════════════════════════════════════════════════════"
echo ""

# Install additional development packages requested
echo -e "${BOLD}[3.a] Installing additional dev packages...${NC}"
if command -v dnf >/dev/null 2>&1; then
  run_step_sudo "dnf install libmaxminddb-devel ncurses-devel pkgconf-pkg-config" \
    dnf install -y libmaxminddb-devel ncurses-devel pkgconf-pkg-config
else
  ERRORS+=("dnf not found; could not install libmaxminddb-devel/ncurses-devel/systemd-devel")
  echo -e "${YELLOW}dnf not found; skipping dev packages.${NC}"
fi

# Install systemd development headers (needed for github.com/coreos/go-systemd/v22/sdjournal)
echo -e "${BOLD}[3.b] Installing systemd-devel (sd-journal headers)...${NC}"
if command -v dnf >/dev/null 2>&1; then
  run_step_sudo "dnf install systemd-devel" dnf install -y systemd-devel
fi

echo ""
echo -e "${BOLD}[4/4] Updating iptables rules and permissions...${NC}"
echo ""
run_step_sudo "iptables rules for NFS/libvirt" ensure_iptables_rules
echo -e "${YELLOW}✓ iptables step finished (check summary at end).${NC}"

run_step_sudo "Ensure libvirt default network connectivity" ensure_default_network_connectivity

CURRENT_USER="${SUDO_USER:-$(id -un)}"
if getent group kvm >/dev/null 2>&1; then
  if id -nG "$CURRENT_USER" | tr ' ' '\n' | grep -Fxq kvm; then
    echo -e "${YELLOW}User ${CURRENT_USER} already in 'kvm' group.${NC}"
  else
    run_step_sudo "Add ${CURRENT_USER} to kvm group" usermod -aG kvm "$CURRENT_USER"
  fi
else
  echo -e "${YELLOW}'kvm' group not present; skipping group membership update.${NC}"
fi

echo ""
echo "═══════════════════════════════════════════════════════════════════════════"
echo ""
echo -e "${BOLD}${YELLOW}✓ SYSTEM RESET FINISHED${NC}"
echo ""

echo -e "${YELLOW}IMPORTANT NEXT STEPS:${NC}"
echo "  1. Log out and log back in for group permissions to take effect"
echo "  2. Verify virtualization services: systemctl status libvirtd"
echo "  3. Verify NFS services: systemctl status nfs-server"
echo "  4. Confirm iptables rules: iptables -S INPUT | grep -E '(2049|20048|111|16509|16514)'"
echo ""

# -----------------------------
# Error summary (final)
# -----------------------------
if (( ${#ERRORS[@]} > 0 )); then
  echo -e "${RED}${BOLD}════════════════════════════════════════════════════════════════════════════${NC}"
  echo -e "${RED}${BOLD}ERROS DETETADOS (${#ERRORS[@]})${NC}"
  echo -e "${RED}${BOLD}════════════════════════════════════════════════════════════════════════════${NC}"
  for e in "${ERRORS[@]}"; do
    echo -e "${RED}- ${e}${NC}"
  done
  echo ""
  echo -e "${YELLOW}O script continuou apesar dos erros. Revê a lista acima.${NC}"
  exit 1
else
  echo -e "${YELLOW}Sem erros. ✅${NC}"
  exit 0
fi
