#!/bin/bash
# reset_gluster.sh — full GlusterFS reset (⚠️ destroys all Gluster data/config)

set -euo pipefail

echo "Stopping GlusterFS services..."
systemctl stop glusterd glusterfsd 2>/dev/null || true

echo "Killing stray Gluster processes..."
pkill -9 glusterfsd 2>/dev/null || true
pkill -9 glusterd 2>/dev/null || true
pkill -9 glusterfs 2>/dev/null || true

echo "Unmounting any Gluster volumes..."
for mnt in $(mount | grep glusterfs | awk '{print $3}'); do
  umount -f "$mnt" || true
done

echo "Removing Gluster configuration..."
rm -rf /var/lib/glusterd/*
rm -rf /var/log/glusterfs/*
rm -rf /etc/glusterfs/*
rm -rf /var/run/gluster/*

echo "Cleaning brick directories..."
find / -type d -path "*/gluster_bricks/*" -exec rm -rf {} + 2>/dev/null || true

echo "Reset complete on $(hostname)"
