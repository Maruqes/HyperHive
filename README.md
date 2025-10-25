# HyperHive
Choose the right install guide, follow the runtime setup, and make sure required ports stay open—especially when slaves sit outside the LAN.

## Installation Guides
- [Solo Install](SOLO-INSTALL.md) — single node hosts every role.
- [Normal Install](NORMAL-INSTALL.md) — one master plus several slaves on the same network.
- [Extra Install](EXTRA-INSTALL.md) — master also serves the isolated `512rede` network.

## After Installation
- [Runtime Configuration & Autostart](RUNTIME-SETUP.md) — configure `.env`, build binaries, and register PM2 services.

## Network Ports
- `50051` — master gRPC endpoint.
- `50052` — slave gRPC endpoint.
- `50053` — master CA distribution.
- `50054` — slave CA distribution.

If a slave lives outside the internal network, ensure these ports are reachable through firewalls/VPNs and adjust NAT rules as needed.
