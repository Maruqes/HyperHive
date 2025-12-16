#!/usr/bin/env bash
# check_nfs.sh - Comprehensive NFS stack report (Fedora/CentOS/RHEL-like)
# Usage:  ./check_nfs.sh         (human-readable)
#         ./check_nfs.sh --brief (shorter summary)
#         ./check_nfs.sh --json  (minimal JSON output)

set -euo pipefail

BRIEF=0
JSON=0

for a in "$@"; do
  case "$a" in
    --brief) BRIEF=1 ;;
    --json)  JSON=1 ;;
    *) echo "Usage: $0 [--brief] [--json]"; exit 2;;
  esac
done

# ---------- formatting helpers ----------
hdr(){ [[ $JSON -eq 0 ]] && echo -e "\n=== $* ==="; }
kv(){  [[ $JSON -eq 0 ]] && printf " - %-28s : %s\n" "$1" "$2"; }
ok(){  [[ $JSON -eq 0 ]] && printf "   OK %s\n" "$*"; }
bad(){ [[ $JSON -eq 0 ]] && printf "   !! %s\n" "$*"; }
have(){ command -v "$1" &>/dev/null; }

# ---------- collectors ----------
jobj_open(){ [[ $JSON -eq 1 ]] && { [[ -z "${_J_STARTED:-}" ]] && { _J_STARTED=1; echo "{"; } || echo ","; }; [[ $JSON -eq 1 ]] && printf "\"%s\":" "$1"; }
jprint_str(){ [[ $JSON -eq 1 ]] && printf "\"%s\"" "$(echo -n "$1" | sed 's/"/\\"/g')"; }
jprint_arr(){
  [[ $JSON -eq 1 ]] || return 0
  echo -n "["
  local first=1
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    [[ $first -eq 0 ]] && echo -n ","
    first=0
    jprint_str "$line"
  done
  echo -n "]"
}

# ---------- System ----------
OS_NAME=$(grep -E '^PRETTY_NAME=' /etc/os-release 2>/dev/null | cut -d= -f2- | tr -d '"')
KERNEL=$(uname -r)
HOST=$(hostname -f 2>/dev/null || hostname)

if [[ $JSON -eq 1 ]]; then jobj_open "host"; printf "{"; printf "\"name\":"; jprint_str "$HOST"; printf ",\"os\":"; jprint_str "$OS_NAME"; printf ",\"kernel\":"; jprint_str "$KERNEL"; printf "}"; else
  hdr "System"
  kv "Host"   "$HOST"
  kv "OS"     "$OS_NAME"
  kv "Kernel" "$KERNEL"
fi

# ---------- Packages & versions ----------
hdr "NFS packages/versions"
PKGS=(nfs-utils libnfsidmap rpcbind nfs4-acl-tools)
declare -a PKG_LINES=()
for p in "${PKGS[@]}"; do
  if rpm -q "$p" &>/dev/null; then
    line=$(rpm -q --qf '%{NAME} %{VERSION}-%{RELEASE}.%{ARCH}\n' "$p")
  else
    line="$p (not installed)"
  fi
  PKG_LINES+=("$line")
done
if [[ $JSON -eq 1 ]]; then
  jobj_open "packages"; jprint_arr < <(printf "%s\n" "${PKG_LINES[@]}")
else
  for l in "${PKG_LINES[@]}"; do kv "pkg" "$l"; done
fi

# ---------- Services ----------
hdr "Relevant systemd services"
SERVS=(nfs-server nfs-mountd nfs-idmapd rpc-statd rpcbind)
declare -a S_LINES=()
for s in "${SERVS[@]}"; do
  if systemctl list-unit-files --type=service | grep -q "^$s"; then
    ACT=$(systemctl is-active "$s" 2>/dev/null || true)
    ENA=$(systemctl is-enabled "$s" 2>/dev/null || true)
    MSK=$(systemctl is-enabled "$s" 2>&1 | grep -qi masked && echo "masked" || echo "no")
    S_LINES+=("$s active=$ACT enabled=$ENA masked=$MSK")
    [[ $JSON -eq 0 ]] && kv "$s" "active=$ACT enabled=$ENA masked=$MSK"
  fi
done
[[ $JSON -eq 1 ]] && { jobj_open "services"; jprint_arr < <(printf "%s\n" "${S_LINES[@]}"); }

# ---------- RPC & ports ----------
hdr "RPC/listening ports"
if have ss; then
  SS=$(ss -tulpn | grep -E ':(2049|20048|111)\b' || true)
  [[ -z "$SS" ]] && [[ $JSON -eq 0 ]] && bad "Nothing listening on :2049/:20048/:111" || [[ $JSON -eq 0 ]] && echo "$SS"
  [[ $JSON -eq 1 ]] && { jobj_open "listening"; jprint_arr < <(echo "$SS"); }
fi
if have rpcinfo; then
  RPC=$(rpcinfo -p localhost 2>&1 || true)
  [[ $JSON -eq 0 ]] && echo "$RPC"
  [[ $JSON -eq 1 ]] && { jobj_open "rpcinfo"; jprint_str "$RPC"; }
fi

