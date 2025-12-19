# HyperHive
HyperHive is our opinionated virtualization fabric for small home "datacenters". A master node orchestrates several Fedora-based “slaves” that expose KVM and other tech via gRPC, while a single-page UI and automation toolkit keep day-to-day operations predictable. The current release targets Fedora hosts configured with an English locale; other distributions are intentionally out of scope.

## Why we built it
- Consolidate virtualization, bare-metal storage, and overlay networking into one workflow.
- Automate repetitive maintenance (backups, SMART tests, auto-start policies) so operators can focus on workloads.
- Offer remote access to isolated networks (`512rede`) without poking extra holes in firewalls.
- Provide enough observability (logs, stream telemetry, push notifications) to run multi-site labs without babysitting every box.

## Core capabilities
**Compute orchestration**
- Cluster-aware VM lifecycle management on libvirt: create, resize, migrate, clone, import/export, and restart workloads directly from the master API.
- Scheduled backups with progress reporting, integrity checks, and notifications when something goes wrong.

**Storage & data protection**
- Btrfs RAID automation (create/mount/remove, compression profiles, degraded-mount detection) tied to the HyperHive DB so shares stay in sync with reality.
- Managed NFS exports per host; VM disks live on shared storage, which lets migrations stay fast and stateless.
- SmartDisk service drives periodic SMART tests, surface scans, and zeroing procedures with a central scheduler.

**Networking & remote access**
- WireGuard controller that distributes keys, enforces overlay addressing, and toggles iptables/sysctl so nodes talk securely across sites.
- Optional “Extra” topology where the master also fronts the `512rede` network, bridging traffic only when policies allow it.
- Built-in helpers for k3s registration and Docker auto-start, allowing mixed VM + container workloads on the same hardware.

**Operations experience**
- A Go control plane that exposes gRPC endpoints to the SPA, REST API, PM2-managed daemons, and notification workers.
- Logs, stream telemetry, and GoAccess dashboards shipped with sensible defaults so incidents stay debuggable.
- WebSocket notifications plus push opt-in pages make it easy to fan out alerts (backup status, disk health, migrations, etc.).

## Architecture in a nutshell
- **Master** – Runs the Go control plane, Web UI, WebSocket server, notification workers, and the “local slave”. It maintains the SQLite DB (`master/db`) and coordinates every remote action.
- **Slave agents** – Go binaries that connect back to the master via WireGuard + gRPC, exposing virsh, btrfs, SMART, Docker, and k3s operations.
- **Shared services** – NFS shares back VM disks, while WireGuard keeps management traffic encrypted. Optional components (novnc, SPA builds, logs) live under `master/`.

## Getting started
Make sure the management and workload networks keep the required ports open, especially when slaves sit outside the LAN. Fedora hosts (English locale) are mandatory for now. Once you clone the repo, pick the path that matches your topology:

- [Solo Install](SOLO-INSTALL.md) — single node hosts every role.
- [Normal Install](NORMAL-INSTALL.md) — one master plus several slaves on the same network.
- [Extra Install](EXTRA-INSTALL.md) — master also serves the isolated `512rede` network.

After completing the install flow, continue with [Runtime Configuration & Autostart](RUNTIME-SETUP.md) to populate `.env`, build the Go binaries, pair the slaves with the master, and register the PM2 services that keep everything alive across reboots.

We keep day-to-day operational notes close to the codebase—browse the `scripts/`, `master/services/`, and `slave/` folders whenever you add new hardware or need to extend the stack. Contributions are welcome; aligning on these high-level goals first helps ensure every change pushes HyperHive toward a more resilient lab platform.
