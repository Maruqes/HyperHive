package services

import (
	"512SvMan/db"
	"512SvMan/extra"
	"512SvMan/nots"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"github.com/Maruqes/512SvMan/logger"
)

// Revert in place: safety backup of the current disk, copy of the chosen backup
// into a temp file next to the disk, then an atomic rename over the disk. The
// disk is untouched until the rename, so every earlier failure only aborts the
// lock. See docs/vm-backup-revert.md.

const revertOperationKind = "revert-vm"

var (
	ErrRevertInvalid  = errors.New("invalid revert request")
	ErrRevertNotFound = errors.New("revert target not found")
	ErrRevertConflict = errors.New("revert conflict")
)

var revertCopyProgress = copyProgress{Type: extraGrpc.WebSocketsMessageType_RevertVM, Label: "Revert progress"}

// Swapped by tests.
var (
	revertLookupVM        = func(name string) (*grpcVirsh.Vm, error) { return (&VirshService{}).GetVmByName(name) }
	revertShutdownVM      = func(name string) error { return (&VirshService{}).ShutdownVM(name) }
	revertStartVM         = func(ctx context.Context, name string) error { return (&VirshService{}).StartVM(ctx, name) }
	revertSafetyBackup    = backupVMDisk
	revertCopyFile        = copyFileWithProgress
	revertFreeBytes       = freeBytes
	revertPollInterval    = 5 * time.Second
	revertShutdownTimeout = 5 * time.Minute
)

// Backup IDs read or written by a running revert; DeleteBackup refuses them.
var revertBackups = struct {
	sync.Mutex
	inUse map[int]string
}{inUse: map[int]string{}}

func revertBackupInUse(id int) (string, bool) {
	revertBackups.Lock()
	defer revertBackups.Unlock()
	vmName, ok := revertBackups.inUse[id]
	return vmName, ok
}

func holdRevertBackup(vmName string, id int) {
	revertBackups.Lock()
	defer revertBackups.Unlock()
	revertBackups.inUse[id] = vmName
}

func releaseRevertBackups(ids ...int) {
	revertBackups.Lock()
	defer revertBackups.Unlock()
	for _, id := range ids {
		delete(revertBackups.inUse, id)
	}
}

// refuseDuringRevert fails while a revert holds vmName.
func refuseDuringRevert(ctx context.Context, vmName string) error {
	op, err := db.GetActiveVMOperation(ctx, vmName)
	if err != nil {
		return fmt.Errorf("check revert lock for %s: %w", vmName, err)
	}
	if op != nil && op.Kind == revertOperationKind {
		return fmt.Errorf("%w: revert in progress for %s", ErrRevertConflict, vmName)
	}
	return nil
}

func freeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	return st.Bavail * uint64(st.Bsize), nil
}

func requireFreeBytes(path string, need int64, what string) error {
	free, err := revertFreeBytes(path)
	if err != nil {
		return err
	}
	if need > 0 && free < uint64(need) {
		return fmt.Errorf("%w: not enough free space on %s for %s: need %d bytes, %d free", ErrRevertConflict, path, what, need, free)
	}
	return nil
}

func revertTempPath(op *db.DiskOperation) string {
	dir, base := filepath.Split(op.DestinationPath)
	return filepath.Join(dir, "."+base+".revert-"+op.ID+".tmp")
}

