#!/usr/bin/env bash
set -Eeuo pipefail
trap 'echo "[x] Falhou na linha $LINENO" >&2' ERR

# --------- parâmetros (podes sobrescrever por env) ----------
LOWER="${LOWER:-512rede}"              # NIC física ligada ao switch
MACVLAN="${MACVLAN:-host-macvlan}"     # nome da macvlan
HOST_ALIAS_IP="${HOST_ALIAS_IP:-192.168.76.2/24}"  # IP livre (fora do pool DHCP)
AUTOCONNECT="${AUTOCONNECT:-yes}"      # yes|no
ZONE="${ZONE:-}"                       # opcional: zona firewalld (ex.: trusted)

# --------- pré-requisitos ----------
command -v nmcli >/dev/null || { echo "[x] nmcli não encontrado." >&2; exit 1; }

# aguarda até a LOWER existir (máx 10s) — útil no boot
for i in {1..10}; do
  if ip link show "$LOWER" &>/dev/null; then break; fi
  sleep 1
done
ip link show "$LOWER" &>/dev/null || { echo "[x] Interface '$LOWER' não encontrada." >&2; exit 1; }

# --------- criar/atualizar ligação NM ----------
if nmcli -t -f NAME connection show | grep -Fxq "$MACVLAN"; then
  echo "[i] Ligação '$MACVLAN' já existe — a atualizar IP e autoconnect..."
  nmcli connection modify "$MACVLAN" \
    ipv4.addresses "$HOST_ALIAS_IP" ipv4.method manual \
    ipv6.method ignore connection.autoconnect "$AUTOCONNECT"
else
  echo "[i] A criar ligação macvlan '$MACVLAN' sobre '$LOWER' (mode=bridge)..."
  nmcli connection add type macvlan ifname "$MACVLAN" dev "$LOWER" mode bridge con-name "$MACVLAN"
  nmcli connection modify "$MACVLAN" \
    ipv4.addresses "$HOST_ALIAS_IP" ipv4.method manual \
    ipv6.method ignore connection.autoconnect "$AUTOCONNECT"
fi

# zona do firewalld (opcional)
if systemctl is-active --quiet firewalld 2>/dev/null && [[ -n "$ZONE" ]]; then
  echo "[i] A associar '$MACVLAN' à zona '$ZONE'..."
  nmcli connection modify "$MACVLAN" connection.zone "$ZONE" || true
fi

# --------- aplicar ----------
# se já estiver ativa, tenta reapply; senão sobe-a
if nmcli -t -f NAME connection show --active | grep -Fxq "$MACVLAN"; then
  nmcli device reapply "$MACVLAN" || nmcli connection down "$MACVLAN" || true
fi
nmcli connection up "$MACVLAN"

# --------- estado ----------
echo
echo "[✓] Macvlan pronta:"
ip -d link show "$MACVLAN" | sed 's/^/  /'
ip addr show "$MACVLAN"   | sed 's/^/  /'

