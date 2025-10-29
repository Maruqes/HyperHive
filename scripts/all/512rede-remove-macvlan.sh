#!/usr/bin/env bash
set -Eeuo pipefail

# --- parâmetros (ajusta se usaste outros nomes) ---
LOWER="${LOWER:-512rede}"         # NIC física ligada ao switch
MACVLAN="${MACVLAN:-host-macvlan}" # nome da macvlan que criaste
CON="${CON:-$MACVLAN}"            # nome da ligação no NetworkManager

echo "[i] A remover macvlan '$MACVLAN' (ligação NM: '$CON') sobre '$LOWER'..."

# 1) Se existir ligação do NetworkManager, baixar e apagar
if nmcli -t -f NAME connection show | grep -Fxq "$CON"; then
  echo "[i] A baixar ligação NM '$CON'..."
  nmcli connection down "$CON" || true
  echo "[i] A apagar ligação NM '$CON'..."
  nmcli connection delete "$CON" || true
else
  echo "[i] Ligação NM '$CON' não encontrada (ok)."
fi

# 2) Se a interface ainda existir em runtime, limpar IPs e apagar
if ip link show "$MACVLAN" &>/dev/null; then
  echo "[i] A limpar IPs de '$MACVLAN'..."
  ip addr flush dev "$MACVLAN" || true
  echo "[i] A apagar interface '$MACVLAN'..."
  ip link delete "$MACVLAN" || true
else
  echo "[i] Interface '$MACVLAN' não está presente (ok)."
fi

echo "[✓] Remoção concluída."
