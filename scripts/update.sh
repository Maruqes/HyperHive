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
# Allow passing the choice as the first argument: ./update.sh 0|1|master|slave
if [ "$#" -gt 0 ]; then
    case "$1" in
        0|master|Master)
            UPDATE_MODE="master"
            ;;
        1|slave|Slave)
            UPDATE_MODE="slave"
            ;;
        -h|--help|help)
            echo "Usage: $0 [0|1|master|slave]"
            echo "  0 or master - update master (and slave when master is chosen)"
            echo "  1 or slave  - update only slave"
            exit 0
            ;;
        *)
            echo -e "${RED}Invalid argument: $1${NC}"
            echo "Usage: $0 [0|1|master|slave]"
            exit 1
            ;;
    esac
else
    echo "Select component to update:"
    echo "  0 - Master"
    echo "  1 - Slave"
    echo ""
    read -p "Enter your choice (0 or 1): " choice

    case $choice in
        0)
            UPDATE_MODE="master"
            ;;
        1)
            UPDATE_MODE="slave"
            ;;
        *)
            echo -e "${RED}Invalid choice. Please enter 0 for Master or 1 for Slave.${NC}"
            exit 1
            ;;
    esac
fi

echo ""
echo -e "${GREEN}Updating ${UPDATE_MODE}...${NC}"
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

# Step 2: Build Go application(s)
if [ "$UPDATE_MODE" = "master" ]; then
    # Build both master and slave
    echo -e "${YELLOW}[2/3] Building master and slave...${NC}"
    
    # Build master
    echo -e "  Building master..."
    cd "$HYPERHIVE_DIR/master" || exit 1
    if go build; then
        echo -e "${GREEN}  ✓ Master build successful${NC}"
    else
        echo -e "${RED}  ✗ Master build failed${NC}"
        exit 1
    fi
    
    # Build slave
    echo -e "  Building slave..."
    cd "$HYPERHIVE_DIR/slave" || exit 1
    if go build; then
        echo -e "${GREEN}  ✓ Slave build successful${NC}"
    else
        echo -e "${RED}  ✗ Slave build failed${NC}"
        exit 1
    fi
else
    # Build only slave
    echo -e "${YELLOW}[2/3] Building slave...${NC}"
    cd "$HYPERHIVE_DIR/slave" || exit 1
    if go build; then
        echo -e "${GREEN}✓ Slave build successful${NC}"
    else
        echo -e "${RED}✗ Slave build failed${NC}"
        exit 1
    fi
fi
echo ""

# Step 3: Restart PM2 service(s)
if [ "$UPDATE_MODE" = "master" ]; then
    # Restart both master and slave
    echo -e "${YELLOW}[3/3] Restarting PM2 services...${NC}"
    
    # Restart master
    echo -e "  Restarting hyperhive-master..."
    if sudo pm2 restart hyperhive-master; then
        echo -e "${GREEN}  ✓ Master service restarted successfully${NC}"
    else
        echo -e "${RED}  ✗ Failed to restart master service${NC}"
        exit 1
    fi
    
    # Restart slave
    echo -e "  Restarting hyperhive-slave..."
    if sudo pm2 restart hyperhive-slave; then
        echo -e "${GREEN}  ✓ Slave service restarted successfully${NC}"
    else
        echo -e "${RED}  ✗ Failed to restart slave service${NC}"
        exit 1
    fi
else
    # Restart only slave
    echo -e "${YELLOW}[3/3] Restarting PM2 service: hyperhive-slave...${NC}"
    if sudo pm2 restart hyperhive-slave; then
        echo -e "${GREEN}✓ Slave service restarted successfully${NC}"
    else
        echo -e "${RED}✗ Failed to restart slave service${NC}"
        exit 1
    fi
fi
echo ""

echo -e "${GREEN}================================${NC}"
echo -e "${GREEN}Update completed successfully!${NC}"
echo -e "${GREEN}================================${NC}"
