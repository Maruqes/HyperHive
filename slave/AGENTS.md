# AGENTS.md — slave

Instructions for AI coding agents working in the slave module. Read the root `AGENTS.md` first for the system model, feature workflow and SilverBullet rules.

## What the binary does

A privileged host agent (runs as root). Each machine, including the master host, runs one. Bootstrap in `main.go`:

1. Load env (`env512`: `MASTER_IP`, `SLAVE_IP`, `MACHINE_NAME`, …) and the logger.
2. Stop and disable firewalld; ensure Docker is installed (runtime for NPM, HH Support and celld; HyperHive does not manage Docker resources).
3. Ensure virt-xml and tuned-adm. If `MASTER_IP == SLAVE_IP`, install the k3s server.
4. Disable swap, install Git, apply dirty-page ratios, set VNC port bounds (no automatic GPU detach).
5. Install NFS, `setupAll` (virtualization setup, inter-slave SSH keys), ensure the VirtIO ISO, set the host UUID source.
6. `--first` returns here. Otherwise `protocol.ConnectGRPC`: start the `:50052` server once, dial master `:50051`, forward logs, call `SetConnection`, then monitor the connection and ping the master.
7. Start Btrfs and SMART background monitors, then block.

**Losing the master connection can re-exec the process, which runs the whole bootstrap again.** Every bootstrap step must be idempotent: check before install, don't duplicate config lines, don't reset user state.

## Package map

Each domain package holds both the host logic and its gRPC service:

| Package | gRPC service | Area |
| --- | --- | --- |
| `protocol/` | `ClientService` | registration, connection, `listenGRPC` (registers every service) |
| `virsh/` | `SlaveVirshService` | VMs: libvirt XML, CPU pinning, hugepages, Hyper-V/KVM flags, machine type, VNC, snapshots, migration, SSH keys |
| `vm_disk/` | `VMDiskService` | qemu-img disks and disk transfers |
| `nfs/` | `NFSService` | NFS exports and mounts |
| `btrfs/` | `BtrFSService` | Btrfs pools and monitoring |
| `smartdisk/` | `SmartDiskService` | SMART data and tests |
| `pci/` | `SlavePCIService` | PCI/GPU passthrough |
| `info/` | `Info` | host telemetry |
| `our_k8s/` | `K8SService` | k3s |
| `celld/` | `CelldService` | celld fleet agents |
| `extra/` | `ExtraService` | updates, restart and shutdown, misc |
| `cpu_pinning/`, `pci-tui/`, `cmd/` | — | helpers and standalone tools |

## Where new code goes

- **Host logic**: plain Go functions in `<area>/<topic>.go` that take simple arguments and return typed structs and errors. No protobuf types here, so they can be unit-tested.
- **gRPC adapter**: in `<area>/setupgrpc.go` or `<topic>_grpc.go`. Methods on the area's service struct (which embeds `pb.Unimplemented…Server`) convert the request, call the host function and convert the response. Keep adapters thin.
- **New service**: register it in `protocol/listenGRPC`. A generated `Unimplemented` stub is not an implementation; check that the method actually exists.
- Shelling out: use `exec.CommandContext` with explicit arguments (no `sh -c` with interpolated input), a timeout, and include captured stderr in the error. Validate names and paths that come from RPCs (no `..`, no unexpected characters) before passing them to libvirt, qemu-img or the filesystem.
- Changing libvirt XML: read the current domain XML, modify it, then define it. Never rebuild the XML from scratch and lose devices or settings. Callers (master/MCP) rely on being able to save the prior XML first.
- Long operations: honor `ctx` cancellation, stream progress back (streaming RPC or `ExtraService` websocket messages), and clean up temp files on failure.
- Destructive host actions (delete disk, wipe, detach, unmount, firewall changes) must check state first and refuse when the target is in use.

## Rules

- gRPC between master and slave is insecure transport; don't add assumptions that it's encrypted or authenticated.
- Build: `go build -tags libvirt_dlopen ./...`; tests: `go test -tags libvirt_dlopen ./...`. Privileged Btrfs tests need a real host; don't run them unless asked.
- `make run` / `make first` run the agent **with sudo and change the host**. They are not build checks.
- Don't edit `logs512/`, `downloads/` or the `slave` binary.
- Update SilverBullet when a feature is done (root `AGENTS.md`).
