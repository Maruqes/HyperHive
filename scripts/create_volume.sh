#!/bin/bash
set -euo pipefail

# add with "sudo gluster peer probe <new-node>"

# ================= CONFIG =================
VOLUME_NAME="shared_vol"
BRICK_DIR="/gluster_bricks/images"         # Brick storage
MOUNT_DIR="/var/lib/libvirt/images"        # Where images will be mounted
NODES=("marques" "swift512")                  # Cluster nodes
SSH_USER="root"                            # SSH user
SSH_PORT=(22 22)                        # per-node SSH ports

# GlusterFS options
REPLICA_COUNT=2                            # Number of replicas
VOLUME_TYPE="replica"                      # Volume type: replica, distributed, etc.
TRANSPORT="tcp"                            # Transport type: tcp, rdma, etc.
# ==========================================

# --- sanity checks ---
len_nodes=${#NODES[@]}
[[ $len_nodes -eq ${#SSH_PORT[@]} ]] || { echo "NODES and SSH_PORT length mismatch"; exit 1; }

# helper: ssh to node i with its configured port
ssh_i () {
  local idx="$1"; shift
  local host="${NODES[$idx]}"
  local port="${SSH_PORT[$idx]}"
  ssh -p "$port" "${SSH_USER}@${host}" "$@"
}

echo "=== Preparing GlusterFS volume: $VOLUME_NAME ==="

# Step 1: Clean up any existing volume
if sudo gluster volume info "$VOLUME_NAME" &>/dev/null; then
    echo "Removing existing volume $VOLUME_NAME..."
    sudo gluster volume stop "$VOLUME_NAME" 2>/dev/null || true
    sudo gluster volume delete "$VOLUME_NAME" 2>/dev/null || true
fi

# Step 2: Ensure brick directory exists on all nodes
for i in "${!NODES[@]}"; do
    echo "Setting up brick directory on ${NODES[$i]}..."
    ssh_i "$i" "mkdir -p '$BRICK_DIR' && umount '$BRICK_DIR' 2>/dev/null || true"
done

# Step 3: Move existing images to brick directory (local node only)
if [ -d "$MOUNT_DIR" ] && [ "$(ls -A "$MOUNT_DIR" 2>/dev/null)" ]; then
    echo "Moving existing images to brick directory..."
    sudo mkdir -p "$BRICK_DIR"
    sudo cp -a "$MOUNT_DIR"/. "$BRICK_DIR"/ 2>/dev/null || true
fi

# Step 4: Create bricks array
BRICKS=()
for i in "${!NODES[@]}"; do
    BRICKS+=("${NODES[$i]}:$BRICK_DIR")
done

# Step 5: Create and start volume
echo "Creating GlusterFS volume..."
sudo gluster volume create "$VOLUME_NAME" $VOLUME_TYPE $REPLICA_COUNT transport $TRANSPORT "${BRICKS[@]}" force
sudo gluster volume start "$VOLUME_NAME"

# Step 6: Mount the volume on all nodes
# Use the first node as the server endpoint for mounts (adjust if desired)
PRIMARY="${NODES[0]}"
for i in "${!NODES[@]}"; do
    echo "Mounting on ${NODES[$i]}..."
    ssh_i "$i" "
        umount '$MOUNT_DIR' 2>/dev/null || true
        mkdir -p '$MOUNT_DIR'
        mount -t glusterfs ${PRIMARY}:/$VOLUME_NAME '$MOUNT_DIR'
    "
done

# Step 7: Verify
sudo gluster volume info "$VOLUME_NAME"
echo '=== GlusterFS volume setup completed ==='
echo "=== $MOUNT_DIR is now shared across all nodes ==="
