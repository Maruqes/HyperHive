#!/bin/bash

# prepare_nodes.sh
# Installs GlusterFS on nodes, adds /etc/hosts entries and prepares brick directories.

set -euo pipefail

# Configuration arrays (edit as needed)
os=("fedora" "fedora")
names=("marques" "swift512")
ips=("192.168.1.200" "192.168.1.169")
brick_paths=("/gluster_bricks/images" "/gluster_bricks/images")
ssh_ports=(22512 22)   # per-node SSH ports

# --- sanity checks on array lengths ---
len=${#ips[@]}
[[ $len -eq ${#names[@]} ]] || { echo "ips and names array lengths differ. Exiting."; exit 1; }
[[ $len -eq ${#os[@]} ]] || { echo "ips and os array lengths differ. Exiting."; exit 1; }
[[ $len -eq ${#brick_paths[@]} ]] || { echo "ips and brick_paths array lengths differ. Exiting."; exit 1; }
[[ $len -eq ${#ssh_ports[@]} ]] || { echo "ips and ssh_ports array lengths differ. Exiting."; exit 1; }

# helper: run ssh to node i with its port
ssh_i () {
  local i="$1"; shift
  local ip="${ips[$i]}"
  local port="${ssh_ports[$i]}"
  ssh -p "$port" "$ip" "$@"
}

# Add all IP/name pairs to local /etc/hosts
for j in "${!ips[@]}"; do
  ip_j="${ips[$j]}"
  name_j="${names[$j]}"
  if ! grep -qE "^$ip_j[[:space:]]+$name_j([[:space:]]|$)" /etc/hosts; then
    echo "$ip_j $name_j" | sudo tee -a /etc/hosts >/dev/null
  fi
done

# For each node, add all IP/name pairs to its /etc/hosts and install GlusterFS
for i in "${!ips[@]}"; do
  ip="${ips[$i]}"
  echo "[$ip] Adding all IP/name pairs to /etc/hosts..."
  for j in "${!ips[@]}"; do
    ip_j="${ips[$j]}"
    name_j="${names[$j]}"
    # note: variables expanded locally; the remote sees the concrete values
    ssh_i "$i" "sudo bash -c 'grep -qE \"^$ip_j\\s+$name_j(\\s|$)\" /etc/hosts || echo \"$ip_j $name_j\" >> /etc/hosts'" \
      || echo "Warning: could not add $ip_j $name_j to /etc/hosts on $ip"
  done

  echo "[$ip] Installing GlusterFS..."
  case "${os[$i]}" in
    ubuntu)
      ssh_i "$i" "sudo apt-get update && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y glusterfs-server"
      ;;
    fedora)
      ssh_i "$i" "sudo dnf install -y glusterfs-server"
      ;;
    *)
      echo "Unsupported OS for $ip: ${os[$i]}"; exit 1
      ;;
  esac

  echo "[$ip] Starting and enabling glusterd..."
  ssh_i "$i" "sudo systemctl enable --now glusterd"

  echo "[$ip] Creating brick directory ${brick_paths[$i]}..."
  ssh_i "$i" "sudo mkdir -p ${brick_paths[$i]} && sudo chown -R \$USER:\$USER ${brick_paths[$i]}"
done

echo "Prepare step complete. Ensure SSH connectivity (including ports) between nodes for 'gluster peer probe'."
