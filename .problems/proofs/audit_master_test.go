package services

// Audit proofs for .problems/. Run them with .problems/proofs/run.sh, which adds
// this file to the package through `go test -overlay`.
//
// Each test asserts the CORRECT behaviour: FAIL means the bug is still present.
// See the table in .problems/README.md for which ones need adapting to your fix.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"512SvMan/db"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

// Problem 01: a dying disk stops answering SMART; its critical state must not vanish silently.
func TestAuditSmartReadFailureKeepsCriticalState(t *testing.T) {
	ctx := context.Background()
	h := newDiskHealthHarness(t)
	info := h.addDisk("a", "/dev/sda", "S1", "sata")
	info.PendingSectors = 50
	info.OfflineUncorrectable = 50
	info.SmartPassed = false
	first := h.poll(t)
	before, err := db.GetDisk(ctx, "S1")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("poll 1: score=%d band=%q alerts=%d", *before.Score, before.Band, len(first))

	h.fetcher.smart["a:/dev/sda"] = nil
	h.fetcher.smartErr["a:/dev/sda"] = errors.New("rpc error: code = DeadlineExceeded")
	second := h.poll(t)
	after, err := db.GetDisk(ctx, "S1")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("poll 2: score=%v band=%q smartAvailable=%v alerts=%d reason=%q", after.Score, after.Band, after.SmartAvailable, len(second), after.Reason)
	if after.Band != "critical" || after.Score == nil {
		t.Errorf("last known critical state was wiped: score=%v band=%q", after.Score, after.Band)
	}
	if len(second) == 0 {
		t.Errorf("no alert when a critical disk stopped answering SMART")
	}
}

// Problem 03: the removed slave loop alerted on these; the new path must surface them.
// Update the signals here to match what the user chooses.
func TestAuditDroppedSignalsStillAlert(t *testing.T) {
	ctx := context.Background()
	h := newDiskHealthHarness(t)
	info := h.addDisk("a", "/dev/sda", "S1", "sata")
	h.poll(t)

	info.EndToEndErrors = 12
	info.SpinRetryCount = 5
	info.CommandTimeouts = 40
	info.HealthStatus = "warning"
	sent := h.poll(t)
	d, err := db.GetDisk(ctx, "S1")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("score=%d band=%q alerts=%d", *d.Score, d.Band, len(sent))
	if d.Band == "good" || len(sent) == 0 {
		t.Errorf("end-to-end errors + spin retries + slave status %q gave band %q and %d alerts", info.HealthStatus, d.Band, len(sent))
	}
}

// Problem 02: something starts the VM during the long copy; the swap must fail closed.
func TestAuditRenameRechecksVMState(t *testing.T) {
	f := newRevertFixture(t)
	copyFn := revertCopyFile
	revertCopyFile = func(ctx context.Context, origin, dest, vmName string, report copyProgress) error {
		err := copyFn(ctx, origin, dest, vmName, report)
		f.setState(grpcVirsh.VmState_RUNNING)
		return err
	}
	if err := (&VirshService{}).RevertBackup(f.ctx, f.backup.Id, false); err != nil {
		t.Fatal(err)
	}
	f.waitRevert(t)
	op := f.lastOperation(t)
	t.Logf("disk=%q phase=%q", readFile(t, f.disk), op.Phase)
	if got := readFile(t, f.disk); got != "current disk" {
		t.Errorf("disk was swapped under a running VM: now %q", got)
	}
}

// Problem 04: after a host reboot recovery runs before the share is mounted, then
// the share appears. Moving the directory stands in for the unmounted share.
// Rewrite this for the fix: the first pass should leave the row active, and a pass
// after the share returns should remove the temp file.
func TestAuditRecoveryBeforeMountRemovesTemp(t *testing.T) {
	root := t.TempDir()
	share := filepath.Join(root, "share")
	unmounted := filepath.Join(root, "not-mounted-yet")
	disk := filepath.Join(share, "guest.qcow2")
	writeFile(t, disk, "original")
	op := &db.DiskOperation{ID: "op1", VMName: "guest", Phase: "copying", DestinationPath: disk, BackupID: 1}
	temp := revertTempPath(op)
	writeFile(t, temp, "partial copy")

	if err := os.Rename(share, unmounted); err != nil {
		t.Fatal(err)
	}
	message := recoverRevert(op)
	if err := os.Rename(unmounted, share); err != nil {
		t.Fatal(err)
	}
	t.Logf("message: %s", message)
	if _, err := os.Stat(temp); err == nil {
		t.Errorf("partial temp file leaked after recovery")
	}
}
