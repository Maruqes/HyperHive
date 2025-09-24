#!/bin/bash

# Script to force allow root SSH login
# This modifies /etc/ssh/sshd_config and restarts SSH service

# É PRECISO FAZER "sudo -i" && "passwd"  

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

# Restart SSH service in a distro-aware way
restart_service() {
    # Try common systemd service names first
    for svc in sshd ssh; do
        if systemctl list-units --full -all | grep -q "^${svc}.service"; then
            echo "Restarting ${svc}.service via systemctl"
            systemctl restart "${svc}.service" && return 0
        fi
    done

    # Fallback to service command (SysV / upstart)
    if command -v service >/dev/null 2>&1; then
        if service sshd status >/dev/null 2>&1; then
            echo "Restarting sshd via service"
            service sshd restart && return 0
        elif service ssh status >/dev/null 2>&1; then
            echo "Restarting ssh via service"
            service ssh restart && return 0
        fi
    fi

    # Last resort: try invoking init script directly
    if [ -x /etc/init.d/sshd ]; then
        echo "Restarting /etc/init.d/sshd"
        /etc/init.d/sshd restart && return 0
    elif [ -x /etc/init.d/ssh ]; then
        echo "Restarting /etc/init.d/ssh"
        /etc/init.d/ssh restart && return 0
    fi

    echo "Warning: could not find a known ssh service to restart. Please restart SSH manually." >&2
    return 1
}

if restart_service; then
    echo "Root SSH login has been enabled. Original config backed up to /etc/ssh/sshd_config.backup"
else
    echo "Root SSH login enabled in config, but automatic restart failed. Please restart SSH manually." >&2
fi