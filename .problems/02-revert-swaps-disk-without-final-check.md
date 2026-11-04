# 02. Revert swaps the disk without re-checking that the VM is still off

| | |
| --- | --- |
| **Severity** | High (data loss, narrow trigger) |
| **Feature** | VM backup revert |
| **Modules** | `master` |
| **Evidence** | Reproduced by `TestAuditRenameRechecksVMState` |
| **User decision** | None |

## Summary

`runRevert` confirms the VM is off once, before the safety backup. It then copies for what can be hours, and renames over the disk without looking again. If the VM started in that window, its disk is replaced while it runs.

## What is impacted

- **The VM's data:** everything written after the safety backup is lost. That data is in neither the safety backup nor the restored disk.
- **The running guest:** qemu keeps writing to a file that no longer exists at that path. The disk is on NFS and the slave is a different NFS client from the master, so expect I/O errors or a stale file handle (this is expected from how NFS works; it was not observed).
- **What the user is told:** the operation ends as `complete` with a success notification, even though the guest is damaged.

## How it is impacted

1. The user reverts a VM. The check at `vm_backup_revert.go:246-259` confirms SHUTOFF.
2. The safety backup and the copy run. On a large disk this takes hours.
3. The VM gets started during that time.
4. `os.Rename` at `vm_backup_revert.go:288` runs with no state check.
5. The row reaches `complete` and the success notification goes out.

**How the VM can start despite the lock:**

- **`StartVM` race.** `StartVM` checks the lock at `master/services/virsh.go:590`, then makes several gRPC calls before starting at `:610`. A revert reserving the VM between those two lines is not seen. If the VM was off, the revert's state check can pass in milliseconds, before the start lands.
- **Migrate, clone and move-disk** are unguarded. That gap was accepted on 2026-11-03, but only on the assumption nobody runs them mid-revert.
- **Anything outside HyperHive,** such as `virsh start` on the host.

`AGENTS.md` requires destructive operations to check preconditions and fail closed, and this rename is the destructive step.

## Root cause

The state check is not next to the operation it protects:

```go
// vm_backup_revert.go, simplified
vm, err := revertLookupVM(vmName)     // :247 the only check
safety, err := revertSafetyBackup(...) // :266 long
err = revertCopyFile(...)              // :282 long
os.Rename(temp, op.DestinationPath)    // :288 no check
```

## Evidence

The proof sets the VM to RUNNING when the copy finishes:

```
disk="known good" phase="complete"
disk was swapped under a running VM: now "known good"
```

## How to start fixing

**Step 1, the main fix (small).** Move the check at lines 246-259 into a helper and call it again immediately before `os.Rename`:

```go
// verifyRevertTarget confirms the VM is off and still on the same disk.
func verifyRevertTarget(vmName string, op *db.DiskOperation) (*grpcVirsh.Vm, error) { ... }
```

If it fails, call `removeRevertTemp(temp)` and `fail("swap check", err)`, then return. The failure message already says the disk was not changed and names the safety backup. A failed lookup, for example a slave that is down, must also abort.

**Step 2, compare the copied size.** `copyFileWithProgress` stops at EOF and never compares byte counts. Before the rename, check that `os.Stat(temp).Size()` equals `op.Length`. It costs one stat and refuses a short copy.

**Step 3, close the `StartVM` race.** Step 1 leaves only a window of milliseconds, but this closes the cause:

- **Recommended:** a per-VM lock held by `StartVM` across its check and start, and by `RevertBackup` across `BeginDiskOperation`.
- **Cheaper:** call `refuseDuringRevert` a second time right before `virsh.StartVm` at `:610`. This narrows the race without closing it.

## Traps

- **Do not reuse `runVmMutex`** (`master/services/virsh.go:1648`) in `StartVM`. Autostart holds it through long sleeps, so API starts would block for minutes.
- **`RevertBackup` picks `running` before taking the lock** (`:197`). If you add locking, keep the post-lock re-check, because that is what catches a VM started in between.
- **Do not widen the accepted gap without asking.** The re-check catches a migrated VM only if it is running or its disk path changed. Guarding migrate, clone and move-disk is a separate decision for the user.
- **Keep the failure text accurate.** `fail()` says the disk was not changed, which stays true only if the check runs before the rename.
- **Keep the order of `removeRevertTemp` and `fail`** used by the existing failure paths, so `assertNoTemp` keeps passing.

## Files to touch

- `master/services/vm_backup_revert.go`: `runRevert`, a new `verifyRevertTarget`
- `master/services/virsh.go`: `StartVM`, for step 3
- `master/services/vm_backup_revert_test.go`: tests

## Done when

- `TestAuditRenameRechecksVMState` passes: the disk stays `current disk`, the row is `aborted`, no temp file remains, and the safety backup exists.
- New tests cover: disk path changed during the copy; lookup error during the final check; copied size mismatch.
- All existing `TestRevert*` tests pass.
- `docs/vm-backup-revert.md` lists the final check in its phase table.
