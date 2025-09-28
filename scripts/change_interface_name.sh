#!/bin/bash

set -euo pipefail

usage() {
    echo "Uso: $(basename "$0") <nome_atual> <novo_nome>" >&2
    exit 1
}

require_root() {
    if [ "$(id -u)" -ne 0 ]; then
        echo "Este script precisa ser executado como root." >&2
        exit 1
    fi
}

validate_iface_name() {
    local name="$1"
    local label="$2"

    if [ -z "$name" ]; then
        echo "${label} não pode ser vazio." >&2
        exit 1
    fi

    if [ "${#name}" -gt 15 ]; then
        echo "${label} deve ter no máximo 15 caracteres (limite do kernel para interfaces)." >&2
        exit 1
    fi

    if [[ ! "$name" =~ ^[[:alnum:]._-]+$ ]]; then
        echo "${label} '${name}' contém caracteres inválidos. Use apenas letras, números, '.', '_' ou '-'." >&2
        exit 1
    fi
}

ensure_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "Comando obrigatório não encontrado: $1" >&2
        exit 1
    fi
}

if [ "$#" -ne 2 ]; then
    usage
fi

require_root
ensure_command ip

OLD_IFACE="$1"
NEW_IFACE="$2"

validate_iface_name "$OLD_IFACE" "Nome atual"
validate_iface_name "$NEW_IFACE" "Novo nome"

if [ "$OLD_IFACE" = "$NEW_IFACE" ]; then
    echo "O nome atual e o novo nome são iguais; nada a fazer." >&2
    exit 0
fi

if ! ip link show "$OLD_IFACE" >/dev/null 2>&1; then
    echo "Interface '${OLD_IFACE}' não encontrada." >&2
    exit 1
fi

if ip link show "$NEW_IFACE" >/dev/null 2>&1; then
    echo "Já existe uma interface chamada '${NEW_IFACE}'." >&2
    exit 1
fi

MAC_ADDRESS=$(cat "/sys/class/net/${OLD_IFACE}/address" 2>/dev/null || true)
if [ -z "$MAC_ADDRESS" ]; then
    echo "Não foi possível obter o endereço MAC de '${OLD_IFACE}'." >&2
    exit 1
fi

printf "Renomeando interface '%s' para '%s'...\n" "$OLD_IFACE" "$NEW_IFACE"
ip link set dev "$OLD_IFACE" down
ip link set dev "$OLD_IFACE" name "$NEW_IFACE"
ip link set dev "$NEW_IFACE" up
printf "Interface renomeada com sucesso.\n"

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

if [ -f "$UDEV_RULE_FILE" ]; then
    cp "$UDEV_RULE_FILE" "${UDEV_RULE_FILE}${BACKUP_SUFFIX}"
fi

cat >"$UDEV_RULE_FILE" <<EOF3
ACTION=="add", SUBSYSTEM=="net", ATTR{address}=="${MAC_ADDRESS}", NAME="${NEW_IFACE}"
EOF3
chmod 0644 "$UDEV_RULE_FILE"

# Assegura que alterações sejam aplicadas em futuros boots
if command -v udevadm >/dev/null 2>&1; then
    udevadm control --reload >/dev/null 2>&1 || true
    udevadm control --reload-rules >/dev/null 2>&1 || true
    udevadm trigger --attr-match=subsystem=net --attr-match=address="$MAC_ADDRESS" >/dev/null 2>&1 || true
fi

printf "Persistência configurada em '%s'." "$LINK_FILE"
printf "\nReinicie ou reconecte a interface para confirmar o novo nome em todos os serviços.\n"

exit 0
