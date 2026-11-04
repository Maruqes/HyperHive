# 04. Revert restart recovery runs before NFS is mounted

| | |
| --- | --- |
| **Severity** | Medium |
| **Feature** | VM backup revert |
| **Modules** | `master` |
| **Evidence** | Reproduced by `TestAuditRecoveryBeforeMountRemovesTemp`, with the mount simulated by moving the directory. The startup hang is **not** reproduced. |
| **User decision** | None |

## Summary

`RecoverInterruptedReverts` runs during startup, before the NFS shares it inspects are mounted. After a host reboot it cannot see the temp file, so it reports the wrong outcome, releases the lock, and leaves a disk-sized hidden file on the share forever.

## What is impacted

- **Storage on the share:** a hidden `.<disk>.revert-<uuid>.tmp` stays in the VM folder, up to the size of the VM's disk. The UI does not show it and nothing else cleans it up. Repeated reverts can then fail their free-space check.
- **The recovery message:** it says the disk "is either the original or the backup; check the VM", when the original is known to be in place, because the temp file still exists.
- **Master startup:** recovery runs on the main thread before the API starts, and shares are mounted `hard` (`slave/nfs/nfs.go:50`). If a share is mounted but its server is down, `os.Stat` can block forever and the API never comes up. This is reasoned from the code, not reproduced.

It only happens when a revert was interrupted. A plain master restart with mounts still in place works correctly.

## How it is impacted

1. A revert is in the `copying` phase, with a partial temp file on the share.
2. The host crashes or loses power.
3. On boot, `main.go:521` starts the gRPC listener. Slaves register, and mounts happen later and asynchronously in `newSlave` (`main.go:133-143`), with up to 20 minutes allowed.
4. `main.go:530` calls `RecoverInterruptedReverts` straight away.
5. `recoverRevert` runs `os.Stat(temp)` at `vm_backup_revert.go:379`. The share is not mounted, so the result is ENOENT.
6. The switch cannot tell "no temp file" from "share not mounted" and takes the `copying` branch.
7. The row is aborted, so the lock is gone and the temp file will never be looked at again.
8. The share mounts. The temp file is back and orphaned.

## Root cause

Two assumptions in `recoverRevert`:

- Startup order is assumed to mean the share is available.
- Any `os.Stat` error is treated as "the file does not exist", including `ESTALE` and `EIO`.

```go
_, statErr := os.Stat(temp)
switch {
case op.Phase == "swapped": ...
case statErr == nil:        // only this path removes the temp file
    removeRevertTemp(temp)
case op.Phase == "copying": // reached when the share is simply not mounted
```

## Evidence

The proof hides the share, runs `recoverRevert`, then restores the share:

```
message: The master restarted during the revert of guest to backup 1 (phase copying).
         The disk is either the original or the backup; check the VM before starting it.
partial temp file leaked after recovery
```

## How to start fixing

**Use the disk itself as the mount probe.** The rename is atomic, so `op.DestinationPath` always exists while the share is mounted. If `os.Stat(op.DestinationPath)` says the file is missing, the share is not ready, and nothing can be concluded yet.

1. Split recovery in two: `recoverReady(op) bool`, which probes the disk path, and the existing `recoverRevert`, which runs only when the probe succeeds.
2. If the share is not ready, **leave the row active.** The VM stays locked, which is the safe state.
3. Run recovery in a background goroutine with retries, for example every 30 s for 30 minutes, so `main` never blocks on NFS.
4. Bound each probe with a timeout: run `os.Stat` in a goroutine and stop waiting after a few seconds. A stat on a dead `hard` mount cannot be cancelled, so that goroutine may leak until the mount returns. One per operation is acceptable.
5. When retries run out, send one notification saying the revert could not be verified and how to release it with reconcile `abort`. Keep the row.
6. In `recoverRevert`, use `os.IsNotExist(statErr)` for "absent" and treat any other error as "not ready".
7. Remove the temp file in the `abort` path of `Reconcile` (`master/services/vm_disk_transfer.go`). Today an operator abort leaves it behind.

## Traps

- **Autostart treats the lock as success and never retries.** In `startAutoStartVms`, `runVm` returns `true` when `refuseDuringRevert` refuses (`master/services/virsh.go:1672`). If recovery holds the row longer than before, a VM with autostart can be skipped for that whole boot. Delayed startup waits `DELAYED_STARTUP_WAIT` (default 15 minutes), so either finish recovery first or return `false` so the existing retry loop tries again.
- **Keep the startup comment true.** `main.go` says recovery happens before backups or starts run. With a background goroutine that is no longer about ordering. The lock row is what protects the VM, so update the comment and check that `LoopAutomaticBaks` still refuses (it uses `refuseDuringRevert`).
- **Keep a synchronous core for tests.** `TestRecoverInterruptedRevertRemovesTempAndAbortsLock` (`master/services/vm_backup_revert_test.go:325`) calls `RecoverInterruptedReverts` and expects the result straight away. Test a synchronous inner function instead of sleeping.
- **That test's last assertion must still pass:** disk visible and no temp file in `copying` still returns "either the original or the backup". This is the real uncertain case, after a rename that was not recorded.
- **The proof needs rewriting.** It calls `recoverRevert` directly and expects the temp to be gone. After the fix, the first pass should leave the row active, and a pass after the "mount" should remove the temp.
- **A VM deleted during the outage** also leaves the disk missing, so the probe alone cannot tell it from an unmounted share. Timing out with the notification in step 5 covers this.
- **The master reads shares through its own host's mount.** If you prefer hooking recovery into `newSlave` after `EnsureNFSReadyForMachine`, it must be the master host's slave, not any slave. `isMasterHost` in `master/services/power_schedule.go:207` shows how to detect that.

## Files to touch

- `master/services/vm_backup_revert.go`: `RecoverInterruptedReverts`, `recoverRevert`
- `master/services/vm_disk_transfer.go`: `Reconcile`
- `master/services/virsh.go`: autostart, if you take the retry option
- `master/main.go`: the call and its comment
- `master/services/vm_backup_revert_test.go`

## Done when

- A share that is not mounted leaves the row active and the temp file untouched.
- A later pass with the share back removes the temp file and reports that the original disk is in place.
- `main` never waits on NFS for recovery.
- New tests cover: not ready then ready; retries exhausted; `ESTALE` is not "absent"; `abort` removes the temp file.
- `TestRecoverInterruptedRevertRemovesTempAndAbortsLock` passes.
- `docs/vm-backup-revert.md` ("Master restart") is updated.
