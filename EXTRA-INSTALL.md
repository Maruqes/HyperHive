# Extra Install (Isolated Network + DHCP/NAT)
Choose this mode when the master must also provide an isolated `512rede` network with DHCP/NAT.

## Prerequisites
- Master: Fedora Server 38+ (SELinux enforcing) with VT-x/AMD-V, ≥8 vCPUs, 64 GB RAM, 500 GB SSD, two NICs (management + `512rede`).
- Slaves: meet Normal mode requirements.
- Dedicated switch for `512rede`, no other DHCP servers on that segment.
- HyperHive repo available on all nodes, `sudo` access, maintenance window.

## Step-by-step
1. **Reset & install on master (destructive).**
   ```bash
   sudo bash scripts/all/install.sh
   ```
   Confirm `YES` and `I UNDERSTAND`. This wipes master VM definitions, libvirt networks, and NFS exports before reinstalling packages.

2. **Reset & install on any slave that needs the full stack.**
   ```bash
   sudo bash scripts/all/install.sh
   ```
   Run sequentially per slave; only execute on hosts you intend to reset.

3. **Configure DHCP/NAT for `512rede` (master).**
   ```bash
   sudo ./scripts/master/setup_dhcp.sh <wan-interface>
   ```
   Supply the upstream/WAN NIC (e.g., `enp1s0`). Before running this step, ensure the master’s isolated interface is already named `512rede` (see Step 4). The script writes `dnsmasq` config, enables masquerading, and starts `dnsmasq-512rede.service`.

4. **Rename the isolated NIC to `512rede` (master + every LAN-connected slave).**
   ```bash
   sudo bash scripts/all/change_interface_name.sh <current-nic-name>
   ```
   Replace `<current-nic-name>` with the detected interface (e.g., `enp7s0`). Run this on the master (prior to Step 3) and on each slave that connects to the isolated LAN so all nodes present the interface as `512rede`. The script makes the rename persistent via systemd/udev. After renaming on every slave, bring the NetworkManager connection up and mark it for autostart so the node actually requests a lease from the master’s DHCP service:
   ```bash
   sudo nmcli con up 512rede              # or the connection name created by the script
   sudo nmcli connection modify 512rede connection.autoconnect yes
   ```

5. **Generate certificates (master twice, then each slave).**
   ```bash
   cd scripts/certsBash
   ./gen_server_cert.sh   # on master: choose master (include mgmt + 512rede SANs)
   ./gen_server_cert.sh   # on master again: choose slave (master also acts as worker)
   ./gen_server_cert.sh   # on each slave: choose slave
   cd -
   ```
   Ensure SAN contains both management and isolated IPs/DNS. Master runs populate `master/certs/` and `slave/certs/`; each physical slave fills its own `slave/certs/`. Distribute `master/certs/ca.crt` and validate fingerprints.

6. **Enable root SSH where needed.**
   ```bash
   sudo bash scripts/all/allow_root_ssh.sh
   ```
   Run on master/slaves requiring root login. Immediately enforce firewall restrictions or key-based auth.

7. **Verify services and networking.**
   ```bash
   systemctl status dnsmasq-512rede libvirtd nfs-server
   ip addr show 512rede
   ss -lupn | grep -E ':67|:53'
   ```
   Confirm DHCP/NAT, virtualization, and NFS are active; ensure `512rede` carries the expected address.

## Tips
- Before Step 3 capture current interface names with `ip -o link show`.
- Use `sudo sysctl -w net.ipv4.ip_forward=1` temporarily if NAT testing is needed before running the DHCP script.
- Re-running `install.sh` requires repeating Steps 3–6 because configuration is reset.

## Next Steps
Configure `.env` values and PM2 autostart using `RUNTIME-SETUP.md`.
