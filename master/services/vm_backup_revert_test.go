package services

import (
	"512SvMan/db"
	"512SvMan/env512"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

type revertFixture struct {
	ctx      context.Context
	disk     string
	backup   *db.VirshBackup
	mu       sync.Mutex
	state    grpcVirsh.VmState
	stops    bool // shutdown request turns the VM off
	started  int
	copies   int
	safeties int
}

func (f *revertFixture) setState(state grpcVirsh.VmState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = state
}

func newRevertFixture(t *testing.T) *revertFixture {
	t.Helper()
	original := db.DB
	var err error
	db.DB, err = sql.Open("sqlite3", filepath.Join(t.TempDir(), "revert.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.DB.SetMaxOpenConns(1)
	ctx := context.Background()
	for _, create := range []func(context.Context) error{db.CreateNFSTable, db.CreateTableBackups, db.CreateVMDiskTable} {
		if err := create(ctx); err != nil {
			t.Fatal(err)
		}
	}

	root := t.TempDir()
	vmShare, backupShare := filepath.Join(root, "vms"), filepath.Join(root, "backups")
	for _, share := range []string{vmShare, backupShare} {
		if err := os.MkdirAll(share, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := db.AddNFSShare(ctx, "host-a", share, "nfs:"+share, share, filepath.Base(share), false); err != nil {
			t.Fatal(err)
		}
	}
	shares, err := db.GetAllNFShares(ctx)
	if err != nil || len(shares) != 2 {
		t.Fatal("shares", err)
	}
	backupNFS := shares[1].Id

	f := &revertFixture{ctx: ctx, state: grpcVirsh.VmState_SHUTOFF, stops: true}
	f.disk = filepath.Join(vmShare, "guest", "guest.qcow2")
	writeFile(t, f.disk, "current disk")
	backupPath := filepath.Join(backupShare, "backup-old", "guest.qcow2")
	writeFile(t, backupPath, "known good")
	f.backup = &db.VirshBackup{Name: "guest", Path: backupPath, NfsId: backupNFS, Automatic: true}
	if err := db.InsertVirshBackup(ctx, f.backup); err != nil {
		t.Fatal(err)
	}

	saved := struct {
		lookup        func(string) (*grpcVirsh.Vm, error)
		shutdown      func(string) error
		start         func(context.Context, string) error
		safety        func(context.Context, *grpcVirsh.Vm, int, bool) (*db.VirshBackup, error)
		copyFn        func(context.Context, string, string, string, copyProgress) error
		free          func(string) (uint64, error)
		poll, timeout time.Duration
		uid, gid      string
	}{revertLookupVM, revertShutdownVM, revertStartVM, revertSafetyBackup, revertCopyFile, revertFreeBytes, revertPollInterval, revertShutdownTimeout, env512.Qemu_UID, env512.Qemu_GID}
	t.Cleanup(func() {
		revertLookupVM, revertShutdownVM, revertStartVM = saved.lookup, saved.shutdown, saved.start
		revertSafetyBackup, revertCopyFile, revertFreeBytes = saved.safety, saved.copyFn, saved.free
		revertPollInterval, revertShutdownTimeout = saved.poll, saved.timeout
		env512.Qemu_UID, env512.Qemu_GID = saved.uid, saved.gid
		db.DB.Close()
		db.DB = original
	})
	// copyFile chowns to the qemu user; the test process owns its files.
	env512.Qemu_UID, env512.Qemu_GID = strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid())

	revertLookupVM = func(name string) (*grpcVirsh.Vm, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return &grpcVirsh.Vm{Name: name, MachineName: "host-a", DiskPath: f.disk, State: f.state}, nil
	}
	revertShutdownVM = func(string) error {
		if f.stops {
			f.setState(grpcVirsh.VmState_SHUTOFF)
		}
		return nil
	}
	revertStartVM = func(context.Context, string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.started++
		return nil
	}
	revertSafetyBackup = func(ctx context.Context, vm *grpcVirsh.Vm, nfsID int, automatic bool) (*db.VirshBackup, error) {
		f.safeties++
		return backupVMDisk(ctx, vm, nfsID, automatic)
	}
	revertCopyFile = func(ctx context.Context, origin, dest, vmName string, report copyProgress) error {
		f.copies++
		return copyFileWithProgress(ctx, origin, dest, vmName, report)
	}
	revertFreeBytes = func(string) (uint64, error) { return 1 << 40, nil }
	revertPollInterval, revertShutdownTimeout = 5*time.Millisecond, 50*time.Millisecond
	return f
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// waitRevert waits until the revert released both its lock and its backups.
func (f *revertFixture) waitRevert(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		op, err := db.GetActiveVMOperation(f.ctx, "guest")
		if err != nil {
			t.Fatal(err)
		}
		_, held := revertBackupInUse(f.backup.Id)
		if op == nil && !held {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("revert did not finish")
}

func (f *revertFixture) assertNoTemp(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(f.disk))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".revert-") {
			t.Fatalf("temp file left behind: %s", entry.Name())
		}
	}
}

func (f *revertFixture) lastOperation(t *testing.T) *db.DiskOperation {
	t.Helper()
	var record string
	if err := db.DB.QueryRowContext(f.ctx, `SELECT id FROM vm_disk_operations ORDER BY updated_at DESC, rowid DESC LIMIT 1`).Scan(&record); err != nil {
		t.Fatal(err)
	}
	op, err := db.GetDiskOperation(f.ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func TestRevertBackupReplacesDiskAndKeepsSafetyBackup(t *testing.T) {
	f := newRevertFixture(t)
	f.setState(grpcVirsh.VmState_RUNNING)
	service := &VirshService{}
	if err := service.RevertBackup(f.ctx, f.backup.Id, true); err != nil {
		t.Fatal(err)
	}
	f.waitRevert(t)

	if got := readFile(t, f.disk); got != "known good" {
		t.Fatalf("disk not reverted: %q", got)
	}
	f.assertNoTemp(t)
	op := f.lastOperation(t)
	if op.Phase != "complete" || op.Kind != revertOperationKind || op.SafetyBackupID == 0 {
		t.Fatalf("unexpected operation: %+v", op)
	}
	safety, err := db.GetVirshBackupById(f.ctx, op.SafetyBackupID)
	if err != nil || safety == nil {
		t.Fatal("safety backup row missing", err)
	}
	if safety.Automatic || safety.NfsId != f.backup.NfsId || readFile(t, safety.Path) != "current disk" {
		t.Fatalf("unexpected safety backup: %+v", safety)
	}
	deadline := time.Now().Add(time.Second)
	for {
		f.mu.Lock()
		started := f.started
		f.mu.Unlock()
		if started == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("VM was not started after revert")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRevertBackupFailuresLeaveDiskUntouched(t *testing.T) {
	for _, tc := range []struct {
		name       string
		setup      func(*revertFixture)
		wantSafety bool
	}{
		{name: "shutdown timeout", setup: func(f *revertFixture) {
			f.setState(grpcVirsh.VmState_RUNNING)
			f.stops = false
		}},
		{name: "safety backup fails", setup: func(f *revertFixture) {
			revertSafetyBackup = func(context.Context, *grpcVirsh.Vm, int, bool) (*db.VirshBackup, error) {
				f.safeties++
				return nil, errors.New("share full")
			}
		}},
		{name: "copy fails", wantSafety: true, setup: func(f *revertFixture) {
			revertCopyFile = func(_ context.Context, _, dest, _ string, _ copyProgress) error {
				f.copies++
				if err := os.WriteFile(dest, []byte("partial"), 0o644); err != nil {
					return err
				}
				return errors.New("read error")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRevertFixture(t)
			tc.setup(f)
			if err := (&VirshService{}).RevertBackup(f.ctx, f.backup.Id, true); err != nil {
				t.Fatal(err)
			}
			f.waitRevert(t)
			if got := readFile(t, f.disk); got != "current disk" {
				t.Fatalf("disk changed: %q", got)
			}
			f.assertNoTemp(t)
			op := f.lastOperation(t)
			if op.Phase != "aborted" {
				t.Fatalf("lock not aborted: %+v", op)
			}
			if (op.SafetyBackupID != 0) != tc.wantSafety {
				t.Fatalf("safety backup id %d, want present=%v", op.SafetyBackupID, tc.wantSafety)
			}
			if f.started != 0 {
				t.Fatal("failed revert started the VM")
			}
			if tc.name == "shutdown timeout" && (f.safeties != 0 || f.copies != 0) {
				t.Fatal("work ran after shutdown timeout")
			}
		})
	}
}

func TestRevertBackupRejectsBeforeLocking(t *testing.T) {
	f := newRevertFixture(t)
	revertFreeBytes = func(string) (uint64, error) { return 1, nil }
	err := (&VirshService{}).RevertBackup(f.ctx, f.backup.Id, false)
	if !errors.Is(err, ErrRevertConflict) || !strings.Contains(err.Error(), "free space") {
		t.Fatalf("expected free space conflict, got %v", err)
	}
	if op, _ := db.GetActiveVMOperation(f.ctx, "guest"); op != nil {
		t.Fatal("lock taken despite rejection")
	}
	if err := (&VirshService{}).RevertBackup(f.ctx, 999, false); !errors.Is(err, ErrRevertNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestRevertLockRefusesStartBackupAndDelete(t *testing.T) {
	f := newRevertFixture(t)
	op := &db.DiskOperation{Kind: revertOperationKind, Name: "vm-revert:guest", VMName: "guest", DestinationPath: f.disk}
	if err := db.BeginDiskOperation(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	holdRevertBackup("guest", f.backup.Id)
	defer releaseRevertBackups(f.backup.Id)
	service := &VirshService{}
	if err := service.StartVM(f.ctx, "guest"); !errors.Is(err, ErrRevertConflict) {
		t.Fatalf("StartVM: %v", err)
	}
	if err := service.BackupVM(f.ctx, "guest", f.backup.NfsId, false); !errors.Is(err, ErrRevertConflict) {
		t.Fatalf("BackupVM: %v", err)
	}
	if err := service.DeleteBackup(f.ctx, f.backup.Id); !errors.Is(err, ErrRevertConflict) {
		t.Fatalf("DeleteBackup: %v", err)
	}
	if err := service.RevertBackup(f.ctx, f.backup.Id, false); !errors.Is(err, ErrRevertConflict) {
		t.Fatalf("second revert: %v", err)
	}
}

func TestRecoverInterruptedRevertRemovesTempAndAbortsLock(t *testing.T) {
	f := newRevertFixture(t)
	op := &db.DiskOperation{Kind: revertOperationKind, Name: "vm-revert:guest", VMName: "guest", DestinationPath: f.disk, BackupID: f.backup.Id, SafetyBackupID: 7}
	if err := db.BeginDiskOperation(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := diskPhase(f.ctx, op, "copying"); err != nil {
		t.Fatal(err)
	}
	temp := revertTempPath(op)
	writeFile(t, temp, "partial")

	RecoverInterruptedReverts(f.ctx)

	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatal("temp file survived recovery", err)
	}
	if readFile(t, f.disk) != "current disk" {
		t.Fatal("recovery changed the disk")
	}
	if active, _ := db.GetActiveVMOperation(f.ctx, "guest"); active != nil {
		t.Fatal("recovery kept the lock")
	}
	if got := recoverRevert(&db.DiskOperation{VMName: "guest", Phase: "copying", DestinationPath: f.disk, ID: "gone"}); !strings.Contains(got, "either the original or the backup") {
		t.Fatalf("missing uncertain outcome: %s", got)
	}
}
