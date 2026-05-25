package vmdisk

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCreateVMDiskCreatesQcow2(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not available")
	}

	base := t.TempDir()
	disk, err := CreateVMDisk(base, "testdisk", 1, "qcow")
	if err != nil {
		t.Fatalf("CreateVMDisk returned error: %v", err)
	}

	wantFolder := filepath.Join(base, "vm_disk_testdisk")
	wantDisk := filepath.Join(wantFolder, "testdisk.qcow2")
	if disk.FolderPath != wantFolder {
		t.Fatalf("folder path = %q, want %q", disk.FolderPath, wantFolder)
	}
	if disk.DiskPath != wantDisk {
		t.Fatalf("disk path = %q, want %q", disk.DiskPath, wantDisk)
	}
	if disk.Format != "qcow2" {
		t.Fatalf("format = %q, want qcow2", disk.Format)
	}
	if disk.SizeGB != 1 {
		t.Fatalf("sizeGB = %d, want 1", disk.SizeGB)
	}
	if info, err := os.Stat(wantDisk); err != nil {
		t.Fatalf("disk was not created: %v", err)
	} else if info.IsDir() {
		t.Fatalf("disk path is a directory")
	}
}

func TestCreateVMDiskRejectsExistingFolder(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not available")
	}

	base := t.TempDir()
	if _, err := CreateVMDisk(base, "duplicate", 1, "raw"); err != nil {
		t.Fatalf("initial CreateVMDisk returned error: %v", err)
	}
	if _, err := CreateVMDisk(base, "duplicate", 1, "raw"); err == nil {
		t.Fatalf("CreateVMDisk succeeded with an existing folder")
	}
}

func TestGrowVMDiskIncreasesVirtualSize(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not available")
	}

	base := t.TempDir()
	if _, err := CreateVMDisk(base, "growdisk", 1, "qcow2"); err != nil {
		t.Fatalf("CreateVMDisk returned error: %v", err)
	}
	disk, err := GrowVMDisk(base, "growdisk", 2)
	if err != nil {
		t.Fatalf("GrowVMDisk returned error: %v", err)
	}
	if disk.SizeGB != 2 {
		t.Fatalf("sizeGB = %d, want 2", disk.SizeGB)
	}
	if _, err := GrowVMDisk(base, "growdisk", 2); err == nil {
		t.Fatalf("GrowVMDisk allowed same size")
	}
}

func TestDeleteVMDiskRemovesFileAndFolder(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not available")
	}

	base := t.TempDir()
	disk, err := CreateVMDisk(base, "deletedisk", 1, "raw")
	if err != nil {
		t.Fatalf("CreateVMDisk returned error: %v", err)
	}
	if _, err := DeleteVMDisk(base, "deletedisk"); err != nil {
		t.Fatalf("DeleteVMDisk returned error: %v", err)
	}
	if _, err := os.Stat(disk.FolderPath); !os.IsNotExist(err) {
		t.Fatalf("folder still exists or stat failed unexpectedly: %v", err)
	}
}

func TestCreateVMDiskRejectsUnsafeName(t *testing.T) {
	if _, err := CreateVMDisk(t.TempDir(), "../bad", 1, "raw"); err == nil {
		t.Fatalf("CreateVMDisk succeeded with an unsafe name")
	}
}
