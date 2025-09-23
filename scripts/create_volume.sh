#!/bin/bash
set -euo pipefail

#add with "sudo gluster peer probe <new-node>"" 

# ================= CONFIG =================
VOLUME_NAME="shared_vol"
BRICK_DIR="/gluster_bricks/images"         # Brick storage
MOUNT_DIR="/var/lib/libvirt/images"        # Where images will be mounted
NODES=("sv1" "sv2" "sv3")                  # Cluster nodes
SSH_USER="root"                            # SSH user
# GlusterFS options
REPLICA_COUNT=3                            # Number of replicas
VOLUME_TYPE="replica"                      # Volume type: replica, distributed, etc.
TRANSPORT="tcp"                            # Transport type: tcp, rdma, etc.
# ==========================================

echo "=== Preparing GlusterFS volume: $VOLUME_NAME ==="

# Step 1: Clean up any existing volume
if sudo gluster volume info "$VOLUME_NAME" &>/dev/null; then
    echo "Removing existing volume $VOLUME_NAME..."
    sudo gluster volume stop "$VOLUME_NAME" 2>/dev/null || true
    sudo gluster volume delete "$VOLUME_NAME" 2>/dev/null || true
fi

# Step 2: Ensure brick directory exists on all nodes
for NODE in "${NODES[@]}"; do
    echo "Setting up brick directory on $NODE..."
    ssh "$SSH_USER@$NODE" "
        mkdir -p '$BRICK_DIR'
        # Clear any existing mount on brick directory
        umount '$BRICK_DIR' 2>/dev/null || true
    "
done

# Step 3: Move existing images to brick directory (local node only)
if [ -d "$MOUNT_DIR" ] && [ "$(ls -A "$MOUNT_DIR" 2>/dev/null)" ]; then
    echo "Moving existing images to brick directory..."
    sudo mkdir -p "$BRICK_DIR"
    sudo cp -a "$MOUNT_DIR"/* "$BRICK_DIR"/ 2>/dev/null || true
fi

# Step 4: Create bricks array
BRICKS=()
for NODE in "${NODES[@]}"; do
    BRICKS+=("$NODE:$BRICK_DIR")
done

# Step 5: Create and start volume
echo "Creating GlusterFS volume..."
sudo gluster volume create "$VOLUME_NAME" $VOLUME_TYPE $REPLICA_COUNT transport $TRANSPORT "${BRICKS[@]}" force
sudo gluster volume start "$VOLUME_NAME"

# Step 6: Mount the volume on all nodes
for NODE in "${NODES[@]}"; do
    echo "Mounting on $NODE..."
    ssh "$SSH_USER@$NODE" "
        umount '$MOUNT_DIR' 2>/dev/null || true
        mkdir -p '$MOUNT_DIR'
        mount -t glusterfs ${NODES[0]}:/$VOLUME_NAME '$MOUNT_DIR'
    "
done

# Step 7: Verify
sudo gluster volume info "$VOLUME_NAME"
echo "=== GlusterFS volume setup completed ==="
echo "=== $MOUNT_DIR is now shared across all nodes ==="