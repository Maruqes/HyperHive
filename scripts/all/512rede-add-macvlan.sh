#!/usr/bin/env bash
set -Eeuo pipefail
trap 'echo "[x] Failed at line $LINENO" >&2' ERR

# ---------- Tunables (override via env) ----------
LOWER="${LOWER:-512rede}"                 # physical NIC connected to the switch
MACVLAN="${MACVLAN:-host-macvlan}"        # macvlan interface name
NET_BASE="${NET_BASE:-192.168.76}"        # base network (no last octet)
CIDR="${CIDR:-24}"                        # subnet mask length (e.g., 24 => /24)
AUTOCONNECT="${AUTOCONNECT:-yes}"         # yes|no
ZONE="${ZONE:-}"                          # optional firewalld zone, e.g., "trusted"
MIN_LAST="${MIN_LAST:-5}"                 # allowed last octet lower bound
MAX_LAST="${MAX_LAST:-45}"                # allowed last octet upper bound

# ---------- Pre-checks ----------
command -v nmcli >/dev/null || { echo "[x] nmcli not found." >&2; exit 1; }
for i in {1..10}; do ip link show "$LOWER" &>/dev/null && break; sleep 1; done
ip link show "$LOWER" &>/dev/null || { echo "[x] Interface '$LOWER' not found." >&2; exit 1; }

# ---------- Ask user for the last octet (5..45) ----------
choose_octet() {
  local n
  while true; do
    read -rp "Enter a UNIQUE number from ${MIN_LAST}-${MAX_LAST} (this will be the last octet; MUST be unique across all slaves): " n
    [[ "$n" =~ ^[0-9]+$ ]] || { echo "  -> Not a number."; continue; }
    (( n >= MIN_LAST && n <= MAX_LAST )) || { echo "  -> Out of range ${MIN_LAST}-${MAX_LAST}."; continue; }
    echo "$n"; return 0
  done
}

LAST_OCTET="${LAST_OCTET:-}"
if [[ -z "${LAST_OCTET}" ]]; then
  LAST_OCTET="$(choose_octet)"
else
  # validate provided LAST_OCTET from env
  [[ "$LAST_OCTET" =~ ^[0-9]+$ ]] && (( LAST_OCTET >= MIN_LAST && LAST_OCTET <= MAX_LAST )) \
    || { echo "[x] LAST_OCTET must be ${MIN_LAST}-${MAX_LAST}."; exit 1; }
fi

IP_ADDR="${NET_BASE}.${LAST_OCTET}/${CIDR}"
IP_NOMASK="${NET_BASE}.${LAST_OCTET}"

echo "[i] Will configure ${MACVLAN} with static IP ${IP_ADDR}"

# ---------- Sanity: ensure this IP is not already on this host ----------
if ip -o -4 addr show | awk '{print $2,$4}' | grep -qE "^[^ ]+ ${NET_BASE//./\\.}\.${LAST_OCTET}/"; then
  WHICH_IF="$(ip -o -4 addr show | awk '$4 ~ /'"${NET_BASE//./\\.}\.${LAST_OCTET}"'\// {print $2; exit}')"
  if [[ "$WHICH_IF" != "$MACVLAN" ]]; then
    echo "[x] IP ${IP_ADDR} is already configured on interface '${WHICH_IF}' of this host. Aborting."
    exit 1
  fi
fi

# ---------- Optional LAN conflict check (best-effort) ----------
if command -v arping >/dev/null; then
  echo "[i] Checking LAN for duplicates of ${IP_NOMASK} (arping)..."
  # DAD mode: exit 0 means FREE, 1 means IN USE
  if ! arping -I "$LOWER" -D -c 2 -w 2 "$IP_NOMASK" >/dev/null 2>&1; then
    echo "[x] IP ${IP_NOMASK} appears to be in use on the LAN. Choose another last octet." >&2
    exit 1
  fi
else
  echo "[i] 'arping' not found; skipping LAN duplicate check."
fi

# ---------- Create/update NM connection ----------
if nmcli -t -f NAME connection show | grep -Fxq "$MACVLAN"; then
  echo "[i] NM connection '$MACVLAN' exists — updating static IP..."
  nmcli connection modify "$MACVLAN" \
    ipv4.addresses "$IP_ADDR" ipv4.method manual \
    ipv6.method ignore connection.autoconnect "$AUTOCONNECT"
else
  echo "[i] Creating macvlan '$MACVLAN' on '$LOWER' (mode=bridge)..."
  nmcli connection add type macvlan ifname "$MACVLAN" dev "$LOWER" mode bridge con-name "$MACVLAN"
  nmcli connection modify "$MACVLAN" \
    ipv4.addresses "$IP_ADDR" ipv4.method manual \
    ipv6.method ignore connection.autoconnect "$AUTOCONNECT"
fi

# ---------- Optional firewalld zone ----------
if systemctl is-active --quiet firewalld 2>/dev/null && [[ -n "$ZONE" ]]; then
  echo "[i] Binding '$MACVLAN' to firewalld zone '$ZONE'..."
  nmcli connection modify "$MACVLAN" connection.zone "$ZONE" || true
fi

# ---------- Apply ----------
# If active, reapply; otherwise just bring it up
if nmcli -t -f NAME connection show --active | grep -Fxq "$MACVLAN"; then
  nmcli device reapply "$MACVLAN" || nmcli connection down "$MACVLAN" || true
fi
nmcli --wait 5 connection up "$MACVLAN" || nmcli connection up "$MACVLAN"

echo
echo "[✓] macvlan ready (static): $IP_ADDR"
ip -d link show "$MACVLAN" | sed 's/^/  /'
ip addr show "$MACVLAN"   | sed 's/^/  /'
