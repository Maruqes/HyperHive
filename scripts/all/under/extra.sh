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

# Install fio (Flexible I/O Tester - disk read/write benchmarking tool)
echo -e "${YELLOW}Installing fio...${NC}"
sudo dnf install -y fio

echo ""
echo -e "${BOLD}${GREEN}✓ Extra packages installation complete${NC}"
echo ""
