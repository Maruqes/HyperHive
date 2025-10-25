# Normal Install (Master + Slaves)
Use this for small production/pre-production clusters with one master and multiple slaves.

## Prerequisites
- Fedora Server 38+ (SELinux enforcing) on every node; VT-x/AMD-V, ≥8 vCPUs, 32 GB RAM, 250 GB SSD.
- Management VLAN with static IPs and working DNS between nodes.
- HyperHive repository cloned on all nodes, `git`, and `sudo` access.
- Coordinated downtime—the reset scripts remove libvirt/NFS configuration.

## Step-by-step
1. **Reset & install on the master (destructive).**
   ```bash
   sudo bash scripts/all/install.sh
   ```
   Respond `YES` then `I UNDERSTAND`. All master VM definitions, libvirt networks, and NFS exports are wiped before reinstalling packages.

2. **Reset & install on each slave that runs virtualization/NFS.**
   ```bash
   sudo bash scripts/all/install.sh
   ```
   Run sequentially per slave with the same confirmations. Only execute on nodes you intend to wipe.

3. **Generate certificates (master twice, then each slave).**
   ```bash
   cd scripts/certsBash
   ./gen_server_cert.sh   # on master: choose master
   ./gen_server_cert.sh   # on master again: choose slave (master doubles as worker)
   ./gen_server_cert.sh   # on each slave: choose slave
   cd -
   ```
   The master’s two runs populate `master/certs/` and `slave/certs/`; each physical slave populates its own `slave/certs/`. Copy `master/certs/ca.crt` everywhere and verify fingerprints.

4. **Enable root SSH where required.**
   ```bash
   sudo bash scripts/all/allow_root_ssh.sh
   ```
   Execute on master and any slave needing root SSH access. Set strong passwords and tighten firewall rules afterwards.

5. **Verify services and connectivity.**
   ```bash
   systemctl status libvirtd nfs-server
   chronyc sources
   ping -c3 <peer-hostname>
   ```
   Confirm libvirt/NFS are running, time sync is healthy, and nodes can reach each other.

## Tips
- Keep `/etc/hosts` or DNS updated with master/slave hostnames before certificate generation.
- If you re-run `install.sh`, repeat the certificate and SSH steps because prior configuration is replaced.

## Next Steps
Complete `.env` setup and PM2 autostart by following `RUNTIME-SETUP.md`.
