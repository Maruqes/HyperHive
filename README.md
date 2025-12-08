# HyperHive
Choose the right install guide, follow the runtime setup, and make sure required ports stay open—especially when slaves sit outside the LAN. HyperHive is currently only supported on Fedora hosts configured with an English locale.

## Installation Guides
- [Solo Install](SOLO-INSTALL.md) — single node hosts every role.
- [Normal Install](NORMAL-INSTALL.md) — one master plus several slaves on the same network.
- [Extra Install](EXTRA-INSTALL.md) — master also serves the isolated `512rede` network.

## After Installation
- [Runtime Configuration & Autostart](RUNTIME-SETUP.md) — configure `.env`, build binaries, and register PM2 services.
