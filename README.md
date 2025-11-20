# HyperHive
Choose the right install guide, follow the runtime setup, and make sure required ports stay open—especially when slaves sit outside the LAN. HyperHive is currently only supported on Fedora hosts configured with an English locale.

## Installation Guides
- [Solo Install](SOLO-INSTALL.md) — single node hosts every role.
- [Normal Install](NORMAL-INSTALL.md) — one master plus several slaves on the same network.
- [Extra Install](EXTRA-INSTALL.md) — master also serves the isolated `512rede` network.

## After Installation
- [Runtime Configuration & Autostart](RUNTIME-SETUP.md) — configure `.env`, build binaries, and register PM2 services.

## Network Ports
- `50051` — master gRPC endpoint.
- `50052` — slave gRPC endpoint.

If a slave lives outside the internal network, ensure these ports are reachable through firewalls/VPNs and adjust NAT rules as needed.

### Exposing Extra Ports
When adding new listeners (for example, a Minecraft server proxied by NPM) make sure both Docker and the host firewall allow the traffic.

1. Update `master/docker-compose.yml`:
   - For services without `network_mode: "host"`, extend the `ports` section (e.g. `- "25565:25565"`).
   - With host networking, skip port mappings and just configure the stream/virtual host inside NPM.
   - Apply the change: `docker compose -f master/docker-compose.yml up -d`.
2. Open the port in `firewalld` on the zone bound to the NIC (`sudo firewall-cmd --get-active-zones` shows the mapping). Example:
   - `sudo firewall-cmd --zone=FedoraServer --add-port=25565/tcp --permanent`
   - (Optional) add UDP if needed, then reload: `sudo firewall-cmd --reload`.
   - Confirm: `sudo firewall-cmd --zone=FedoraServer --list-ports`.
3. Re-test from outside the host (`nc -zv <host-ip> 25565`) and verify traffic reaches the service.
