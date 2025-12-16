#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

REQUIRED_CMDS=(sed awk grep readlink mktemp)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ENV_PATH="${ENV_PATH:-${PROJECT_ROOT}/.env}"

TMP_ENV=""
trap '[[ -n "${TMP_ENV}" && -f "${TMP_ENV}" ]] && rm -f "${TMP_ENV}"' EXIT

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

normalize_ip_list() {
  local raw="$1"; raw=${raw//,/ };
  read -ra parts <<<"$raw"
  local out=()
  local seen=()
  for ip in "${parts[@]}"; do
    [[ -z "$ip" ]] && continue
    if is_ipv4 "$ip"; then
      if [[ -z "${seen[$ip]:-}" ]]; then
        out+=("$ip")
        seen[$ip]=1
      fi
    else
      return 1
    fi
  done
  if [[ ${#out[@]} -eq 0 ]]; then
    return 1
  fi
  (IFS=,; echo "${out[*]}")
}

prompt_ip_list() {
  local name="$1" hint="$2" default_value="$3" input normalized
  while true; do
    input=$(prompt_with_default "${name} - ${hint}" "${default_value}" "no")
    normalized=$(normalize_ip_list "$input" 2>/dev/null || true)
    if [[ -n "$normalized" ]]; then
      echo "$normalized"
      return 0
    fi
    echo "Invalid list; separate with comma or space." >&2
  done
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

ask_slave_vars() {
  echo "Fill the slave .env file."
  NEW_VARS[MODE]=$(prompt_mode "$(current_or_default MODE "prod")")
  echo "QEMU_UID/GID: UID and GID used by the VMs (usually 107)."
  NEW_VARS[QEMU_UID]=$(prompt_int_nonneg "QEMU_UID" "$(current_or_default QEMU_UID "107")")
  NEW_VARS[QEMU_GID]=$(prompt_int_nonneg "QEMU_GID" "$(current_or_default QEMU_GID "107")")
  echo "SPRITE_MIN/MAX: reserved port range for this host. Avoid collisions with other hosts."
  ask_sprite_range
  echo "MASTER_IP: master IP reachable by this slave."
  NEW_VARS[MASTER_IP]=$(prompt_ipv4 "MASTER_IP" "master IPv4" "$(current_or_default MASTER_IP "")")
  echo "SLAVE_IP: IPv4 for this slave."
  NEW_VARS[SLAVE_IP]=$(prompt_ipv4 "SLAVE_IP" "this host IPv4" "$(current_or_default SLAVE_IP "")")
  echo "SLAVES_IPS: list all slave IPs (include this one), separated by comma or space."
  while true; do
    NEW_VARS[SLAVES_IPS]=$(prompt_ip_list "SLAVES_IPS" "required list" "$(current_or_default SLAVES_IPS "${NEW_VARS[SLAVE_IP]}")")
    if [[ ",${NEW_VARS[SLAVES_IPS]}," != *,${NEW_VARS[SLAVE_IP]},* ]]; then
      read -r -p "List does not include this SLAVE_IP. Add automatically? (y/n): " ans || exit 1
      case "${ans,,}" in
        y|yes)
          NEW_VARS[SLAVES_IPS]="${NEW_VARS[SLAVE_IP]},${NEW_VARS[SLAVES_IPS]}"
          NEW_VARS[SLAVES_IPS]=$(normalize_ip_list "${NEW_VARS[SLAVES_IPS]}")
          break ;;
        n|no) echo "Keeping list without SLAVE_IP."; break ;;
        *) echo "Reply y or n." >&2 ;;
      esac
    else
      break
    fi
  done
  echo "MAIN_LINK: main base URL/domain for the system (public endpoint)."
  NEW_VARS[MAIN_LINK]=$(prompt_non_empty "MAIN_LINK" "e.g., https://example.domain" "$(current_or_default MAIN_LINK "")")
}

show_preview() {
  echo "\nPreview of .env (${ENV_PATH}):"
  for key in "${ORDERED_KEYS[@]}"; do
    echo "${key}=${NEW_VARS[$key]}"
  done
  echo
}

edit_one() {
  local key="$1"
  case "$key" in
    MODE) NEW_VARS[MODE]=$(prompt_mode "${NEW_VARS[MODE]}") ;;
    QEMU_UID) NEW_VARS[QEMU_UID]=$(prompt_int_nonneg "QEMU_UID" "${NEW_VARS[QEMU_UID]}") ;;
    QEMU_GID) NEW_VARS[QEMU_GID]=$(prompt_int_nonneg "QEMU_GID" "${NEW_VARS[QEMU_GID]}") ;;
    SPRITE_MIN|SPRITE_MAX) ask_sprite_range ;;
    MASTER_IP) NEW_VARS[MASTER_IP]=$(prompt_ipv4 "MASTER_IP" "master IPv4" "${NEW_VARS[MASTER_IP]}") ;;
    SLAVE_IP) NEW_VARS[SLAVE_IP]=$(prompt_ipv4 "SLAVE_IP" "this host IPv4" "${NEW_VARS[SLAVE_IP]}") ;;
    SLAVES_IPS)
      echo "SLAVES_IPS: list all slave IPs (include this one), separated by comma or space."
      while true; do
        NEW_VARS[SLAVES_IPS]=$(prompt_ip_list "SLAVES_IPS" "required list" "${NEW_VARS[SLAVES_IPS]}")
        if [[ ",${NEW_VARS[SLAVES_IPS]}," != *,${NEW_VARS[SLAVE_IP]},* ]]; then
          read -r -p "List does not include this SLAVE_IP. Add automatically? (y/n): " ans || exit 1
          case "${ans,,}" in
            y|yes)
              NEW_VARS[SLAVES_IPS]="${NEW_VARS[SLAVE_IP]},${NEW_VARS[SLAVES_IPS]}"
              NEW_VARS[SLAVES_IPS]=$(normalize_ip_list "${NEW_VARS[SLAVES_IPS]}")
              break ;;
            n|no) echo "Keeping list without SLAVE_IP."; break ;;
            *) echo "Reply y or n." >&2 ;;
          esac
        else
          break
        fi
      done
      ;;
    MAIN_LINK) NEW_VARS[MAIN_LINK]=$(prompt_non_empty "MAIN_LINK" "e.g., https://example.domain" "${NEW_VARS[MAIN_LINK]}") ;;
  esac
}

edit_loop() {
  while true; do
    read -r -p "Change anything before saving? (y/n): " ans || exit 1
    case "${ans,,}" in
      n|no) return 0 ;;
      y|yes)
        read -r -p "Which variable? (exact name or all): " var || exit 1
        local var_lower="${var,,}"
        local var_upper="${var^^}"
        if [[ "$var_lower" == "all" ]]; then
          ask_slave_vars
          continue
        fi
        local found="no"
        for key in "${ORDERED_KEYS[@]}"; do
          if [[ "$var_upper" == "$key" ]]; then
            found="yes"
            edit_one "$key"
            break
          fi
        done
        if [[ "$found" == "no" ]]; then
          echo "Invalid name. Use the exact variable name." >&2
        fi
        ;;
      *) echo "Reply y or n." >&2 ;;
    esac
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
  load_current_env
  declare -gA NEW_VARS=()
  ORDERED_KEYS=(MODE QEMU_UID QEMU_GID SPRITE_MIN SPRITE_MAX MASTER_IP SLAVE_IP SLAVES_IPS MAIN_LINK)
  ask_slave_vars
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