// RevertBackup validates the revert, reserves the VM and starts the work in the
// background. A nil error means "started"; the outcome arrives as a notification.
func (v *VirshService) RevertBackup(ctx context.Context, backupID int, startAfter bool) error {
	backup, err := db.GetVirshBackupById(ctx, backupID)
	if err != nil {
		return fmt.Errorf("load backup %d: %w", backupID, err)
	}
	if backup == nil {
		return fmt.Errorf("%w: backup %d", ErrRevertNotFound, backupID)
	}
	backupInfo, err := os.Stat(backup.Path)
	if err != nil || !backupInfo.Mode().IsRegular() {
		return fmt.Errorf("%w: backup file %s is missing", ErrRevertNotFound, backup.Path)
	}

	vm, err := revertLookupVM(backup.Name)
	if err != nil {
		return fmt.Errorf("look up vm %s: %w", backup.Name, err)
	}
	if vm == nil {
		return fmt.Errorf("%w: vm %s no longer exists", ErrRevertNotFound, backup.Name)
	}
	if vm.State == grpcVirsh.VmState_PAUSED || vm.State == grpcVirsh.VmState_PMSUSPENDED {
		return fmt.Errorf("%w: vm %s is suspended; resume or stop it first", ErrRevertConflict, vm.Name)
	}
	diskPath := filepath.Clean(strings.TrimSpace(vm.DiskPath))
	if strings.TrimSpace(vm.DiskPath) == "" {
		return fmt.Errorf("%w: vm %s has no disk path", ErrRevertInvalid, vm.Name)
	}
	if diskPath == filepath.Clean(backup.Path) {
		return fmt.Errorf("%w: backup %d is the vm disk itself", ErrRevertInvalid, backupID)
	}
	diskInfo, err := os.Stat(diskPath)
	if err != nil || !diskInfo.Mode().IsRegular() {
		return fmt.Errorf("%w: vm disk %s is not a regular file", ErrRevertInvalid, diskPath)
	}

	diskNFS, err := v.GetNfsByVM(ctx, vm)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRevertInvalid, err)
	}
	backupShare, err := db.GetNFSShareByID(ctx, backup.NfsId)
	if err != nil {
		return fmt.Errorf("load backup share %d: %w", backup.NfsId, err)
	}
	if backupShare == nil {
		return fmt.Errorf("%w: backup share %d", ErrRevertNotFound, backup.NfsId)
	}

	// The safety backup needs the current disk size on the backup share; the
	// temp file needs the backup size next to the disk.
	diskDir := filepath.Dir(diskPath)
	if diskNFS == backup.NfsId {
		if err := requireFreeBytes(diskDir, diskInfo.Size()+backupInfo.Size(), "the safety backup and the temp copy"); err != nil {
			return err
		}
	} else {
		if err := requireFreeBytes(backupShare.Target, diskInfo.Size(), "the safety backup"); err != nil {
			return err
		}
		if err := requireFreeBytes(diskDir, backupInfo.Size(), "the temp copy"); err != nil {
			return err
		}
	}

	op := &db.DiskOperation{
		Kind:            revertOperationKind,
		Name:            "vm-revert:" + vm.Name,
		VMName:          vm.Name,
		MachineName:     vm.MachineName,
		SourceNFS:       backup.NfsId,
		DestinationNFS:  diskNFS,
		SourcePath:      backup.Path,
		DestinationPath: diskPath,
		Length:          backupInfo.Size(),
		BackupID:        backup.Id,
	}
	if err := db.BeginDiskOperation(ctx, op); err != nil {
		return fmt.Errorf("%w: vm %s has another unresolved operation: %v", ErrRevertConflict, vm.Name, err)
	}
	holdRevertBackup(vm.Name, backup.Id)

	go func() {
		taskCtx, cancel := context.WithTimeout(context.Background(), longTaskTimeout)
		defer cancel()
		v.runRevert(taskCtx, op, backup, vm.State != grpcVirsh.VmState_SHUTOFF, startAfter)
	}()
	return nil
}

func (v *VirshService) runRevert(ctx context.Context, op *db.DiskOperation, backup *db.VirshBackup, running, startAfter bool) {
	vmName := op.VMName
	// Reconcile refuses to abort the row while this worker holds it.
	if lock, err := diskWorker(op.ID); err == nil {
		defer lock.Unlock()
	}
	held := []int{backup.Id}
	defer func() { releaseRevertBackups(held...) }()

	progressID := fmt.Sprintf("%s-%d", vmName, time.Now().Unix())
	progress := func(message string) {
		extra.SendWebsocketMessage(extraGrpc.WebSocketsMessageType_RevertVM, message, progressID)
	}
	fail := func(stage string, err error) {
		abortCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		message := fmt.Sprintf("Revert of %s to backup %d failed during %s: %v. The VM disk was not changed.", vmName, backup.Id, stage, err)
		if op.SafetyBackupID != 0 {
			message += fmt.Sprintf(" Safety backup %d was kept.", op.SafetyBackupID)
		}
		if abortErr := db.AbortDiskOperation(abortCtx, op); abortErr != nil {
			message += fmt.Sprintf(" Releasing the revert lock also failed (%v); abort operation %s from the disk operations list.", abortErr, op.ID)
		}
		logger.Error(message)
		extra.SendWebsocketMessage(extraGrpc.WebSocketsMessageType_Error, message, vmName)
		notifyOperation(context.Background(), "operation.backup_revert", nots.SeverityError, op.MachineName, "Backup revert failed", message)
	}

	if running {
		if err := diskPhase(ctx, op, "stopping"); err != nil {
			fail("shutdown", err)
			return
		}
		progress(fmt.Sprintf("Revert: shutting down %s", vmName))
		if err := revertShutdownVM(vmName); err != nil {
			fail("shutdown", err)
			return
		}
		if err := waitForShutoff(ctx, vmName); err != nil {
			fail("shutdown", err)
			return
		}
	}

	// Re-check after the lock: the VM must be off and still on the same disk.
	vm, err := revertLookupVM(vmName)
	if err != nil {
		fail("state check", err)
		return
	}
	if vm == nil || vm.State != grpcVirsh.VmState_SHUTOFF {
		fail("state check", errors.New("vm must be confirmed shut off"))
		return
	}
	if filepath.Clean(vm.DiskPath) != op.DestinationPath {
		fail("state check", fmt.Errorf("vm disk changed to %s", vm.DiskPath))
		return
	}

	if err := diskPhase(ctx, op, "safety-backup"); err != nil {
		fail("safety backup", err)
		return
	}
	progress(fmt.Sprintf("Revert: taking a safety backup of %s", vmName))
	safety, err := revertSafetyBackup(ctx, vm, backup.NfsId, false)
	if err != nil {
		fail("safety backup", err)
		return
	}
	op.SafetyBackupID = safety.Id
	holdRevertBackup(vmName, safety.Id)
	held = append(held, safety.Id)

	if err := diskPhase(ctx, op, "copying"); err != nil {
		fail("copy", err)
		return
	}
	temp := revertTempPath(op)
	report := revertCopyProgress
	report.ID = progressID
	if err := revertCopyFile(ctx, backup.Path, temp, vmName, report); err != nil {
		removeRevertTemp(temp)
		fail("copy", err)
		return
	}

	if err := os.Rename(temp, op.DestinationPath); err != nil {
		removeRevertTemp(temp)
		fail("swap", err)
		return
	}
	// The rename is done; from here on failures are reported, never rolled back.
	if err := syncDir(filepath.Dir(op.DestinationPath)); err != nil {
		logger.Warnf("revert %s: fsync disk folder: %v", vmName, err)
	}
	if err := diskPhase(ctx, op, "swapped"); err != nil {
		logger.Errorf("revert %s: record swapped phase: %v", vmName, err)
	}
	completeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	message := fmt.Sprintf("%s now runs on backup %d (created %s). The previous disk was saved as backup %d.", vmName, backup.Id, backup.CreatedAt, safety.Id)
	if err := diskPhase(completeCtx, op, "complete"); err != nil {
		message += fmt.Sprintf(" Releasing the revert lock failed (%v); abort operation %s from the disk operations list.", err, op.ID)
	}
	releaseRevertBackups(held...)
	held = nil
	progress(fmt.Sprintf("Revert of %s complete", vmName))
	notifyOperation(context.Background(), "operation.backup_revert", nots.SeverityInfo, op.MachineName, "Backup revert complete", message)

	if startAfter {
		if err := revertStartVM(ctx, vmName); err != nil {
			notifyOperation(context.Background(), "operation.backup_revert", nots.SeverityWarning, op.MachineName, "VM did not start after revert",
				fmt.Sprintf("The revert of %s succeeded but starting it failed: %v", vmName, err))
		}
	}
}

