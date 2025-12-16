#!/usr/bin/env bash
set -euo pipefail

# HyperHive Setup Orchestrator (Step 1)
# Order:
# 1) allow_root_ssh.sh
# 2) change_interface_name.sh  (asks user which current interface name to use)
# 3) install.sh
# 4) reboot

log() { echo -e "[setup1] $*"; }
die() { echo -e "[setup1] ERROR: $*" >&2; exit 1; }

# Re-run as root if needed
if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  if command -v sudo >/dev/null 2>&1; then
    exec sudo -E bash "$0" "$@"
  else
    die "This script must be run as root (sudo not found)."
  fi
fi

# Resolve scripts paths reliably
SCRIPTS_DIR="$(cd "$(dirname "$0")/../.." && pwd)"   # -> .../scripts
ALL_DIR="$SCRIPTS_DIR/all"

ALLOW_ROOT="$ALL_DIR/allow_root_ssh.sh"
CHANGE_IF="$ALL_DIR/change_interface_name.sh"
INSTALL="$ALL_DIR/install.sh"

[[ -f "$ALLOW_ROOT" ]] || die "Missing: $ALLOW_ROOT"
[[ -f "$CHANGE_IF"  ]] || die "Missing: $CHANGE_IF"
[[ -f "$INSTALL"    ]] || die "Missing: $INSTALL"

log "Scripts root: $SCRIPTS_DIR"
log "Step 1/4: Allow root SSH..."
bash "$ALLOW_ROOT"

log "Step 2/4: Change network interface name..."
log "Detecting network interfaces (excluding loopback)..."

# Show interfaces to the user
ifaces=()
while IFS= read -r line; do
  # example: "2: enp3s0: <BROADCAST,MULTICAST,UP,LOWER_UP> ..."
  name="$(awk -F': ' '{print $2}' <<<"$line" | awk -F': ' '{print $1}' | awk '{print $1}')"
  [[ -z "$name" ]] && continue
  [[ "$name" == "lo" ]] && continue
  ifaces+=("$name")
done < <(ip -o link show)

if [[ "${#ifaces[@]}" -eq 0 ]]; then
  die "No network interfaces were found (unexpected)."
fi

log "Available interfaces:"
for i in "${!ifaces[@]}"; do
  idx=$((i + 1))
  echo "  $idx) ${ifaces[$i]}"
done

echo
read -r -p "Pick the CURRENT interface name to rename (type the number or the exact name): " pick

chosen=""
if [[ "$pick" =~ ^[0-9]+$ ]]; then
  n="$pick"
  if (( n < 1 || n > ${#ifaces[@]} )); then
    die "Invalid selection number."
  fi
  chosen="${ifaces[$((n - 1))]}"
else
  # user typed a name
  for x in "${ifaces[@]}"; do
    if [[ "$x" == "$pick" ]]; then
      chosen="$pick"
      break
    fi
  done
fi

[[ -n "$chosen" ]] || die "Could not resolve your selection to a valid interface."

log "Running: $(basename "$CHANGE_IF") <current_name>"
log "Using current_name: $chosen"
bash "$CHANGE_IF" "$chosen"

log "Step 3/4: Running install..."
bash "$INSTALL"

log "Step 4/4: Rebooting system..."
echo
read -r -p "Install finished. Reboot now? [Y/n]: " ans
ans="${ans:-Y}"
if [[ "$ans" =~ ^[Yy]$ ]]; then
  log "Rebooting..."
  reboot
else
  log "Reboot skipped. Please reboot manually later."
fi
