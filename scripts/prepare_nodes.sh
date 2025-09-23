#!/bin/bash

# prepare_nodes.sh
# Installs GlusterFS on nodes, adds /etc/hosts entries and prepares brick directories.

set -euo pipefail

# Configuration arrays (edit as needed)
os=("ubuntu" "ubuntu" "ubuntu" "ubuntu")
names=("sv1" "sv2" "sv3" "sv4")
ips=("192.168.122.51" "192.168.122.169" "192.168.122.67" "192.168.122.208")
brick_paths=("/gluster_bricks/images" "/gluster_bricks/images" "/gluster_bricks/images" "/gluster_bricks/images")

if [ ${#ips[@]} -ne ${#names[@]} ]; then
  echo "IPs and names array lengths differ. Exiting."
  exit 1
fi

# Add hosts entries on local machine and on remote nodes

# Add all IP/name pairs to local /etc/hosts
for j in "${!ips[@]}"; do
  ip_j="${ips[$j]}"
  name_j="${names[$j]}"
  if ! grep -qE "^$ip_j\s+$name_j(\s|$)" /etc/hosts; then
    echo "$ip_j $name_j" | sudo tee -a /etc/hosts > /dev/null
  fi
done

# For each node, add all IP/name pairs to its /etc/hosts
for i in "${!ips[@]}"; do
  ip="${ips[$i]}"
  echo "Adding all IP/name pairs to /etc/hosts on $ip..."
  for j in "${!ips[@]}"; do
    ip_j="${ips[$j]}"
    name_j="${names[$j]}"
    ssh "$ip" "sudo bash -c 'grep -qE \"^$ip_j\\s+$name_j(\\s|$)\" /etc/hosts || echo \"$ip_j $name_j\" >> /etc/hosts'" || echo "Warning: could not add $ip_j $name_j to /etc/hosts on $ip"
  done

  echo "Installing GlusterFS on $ip..."
  case "${os[$i]}" in
    ubuntu)
      ssh "$ip" "sudo apt-get update && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y glusterfs-server"
      ;;
    fedora)
      ssh "$ip" "sudo dnf install -y glusterfs-server"
      ;;
    *)
      echo "Unsupported OS for $ip: ${os[$i]}"; exit 1
      ;;
  esac

  echo "Starting and enabling glusterd on $ip..."
  ssh "$ip" "sudo systemctl enable --now glusterd"

  echo "Creating brick directory ${brick_paths[$i]} on $ip..."
  ssh "$ip" "sudo mkdir -p ${brick_paths[$i]} && sudo chown -R \$USER:\$USER ${brick_paths[$i]}"
done

echo "Prepare step complete. Ensure SSH connectivity between nodes for gluster peer probe."