#!/bin/bash

set -euo pipefail

TARGET_IFACE_NAME="512rede"

usage() {
    echo "Usage: $(basename "$0") <current_name>" >&2
    exit 1
}

require_root() {
    if [ "$(id -u)" -ne 0 ]; then
        echo "This script must be run as root." >&2
        exit 1
    fi
}

validate_iface_name() {
    local name="$1"
    local label="$2"
    local allow_long="${3:-no}"

    if [ -z "$name" ]; then
        echo "${label} cannot be empty." >&2
        exit 1
    fi

    if [ "$allow_long" != "yes" ] && [ "${#name}" -gt 15 ]; then
        echo "${label} must have at most 15 characters (kernel limit for interfaces)." >&2
        exit 1
    fi

    if [[ ! "$name" =~ ^[[:alnum:]._-]+$ ]]; then
        echo "${label} '${name}' contains invalid characters. Use only letters, numbers, '.', '_' or '-'." >&2
        exit 1
    fi
}

ensure_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "Required command not found: $1" >&2
        exit 1
    fi
}

# Move old persistent configs for this MAC aside so only the latest rule is active.
backup_conflicting_files() {
    local dir="$1"
    local pattern="$2"
    local match="$3"
    local skip="$4"

    if [ ! -d "$dir" ]; then
        return
    fi

    while IFS= read -r -d '' file; do
        if [ "$file" = "$skip" ]; then
            continue
        fi
        if grep -Fq "$match" "$file"; then
            mv "$file" "${file}${BACKUP_SUFFIX}"
        fi
    done < <(find "$dir" -maxdepth 1 -type f -name "$pattern" -print0)
}

if [ "$#" -ne 1 ]; then
    usage
fi

require_root
ensure_command ip

OLD_IFACE="$1"
NEW_IFACE="$TARGET_IFACE_NAME"

validate_iface_name "$OLD_IFACE" "Current name" "yes"
validate_iface_name "$NEW_IFACE" "New name"

if [ "$OLD_IFACE" = "$NEW_IFACE" ]; then
    echo "The current interface name already matches the target name; nothing to do." >&2
    exit 0
fi

if ! ip link show "$OLD_IFACE" >/dev/null 2>&1; then
    echo "Interface '${OLD_IFACE}' not found." >&2
    exit 1
fi

if ip link show "$NEW_IFACE" >/dev/null 2>&1; then
    echo "An interface named '${NEW_IFACE}' already exists." >&2
    exit 1
fi

MAC_ADDRESS=$(cat "/sys/class/net/${OLD_IFACE}/address" 2>/dev/null || true)
if [ -z "$MAC_ADDRESS" ]; then
    echo "Unable to determine the MAC address for '${OLD_IFACE}'." >&2
    exit 1
fi

printf "Renaming interface '%s' to '%s'...\n" "$OLD_IFACE" "$NEW_IFACE"
ip link set dev "$OLD_IFACE" down
ip link set dev "$OLD_IFACE" name "$NEW_IFACE"
ip link set dev "$NEW_IFACE" up
printf "Interface renamed successfully.\n"

LINK_DIR="/etc/systemd/network"
LINK_PRIORITY="01"
mkdir -p "$LINK_DIR"

sanitize() {
    local input="$1"
    echo "$input" | tr -c '[:alnum:]' '_'
}

SAFE_OLD=$(sanitize "$OLD_IFACE")
SAFE_NEW=$(sanitize "$NEW_IFACE")
LINK_FILE="${LINK_DIR}/${LINK_PRIORITY}-rename-${SAFE_OLD}-to-${SAFE_NEW}.link"
BACKUP_SUFFIX=".bak.$(date +%Y%m%d%H%M%S)"

LEGACY_LINK="${LINK_DIR}/10-rename-${SAFE_OLD}-to-${SAFE_NEW}.link"
if [ -f "$LEGACY_LINK" ] && [ "$LEGACY_LINK" != "$LINK_FILE" ]; then
    mv "$LEGACY_LINK" "${LEGACY_LINK}${BACKUP_SUFFIX}"
fi

backup_conflicting_files "$LINK_DIR" '*.link' "MACAddress=${MAC_ADDRESS}" "$LINK_FILE"

if [ -f "$LINK_FILE" ]; then
    cp "$LINK_FILE" "${LINK_FILE}${BACKUP_SUFFIX}"
fi

cat >"$LINK_FILE" <<EOF2
[Match]
MACAddress=${MAC_ADDRESS}

[Link]
Name=${NEW_IFACE}
EOF2
chmod 0644 "$LINK_FILE"

UDEV_RULES_DIR="/etc/udev/rules.d"
UDEV_RULE_FILE="${UDEV_RULES_DIR}/82-rename-net-${SAFE_NEW}.rules"
mkdir -p "$UDEV_RULES_DIR"

backup_conflicting_files "$UDEV_RULES_DIR" '*.rules' "ATTR{address}==\"${MAC_ADDRESS}\"" "$UDEV_RULE_FILE"

if [ -f "$UDEV_RULE_FILE" ]; then
    cp "$UDEV_RULE_FILE" "${UDEV_RULE_FILE}${BACKUP_SUFFIX}"
fi

cat >"$UDEV_RULE_FILE" <<EOF3
ACTION=="add", SUBSYSTEM=="net", ATTR{address}=="${MAC_ADDRESS}", NAME="${NEW_IFACE}"
EOF3
chmod 0644 "$UDEV_RULE_FILE"

# Ensure changes are applied on future boots
if command -v udevadm >/dev/null 2>&1; then
    udevadm control --reload >/dev/null 2>&1 || true
    udevadm control --reload-rules >/dev/null 2>&1 || true
    udevadm trigger --attr-match=subsystem=net --attr-match=address="$MAC_ADDRESS" >/dev/null 2>&1 || true
fi

printf "Persistence configuration written to '%s'.\n" "$LINK_FILE"
printf "Restart or reconnect the interface to propagate the new name to every service.\n"

exit 0
