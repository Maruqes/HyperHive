#!/usr/bin/env bash
set -euo pipefail

# ===========================
#  FULL SYSTEM RESET SCRIPT
# ===========================
# This script will reset both NFS and Virtualization systems
# causing potential data loss and system disruption.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/under" && pwd)"

# Colors for output
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color
BOLD='\033[1m'

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
read -p "Do you understand and wish to continue? (type 'YES' in capital letters): " -r
echo ""
if [[ "$REPLY" != "YES" ]]; then
    echo "Operation cancelled. No changes were made."
    exit 0
fi

# Second confirmation
echo -e "${RED}${BOLD}FINAL CONFIRMATION${NC}"
echo "This is your last chance to abort before destructive operations begin."
echo ""
read -p "Are you absolutely sure? (type 'I UNDERSTAND' to proceed): " -r
echo ""
if [[ "$REPLY" != "I UNDERSTAND" ]]; then
    echo "Operation cancelled. No changes were made."
    exit 0
fi

echo ""
echo -e "${YELLOW}Starting system reset in 5 seconds...${NC}"
echo "Press Ctrl+C NOW to abort!"
sleep 5

echo ""
echo "═══════════════════════════════════════════════════════════════════════════"
echo ""

# Execute reset_virt.sh
if [[ -f "${SCRIPT_DIR}/reset_virt.sh" ]]; then
    echo -e "${BOLD}[1/3] Running Virtualization Reset...${NC}"
    echo ""
    bash "${SCRIPT_DIR}/reset_virt.sh"
    echo ""
    echo -e "${YELLOW}✓ Virtualization reset completed${NC}"
else
    echo -e "${RED}ERROR: reset_virt.sh not found at ${SCRIPT_DIR}/reset_virt.sh${NC}"
    exit 1
fi

echo ""
echo "═══════════════════════════════════════════════════════════════════════════"
echo ""

# Execute reset_nfs.sh
if [[ -f "${SCRIPT_DIR}/reset_nfs.sh" ]]; then
    echo -e "${BOLD}[2/3] Running NFS Reset...${NC}"
    echo ""
    bash "${SCRIPT_DIR}/reset_nfs.sh"
    echo ""
    echo -e "${YELLOW}✓ NFS reset completed${NC}"
else
    echo -e "${RED}ERROR: reset_nfs.sh not found at ${SCRIPT_DIR}/reset_nfs.sh${NC}"
    exit 1
fi

echo ""
echo "═══════════════════════════════════════════════════════════════════════════"
echo ""

# Execute extra.sh
if [[ -f "${SCRIPT_DIR}/extra.sh" ]]; then
    echo -e "${BOLD}[3/3] Installing Extra Packages...${NC}"
    echo ""
    bash "${SCRIPT_DIR}/extra.sh"
    echo ""
    echo -e "${YELLOW}✓ Extra packages installation completed${NC}"
else
    echo -e "${RED}WARNING: extra.sh not found at ${SCRIPT_DIR}/extra.sh${NC}"
fi

echo ""
echo "═══════════════════════════════════════════════════════════════════════════"
echo ""
echo -e "${BOLD}${YELLOW}✓ SYSTEM RESET COMPLETE${NC}"
echo ""
echo -e "${YELLOW}IMPORTANT NEXT STEPS:${NC}"
echo "  1. Log out and log back in for group permissions to take effect"
echo "  2. Verify virtualization services: systemctl status libvirtd"
echo "  3. Verify NFS services: systemctl status nfs-server"
echo "  4. Reconfigure any custom settings as needed"
echo "  5. Redefine VMs and networks if necessary"
echo "  6. Re-export NFS shares if needed"
echo ""
echo -e "${YELLOW}Check logs for any errors or warnings above.${NC}"
echo ""
