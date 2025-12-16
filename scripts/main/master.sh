#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

REQUIRED_CMDS=(ip sed awk grep readlink mktemp)
TARGET_IFACE="512rede"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ENV_PATH="${ENV_PATH:-${PROJECT_ROOT}/.env}"
CHANGE_IFACE_SCRIPT="${PROJECT_ROOT}/scripts/all/change_interface_name.sh"
SETUP_DHCP_SCRIPT="${PROJECT_ROOT}/scripts/master/setup_dhcp.sh"

TMP_ENV=""
trap '[[ -n "${TMP_ENV}" && -f "${TMP_ENV}" ]] && rm -f "${TMP_ENV}"' EXIT

require_root() {
  if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
    echo "You need root (use sudo)." >&2
    exit 1
  fi
}

require_cmds() {
  for cmd in "${REQUIRED_CMDS[@]}"; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      echo "Missing command: $cmd" >&2
      exit 1
    fi
  done
}

is_ipv4() {
  local ip="$1"
  [[ "$ip" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || return 1
  local IFS=.
  read -r a b c d <<<"$ip"
  for part in "$a" "$b" "$c" "$d"; do
    [[ "$part" =~ ^[0-9]+$ ]] || return 1
    (( part >= 0 && part <= 255 )) || return 1
  done
  return 0
}

is_port() {
  local p="$1"
  [[ "$p" =~ ^[0-9]+$ ]] || return 1
  (( p >= 1 && p <= 65535 ))
}

prompt_with_default() {
  local question="$1" default_value="$2" allow_empty="$3" input
  while true; do
    if [[ -n "$default_value" ]]; then
      read -r -p "${question} [${default_value}]: " input || exit 1
    else
      read -r -p "${question}: " input || exit 1
    fi
    if [[ -z "$input" ]]; then
      input="$default_value"
    fi
    if [[ -z "$input" && "$allow_empty" != "yes" ]]; then
      echo "Value is required." >&2
      continue
    fi
    echo "$input"
    return 0
  done
}

prompt_mode() {
  local default_value="$1" input
  while true; do
    input=$(prompt_with_default "MODE (dev or prod)" "${default_value}" "no")
    case "${input,,}" in
      dev|prod) echo "${input,,}"; return 0 ;;
      *) echo "Use dev or prod." >&2 ;;
    esac
  done
}

prompt_int_nonneg() {
  local name="$1" default_value="$2" input
  while true; do
    input=$(prompt_with_default "${name} (integer >=0)" "${default_value}" "no")
    if [[ "$input" =~ ^[0-9]+$ ]]; then
      echo "$input"
      return 0
    fi
    echo "Invalid value; use integer >=0." >&2
  done
}

prompt_bool() {
  local name="$1" default_value="$2" input
  while true; do
    input=$(prompt_with_default "${name} (true/false)" "${default_value}" "no")
    case "${input,,}" in
      true|false) echo "${input,,}"; return 0 ;;
      *) echo "Use true or false." >&2 ;;
    esac
  done
}

prompt_ipv4() {
  local name="$1" hint="$2" default_value="$3" input
  while true; do
    input=$(prompt_with_default "${name} - ${hint}" "${default_value}" "no")
    if is_ipv4 "$input"; then
      echo "$input"
      return 0
    fi
    echo "Invalid IPv4." >&2
  done
}

prompt_port() {
  local name="$1" hint="$2" default_value="$3" input
  while true; do
    input=$(prompt_with_default "${name} - ${hint}" "${default_value}" "no")
    if is_port "$input"; then
      echo "$input"
      return 0
    fi
    echo "Invalid port (1-65535)." >&2
  done
}

prompt_non_empty() {
  local name="$1" hint="$2" default_value="$3" input
  while true; do
    input=$(prompt_with_default "${name} - ${hint}" "${default_value}" "no")
    if [[ -n "$input" ]]; then
      echo "$input"
      return 0
    fi
    echo "Cannot be empty." >&2
  done
}

prompt_optional() {
  local name="$1" hint="$2" default_value="$3"
  prompt_with_default "${name} - ${hint}" "${default_value}" "yes"
}

detect_interface_ipv4() {
  local iface="$1"
  ip -4 -o addr show dev "$iface" 2>/dev/null | awk 'NR==1{split($4,a,"/"); print a[1]}'
}

list_interfaces() {
  echo "Interfaces with IPv4:" 
  if ! ip -4 -o addr show 2>/dev/null | awk '{printf "- %s (%s)\n", $2, $4}'; then
    echo "No IPv4 interfaces detected."
  fi
}

choose_network_mode() {
  local input
  while true; do
    read -r -p "Use internal network (DHCP) or external (internet)? [external]: " input || exit 1
    input="${input:-external}"
    input="${input,,}"
    case "$input" in
      interna|interno|internal) echo "interna"; return 0 ;;
      externa|externo|external) echo "externa"; return 0 ;;
      *) echo "Reply with internal or external." >&2 ;;
    esac
  done
}

