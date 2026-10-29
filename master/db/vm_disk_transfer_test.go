package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func diskTestDB(t *testing.T) context.Context {
	t.Helper()
	original := DB
	var err error
	DB, err = sql.Open("sqlite3", filepath.Join(t.TempDir(), "disk.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { DB.Close(); DB = original })
	ctx := context.Background()
	if err = CreateVMDiskTable(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx
}
func TestDiskOperationsConflictAndCAS(t *testing.T) {
	ctx := diskTestDB(t)
	id, err := AddVMDisk(ctx, "data", 1, "/one/data.raw", "/one", "raw", 1)
	if err != nil {
		t.Fatal(err)
	}
	op := &DiskOperation{Kind: "move", DiskID: id, Name: "data"}
	if err = BeginDiskOperation(ctx, op); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []*DiskOperation{{Kind: "import", Name: "data"}, {Kind: "grow", DiskID: id, Name: "renamed"}} {
		if err = BeginDiskOperation(ctx, candidate); err == nil {
			t.Fatal("accepted conflicting operation")
		}
	}
	op.Phase = "published"
	if err = SaveDiskOperation(ctx, op, "reserved"); err != nil {
		t.Fatal(err)
	}
	op.Phase = "complete"
	if err = SaveDiskOperation(ctx, op, "reserved"); err == nil {
		t.Fatal("accepted stale transition")
	}
	loaded, err := GetDiskOperation(ctx, op.ID)
	if err != nil || loaded.Phase != "published" {
		t.Fatal("uncertain record lost", err)
	}
	loaded.Phase = "aborted"
	if err = SaveDiskOperation(ctx, loaded, "published"); err != nil {
		t.Fatal(err)
	}
	if _, err = BeginDiskMutation(ctx, id, "grow"); err != nil {
		t.Fatal("terminal operation still locks disk", err)
	}
}

func TestAbortDiskOperationReleasesReservationAndListIsBounded(t *testing.T) {
	ctx := diskTestDB(t)
	for _, name := range []string{"one", "two"} {
		op := &DiskOperation{Kind: "import", Name: name}
		if err := BeginDiskOperation(ctx, op); err != nil {
			t.Fatal(err)
		}
		if name == "one" {
			if err := AbortDiskOperation(ctx, op); err != nil {
				t.Fatal(err)
			}
		}
	}
	ops, err := ListDiskOperations(ctx, 1)
	if err != nil || len(ops) != 1 || ops[0].Name != "two" {
		t.Fatalf("bounded unresolved operations = %+v, %v", ops, err)
	}
	if err := BeginDiskOperation(ctx, &DiskOperation{Kind: "import", Name: "one"}); err != nil {
		t.Fatalf("aborted reservation still blocks name: %v", err)
	}
}
func TestDiskMoveCommitIsConditionalAndPreservesAttachment(t *testing.T) {
	ctx := diskTestDB(t)
	id, err := AddVMDisk(ctx, "data", 1, "/one/data.raw", "/one", "raw", 1)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := ReserveVMDiskAttachment(ctx, id, "guest", "host"); err != nil || !ok {
		t.Fatal(err)
	}
	disk, _ := GetVMDiskByID(ctx, id)
	op := &DiskOperation{Kind: "move", DiskID: id, Name: "data", SourceNFS: 1, SourcePath: disk.DiskPath, DestinationNFS: 2, VMName: "guest", MachineName: "host"}
	if err = BeginDiskOperation(ctx, op); err != nil {
		t.Fatal(err)
	}
	destination := &VMDisk{NFSID: 2, DiskPath: "/two/data.raw", FolderPath: "/two"}
	bad := *op
	bad.SourcePath = "/wrong"
	if err = CommitDiskMove(ctx, &bad, disk, destination); err == nil {
		t.Fatal("accepted wrong source")
	}
	if err = CommitDiskMove(ctx, op, disk, destination); err != nil {
		t.Fatal(err)
	}
	got, _ := GetVMDiskByID(ctx, id)
	if got.Id != id || got.NFSID != 2 || got.AttachedVMName != "guest" || got.AttachedMachineName != "host" {
		t.Fatalf("changed identity/attachment: %+v", got)
	}
	saved, _ := GetDiskOperation(ctx, op.ID)
	if saved.Phase != "committed" {
		t.Fatal("disk commit and operation not atomic")
	}
}
func TestImportRegistrationAtomic(t *testing.T) {
	ctx := diskTestDB(t)
	op := &DiskOperation{Kind: "import", Name: "image", DestinationNFS: 2}
	if err := BeginDiskOperation(ctx, op); err != nil {
		t.Fatal(err)
	}
	disk := &VMDisk{DiskPath: "/dest/image.raw", FolderPath: "/dest", Format: "raw", SizeGB: 1}
	if err := CommitDiskImport(ctx, op, disk); err != nil {
		t.Fatal(err)
	}
	saved, _ := GetDiskOperation(ctx, op.ID)
	got, _ := GetVMDiskByID(ctx, saved.DiskID)
	if saved.Phase != "complete" || got == nil || got.Name != "image" {
		t.Fatal("import not registered atomically")
	}
	if err := CommitDiskImport(ctx, op, disk); err == nil {
		t.Fatal("registered duplicate")
	}
}
