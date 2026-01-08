#!/usr/bin/env bash
set -euo pipefail

# Lists current leases managed by the dedicated dnsmasq from setup_dhcp.sh.
# Accepts as arguments the LAN interface name (e.g., 512rede-host) or the full
# path of a *.leases file. Without arguments, scans all leases in
# ${DNSMASQ_LEASE_DIR:-/var/lib/dnsmasq}.

LEASE_DIR="${DNSMASQ_LEASE_DIR:-/var/lib/dnsmasq}"
declare -a lease_files=()

usage() {
  cat <<'USAGE'
Usage: show_dhcp_leases.sh [interface_or_leasefile ...]

Examples:
  sudo ./show_dhcp_leases.sh                # all known leases
  sudo ./show_dhcp_leases.sh 512rede-host   # leases for a specific segment
  sudo ./show_dhcp_leases.sh /tmp/foo.leases
USAGE
  exit 1
}

[[ ${EUID:-0} -eq 0 ]] || echo "[WARN] Running as root ensures access to all leases." >&2

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage ;;
    *)
      if [[ -f "$1" ]]; then
        lease_files+=("$1")
      else
        lease_files+=("${LEASE_DIR}/$1.leases")
      fi
      ;;
  esac
  shift
done

if [[ ${#lease_files[@]} -eq 0 ]]; then
  shopt -s nullglob
  lease_files=("${LEASE_DIR}"/*.leases)
  shopt -u nullglob
fi

if [[ ${#lease_files[@]} -eq 0 ]]; then
  echo "[INFO] No lease files found in ${LEASE_DIR}."
  exit 0
fi

human_duration() {
  local seconds=$1
  local sign suffix
  if (( seconds < 0 )); then
    sign="-"; seconds=$(( -seconds )); suffix="ago"
  else
    sign=""; suffix="remaining"
  fi
  local days=$(( seconds / 86400 ))
  local hours=$(( (seconds % 86400) / 3600 ))
  local minutes=$(( (seconds % 3600) / 60 ))
  local secs=$(( seconds % 60 ))
  local parts=()
  (( days > 0 )) && parts+=("${days}d")
  (( hours > 0 )) && parts+=("${hours}h")
  (( minutes > 0 )) && parts+=("${minutes}m")
  (( secs > 0 || ${#parts[@]} == 0 )) && parts+=("${secs}s")
  echo "${sign}${parts[*]} ${suffix}"
}

print_lease_file() {
  local file=$1
  if [[ ! -r "${file}" ]]; then
    echo "[WARN] Ignored (no read access): ${file}" >&2
    return
  fi

  local now title
  now=$(date +%s)
  title=$(basename "${file}")
  echo "=== ${title} (${file}) ==="
  printf "%-15s %-17s %-20s %-30s %-15s %-10s\n" "IP" "MAC" "Hostname" "Expires" "Client-ID" "Status"

  local had_entries=0
  while read -r expiry mac ip host client_id _rest; do
    [[ -z "${expiry}" ]] && continue
    if [[ "${expiry}" == "duid" ]]; then
      # IPv6/DUID entry; ignore in current view.
      continue
    fi
    had_entries=1

    local host_display client_display expiry_display status diff
    host_display=$([[ "${host}" == "*" ]] && echo "-" || echo "${host}")
    client_display=$([[ -z "${client_id:-}" || "${client_id}" == "*" ]] && echo "-" || echo "${client_id}")

    if [[ "${expiry}" == "0" ]]; then
      expiry_display="infinite"
      status="active"
    else
      diff=$(( expiry - now ))
      expiry_display="$(date -d "@${expiry}" '+%Y-%m-%d %H:%M:%S') ($(human_duration "${diff}"))"
      status=$([[ ${diff} -ge 0 ]] && echo "active" || echo "expired")
    fi

    printf "%-15s %-17s %-20s %-30s %-15s %-10s\n" "${ip}" "${mac}" "${host_display}" "${expiry_display}" "${client_display}" "${status}"
  done <"${file}"

  if (( had_entries == 0 )); then
    echo "(no active leases)"
  fi
  echo
}

for file in "${lease_files[@]}"; do
  print_lease_file "${file}"
done
