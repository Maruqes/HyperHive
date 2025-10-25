# Solo Install (Single Node)
Run this when a single machine must host every HyperHive role (control + worker).

## Prerequisites
- Fedora Server 38+ (SELinux enforcing) on x86_64 with VT-x/AMD-V, ≥4 vCPUs (8 recommended), 32 GB RAM, 200 GB SSD, 1 GbE NIC.
- Local `git` checkout of HyperHive and permission to execute scripts.
- `sudo`/root access and a maintenance window—this workflow wipes existing libvirt and NFS state.

## Step-by-step
1. **Reset and install the platform (destructive).**
   ```bash
   sudo bash scripts/all/install.sh
   ```
   Confirm `YES` and `I UNDERSTAND`. All VM definitions, libvirt networks, and NFS exports are removed before packages are reinstalled.

2. **Enable root SSH if required for automation.**
   ```bash
   sudo bash scripts/all/allow_root_ssh.sh
   ```
   Script backs up `sshd_config`, forces `PermitRootLogin yes`, restarts SSH, and calls `sudo passwd root`. Lock down firewall and keys immediately after.

3. **Verify services.**
   ```bash
   systemctl status libvirtd nfs-server
   virsh list --all
   ```
   Ensure services are running and no unexpected VMs remain.

## Next Steps
Proceed to `RUNTIME-SETUP.md` to configure `.env` files and register the services with PM2.
