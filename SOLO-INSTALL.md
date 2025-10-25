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

2. **Generate certificates (master & slave).**
   ```bash
   cd scripts/certsBash
   ./gen_server_cert.sh   # choose master, fill SAN with real DNS/IP
   ./gen_server_cert.sh   # run again, choose slave, provide SAN
   cd -
   ```
   Outputs land in `master/certs/` and `slave/certs/`. Distribute `master/certs/ca.crt` to clients and validate the fingerprint.

3. **Enable root SSH if required for automation.**
   ```bash
   sudo bash scripts/all/allow_root_ssh.sh
   ```
   Script backs up `sshd_config`, forces `PermitRootLogin yes`, restarts SSH, and calls `sudo passwd root`. Lock down firewall and keys immediately after.

4. **Verify services.**
   ```bash
   systemctl status libvirtd nfs-server
   virsh list --all
   ```
   Ensure services are running and no unexpected VMs remain.

## Certificate Notes
- Include every reachable DNS/IP in SAN (e.g., `solo.hyperhive.local`, `192.168.50.10`, `127.0.0.1`).
- CN is informational only; clients validate SAN values.
- Unsure what to answer? You can literally type:
  ```
  Is this for a slave or master node? (slave/master) [master]: master
  SAN mode (dns/ip/both) [both]: both
  Comma-separated DNS names [...]: solo.hyperhive.local,solo.lab.local
  Comma-separated IPs [...]: 192.168.50.10,127.0.0.1
  Server CN [hyper-hive-cn]: solo-master-label
  ```
  Then rerun the script, pick `slave`, and swap in the slave’s DNS/IP (e.g., `solo-slave.hyperhive.local`, `192.168.50.11`). It may look odd at first, but just feed the prompts with the real names and addresses your clients use.

## Next Steps
Proceed to `RUNTIME-SETUP.md` to configure `.env` files and register the services with PM2.
