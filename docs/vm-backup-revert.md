# VM backup timeline and revert in place

A VM backup is a byte copy of the VM's primary disk on an NFS share (`virsh_backups` row plus `backup-<uuid>/<vm>.qcow2`). This feature adds:

- a per-VM backup list (`GET /virsh/backups?vm_name=`), shown as a timeline in the VM details modal of the web app;
- **revert in place**: put a VM back on one of its backups without creating a new VM.

There are no libvirt snapshots, no XML changes and no new tables. Revert is not exposed through MCP.

## API

| Route | Behavior |
| --- | --- |
| `GET /virsh/backups?vm_name=<vm>` | Same response as `GET /virsh/backups` (`[{db_res, live, size}]`), filtered to the backups whose name is `<vm>`, newest first. Without `vm_name` it lists everything. |
| `POST /virsh/revertBackup/{backup_id}` | Body `{"start_after": bool}` (optional; default `false`). Validates, takes the lock and answers **202** `{"status":"started","vm_name":…,"backup_id":…}`. The result arrives later as a notification. |

Errors from `revertBackup`: **400** invalid request (no disk path, disk is not a regular file, VM disk share not resolvable); **404** backup row, backup file, backup share or VM missing; **409** not enough free space, VM paused or suspended, or another operation (revert, disk move, attach/detach…) holds the VM.

## Flow

Synchronous checks, before anything is locked:

1. The backup row and its file exist; a VM named like the backup exists.
2. The VM's disk path is a regular file and resolves to a registered NFS share.
3. Free space (`statfs`): the backup share must hold the current disk size (safety backup) and the disk's folder must hold the backup size (temp copy). When both are on the same share it must hold the sum.
4. A `vm_disk_operations` row is reserved: kind `revert-vm`, name `vm-revert:<vm>`, `vm_name` `<vm>`. The backup ID is marked in use.

Then a background task runs (7-day bound), recording each phase in the row:

| Phase | Step | Failure leaves |
| --- | --- | --- |
| `stopping` | Only if the VM is not SHUTOFF: graceful ACPI shutdown, then poll every 5 s for up to 5 minutes. Never forces it off. | Disk untouched. The user must force-stop the VM and retry. |
| (check) | Re-read the VM: it must be SHUTOFF and still use the same disk path. | Disk untouched. |
| `safety-backup` | Normal (manual, not automatic) backup of the current disk onto the **backup's** NFS share, without freezing. | Disk untouched; a partial `backup-*` folder may remain. |
| `copying` | Copy the backup to `<disk dir>/.<disk name>.revert-<operation id>.tmp` (fsync, qemu owner). Progress is sent as websocket type `RevertVM`. | Temp file removed, disk untouched. The safety backup is kept. |
| `swapped` | `rename(temp, disk)` (atomic on the same file system), then fsync of the folder. | If the rename fails: temp removed, disk untouched. |
| `complete` | Lock released, success notification naming the safety backup ID. If `start_after` was set the VM is started; a start failure is a separate warning, the revert still succeeded. | — |

Every failure before the rename aborts the row (`phase = aborted`), so a clean failure never leaves the VM locked, and the VM is never started. Notifications use event `operation.backup_revert`.

## Lock scope

While a `revert-vm` row is active:

- refused: `StartVM` (API and MCP `start_vm`, HTTP 409), autostart, manual and automatic backups of the VM (the automatic loop skips the VM that round, with no pruning), deleting the source backup or the safety backup (HTTP 409), a second revert;
- refused through the existing unique index on `vm_name`: VM deletion, disk attach/detach and other disk operations that reserve the VM;
- **not guarded: migrate, clone and move-disk.** The VM is SHUTOFF mid-revert, so their own state checks pass. This gap was accepted on 2026-11-03; do not run them on a VM while it is being reverted.

## Master restart

`RecoverInterruptedReverts` runs at startup, before the automatic-backup loop and the API. For every active `revert-vm` row it:

- removes the temp file if it still exists (the original disk is in place);
- reports `swapped` as "reverted"; reports `copying` with no temp file as uncertain (original or backup), so the user checks the VM before starting it;
- aborts the row and sends a warning notification naming the safety backup.

A stuck row can also be released with the disk-operations reconcile action `abort`, which now accepts kind `revert-vm`.

## Known limits

- Backups are keyed by VM name: a VM deleted and recreated under the same name shows, and can be reverted to, the old VM's backups.
- Backups are plain byte copies. A disk with a qcow2 backing file is not handled (HyperHive-created VMs have none).
- Only the primary disk is reverted; attached data disks and domain XML stay as they are.

## Source

- `master/services/vm_backup_revert.go` — validation, background flow, lock helpers, restart recovery.
- `master/services/virshBackups.go` — `backupVMDisk`, `copyFileWithProgress`, guards in `BackupVM`, `DeleteBackup` and `LoopAutomaticBaks`.
- `master/db/virshBackups.go` (`GetVirshBackupsByName`), `master/db/vm_disk_transfer.go` (`GetActiveVMOperation`, `ListActiveDiskOperationsByKind`).
- `master/api/virsh.go` — `getAllBackups` filter, `revertBackup` handler.
- Frontend (`HyperHiveFront`): `components/modals/VmBackupTimeline.tsx`, `components/modals/RevertBackupDialog.tsx`, `app/vms.tsx`, `utils/vmProgress.ts`.
