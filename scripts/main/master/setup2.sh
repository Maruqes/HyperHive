#!/usr/bin/env bash
set -euo pipefail

# HyperHive Master Setup Orchestrator (Step 2)
# - Verifies the "512rede" interface exists (created/renamed by setup1)
# - Asks if you use an internal HyperHive network (master provides DHCP+Internet to slaves)
#   - If yes, runs scripts/master/setup_dhcp.sh
# - Collects answers to generate:
#     - <repo>/master/.env
#     - <repo>/slave/.env
#   (master generates BOTH, because it runs both processes)

log() { echo -e "[setup2] $*"; }
die() { echo -e "[setup2] ERROR: $*" >&2; exit 1; }

# -----------------------------
# root / sudo
# -----------------------------
if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  if command -v sudo >/dev/null 2>&1; then
    exec sudo -E bash "$0" "$@"
  else
    die "This script must be run as root (sudo not found)."
  fi
fi

# -----------------------------
# paths
# -----------------------------
REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"   # .../HyperHive
SCRIPTS_DIR="$REPO_ROOT/scripts"

MASTER_ENV="$REPO_ROOT/master/.env"
SLAVE_ENV="$REPO_ROOT/slave/.env"

SETUP_DHCP="$SCRIPTS_DIR/master/setup_dhcp.sh"

# -----------------------------
# helpers
# -----------------------------
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

ask_yes_no() {
  local prompt="$1"
  local def="${2:-Y}" # Y or N
  local ans=""
  while true; do
    if [[ "$def" == "Y" ]]; then
      read -r -p "$prompt [Y/n]: " ans
      ans="${ans:-Y}"
    else
      read -r -p "$prompt [y/N]: " ans
      ans="${ans:-N}"
    fi
    case "$ans" in
      Y|y) return 0 ;;
      N|n) return 1 ;;
      *) echo "  -> Please answer Y or N." ;;
    esac
  done
}

