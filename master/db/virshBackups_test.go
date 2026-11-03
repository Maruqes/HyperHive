package db

import "testing"

func TestBackupsByNameAndActiveVMOperation(t *testing.T) {
	ctx := diskTestDB(t)
	if err := CreateTableBackups(ctx); err != nil {
		t.Fatal(err)
	}
	for _, b := range []*VirshBackup{{Name: "web", Path: "/b/1"}, {Name: "db", Path: "/b/2"}, {Name: "web", Path: "/b/3", Automatic: true}} {
		if err := InsertVirshBackup(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	backups, err := GetVirshBackupsByName(ctx, "web")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 2 || backups[0].Path != "/b/3" || backups[1].Path != "/b/1" {
		t.Fatalf("unexpected backups: %+v", backups)
	}

	if op, err := GetActiveVMOperation(ctx, "web"); err != nil || op != nil {
		t.Fatalf("unexpected active operation %+v: %v", op, err)
	}
	op := &DiskOperation{Kind: "revert-vm", Name: "vm-revert:web", VMName: "web", BackupID: 3}
	if err = BeginDiskOperation(ctx, op); err != nil {
		t.Fatal(err)
	}
	active, err := GetActiveVMOperation(ctx, "web")
	if err != nil || active == nil || active.ID != op.ID || active.BackupID != 3 {
		t.Fatalf("active operation not found: %+v %v", active, err)
	}
	if other, _ := GetActiveVMOperation(ctx, "db"); other != nil {
		t.Fatal("operation leaked to another vm")
	}
	byKind, err := ListActiveDiskOperationsByKind(ctx, "revert-vm")
	if err != nil || len(byKind) != 1 {
		t.Fatalf("list by kind: %+v %v", byKind, err)
	}
	if err = AbortDiskOperation(ctx, op); err != nil {
		t.Fatal(err)
	}
	if active, _ = GetActiveVMOperation(ctx, "web"); active != nil {
		t.Fatal("aborted operation still active")
	}
}