# ---------- Exports ----------
hdr "/etc/exports & exportfs"
EXP_FILE=$(test -f /etc/exports && cat /etc/exports || echo "(no /etc/exports)")
EXP_D=$(test -d /etc/exports.d && grep -H . /etc/exports.d/* 2>/dev/null || true)
EXPV=$(exportfs -v 2>&1 || true)
[[ $JSON -eq 0 ]] && { echo "--- /etc/exports ---"; echo "$EXP_FILE"; [[ -n "$EXP_D" ]] && { echo "--- /etc/exports.d ---"; echo "$EXP_D"; }; echo "--- exportfs -v ---"; echo "$EXPV"; }
[[ $JSON -eq 1 ]] && { jobj_open "exports_file"; jprint_str "$EXP_FILE"; jobj_open "exports_d"; jprint_str "$EXP_D"; jobj_open "exportfs_v"; jprint_str "$EXPV"; }

# ---------- NFS mounts (client) ----------
hdr "NFS mounts (client)"
MOUNTS=$(grep -E ' nfs4? ' /proc/mounts || true)
if [[ -z "$MOUNTS" ]]; then
  [[ $JSON -eq 0 ]] && bad "No NFS mounts detected on the client."
else
  if [[ $JSON -eq 0 ]]; then echo "$MOUNTS"; fi
fi
[[ $JSON -eq 1 ]] && { jobj_open "mounts"; jprint_str "$MOUNTS"; }

# ---------- nfs.conf ----------
hdr "nfs.conf"
NFS_CONF=$(test -f /etc/nfs.conf && cat /etc/nfs.conf || echo "(no /etc/nfs.conf)")
[[ $JSON -eq 0 ]] && echo "$NFS_CONF"
[[ $JSON -eq 1 ]] && { jobj_open "nfs_conf"; jprint_str "$NFS_CONF"; }

# ---------- SELinux ----------
hdr "SELinux"
if have getenforce; then
  SE=$(getenforce)
  [[ $JSON -eq 0 ]] && kv "getenforce" "$SE"
  [[ $JSON -eq 1 ]] && { jobj_open "selinux_getenforce"; jprint_str "$SE"; }
fi
if have getsebool; then
  B_VIRT=$(getsebool virt_use_nfs 2>/dev/null || echo "virt_use_nfs (unknown)")
  B_HOME=$(getsebool use_nfs_home_dirs 2>/dev/null || echo "use_nfs_home_dirs (unknown)")
  [[ $JSON -eq 0 ]] && { kv "virt_use_nfs" "$B_VIRT"; kv "use_nfs_home_dirs" "$B_HOME"; }
  [[ $JSON -eq 1 ]] && { jobj_open "selinux_bools"; jprint_arr < <(printf "%s\n%s\n" "$B_VIRT" "$B_HOME"); }
fi

# ---------- Firewall (iptables) ----------
hdr "Firewall"
if have iptables; then
  IPT_INPUT=$(iptables -S INPUT 2>/dev/null | grep -E '--dport (2049|20048|111)' || true)
  IPT_NAT=$(iptables -t nat -S 2>/dev/null | grep -i MASQUERADE || true)
  if [[ $JSON -eq 0 ]]; then
    kv "INPUT rules" "${IPT_INPUT:-(none)}"
    kv "NAT" "${IPT_NAT:-(none)}"
  else
    jobj_open "iptables_input"; jprint_str "${IPT_INPUT:-none}"; jobj_open "iptables_nat"; jprint_str "${IPT_NAT:-none}"
  fi
else
  [[ $JSON -eq 0 ]] && bad "iptables indisponível."
fi

# ---------- Kernel modules ----------
hdr "Kernel modules"
MODS=$(lsmod | grep -E '^(nfs|nfsd|lockd|grace|sunrpc|fscache|netfs|rpcsec_gss_krb5)\b' || true)
[[ $JSON -eq 0 ]] && echo "$MODS"
[[ $JSON -eq 1 ]] && { jobj_open "kernel_modules"; jprint_str "$MODS"; }

# ---------- sysctl keys ----------
hdr "sysctl (common keys)"
for key in fs.nfs.nfs4_disable_idmapping fs.nfs.nlm_tcpport fs.nfs.nlm_udpport sunrpc.tcp_slot_table_entries; do
  val=$(sysctl -n "$key" 2>/dev/null || echo "na")
  [[ $JSON -eq 0 ]] && kv "$key" "$val"
done
if [[ $JSON -eq 1 ]]; then
  jobj_open "sysctl"
  printf "{"
  printf "\"fs.nfs.nfs4_disable_idmapping\":"; jprint_str "$(sysctl -n fs.nfs.nfs4_disable_idmapping 2>/dev/null || echo na)"; echo -n ","
  printf "\"fs.nfs.nlm_tcpport\":"; jprint_str "$(sysctl -n fs.nfs.nlm_tcpport 2>/dev/null || echo na)"; echo -n ","
  printf "\"fs.nfs.nlm_udpport\":"; jprint_str "$(sysctl -n fs.nfs.nlm_udpport 2>/dev/null || echo na)"; echo -n ","
  printf "\"sunrpc.tcp_slot_table_entries\":"; jprint_str "$(sysctl -n sunrpc.tcp_slot_table_entries 2>/dev/null || echo na)"; printf "}"
fi

# ---------- Resumo ----------
if [[ $JSON -eq 0 && $BRIEF -eq 1 ]]; then
  echo
  ok "Brief diagnostics complete."
elif [[ $JSON -eq 0 ]]; then
  echo
  ok "Full diagnostics complete."
fi

[[ $JSON -eq 1 ]] && echo "}"