get_default_src_ip() {
  # tries to get the "src" IP used to reach the internet
  ip -4 route get 1.1.1.1 2>/dev/null | awk '
    {for(i=1;i<=NF;i++) if($i=="src") {print $(i+1); exit}}
  '
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
  echo "This usually means you didn't run setup1.sh (or interface rename) on this machine."
  echo
  echo "Please run:"
  echo "  $SCRIPTS_DIR/main/master/setup1.sh"
  echo
  exit 1
fi

log "Interface '512rede' exists."
echo
log "Current IPv4 addresses (for reference):"
ip -o -4 addr show | awk '{printf "  - %-12s %s\n",$2,$4}' || true
echo

MASTER_512REDE_IP_DEFAULT="$(get_iface_ipv4 512rede)"
[[ -n "$MASTER_512REDE_IP_DEFAULT" ]] || MASTER_512REDE_IP_DEFAULT="192.168.76.1"

# -----------------------------
# 2) internal vs external network
# -----------------------------
echo "Network mode question:"
echo "  - Internal HyperHive network: the MASTER runs DHCP and shares Internet to other slaves."
echo "  - External normal network: everyone is connected to a regular LAN/router (no DHCP sharing by HyperHive)."
echo

if ask_yes_no "Are you using the INTERNAL HyperHive network (master provides DHCP+Internet)?" "Y"; then
  [[ -f "$SETUP_DHCP" ]] || die "Missing DHCP setup script: $SETUP_DHCP"
  log "Running DHCP setup: $SETUP_DHCP"
  bash "$SETUP_DHCP"
else
  log "Skipping DHCP setup (external network selected)."
fi

echo

# -----------------------------
# 3) build master/.env
# -----------------------------
log "Collecting values for MASTER .env"

DEFAULT_MASTER_INTERNET_IP="$(get_default_src_ip)"
if [[ -z "$DEFAULT_MASTER_INTERNET_IP" ]]; then
  DEFAULT_MASTER_INTERNET_IP="$MASTER_512REDE_IP_DEFAULT"
fi

MODE_MASTER="$(ask "MODE (dev/prod)" "prod")"
QEMU_UID="$(ask "QEMU_UID" "107")"
QEMU_GID="$(ask "QEMU_GID" "107")"

SPRITE_MIN="$(ask_required "SPRITE_MIN port" "9600")"
SPRITE_MAX="$(ask_required "SPRITE_MAX port" "9700")"

MASTER_INTERNET_IP="$(ask_required "MASTER_INTERNET_IP (IP used for outside/internet access)" "$DEFAULT_MASTER_INTERNET_IP")"
MAIN_LINK="$(ask_required "MAIN_LINK (public API base URL)" "https://hyperhive.maruqes.com/api")"

# Optional fields (default empty, but still asked)
GOACCESS_ENABLE_PANELS="$(ask "GOACCESS_ENABLE_PANELS (optional, leave empty for default)" "")"
GOACCESS_DISABLE_PANELS="$(ask "GOACCESS_DISABLE_PANELS (optional, leave empty for default)" "")"
GOACCESS_GEOIP_LICENSE_KEY="$(ask "GOACCESS_GEOIP_LICENSE_KEY (optional)" "")"
GOACCESS_GEOIP_EDITION="$(ask "GOACCESS_GEOIP_EDITION (optional)" "GeoLite2-City")"
VAPID_PUBLIC_KEY="$(ask "VAPID_PUBLIC_KEY (optional)" "")"
VAPID_PRIVATE_KEY="$(ask "VAPID_PRIVATE_KEY (optional)" "")"

mkdir -p "$(dirname "$MASTER_ENV")"
backup_if_exists "$MASTER_ENV"

cat > "$MASTER_ENV" <<EOF
MODE=${MODE_MASTER}
QEMU_UID=${QEMU_UID}
QEMU_GID=${QEMU_GID}
SPRITE_MIN=${SPRITE_MIN}
SPRITE_MAX=${SPRITE_MAX}
MASTER_INTERNET_IP=${MASTER_INTERNET_IP}
MAIN_LINK=${MAIN_LINK}
GOACCESS_ENABLE_PANELS=${GOACCESS_ENABLE_PANELS}
GOACCESS_DISABLE_PANELS=${GOACCESS_DISABLE_PANELS}
GOACCESS_GEOIP_LICENSE_KEY=${GOACCESS_GEOIP_LICENSE_KEY}
GOACCESS_GEOIP_EDITION=${GOACCESS_GEOIP_EDITION}
VAPID_PUBLIC_KEY=${VAPID_PUBLIC_KEY}
VAPID_PRIVATE_KEY=${VAPID_PRIVATE_KEY}
EOF

log "Wrote: $MASTER_ENV"
echo

# Also keep a copy in scripts/.env (useful for scripts that source env from scripts folder)
# If you don't want this, just delete this block.
SCRIPTS_ENV="$SCRIPTS_DIR/.env"
backup_if_exists "$SCRIPTS_ENV"
cp -a "$MASTER_ENV" "$SCRIPTS_ENV"
log "Copied master env to: $SCRIPTS_ENV"
echo

# -----------------------------
# 4) build slave/.env (on master, for the slave-process config)
# -----------------------------
log "Collecting values for SLAVE .env (master also runs a slave process)"

MASTER_IP="$(ask_required "MASTER_IP (typically the 512rede IP of master)" "$MASTER_512REDE_IP_DEFAULT")"
SLAVE_IP="$(ask_required "SLAVE_IP (CURRENT machine slave IP; on master this is usually the same as MASTER_IP)" "$MASTER_IP")"

echo
echo "Other slaves IPs rule:"
echo "  - Add ALL other slave IPs (including MASTER_IP if it also runs workloads),"
echo "  - BUT exclude the current SLAVE_IP."
echo "Example: if SLAVE_IP=192.168.76.55, then OTHER_* can include 192.168.76.1 and 192.168.76.188"
echo

OTHERS_RAW="$(ask "Other slave IPs (comma-separated, optional)" "")"
# Parse into array, trim spaces, filter empties + filter SLAVE_IP
IFS=',' read -r -a others_arr <<< "$OTHERS_RAW"
others_clean=()
for ipx in "${others_arr[@]}"; do
  ipx="${ipx//[[:space:]]/}"
  [[ -z "$ipx" ]] && continue
  [[ "$ipx" == "$SLAVE_IP" ]] && continue
  others_clean+=("$ipx")
done

OTHER1="${others_clean[0]:-}"
OTHER2="${others_clean[1]:-}"

DIRTY_RATIO_PERCENT="$(ask "DIRTY_RATIO_PERCENT" "15")"
DIRTY_BACKGROUND_RATIO_PERCENT="$(ask "DIRTY_BACKGROUND_RATIO_PERCENT" "8")"
MODE_SLAVE="$(ask "MODE (dev/prod)" "dev")"

DEFAULT_MACHINE_NAME="$(hostname -s 2>/dev/null || hostname 2>/dev/null || echo "")"
MACHINE_NAME="$(ask_required "MACHINE_NAME" "$DEFAULT_MACHINE_NAME")"

VNC_MIN_PORT="$(ask_required "VNC_MIN_PORT" "35000")"
VNC_MAX_PORT="$(ask_required "VNC_MAX_PORT" "35999")"

QEMU_UID_S="$(ask "QEMU_UID" "107")"
QEMU_GID_S="$(ask "QEMU_GID" "107")"

# Optional (but handy): extra IPs reachable from k8s (e.g., master external IP)
EXTRA_K8S_IPS="$(ask "EXTRA_K8S_IPS (optional, e.g. master external IP; leave empty to skip)" "$MASTER_INTERNET_IP")"

mkdir -p "$(dirname "$SLAVE_ENV")"
backup_if_exists "$SLAVE_ENV"

cat > "$SLAVE_ENV" <<EOF
MASTER_IP=${MASTER_IP}
SLAVE_IP=${SLAVE_IP}

OTHER_SLAVE1_IP=${OTHER1}
OTHER_SLAVE2_IP=${OTHER2}

DIRTY_RATIO_PERCENT=${DIRTY_RATIO_PERCENT}
DIRTY_BACKGROUND_RATIO_PERCENT=${DIRTY_BACKGROUND_RATIO_PERCENT}

MODE=${MODE_SLAVE}
MACHINE_NAME=${MACHINE_NAME}
VNC_MIN_PORT=${VNC_MIN_PORT}
VNC_MAX_PORT=${VNC_MAX_PORT}

QEMU_UID=${QEMU_UID_S}
QEMU_GID=${QEMU_GID_S}

EXTRA_K8S_IPS=${EXTRA_K8S_IPS}
EOF

log "Wrote: $SLAVE_ENV"
echo
log "Done."
log "Next: you can proceed with the next scripts that depend on these env files."