choose_interface() {
  list_interfaces
  local iface
  while true; do
    read -r -p "Select interface to rename to ${TARGET_IFACE}: " iface || exit 1
    if [[ -z "$iface" ]]; then
      echo "Interface name is required." >&2
      continue
    fi
    if ip link show "$iface" >/dev/null 2>&1; then
      echo "$iface"
      return 0
    fi
    echo "Unknown interface." >&2
  done
}

rename_interface() {
  local iface="$1"
  if [[ ! -x "$CHANGE_IFACE_SCRIPT" ]]; then
    echo "Missing helper script: ${CHANGE_IFACE_SCRIPT}" >&2
    exit 1
  fi
  echo "Renaming ${iface} -> ${TARGET_IFACE} (via ${CHANGE_IFACE_SCRIPT})"
  "$CHANGE_IFACE_SCRIPT" "$iface"
}

run_setup_dhcp() {
  if [[ ! -x "$SETUP_DHCP_SCRIPT" ]]; then
    echo "Missing DHCP script: ${SETUP_DHCP_SCRIPT}" >&2
    exit 1
  fi
  echo "Configuring DHCP on ${TARGET_IFACE} using ${SETUP_DHCP_SCRIPT}"
  "$SETUP_DHCP_SCRIPT"
}

load_current_env() {
  declare -gA CURRENT=()
  [[ -f "$ENV_PATH" ]] || return 0
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
    local key="${line%%=*}"
    local val="${line#*=}"
    CURRENT["$key"]="$val"
  done <"$ENV_PATH"
}

current_or_default() {
  local key="$1" fallback="$2"
  if [[ -n "${NEW_VARS[$key]:-}" ]]; then
    echo "${NEW_VARS[$key]}"
  elif [[ -n "${CURRENT[$key]:-}" ]]; then
    echo "${CURRENT[$key]}"
  else
    echo "$fallback"
  fi
}

ask_sprite_range() {
  while true; do
    local dmin dmax
    dmin=$(current_or_default "SPRITE_MIN" "50000")
    dmax=$(current_or_default "SPRITE_MAX" "50100")
    NEW_VARS[SPRITE_MIN]=$(prompt_port "SPRITE_MIN" "start port of reserved range" "$dmin")
    NEW_VARS[SPRITE_MAX]=$(prompt_port "SPRITE_MAX" "end port of reserved range" "$dmax")
    if (( NEW_VARS[SPRITE_MIN] < NEW_VARS[SPRITE_MAX] )); then
      return 0
    fi
    echo "SPRITE_MIN must be less than SPRITE_MAX." >&2
  done
}

ask_master_vars() {
  echo "Fill the master .env file."
  NEW_VARS[MODE]=$(prompt_mode "$(current_or_default MODE "prod")")
  echo "QEMU_UID/GID: UID and GID used by the VMs (usually 107)."
  NEW_VARS[QEMU_UID]=$(prompt_int_nonneg "QEMU_UID" "$(current_or_default QEMU_UID "107")")
  NEW_VARS[QEMU_GID]=$(prompt_int_nonneg "QEMU_GID" "$(current_or_default QEMU_GID "107")")
  echo "SPRITE_MIN/MAX: reserved port range for this host. Avoid collisions with other hosts."
  ask_sprite_range
  local detected_ip="$(detect_interface_ipv4 "${TARGET_IFACE}")"
  local master_ip_default="$(current_or_default MASTER_INTERNET_IP "${detected_ip}")"
  echo "MASTER_INTERNET_IP: master IP on the interface that reaches the internet or main network."
  NEW_VARS[MASTER_INTERNET_IP]=$(prompt_ipv4 "MASTER_INTERNET_IP" "IPv4 of the interface with internet" "$master_ip_default")
  echo "MAIN_LINK: main base URL/domain for the system (public endpoint)."
  NEW_VARS[MAIN_LINK]=$(prompt_non_empty "MAIN_LINK" "e.g., https://example.domain" "$(current_or_default MAIN_LINK "")")
  echo "GO_ACCESS_ENABLE_PANELS: enable GoAccess panels (true/false)."
  NEW_VARS[GO_ACCESS_ENABLE_PANELS]=$(prompt_bool "GO_ACCESS_ENABLE_PANELS" "$(current_or_default GO_ACCESS_ENABLE_PANELS "false")")
  echo "GO_ACCESS_DISABLE_PANELS: list/flags of GoAccess panels to disable (optional)."
  NEW_VARS[GO_ACCESS_DISABLE_PANELS]=$(prompt_optional "GO_ACCESS_DISABLE_PANELS" "can be empty" "$(current_or_default GO_ACCESS_DISABLE_PANELS "")")
  echo "GO_ACCESS_GEOIP_LICENSE_KEY: GeoIP license key from MaxMind. Leave empty if you do not have one."
  NEW_VARS[GO_ACCESS_GEOIP_LICENSE_KEY]=$(prompt_optional "GO_ACCESS_GEOIP_LICENSE_KEY" "can be empty" "$(current_or_default GO_ACCESS_GEOIP_LICENSE_KEY "")")
}

