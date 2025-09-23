
#!/bin/bash
set -euo pipefail

# Usage: ./add_peer.sh <NEW_NODE_NAME> <NEW_NODE_IP>
# Example: ./add_peer.sh sv4 192.168.122.100

VOLUME_NAME="shared_vol"
BRICK_DIR="/gluster_bricks/images"
MOUNT_DIR="/var/lib/libvirt/images"
SSH_USER="root"
EXISTING_NODE="$(hostname -s)"

NEW_NODE_NAME="${1:-}"
NEW_NODE_IP="${2:-}"

if [[ -z "$NEW_NODE_NAME" || -z "$NEW_NODE_IP" ]]; then
	echo "Usage: $0 <NEW_NODE_NAME> <NEW_NODE_IP>"
	exit 1
fi

echo "Probing new node $NEW_NODE_NAME ($NEW_NODE_IP) from $EXISTING_NODE..."
sudo gluster peer probe "$NEW_NODE_NAME"

echo "Setting up brick directory on $NEW_NODE_NAME..."
ssh "$SSH_USER@$NEW_NODE_NAME" "mkdir -p '$BRICK_DIR' && umount '$BRICK_DIR' 2>/dev/null || true"

echo "Setting up mount directory on $NEW_NODE_NAME..."
ssh "$SSH_USER@$NEW_NODE_NAME" "umount '$MOUNT_DIR' 2>/dev/null || true && mkdir -p '$MOUNT_DIR'"

echo "Mounting GlusterFS volume on $NEW_NODE_NAME..."
ssh "$SSH_USER@$NEW_NODE_NAME" "mount -t glusterfs $EXISTING_NODE:/$VOLUME_NAME '$MOUNT_DIR'"

echo "Peer $NEW_NODE_NAME added and volume mounted. Check with: sudo gluster peer status"
