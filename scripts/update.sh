#!/bin/bash

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get the HyperHive root directory (parent of scripts folder)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HYPERHIVE_DIR="$(dirname "$SCRIPT_DIR")"

echo -e "${YELLOW}HyperHive Update Script${NC}"
echo "================================"
echo ""
echo "Select component to update:"
echo "  0 - Master"
echo "  1 - Slave"
echo ""
read -p "Enter your choice (0 or 1): " choice

case $choice in
    0)
        COMPONENT="master"
        SERVICE="hyperhive-master"
        ;;
    1)
        COMPONENT="slave"
        SERVICE="hyperhive-slave"
        ;;
    *)
        echo -e "${RED}Invalid choice. Please enter 0 for Master or 1 for Slave.${NC}"
        exit 1
        ;;
esac

echo ""
echo -e "${GREEN}Updating ${COMPONENT}...${NC}"
echo ""

# Step 1: Git pull
echo -e "${YELLOW}[1/3] Pulling latest changes from git...${NC}"
cd "$HYPERHIVE_DIR" || exit 1
if git pull; then
    echo -e "${GREEN}✓ Git pull successful${NC}"
else
    echo -e "${RED}✗ Git pull failed${NC}"
    exit 1
fi
echo ""

# Step 2: Build Go application
echo -e "${YELLOW}[2/3] Building ${COMPONENT}...${NC}"
cd "$HYPERHIVE_DIR/$COMPONENT" || exit 1
if go build; then
    echo -e "${GREEN}✓ Build successful${NC}"
else
    echo -e "${RED}✗ Build failed${NC}"
    exit 1
fi
echo ""

# Step 3: Restart PM2 service
echo -e "${YELLOW}[3/3] Restarting PM2 service: ${SERVICE}...${NC}"
if sudo pm2 restart "$SERVICE"; then
    echo -e "${GREEN}✓ Service restarted successfully${NC}"
else
    echo -e "${RED}✗ Failed to restart service${NC}"
    exit 1
fi
echo ""

echo -e "${GREEN}================================${NC}"
echo -e "${GREEN}Update completed successfully!${NC}"
echo -e "${GREEN}================================${NC}"
