#!/bin/bash

# Script to force allow root SSH login
# This modifies /etc/ssh/sshd_config and restarts SSH service

# Backup the original config
cp /etc/ssh/sshd_config /etc/ssh/sshd_config.backup

# Check if PermitRootLogin is already set
if grep -q "^PermitRootLogin" /etc/ssh/sshd_config; then
    # Replace existing line
    sed -i 's/^PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config
else
    # Add the line
    echo "PermitRootLogin yes" >> /etc/ssh/sshd_config
fi

# Restart SSH service
systemctl restart ssh

echo "Root SSH login has been enabled. Original config backed up to /etc/ssh/sshd_config.backup"