# GPU host mode: Grill / Discovery Notes
Date: 2026-11-15 · Goal: add a third GPU mode "host" (besides none / VM), enforce the chosen mode when HyperHive starts, support NVIDIA and AMD, driver install always by the user.

## Context found in code (no need to ask)
- Today a GPU's "VM attachment" lives only in libvirt domain XML (`<hostdev managed='yes'>`, all IOMMU-group functions). No master DB row.
- `slave/pci/pci.go`: `AttachGPUToVM` (bind vfio-pci + hostdev), `DetachGPUFromVM` / `ReturnGPUToHost` (reattach then force driver to "none").
- Slave startup step [10/16] `DetachAllGPUs` is commented out ("skipped auto detach") → today nothing is enforced at start.
- Prototype `gpu-mode.sh` (NVIDIA only, tested on marques2673sv): host/vm/off switching via `driver_override` + unbind + `drivers_probe`, refuses (never kills) when VM or host processes hold the GPU.
- Known traps from handoff: `managed='yes'` lets libvirt rebind behind our back; unknown process loads nvidia ~70 s after boot; `driver_override` not persistent across reboots.

## Summary / key decisions
- Desired mode per (machine, GPU slot) stored in **master SQLite** (mode + vm name). Master pushes/reconciles when a slave connects.
- Upgrade: GPUs without a row adopt current state (XML hostdev → vm:<VM>, else none).
- Host driver chosen by PCI vendor, no config: 10de→nvidia, 1002→amdgpu, 8086→i915/xe. Explicit modprobe (bypasses blacklist). Missing module → clear error telling the user to install the driver. Nouveau never used.
- Never kill: leaving host mode refuses while host processes hold the GPU (/dev/nvidia*, the card's /dev/dri nodes); error names pid(command). Only auto-action: stop nvidia-persistenced.
- VM hostdevs become managed='no'. HH owns the binding; vm mode keeps the group on vfio-pci even while the VM is off. Existing managed='yes' hostdevs rewritten at reconcile.
- Reconcile failure: leave GPU as is, keep DB unchanged, log + HH notification, UI shows desired vs actual + error. Slave startup continues.
- Stickiness via driver_override on every function in the IOMMU group: vfio-pci (vm), vendor driver (host), dummy "none" (none). Reconcile only on slave connect; no timer. gpu-unload.timer + ~/gpu-mode.sh can be removed from marques2673sv afterwards.
- API: POST /pci/mode/{machine} {gpu_ref, mode: none|host|vm, vm_name}. Old attach/detach/return kept as wrappers. GET /pci/host adds desired_mode, desired_vm, actual_mode, last_error.
- Leaving vm mode removes the hostdev from that VM's XML (VM must be shut off, else refuse). Re-entering vm mode re-adds it.
- Reconcile: DB wins for VM XML, but only on shut-off VMs (remove stray hostdevs, add missing one to desired VM). Running conflicting VM → don't touch, warn. Upgrade with GPU in 2+ XMLs → adopt none + warn. Desired VM missing (deleted/renamed) → warn, leave (decided by default, Q6 rule).
- Modules: unbind per device only; unload the vendor module only if no other device is bound to it (best effort, failure = warning).
- Boot/console GPU (boot_vga=1): badge + confirm warning in UI; all modes allowed.
- No MCP changes in this feature.
- Concurrency (decided by default): slave serializes GPU mode changes with a mutex; with managed='no' a VM start racing a switch just fails in libvirt (GPU not on vfio), which is safe.

## Q&A log

### Q1 — Where desired state lives
- Asked: master DB vs slave local file vs libvirt XML + file.
- Captured: **Master DB.** New table per (machine, GPU slot): mode (none/host/vm) + vm name. Master reconciles the slave when it connects. SQLite = intended, host = actual.
- Flags: none

### Q2 — GPUs with no DB row (upgrade)
- Asked: adopt current state vs unmanaged vs always none.
- Captured: **Adopt current state.** GPU found as `<hostdev>` in a VM's XML → row `vm:<that VM>`; otherwise → `none`. Write the row, then enforce from then on. No surprise changes on upgrade.
- Flags: what if the GPU is in 2+ VMs' XML (possible today?) → decide in a later question.

### Q3 — Host-mode driver selection
- Asked: vendor map vs vendor + override vs modalias.
- Captured: **By vendor, no config.** nvidia / amdgpu / i915|xe. Fail with "install the driver yourself" if module missing for `uname -r`. Never nouveau. HH never installs drivers.
- Flags: Intel included only because it's free; untested.

### Q4 — Leaving host mode while GPU busy
- Asked: refuse vs refuse+force vs kill.
- Captured: **Refuse, name users.** Same as gpu-mode.sh. Holders = /dev/nvidia* or the card's /dev/dri/renderD*/card*. Auto-stop nvidia-persistenced only. No force option.
- Flags: none

### Q5 — libvirt managed='yes'
- Asked: switch to managed='no' vs keep.
- Captured: **managed='no'.** vfio-pci held in vm mode whether the VM runs or not. Reconcile rewrites existing hostdevs to managed='no' (persistent XML; running VM picks it up next boot).
- Flags: none

### Q6 — Reconcile can't apply desired mode
- Asked: leave+warn vs fall back to none vs periodic retry.
- Captured: **Leave it, warn.** No DB change, notification with reason, desired vs actual visible in UI. Non-fatal for slave startup.
- Flags: none

### Q7 — Drift after start
- Asked: driver_override pinning vs + periodic check.
- Captured: **driver_override pins it.** No periodic check.
- Flags: driver_override is lost on reboot; between boot and slave start a driver may bind — reconcile fixes it (subject to Q4 refuse rule).

### Q8 — API shape
- Asked: one set-mode endpoint vs extra host-attach endpoint.
- Captured: **One set-mode endpoint** + old endpoints as wrappers (attach→vm, detach/return→none). Inventory gains desired vs actual + last error.
- Flags: frontend `app/gpus.tsx` + `services/pci.ts` must move to the new endpoint.

### Q9 — Hostdev in XML when leaving vm mode
- Asked: remove vs keep.
- Captured: **Remove hostdev.** Running VM → refuse. Back to vm → re-add.
- Flags: none

### Q10 — XML disagrees with DB at reconcile
- Asked: DB wins (VMs off only) vs only warn.
- Captured: **DB wins, only if VMs off.** Running conflicting VM → warn. Ambiguous upgrade (2+ VMs) → none + warn.
- Flags: none

### Q11 — Module unloading
- Asked: per-device unbind vs always unload.
- Captured: **Unbind per device only**, opportunistic unload when unused. Safe for multi-GPU / AMD iGPU hosts.
- Flags: none

### Q12 — Boot/console GPU
- Asked: warn vs block vs ignore.
- Captured: **Warn only.** Badge + confirmation dialog; no blocking.
- Flags: none

### Q13 — MCP
- Captured: **None for now.** Possible follow-up: read-only list tool.
- Flags: none

### Q14 — Completeness
- Captured: done. Keep `/etc/modprobe.d/gpu-mode.conf` blacklist on marques2673sv (blocks autoload before slave starts). User removes gpu-unload.timer/service and ~/gpu-mode.sh by hand after testing.

## Open flags (pending input)
- AMD GPU hardware for testing -> user (only NVIDIA available)

## Build plan

Order matters: contract → slave → master → frontend → docs.

1. **Contract** (`api/proto/pci.proto`, regenerate with `make setup`)
   - `enum GPUMode { GPU_MODE_NONE = 0; GPU_MODE_HOST = 1; GPU_MODE_VM = 2; }`
   - `rpc SetGPUMode(SetGPUModeRequest{gpu_ref, mode, vm_name}) returns (OkResponse)` — idempotent; used for both user switches and reconcile.
   - `HostGPU` gains `actual_mode`, `boot_vga`, `host_driver` (vendor map result). New fields only, no renumbering.
2. **Slave** (`slave/pci/gpu_mode.go` + test, expose in `setupgrpc.go`)
   - Package mutex around every mode change.
   - Group = all functions in the GPU's IOMMU group (existing `relatedPCIDevicesForGPU`).
   - Holders: `/dev/vfio/<group>` + VMs with matching hostdev (VM users); `/dev/nvidia*` (NVIDIA) and the card's `/sys/.../drm/{card*,renderD*}` → `/dev/dri/...` (host users). Found by scanning `/proc/*/fd`, no `fuser` dependency.
   - `none`: refuse on VM/host users (VM must be off); remove hostdevs from shut-off VMs; per function: override=`none`, unbind.
   - `host`: refuse on VM users; remove hostdevs; vendor map (10de→nvidia+nvidia_uvm, 1002→amdgpu, 8086→i915); `.0` override=driver, unbind, `modprobe <driver>` (clear error "install the driver yourself" if missing for `uname -r`), `drivers_probe`; other functions override cleared + probe (native audio/USB). Verify `.0` bound.
   - `vm`: target VM must exist and be shut off (except already-vm-and-running = no-op); refuse on host users (auto-stop nvidia-persistenced); `modprobe vfio-pci`; all functions override=vfio-pci, unbind, probe, verify; remove stray hostdevs from other shut-off VMs; ensure hostdev on target VM with `managed='no'` (rewrite managed='yes').
   - After leaving host: `modprobe -r` vendor module only if `/sys/module/<m>/drivers/pci:<m>/` has no bound devices; failure = warning.
   - Old `AttachGPUToVM` / `DetachGPUFromVM` / `ReturnGPUToHost` RPCs delegate to the same code (vm / none / none).
   - Slave startup step [10/16] stays a no-op (master drives reconcile).
3. **Master**
   - `db/pci.go`: table `gpu_modes(machine_name, gpu_address, mode, vm_name, last_error, updated_at, PK(machine,address))`.
   - `services/pci.go`: `SetGPUMode` (call slave; on success upsert row, clear error); `ReconcileGPUs(machine)`: list GPUs; no row → adopt (single VM in `attached_to_vms` → vm, 0 → none, 2+ → none + warn); apply via `SetGPUMode`; on failure store `last_error` + notification, leave DB mode.
   - `main.go` `newSlave`: call `ReconcileGPUs` **before** `scheduleDelayedSlaveStartup` (VM autostart needs vfio in place).
   - `api/pci.go`: `POST /pci/mode/{machine}`; old attach/detach/return wrap it; `GET /pci/host` merges desired_mode/desired_vm/last_error.
4. **Frontend** (`HyperHiveFront`: `app/gpus.tsx`, `services/pci.ts`, `types/pci.ts`): 3-way mode picker (None / Host / VM + VM select), desired vs actual + last error, boot-GPU badge + confirm.
5. **Verify**: `go build/test -tags libvirt_dlopen` in api, slave, master; unit tests for vendor map, holder parsing, adoption rule. Live test on marques2673sv (NVIDIA) only with user's go-ahead: none↔host↔vm, restart slave → mode re-applied, refuse while CUDA process / VM running. AMD: code path only, untested.
6. **Docs**: `docs/` GPU page + SilverBullet feature page (SilverBullet MCP currently down).

Not doing: MCP tools, periodic drift check, force/kill option, driver installation, per-GPU driver override.
