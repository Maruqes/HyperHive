#!/usr/bin/env bash
# show_ip_logs.sh — simple: ask for IP and print matching log lines
# Usage:
#   ./show_ip_logs.sh            # then it will ask you for IP
#   ./show_ip_logs.sh 1.2.3.4    # or pass IP as argument

set -euo pipefail

# default log dir (change if your logs are elsewhere)
LOG_DIR="$../master/npm-data/logs"
LOG_GLOB="proxy-host-*_access.log*"

IP="${1:-}"

if [[ -z "$IP" ]]; then
  read -rp "IP to search for: " IP
  if [[ -z "$IP" ]]; then
    echo "No IP provided. Exiting."
    exit 1
  fi
fi

echo "Searching for IP: $IP in $LOG_DIR/$LOG_GLOB"
echo "---------------------------------------------"

shopt -s nullglob
FILES=("$LOG_DIR"/$LOG_GLOB)
shopt -u nullglob

if [[ ${#FILES[@]} -eq 0 ]]; then
  echo "No log files found at: $LOG_DIR (pattern: $LOG_GLOB)"
  exit 1
fi

# Use zgrep so it handles .gz files too. -n prints line numbers, -H prints filename.
# If nothing found, print a friendly message.
if zgrep -n -H -- "$IP" "$LOG_DIR"/$LOG_GLOB 2>/dev/null | tee /dev/stderr | grep -q .; then
  # results already printed by zgrep (tee used so user sees results and we can detect emptiness)
  :
else
  echo "No matches for $IP in $LOG_DIR."
fi
