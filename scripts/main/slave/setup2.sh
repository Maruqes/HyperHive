#!/usr/bin/env bash
set -euo pipefail

# HyperHive Slave Setup Orchestrator (Step 2)
# - Verifies the "512rede" interface exists
# - Asks user questions and writes: <repo>/slave/.env

log() { echo -e "[setup2-slave] $*"; }
die() { echo -e "[setup2-slave] ERROR: $*" >&2; exit 1; }

# Re-run as root if needed
if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  if command -v sudo >/dev/null 2>&1; then
    exec sudo -E bash "$0" "$@"
  else
    die "This script must be run as root (sudo not found)."
  fi
fi

# Resolve repo root (assumes this file is in: scripts/main/slave/setup2.sh)
REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"   # .../HyperHive
SLAVE_ENV="$REPO_ROOT/slave/.env"
SCRIPTS_DIR="$REPO_ROOT/scripts"

backup_if_exists() {
  local f="$1"
  if [[ -f "$f" ]]; then
    local ts
    ts="$(date +%Y%m%d_%H%M%S)"
    cp -a "$f" "${f}.bak.${ts}"
    log "Backed up existing $(basename "$f") -> $(basename "${f}.bak.${ts}")"
  fi
}

ask() {
  # ask "Prompt" "default"
  local prompt="$1"
  local def="${2:-}"
  local ans=""
  if [[ -n "$def" ]]; then
    read -r -p "$prompt [$def]: " ans
    ans="${ans:-$def}"
  else
    read -r -p "$prompt: " ans
  fi
  printf "%s" "$ans"
}

ask_required() {
  local prompt="$1"
  local def="${2:-}"
  local ans=""
  while true; do
    ans="$(ask "$prompt" "$def")"
    if [[ -n "${ans// }" ]]; then
      printf "%s" "$ans"
      return 0
    fi
    echo "  -> This value is required."
  done
}

get_iface_ipv4() {
  local iface="$1"
  ip -o -4 addr show dev "$iface" 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -n1
}

# -----------------------------
# 1) check 512rede exists
# -----------------------------
log "Checking required interface: 512rede"
if ! ip link show "512rede" >/dev/null 2>&1; then
  echo
  echo "The network interface '512rede' was NOT found."
  echo "Please run setup1.sh on this machine first (it creates/renames the interface)."
  echo
  echo "Example:"
  echo "  $SCRIPTS_DIR/main/slave/setup1.sh"
  echo
  exit 1
fi

log "Interface '512rede' exists."
echo
log "Current IPv4 addresses (for reference):"
ip -o -4 addr show | awk '{printf "  - %-12s %s\n",$2,$4}' || true
echo

SLAVE_IP_DEFAULT="$(get_iface_ipv4 512rede)"
[[ -n "$SLAVE_IP_DEFAULT" ]] || SLAVE_IP_DEFAULT="192.168.76.55"

# -----------------------------
# 2) build slave/.env
# -----------------------------
log "Collecting values for SLAVE .env"

MASTER_IP="$(ask_required "MASTER_IP (IP of the master on the 512rede network)" "192.168.76.1")"
SLAVE_IP="$(ask_required "SLAVE_IP (current machine IP on 512rede)" "$SLAVE_IP_DEFAULT")"

echo
echo "Other slaves IPs rule:"
echo "  - Add ALL other slave IPs (and you may include MASTER_IP),"
echo "  - BUT exclude the current SLAVE_IP."
echo "Enter them comma-separated (optional)."
echo

OTHERS_RAW="$(ask "Other slave IPs (comma-separated, optional)" "")"

IFS=',' read -r -a others_arr <<< "$OTHERS_RAW"
others_clean=()
for ipx in "${others_arr[@]}"; do
  ipx="${ipx//[[:space:]]/}"          # trim spaces
  [[ -z "$ipx" ]] && continue
  [[ "$ipx" == "$SLAVE_IP" ]] && continue

  # optional: avoid duplicates
  skip=0
  for e in "${others_clean[@]}"; do
    [[ "$e" == "$ipx" ]] && skip=1 && break
  done
  [[ "$skip" -eq 1 ]] && continue

  others_clean+=("$ipx")
done

# Build dynamic OTHER_SLAVEx_IP lines
OTHER_SLAVES_LINES=""
idx=1
for ipx in "${others_clean[@]}"; do
  OTHER_SLAVES_LINES+=$'OTHER_SLAVE'"${idx}"$'_IP='"${ipx}"$'\n'
  ((idx++))
done


DIRTY_RATIO_PERCENT="$(ask "DIRTY_RATIO_PERCENT" "15")"
DIRTY_BACKGROUND_RATIO_PERCENT="$(ask "DIRTY_BACKGROUND_RATIO_PERCENT" "8")"
MODE_SLAVE="$(ask "MODE (dev/prod)" "prod")"

DEFAULT_MACHINE_NAME="$(hostname -s 2>/dev/null || hostname 2>/dev/null || echo "")"
MACHINE_NAME="$(ask_required "MACHINE_NAME" "$DEFAULT_MACHINE_NAME")"

VNC_MIN_PORT="$(ask_required "VNC_MIN_PORT" "35000")"
VNC_MAX_PORT="$(ask_required "VNC_MAX_PORT" "35999")"

QEMU_UID="$(ask "QEMU_UID" "107")"
QEMU_GID="$(ask "QEMU_GID" "107")"

EXTRA_K8S_IPS="$(ask "EXTRA_K8S_IPS (optional, leave empty if not needed)" "")"

mkdir -p "$(dirname "$SLAVE_ENV")"
backup_if_exists "$SLAVE_ENV"

cat > "$SLAVE_ENV" <<EOF
MASTER_IP=${MASTER_IP}
SLAVE_IP=${SLAVE_IP}

${OTHER_SLAVES_LINES}

DIRTY_RATIO_PERCENT=${DIRTY_RATIO_PERCENT}
DIRTY_BACKGROUND_RATIO_PERCENT=${DIRTY_BACKGROUND_RATIO_PERCENT}

MODE=${MODE_SLAVE}
MACHINE_NAME=${MACHINE_NAME}
VNC_MIN_PORT=${VNC_MIN_PORT}
VNC_MAX_PORT=${VNC_MAX_PORT}

QEMU_UID=${QEMU_UID}
QEMU_GID=${QEMU_GID}

EXTRA_K8S_IPS=${EXTRA_K8S_IPS}
EOF

log "Wrote: $SLAVE_ENV"
log "Done."
