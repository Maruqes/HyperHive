#!/usr/bin/env bash
set -euo pipefail

# ===========================
#  EXTRA PACKAGES INSTALLATION
# ===========================
# This script installs additional useful packages

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color
BOLD='\033[1m'

echo -e "${BOLD}${GREEN}Installing Extra Packages...${NC}"
echo ""

# Install tools:
# - fio: disk read/write benchmarking tool
# - stress-ng: CPU/memory/disk stress testing tool
# - memtester: user-space memory tester
echo -e "${YELLOW}Installing: fio, stress-ng, memtester...${NC}"
sudo dnf install -y fio stress-ng memtester

echo ""
echo -e "${BOLD}${GREEN}✓ Extra packages installation complete${NC}"
echo ""