show_preview() {
  echo "\nPreview of .env (${ENV_PATH}):"
  for key in "${ORDERED_KEYS[@]}"; do
    echo "${key}=${NEW_VARS[$key]}"
  done
  echo
}

edit_loop() {
  while true; do
    read -r -p "Change anything? (variable name/all/no): " choice || exit 1
    choice="${choice,,}"
    if [[ "$choice" == "no" || "$choice" == "n" ]]; then
      return 0
    fi
    if [[ "$choice" == "all" ]]; then
      ask_master_vars
      continue
    fi
    local found="no"
    for key in "${ORDERED_KEYS[@]}"; do
      if [[ "${choice^^}" == "$key" ]]; then
        found="yes"
        case "$key" in
          MODE) NEW_VARS[MODE]=$(prompt_mode "${NEW_VARS[MODE]}") ;;
          QEMU_UID) NEW_VARS[QEMU_UID]=$(prompt_int_nonneg "QEMU_UID" "${NEW_VARS[QEMU_UID]}") ;;
          QEMU_GID) NEW_VARS[QEMU_GID]=$(prompt_int_nonneg "QEMU_GID" "${NEW_VARS[QEMU_GID]}") ;;
          SPRITE_MIN|SPRITE_MAX) ask_sprite_range ;;
          MASTER_INTERNET_IP) NEW_VARS[MASTER_INTERNET_IP]=$(prompt_ipv4 "MASTER_INTERNET_IP" "IPv4 of the interface with internet" "${NEW_VARS[MASTER_INTERNET_IP]}") ;;
          MAIN_LINK) NEW_VARS[MAIN_LINK]=$(prompt_non_empty "MAIN_LINK" "e.g., https://example.domain" "${NEW_VARS[MAIN_LINK]}") ;;
          GO_ACCESS_ENABLE_PANELS) NEW_VARS[GO_ACCESS_ENABLE_PANELS]=$(prompt_bool "GO_ACCESS_ENABLE_PANELS" "${NEW_VARS[GO_ACCESS_ENABLE_PANELS]}") ;;
          GO_ACCESS_DISABLE_PANELS) NEW_VARS[GO_ACCESS_DISABLE_PANELS]=$(prompt_optional "GO_ACCESS_DISABLE_PANELS" "can be empty" "${NEW_VARS[GO_ACCESS_DISABLE_PANELS]}") ;;
          GO_ACCESS_GEOIP_LICENSE_KEY) NEW_VARS[GO_ACCESS_GEOIP_LICENSE_KEY]=$(prompt_optional "GO_ACCESS_GEOIP_LICENSE_KEY" "can be empty" "${NEW_VARS[GO_ACCESS_GEOIP_LICENSE_KEY]}") ;;
        esac
      fi
    done
    if [[ "$found" == "no" ]]; then
      echo "Invalid name. Use the exact variable name." >&2
    else
      return 0
    fi
  done
}

write_env() {
  local env_dir
  env_dir="$(dirname "$ENV_PATH")"
  mkdir -p "$env_dir"
  TMP_ENV="$(mktemp "${env_dir}/.env.tmp.XXXX")"
  if [[ -f "$ENV_PATH" ]]; then
    while IFS= read -r line || [[ -n "$line" ]]; do
      local skip="no"
      for key in "${ORDERED_KEYS[@]}"; do
        if [[ "$line" =~ ^[[:space:]]*${key}= ]]; then
          skip="yes"
          break
        fi
      done
      [[ "$skip" == "no" ]] && echo "$line" >>"$TMP_ENV"
    done <"$ENV_PATH"
  fi
  for key in "${ORDERED_KEYS[@]}"; do
    echo "${key}=${NEW_VARS[$key]}" >>"$TMP_ENV"
  done
  mv "$TMP_ENV" "$ENV_PATH"
  TMP_ENV=""
  echo "Saved to ${ENV_PATH}."
}

main() {
  require_cmds
  require_root
  load_current_env
  NETWORK_MODE=$(choose_network_mode)
  echo
  SELECTED_IFACE=$(choose_interface)
  rename_interface "$SELECTED_IFACE"
  if [[ "$NETWORK_MODE" == "interna" ]]; then
    run_setup_dhcp
  else
    echo "External mode chosen; DHCP will not be configured."
  fi

  declare -gA NEW_VARS=()
  ORDERED_KEYS=(MODE QEMU_UID QEMU_GID SPRITE_MIN SPRITE_MAX MASTER_INTERNET_IP MAIN_LINK GO_ACCESS_ENABLE_PANELS GO_ACCESS_DISABLE_PANELS GO_ACCESS_GEOIP_LICENSE_KEY)
  ask_master_vars
  while true; do
    show_preview
    read -r -p "Confirm? (y/n): " ans || exit 1
    case "${ans,,}" in
      y|yes) write_env; break ;;
      n|no) edit_loop ;;
      *) echo "Reply y or n." >&2 ;;
    esac
  done
}

main "$@"
