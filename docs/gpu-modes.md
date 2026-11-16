# GPU modes

Every GPU of a machine is in exactly one mode:

| Mode | Driver on the GPU | Other functions of its IOMMU group | VM definitions |
| --- | --- | --- | --- |
| `none` | no driver | no driver | GPU removed from every VM |
| `host` | vendor driver | their own drivers (audio, USB) | GPU removed from every VM |
| `vm` | `vfio-pci` | `vfio-pci` | `managed='no'` hostdevs on one VM |

HyperHive owns the driver binding. It pins every function with `driver_override`, so neither libvirt nor a late module load rebinds the GPU. In `vm` mode the group stays on `vfio-pci` even while the VM is off.

## API

`POST /pci/mode/{machine_name}` with `{"gpu_ref": "0000:01:00.0", "mode": "none|host|vm", "vm_name": "..."}` (`vm_name` only for `vm`). The call returns when the switch is done.

- `400` invalid mode or address, missing `vm_name`.
- `404` machine not connected, GPU or VM not found.
- `409` the GPU is in use; the message names the running VM or the host processes (`pid(command)`).
- `500` anything else, e.g. the host driver is not installed.

`POST /pci/attach`, `/pci/detach` and `/pci/return` still work: attach is `vm` mode, the other two are `none` mode.

`GET /pci/host/{machine_name}` adds per GPU: `actual_mode`, `desired_mode`, `desired_vm`, `last_error`, `boot_vga`, `host_driver`.

## Rules

- **Nothing is ever killed.** A switch is refused while a running VM has the GPU or a host process has `/dev/nvidia*` or the card's `/dev/dri` nodes open. The only thing stopped automatically is `nvidia-persistenced`; it is not started again.
- **Host driver** is chosen by PCI vendor: NVIDIA `nvidia`, AMD `amdgpu`, Intel `i915` then `xe`. `nouveau` and `radeon` are never used. HyperHive loads the module with `modprobe` (a blacklist does not stop that) but never installs drivers; a missing module is an error asking you to install it.
- **VM mode** needs the VM shut down, unless it already has the GPU. The GPU is removed from any other shut-off VM. Existing `managed='yes'` hostdevs are rewritten to `managed='no'` in the stored definition; a running VM uses it from its next boot.
- **Leaving a vendor driver** unloads its module only when no other device is bound to it. A failed unload is a warning.
- **Boot GPU** (`boot_vga`): every mode is allowed; the frontend asks for confirmation because the host console can go dark.

## Stored mode and startup

The wanted mode lives in the master database (`gpu_modes`, per machine and GPU address). The driver binding and VM definitions are the actual state.

Each time a slave connects, before VM autostart, the master applies the stored mode of each GPU. `driver_override` does not survive a reboot, so this is what makes a mode stick. There is no periodic check.

- A GPU without a row adopts its current state: in one VM's definition → `vm` for that VM; bound to a host driver → `host`; otherwise `none`. A GPU defined in several VMs becomes `none` with a warning.
- If a mode cannot be applied (VM running, host process using the GPU, VM deleted, driver missing) the GPU is left as it is, the stored mode is kept, `last_error` is saved and a notification is sent. Slave startup continues.

## Failure states

A switch runs checks, then drivers, then VM definitions, with no rollback. A failure after the checks can leave the group without a driver, or bound correctly with a VM definition not yet updated. Requesting the mode again repairs both.

If the host switched but the master could not save the row, the request fails and the next reconnect puts the GPU back in the previously stored mode.

## Limits

- Two NVIDIA GPUs on one host share `/dev/nvidiactl`: a process using one blocks leaving host mode on the other.
- Two GPUs in the same IOMMU group cannot have different modes.
- A VM that was running with a `managed='yes'` hostdev when the mode was applied gives the GPU back to libvirt once, at its next shutdown; the next reconnect re-pins it.
- AMD and Intel paths are untested on hardware.
