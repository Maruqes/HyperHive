#!/bin/bash
# prepare_nodes.sh
# Instala GlusterFS nos nós, atualiza /etc/hosts, prepara diretórios de brick
# e ABRE AS PORTAS DE FIREWALL necessárias.

set -euo pipefail

# =================== CONFIG ===================
os=("fedora" "fedora")
names=("marques512sv" "marques")
ips=("10.42.0.1" "10.42.0.43")
brick_paths=("/gluster_bricks/images" "/gluster_bricks/images")
ssh_ports=(22 22)             # porta SSH por nó
bricks_per_node=(1 1)            # quantos bricks por nó (1 porta por brick a partir de 49152)
enable_gnfs=false                # true se fores exportar via Gluster NFS (gNFS)
# ==============================================

# --- sanidade ---
len=${#ips[@]}
[[ $len -eq ${#names[@]} ]] || { echo "ips e names com tamanhos diferentes"; exit 1; }
[[ $len -eq ${#os[@]} ]] || { echo "ips e os com tamanhos diferentes"; exit 1; }
[[ $len -eq ${#brick_paths[@]} ]] || { echo "ips e brick_paths com tamanhos diferentes"; exit 1; }
[[ $len -eq ${#ssh_ports[@]} ]] || { echo "ips e ssh_ports com tamanhos diferentes"; exit 1; }
[[ $len -eq ${#bricks_per_node[@]} ]] || { echo "ips e bricks_per_node com tamanhos diferentes"; exit 1; }

# helper: ssh para o nó i
ssh_i () {
  local i="$1"; shift
  local ip="${ips[$i]}"
  local port="${ssh_ports[$i]}"
  ssh -o StrictHostKeyChecking=no -p "$port" "$ip" "$@"
}

# helper: abre portas no firewalld (Fedora/RHEL)
fw_firewalld_allow () {
  local i="$1"
  local ports_tcp=("$2")   # string com portas/ranges separados por espaço, ex: "24007 24008 49152-49152"
  local ports_udp=("$3")
  # aplica
  for p in ${ports_tcp}; do
    ssh_i "$i" "sudo firewall-cmd --add-port=${p}/tcp --permanent >/dev/null"
  done
  for p in ${ports_udp}; do
    ssh_i "$i" "sudo firewall-cmd --add-port=${p}/udp --permanent >/dev/null"
  done
  ssh_i "$i" "sudo firewall-cmd --reload >/dev/null"
}

# helper: abre portas no ufw (Ubuntu/Debian)
fw_ufw_allow () {
  local i="$1"
  local ports_tcp=("$2")
  local ports_udp=("$3")
  ssh_i "$i" "sudo ufw --force enable >/dev/null 2>&1 || true"
  for p in ${ports_tcp}; do
    ssh_i "$i" "sudo ufw allow ${p}/tcp >/dev/null"
  done
  for p in ${ports_udp}; do
    ssh_i "$i" "sudo ufw allow ${p}/udp >/dev/null"
  done
}

# Atualiza /etc/hosts local
for j in "${!ips[@]}"; do
  ip_j="${ips[$j]}"
  name_j="${names[$j]}"
  if ! grep -qE "^$ip_j[[:space:]]+$name_j([[:space:]]|$)" /etc/hosts; then
    echo "$ip_j $name_j" | sudo tee -a /etc/hosts >/dev/null
  fi
done

# Para cada nó
for i in "${!ips[@]}"; do
  ip="${ips[$i]}"
  echo "[$ip] A adicionar entradas /etc/hosts..."
  for j in "${!ips[@]}"; do
    ip_j="${ips[$j]}"
    name_j="${names[$j]}"
    ssh_i "$i" "sudo bash -c 'grep -qE \"^$ip_j\\s+$name_j(\\s|$)\" /etc/hosts || echo \"$ip_j $name_j\" >> /etc/hosts'" \
      || echo "Aviso: não foi possível adicionar $ip_j $name_j a /etc/hosts em $ip"
  done

  echo "[$ip] A instalar GlusterFS..."
  case "${os[$i]}" in
    ubuntu|debian)
      ssh_i "$i" "sudo apt-get update && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y glusterfs-server"
      ;;
    fedora|rhel|centos|rocky|almalinux)
      ssh_i "$i" "sudo dnf install -y glusterfs-server || sudo yum install -y glusterfs-server"
      ;;
    *)
      echo "OS não suportado para $ip: ${os[$i]}"; exit 1;;
  esac

  echo "[$ip] A iniciar e ativar glusterd..."
  ssh_i "$i" "sudo systemctl enable --now glusterd"

  echo "[$ip] A criar diretório de brick ${brick_paths[$i]}..."
  ssh_i "$i" "sudo mkdir -p ${brick_paths[$i]} && sudo chown -R \$USER:\$USER ${brick_paths[$i]}"

  # ===== Firewall =====
  # Portas base do Gluster
  base_tcp="24007 24008"
  base_udp="24007 24008"

  # Portas de bricks: 1 porta por brick a partir de 49152
  n_bricks=${bricks_per_node[$i]}
  start=49152
  end=$(( start + n_bricks - 1 ))
  brick_range="${start}-${end}"

  # gNFS opcional
  gnfs_tcp=""
  gnfs_udp=""
  if $enable_gnfs; then
    # rpcbind 111 (tcp/udp) e gNFS 38465-38467 (tcp/udp)
    gnfs_tcp="111 38465-38467"
    gnfs_udp="111 38465-38467"
  fi

  # Aplica por OS
  case "${os[$i]}" in
    ubuntu|debian)
      echo "[$ip] A abrir firewall (ufw) para Gluster..."
      fw_ufw_allow "$i" "$base_tcp $brick_range $gnfs_tcp" "$base_udp $brick_range $gnfs_udp"
      ;;
    fedora|rhel|centos|rocky|almalinux)
      echo "[$ip] A abrir firewall (firewalld) para Gluster..."
      # certifica que o firewalld está ativo (ignora erro se já estiver)
      ssh_i "$i" "sudo systemctl enable --now firewalld >/dev/null 2>&1 || true"
      fw_firewalld_allow "$i" "$base_tcp $brick_range $gnfs_tcp" "$base_udp $brick_range $gnfs_udp"
      ;;
  esac
  echo "[$ip] Firewall configurada (TCP/UDP: ${base_tcp}, bricks: ${brick_range}${enable_gnfs:+, gNFS})."
done

echo "Preparação concluída. Agora já deves conseguir 'gluster peer probe' entre os nós."
