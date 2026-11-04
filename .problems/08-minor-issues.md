# 08. Smaller issues

All low severity and found by source review. None has a proof test. Each is independent, so pick them up in any order.

## 8.1 `EditVM` is not blocked during a revert

- **What is impacted:** the disk being reverted, and the accuracy of the lock scope in `docs/vm-backup-revert.md`.
- **How:** `EditVM` (`master/services/virsh.go:894`) does not check the revert lock. The slave's `EditVm` (`slave/virsh/common.go:1284`) requires the VM to be SHUTOFF, and a revert guarantees that. It can then run `qemu-img resize` on the disk. The API reports success, but the rename discards the resize. If the resize lands during the safety backup, that backup is read while the file changes.
- **Why it is not covered:** the docs list migrate, clone and move-disk as the accepted gap. `EditVM` is not on that list, so nobody accepted it.
- **Start fixing:** call `refuseDuringRevert` at the top of `EditVM`, which has no `ctx` parameter today, so add one. Map `ErrRevertConflict` to 409 in the handler at `master/api/virsh.go:436`, as `startVM` does. Add it to `TestRevertLockRefusesStartBackupAndDelete`.
- **Also check** other routes that change a stopped VM's disk or domain XML. The final check from problem 02 catches a changed disk path, but not a resize.

## 8.2 A failed revert leaves a running VM off and does not say so

- **What is impacted:** the availability of the VM, and what the user is told.
- **How:** the revert shuts the VM down, then the safety backup or copy fails. `fail()` (`master/services/vm_backup_revert.go:218`) reports only that the disk was not changed. The VM stays off, even when `start_after` was set. The UI warning only covers the case where the VM refuses to stop.
- **Start fixing:** when `running` is true and the shutdown succeeded, add "The VM was shut down for the revert and is still off." to the message.
- **User decision:** whether to also restart it on failure when `start_after` is set. `docs/vm-backup-revert.md:39` says the VM is never started after a failure, so that was deliberate. Do not change the behaviour without asking.

## 8.3 A failed safety backup leaves a partial folder

- **What is impacted:** space on the backup share, and retries of the revert.
- **How:** `prepareBackup` creates `backup-<uuid>/` and `copyBackup` fills it. On failure nothing removes it. There is no database row, so the UI never shows it, and only the partial file's size is lost. A retry can then fail its free-space check with a 409 and no visible cause. The docs mention the leftover folder but not the retry problem.
- **Start fixing:** in `backupVMDisk` (`master/services/virshBackups.go`), when `copyBackup` fails, remove the file and call `removeBackupDirectory`, after `validateBackupDirectory`. The async path in `BackupVM` has the same leak, and that behaviour is older than this feature, so fix both or say why not.
- **Why removal is safe:** `copyBackup` returns an error only when no row was inserted, including when `InsertVirshBackup` itself fails after a complete copy. No row can point at the folder you remove.

## 8.4 `docs/disk-health.md:69` overstates alert retries

- **What is impacted:** anyone relying on the doc. A failed push is not retried.
- **How:** the doc says "If sending fails, the old state is kept so the next poll retries." `nots.Notify` returns an error only if saving history fails. Websocket and push delivery run in the background, and their errors are only logged (`master/nots/notify.go`). `evaluateAlert` then moves the state on, so a failed push is never retried. History and the websocket toast still have it.
- **Start fixing:** correct the sentence. The SilverBullet page is already fixed. `docs/disk-health.md` has uncommitted edits by someone else, on a different line, so edit only line 69. Retrying push failures would be a separate design change.

## 8.5 smartctl's "hard deadline" is not quite hard

- **What is impacted:** slave handler goroutines, when a drive is dying.
- **How:** `exec.CommandContext` kills smartctl at the deadline, but `CombinedOutput` then waits for the process to exit. A process stuck in uninterruptible I/O on a failing disk does not die from SIGKILL until the I/O returns. `cmd.WaitDelay` is not set, so `Wait` can block past the deadline. The master gives up at 60 s and continues, so polling survives, but each attempt can leave a stuck goroutine and process on the slave. **Reasoned from Go's documented behaviour, not reproduced.**
- **Start fixing:** set `cmd.WaitDelay = 5 * time.Second` on the four smartctl commands: `slave/smartdisk/smartdisk.go:133`, `slave/smartdisk/smartdisk.go:181`, `slave/smartdisk/smartdiskGRPC.go:267` and `slave/smartdisk/smartdiskGRPC.go:320`. The process may remain, but the handler returns.
- **Trap:** with `WaitDelay`, `Wait` can return `exec.ErrWaitDelay` even after a clean exit if a child holds the output pipe. The existing `ctx.Err()` checks already run first, so confirm that path.

## 8.6 The Backups modal shows a stale VM state (HyperHiveFront)

- **What is impacted:** the revert dialog's shutdown warning and the default of "Start VM after revert".
- **How:** `backupsVm` (`app/vms.tsx:318`) stores a copy of the VM when the modal opens (`:2681`, `:4069`, `:4436`), and `vmRunning` comes from that copy (`:2717`). `vms` refreshes in the background but the copy does not. If the VM stops or starts while the modal is open, the warning and the default are wrong. The backend is unaffected, because it re-reads the real state.
- **Start fixing:** store only the VM's name, for example `backupsVmName`, and derive the object from `vms` on each render. Handle the VM disappearing from the list.
- **Trap:** `RevertBackupDialog` resets on `[backup?.id, vmRunning]`. Once `vmRunning` is live, a state change while the dialog is open clears the typed confirmation. Depend on `backup?.id` only, and keep the rest as is.

## Cleanup after the uncommitted frontend work lands

`scripts/patch-react-native-svg.mjs` says it runs from a `postinstall` hook, but `package.json` has no such hook, and the local `node_modules` file is unpatched, so it has never run.

The uncommitted `metro.config.js` change maps `victory-native` to `victory` on web, which removes the need for the patch. Once that work is committed, delete the script. If it is dropped instead, add the `postinstall` hook. Do not act on this while those changes are uncommitted.