// waitForShutoff polls until the guest reports SHUTOFF. It never forces it off.
func waitForShutoff(ctx context.Context, vmName string) error {
	deadline := time.Now().Add(revertShutdownTimeout)
	for {
		vm, err := revertLookupVM(vmName)
		if err == nil && vm != nil && vm.State == grpcVirsh.VmState_SHUTOFF {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("vm did not shut down within %s; force-stop it yourself and retry", revertShutdownTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(revertPollInterval):
		}
	}
}

func removeRevertTemp(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logger.Errorf("remove revert temp file %s: %v", path, err)
	}
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// RecoverInterruptedReverts releases revert locks left by a master restart and
// tells the user which disk is in place. Call it before serving the API.
func RecoverInterruptedReverts(ctx context.Context) {
	ops, err := db.ListActiveDiskOperationsByKind(ctx, revertOperationKind)
	if err != nil {
		logger.Errorf("list interrupted reverts: %v", err)
		return
	}
	for i := range ops {
		op := &ops[i]
		outcome := recoverRevert(op)
		if err := db.AbortDiskOperation(ctx, op); err != nil {
			outcome += fmt.Sprintf(" Releasing the revert lock failed (%v); abort operation %s from the disk operations list.", err, op.ID)
		}
		logger.Warn(outcome)
		notifyOperation(ctx, "operation.backup_revert", nots.SeverityWarning, op.MachineName, "Backup revert interrupted", outcome)
	}
}

func recoverRevert(op *db.DiskOperation) string {
	safety := ""
	if op.SafetyBackupID != 0 {
		safety = fmt.Sprintf(" Safety backup %d holds the previous disk.", op.SafetyBackupID)
	}
	prefix := fmt.Sprintf("The master restarted during the revert of %s to backup %d (phase %s).", op.VMName, op.BackupID, op.Phase)
	temp := revertTempPath(op)
	_, statErr := os.Stat(temp)
	switch {
	case op.Phase == "swapped":
		return prefix + " The disk was reverted to the backup." + safety
	case statErr == nil:
		removeRevertTemp(temp)
		return prefix + " The original disk is still in place; the partial copy was removed." + safety
	case op.Phase == "copying":
		return prefix + " The disk is either the original or the backup; check the VM before starting it." + safety
	case op.Phase == "safety-backup":
		return prefix + " The original disk is still in place. A partial backup-* folder may remain on the backup share."
	default:
		return prefix + " The original disk is still in place."
	}
}
