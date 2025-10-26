# HyperHive Network Setup Guide

This guide provides step-by-step instructions for setting up the local network configuration for HyperHive, including changing the network interface name to `512rede` and creating a bridge for virtual machine networking.

## Overview

The network setup process involves two main steps:
1. **Renaming the network interface** to `512rede` (standardized name for HyperHive)
2. **Creating a bridge** (`br512`) to allow VMs to communicate on the same LAN as the host

## Prerequisites

- Root/sudo access
- NetworkManager installed and running
- Basic understanding of network interfaces
- Physical network interface that you want to use (e.g., `eth0`, `enp0s3`, etc.)

## Quick Setup (Automated)

If you want the easiest setup experience, we provide automated scripts that handle everything for you.

### Step 1: Identify Your Network Interface

First, identify the name of your physical network interface:

```bash
ip link show
```

Look for your active network interface (e.g., `eth0`, `enp0s3`, `eno1`, etc.). Ignore `lo` (loopback) and any virtual interfaces.

### Step 2: Run the Automated Setup

From the HyperHive root directory, run these commands as root:

```bash
# Navigate to the scripts directory
cd scripts/all

# Step 1: Rename your interface to 512rede
sudo ./change_interface_name.sh <your_current_interface_name>
# Example: sudo ./change_interface_name.sh eth0

# Step 2: Create the bridge
sudo ./create_bridge.sh 512rede
```

**That's it!** The scripts will handle all the configuration for you.

---

## Verification

### After Renaming Interface

#### Verification

After running the script, verify the interface was renamed:

```bash
ip link show 512rede
```

You should see your interface with the new name.

### After Creating Bridge

#### Verification

After creating the bridge, verify everything is working:

```bash
# Check bridge status
ip addr show br512

# Check bridge connections
nmcli connection show

# Verify 512rede is enslaved to br512
bridge link show

# Test internet connectivity
ping -c 3 8.8.8.8
```

Expected output for `bridge link show`:
```
XX: 512rede: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 master br512 state forwarding priority 32 cost 100
```

---

## Reverting Changes

If you need to revert the bridge configuration and go back to using the physical interface directly:

```bash
cd scripts/all
sudo ./revert_br512.sh 512rede
```

This script will:
- Remove the bridge configuration
- Restore your original network settings
- Return `512rede` to direct operation

**Note:** This does not revert the interface name change (`512rede` → original name). If you need to rename it back, you would need to manually edit the systemd and udev rules.

---

## Troubleshooting

### Interface Name Doesn't Persist After Reboot

If the interface reverts to its original name after reboot:

1. Check systemd network configuration:
   ```bash
   ls -la /etc/systemd/network/*.link
   cat /etc/systemd/network/01-rename-*.link
   ```

2. Check udev rules:
   ```bash
   ls -la /etc/udev/rules.d/*rename*.rules
   cat /etc/udev/rules.d/82-rename-*.rules
   ```

3. Ensure the MAC address in the files matches your interface:
   ```bash
   cat /sys/class/net/512rede/address
   ```

### Bridge Doesn't Come Up

If the bridge fails to activate:

1. Check NetworkManager status:
   ```bash
   sudo systemctl status NetworkManager
   ```

2. Check connection details:
   ```bash
   nmcli connection show br512
   ```

3. Try manually bringing it up:
   ```bash
   sudo nmcli connection up br512
   ```

4. Check system logs:
   ```bash
   sudo journalctl -u NetworkManager -n 50
   ```

### No Internet After Bridge Creation

If you lose internet connectivity:

1. Verify the bridge has an IP address:
   ```bash
   ip addr show br512
   ```

2. Check routing table:
   ```bash
   ip route
   ```

3. Try requesting a new DHCP lease:
   ```bash
   sudo nmcli connection down br512
   sudo nmcli connection up br512
   ```

4. If still failing, revert the bridge:
   ```bash
   sudo ./revert_br512.sh 512rede
   ```

### VMs Can't Access Network

If VMs can't access the network through the bridge:

1. Verify libvirt network configuration references the bridge:
   ```bash
   virsh net-list --all
   virsh net-dumpxml <network_name>
   ```

2. Ensure the bridge is in forwarding mode:
   ```bash
   bridge link show
   ```

3. Check firewall rules aren't blocking bridge traffic:
   ```bash
   sudo iptables -L -n -v
   ```

---

## Integration with HyperHive

Once your network is properly configured with the `br512` bridge:

1. **VMs will automatically use the bridge** for networking (when configured to use bridged mode in libvirt)
2. **VMs will appear on the same LAN** as your host machine
3. **VMs can receive DHCP addresses** from your router/DHCP server
4. **VMs are accessible** from other machines on your network

This is the recommended network configuration for HyperHive clusters where VMs need to communicate directly with each other and with external clients.

---

## Script Locations

All network setup scripts are located in the `scripts/all/` directory:

- `change_interface_name.sh` - Rename interface to 512rede
- `create_bridge.sh` - Create br512 bridge
- `revert_br512.sh` - Revert bridge configuration
- `install.sh` - Complete system installation (includes network setup)

---

## Best Practices

1. **Always run scripts as root** (using `sudo`)
2. **Back up your network configuration** before making changes
3. **Test connectivity** after each step
4. **Document your original interface name** in case you need to revert
5. **Ensure SSH access** from another machine before making network changes on remote servers
6. **Use static IP** on the bridge for production servers to avoid DHCP changes

---

## Next Steps

After completing the network setup:

1. Proceed with the HyperHive installation (see `NORMAL-INSTALL.md`, `SOLO-INSTALL.md`, or `EXTRA-INSTALL.md`)
2. Configure libvirt to use the `br512` bridge
3. Create and test your first virtual machine
4. Set up additional HyperHive services (master/slave nodes)

For more information, refer to:
- `README.md` - Project overview
- `RUNTIME-SETUP.md` - Runtime configuration
- `NORMAL-INSTALL.md` - Standard installation
- `SOLO-INSTALL.md` - Single-node installation
- `EXTRA-INSTALL.md` - Advanced installation
