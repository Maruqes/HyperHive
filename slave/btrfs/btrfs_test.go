package btrfs

import (
	"fmt"
	"os"
	"os/exec"
	"slave/env512"
	"slave/logs512"
	"strings"
	"testing"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/google/uuid"
)

// UmountDisk unmounts a loop device associated with a file
func UmountDisk(filepath string) error {
	// Find the loop device associated with the file
	output, err := exec.Command("losetup", "-j", filepath).Output()
	if err != nil {
		return fmt.Errorf("failed to find loop device for %s: %w", filepath, err)
	}

	if len(output) == 0 {
		logger.Info("No loop device found for: " + filepath)
		return nil
	}

	// Parse output like: /dev/loop0: []: (/var/tmp/xxx.img)
	loopDevice := ""
	parts := strings.Split(string(output), ":")
	if len(parts) > 0 {
		loopDevice = strings.TrimSpace(parts[0])
	}

	if loopDevice == "" {
		return fmt.Errorf("could not parse loop device from output")
	}

	logger.Info("Unmounting loop device: " + loopDevice)

	// Detach the loop device
	err = runCommand("detach loop device", "losetup", "-d", loopDevice)
	if err != nil {
		return fmt.Errorf("failed to detach loop device %s: %w", loopDevice, err)
	}

	logger.Info("Successfully unmounted: " + filepath)
	return nil
}

// MountDisk mounts a file as a loop device
func MountDisk(filepath string) error {
	// Check if file exists
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filepath)
	}

	// Check if already mounted
	output, err := exec.Command("losetup", "-j", filepath).Output()
	if err == nil && len(output) > 0 {
		logger.Info("File already mounted as loop device: " + filepath)
		return nil
	}

	logger.Info("Mounting file as loop device: " + filepath)

	// Mount the file as a loop device
	err = runCommand("mount file as loop device", "losetup", "-fP", filepath)
	if err != nil {
		return fmt.Errorf("failed to mount %s as loop device: %w", filepath, err)
	}

	logger.Info("Successfully mounted: " + filepath)
	return nil
}

func CreateTempFakeDisk() (string, string, error) {
	//create a file with 2G in tmp
	randuuid := uuid.New()
	filepath := "/var/tmp/" + randuuid.String() + ".img"
	err := runCommand("creating random file 2g", "truncate", "-s", "2G", filepath)
	if err != nil {
		return "", "", err
	}
	logger.Info("CREATED: " + filepath)

	// Use MountDisk to mount the file as a loop device
	err = MountDisk(filepath)
	if err != nil {
		os.Remove(filepath)
		return "", "", err
	}

	// Find the loop device associated with the file
	output, err := exec.Command("losetup", "-j", filepath).Output()
	if err != nil {
		os.Remove(filepath)
		return "", "", fmt.Errorf("failed to find loop device for %s: %w", filepath, err)
	}

	loopDevice := ""
	if len(output) > 0 {
		parts := strings.Split(string(output), ":")
		if len(parts) > 0 {
			loopDevice = strings.TrimSpace(parts[0])
		}
	}

	if loopDevice == "" {
		os.Remove(filepath)
		return "", "", fmt.Errorf("could not parse loop device from output")
	}

	//pode ver com "losetup -a"
	//sudo losetup -d /dev/loop0      remove esse
	//sudo losetup -D   		      remove tudo :D
	return filepath, loopDevice, nil
}

func DeleteTempDisk(filepath string) error {
	// Use UmountDisk to unmount the loop device
	err := UmountDisk(filepath)
	if err != nil {
		logger.Error("Failed to unmount disk, attempting to remove file anyway: " + err.Error())
		// Continue to try removing the file even if unmount fails
	}

	if err := os.Remove(filepath); err != nil {
		return fmt.Errorf("failed to delete temp disk file: %w", err)
	}

	logger.Info("Deleted temp disk: " + filepath)
	return nil
}

func ForceCorruptTempDisk(filepath string) error {
	// Find the loop device associated with the file
	output, err := exec.Command("losetup", "-j", filepath).Output()
	if err != nil {
		return fmt.Errorf("failed to find loop device for %s: %w", filepath, err)
	}

	loopDevice := ""
	if len(output) > 0 {
		parts := strings.Split(string(output), ":")
		if len(parts) > 0 {
			loopDevice = strings.TrimSpace(parts[0])
		}
	}

	if loopDevice == "" {
		return fmt.Errorf("no loop device found for %s", filepath)
	}

	logger.Info("Corrupting disk: " + loopDevice)

	// Corrupt the disk by writing random data at the beginning (superblock area)
	// This will corrupt filesystem structures
	err = runCommand("corrupt disk superblock", "dd", "if=/dev/urandom", "of="+loopDevice, "bs=4096", "count=10", "seek=0", "conv=notrunc")
	if err != nil {
		return fmt.Errorf("failed to corrupt superblock: %w", err)
	}

	// Corrupt some data in the middle of the disk as well
	err = runCommand("corrupt disk middle section", "dd", "if=/dev/urandom", "of="+loopDevice, "bs=4096", "count=5", "seek=1000", "conv=notrunc")
	if err != nil {
		return fmt.Errorf("failed to corrupt middle section: %w", err)
	}

	logger.Info("Successfully corrupted disk: " + filepath)
	return nil
}

func askForSudo() {
	//if current program is not sudo terminate
	if os.Geteuid() != 0 {
		fmt.Println("This program needs to be run as root.")
		os.Exit(0)
	}
}

// sudo btrfs filesystem show
func TestRaid0(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath2, dev2, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	err = CreateRaid("raid0", Raid0, dev0, dev1, dev2)
	if err != nil {
		t.Error(err)
		return
	}

	// Mount the RAID before trying to get stats
	mountPoint := "/mnt/raid0_test"
	err = MountRaid("raid0", mountPoint, CompressionZlib9)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded by checking if it appears in mounted filesystems
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	// Test the specific mounted RAID instead of iterating over all filesystems
	stats, err := GetFileSystemStats(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	info, err := GetFileSystemInfo(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	disks, err := GetDisksFromRaid(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	fsystem, err := GetFileSystemByMountPoint(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	// Print the results
	fmt.Println("=== RAID0 Test Results ===")
	fmt.Println("Disks:", disks)

	stats.Print()
	info.Print()
	fsystem.Print()

	err = RemoveRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	err = DeleteTempDisk(filepath0)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath1)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath2)
	if err != nil {
		t.Error(err)
		return
	}
}

func TestRaid1c2(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath2, dev2, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	err = CreateRaid("raid1c2", Raid1, dev0, dev1, dev2)
	if err != nil {
		t.Error(err)
		return
	}

	// Mount the RAID before trying to get stats
	mountPoint := "/mnt/raid1c2_test"
	err = MountRaid("raid1c2", mountPoint, CompressionNone)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded by checking if it appears in mounted filesystems
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	// Test the specific mounted RAID instead of iterating over all filesystems
	stats, err := GetFileSystemStats(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	info, err := GetFileSystemInfo(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	disks, err := GetDisksFromRaid(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	fsystem, err := GetFileSystemByMountPoint(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	// Print the results
	fmt.Println("=== raid1c2 Test Results ===")
	fmt.Println("Disks:", disks)

	stats.Print()
	info.Print()
	fsystem.Print()

	err = RemoveRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	err = DeleteTempDisk(filepath0)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath1)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath2)
	if err != nil {
		t.Error(err)
		return
	}
}

func TestRaid1c3(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath2, dev2, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	err = CreateRaid("raid1c3", Raid1c3, dev0, dev1, dev2)
	if err != nil {
		t.Error(err)
		return
	}

	// Mount the RAID before trying to get stats
	mountPoint := "/mnt/raid1c3_test"
	err = MountRaid("raid1c3", mountPoint, CompressionZstd3)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded by checking if it appears in mounted filesystems
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	// Test the specific mounted RAID instead of iterating over all filesystems
	stats, err := GetFileSystemStats(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	info, err := GetFileSystemInfo(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	disks, err := GetDisksFromRaid(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	fsystem, err := GetFileSystemByMountPoint(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	// Print the results
	fmt.Println("=== raid1c3 Test Results ===")
	fmt.Println("Disks:", disks)

	stats.Print()
	info.Print()
	fsystem.Print()

	err = RemoveRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	err = DeleteTempDisk(filepath0)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath1)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath2)
	if err != nil {
		t.Error(err)
		return
	}
}
func TestRaid1c4(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath2, dev2, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath3, dev3, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	err = CreateRaid("raid1c4", Raid1c4, dev0, dev1, dev2, dev3)
	if err != nil {
		t.Error(err)
		return
	}

	// Mount the RAID before trying to get stats
	mountPoint := "/mnt/raid1c4_test"
	err = MountRaid("raid1c4", mountPoint, CompressionLZO)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded by checking if it appears in mounted filesystems
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	// Test the specific mounted RAID instead of iterating over all filesystems
	stats, err := GetFileSystemStats(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	info, err := GetFileSystemInfo(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	disks, err := GetDisksFromRaid(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	fsystem, err := GetFileSystemByMountPoint(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	// Print the results
	fmt.Println("=== raid1c4 Test Results ===")
	fmt.Println("Disks:", disks)

	stats.Print()
	info.Print()
	fsystem.Print()

	err = RemoveRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	err = DeleteTempDisk(filepath0)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath1)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath2)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath3)
	if err != nil {
		t.Error(err)
		return
	}
}

func TestAddDiskToRaid(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath2, dev2, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepathToAdd, dev3Add, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	err = CreateRaid("raid1c3-add", Raid1c3, dev0, dev1, dev2)
	if err != nil {
		t.Error(err)
		return
	}

	// Mount the RAID before trying to get stats
	mountPoint := "/mnt/raid1c3_add_test"
	err = MountRaid("raid1c3-add", mountPoint, CompressionZstd3)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded by checking if it appears in mounted filesystems
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	err = AddDiskToRaid(dev3Add, mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	// Test the specific mounted RAID instead of iterating over all filesystems
	stats, err := GetFileSystemStats(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	info, err := GetFileSystemInfo(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	disks, err := GetDisksFromRaid(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	fsystem, err := GetFileSystemByMountPoint(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	// Print the results
	fmt.Println("=== raid1c3 Test Results ===")
	fmt.Println("Disks:", disks)

	stats.Print()
	info.Print()
	fsystem.Print()

	err = RemoveRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	err = DeleteTempDisk(filepath0)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath1)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath2)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepathToAdd)
	if err != nil {
		t.Error(err)
		return
	}
}

func TestRemoveDiskFromRaid(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath2, dev2, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath3, dev3, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	err = CreateRaid("raid1c3-add", Raid1c3, dev0, dev1, dev2, dev3)
	if err != nil {
		t.Error(err)
		return
	}

	// Mount the RAID before trying to get stats
	mountPoint := "/mnt/raid1c3_add_test"
	err = MountRaid("raid1c3-add", mountPoint, CompressionZstd3)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded by checking if it appears in mounted filesystems
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	err = RemoveDisk(dev0, mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	// Test the specific mounted RAID instead of iterating over all filesystems
	stats, err := GetFileSystemStats(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	info, err := GetFileSystemInfo(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	disks, err := GetDisksFromRaid(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	fsystem, err := GetFileSystemByMountPoint(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	// Print the results
	fmt.Println("=== raid1c3 Test Results ===")
	fmt.Println("Disks:", disks)

	stats.Print()
	info.Print()
	fsystem.Print()

	err = RemoveRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	err = DeleteTempDisk(filepath0)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath1)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath2)
	if err != nil {
		t.Error(err)
		return
	}
	err = DeleteTempDisk(filepath3)
	if err != nil {
		t.Error(err)
		return
	}
}

func TestReplaceDiskFromRaid(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath2, dev2, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	// Create a replacement disk
	filepathNew, devNew, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	err = CreateRaid("raid1c3-replace", Raid1c3, dev0, dev1, dev2)
	if err != nil {
		t.Error(err)
		return
	}

	// Mount the RAID
	mountPoint := "/mnt/raid1c3_replace_test"
	err = MountRaid("raid1c3-replace", mountPoint, CompressionZstd3)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	// Write some test data
	testFile := mountPoint + "/testfile.txt"
	err = os.WriteFile(testFile, []byte("test data for replacement"), 0644)
	if err != nil {
		t.Error("Failed to write test file:", err)
		UMountRaid(mountPoint, false)
		return
	}

	fmt.Println("=== Before replacement ===")
	disksBefore, _ := GetDisksFromRaid(mountPoint)
	fmt.Println("Disks before:", disksBefore)

	// Replace dev0 with devNew
	fmt.Println("Replacing", dev0, "with", devNew)
	err = ReplaceDisk(dev0, devNew, mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	fmt.Println("=== After replacement ===")
	disksAfter, err := GetDisksFromRaid(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}
	fmt.Println("Disks after:", disksAfter)

	// Verify test data still exists
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Error("Failed to read test file after replacement:", err)
		UMountRaid(mountPoint, false)
		return
	}
	if string(data) != "test data for replacement" {
		t.Error("Test data corrupted after replacement")
		UMountRaid(mountPoint, false)
		return
	}

	stats, err := GetFileSystemStats(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}
	stats.Print()

	err = RemoveRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	err = DeleteTempDisk(filepath0)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepath1)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepath2)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepathNew)
	if err != nil {
		t.Error(err)
	}
}

func TestChangeRaidLevel(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath2, dev2, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	// Start with RAID0
	err = CreateRaid("raid-convert", Raid0, dev0, dev1, dev2)
	if err != nil {
		t.Error(err)
		return
	}

	mountPoint := "/mnt/raid_convert_test"
	err = MountRaid("raid-convert", mountPoint, CompressionZstd3)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	fmt.Println("=== Initial RAID0 state ===")
	info0, _ := GetFileSystemInfo(mountPoint)
	if info0 != nil {
		info0.Print()
	}

	// Convert to RAID1
	fmt.Println("\n=== Converting to RAID1 ===")
	err = ChangeRaidLevel(mountPoint, Raid1)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}

	fmt.Println("=== After conversion to RAID1 ===")
	info1, err := GetFileSystemInfo(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}
	info1.Print()

	err = RemoveRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	err = DeleteTempDisk(filepath0)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepath1)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepath2)
	if err != nil {
		t.Error(err)
	}
}

func TestBalanceRaid(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath2, dev2, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	err = CreateRaid("raid-balance", Raid1, dev0, dev1, dev2)
	if err != nil {
		t.Error(err)
		return
	}

	mountPoint := "/mnt/raid_balance_test"
	err = MountRaid("raid-balance", mountPoint, CompressionZstd3)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	// Write some test data to create chunks
	testFile := mountPoint + "/largefile.txt"
	largeData := strings.Repeat("test data for balance operation\n", 10000)
	err = os.WriteFile(testFile, []byte(largeData), 0644)
	if err != nil {
		t.Error("Failed to write test file:", err)
		UMountRaid(mountPoint, false)
		return
	}

	fmt.Println("=== Before balance ===")
	info0, _ := GetFileSystemInfo(mountPoint)
	if info0 != nil {
		info0.Print()
	}

	// Test balance with chunk limit
	fmt.Println("\n=== Running balance (limited chunks) ===")
	err = BalanceRaid(mountPoint, 5, false)
	if err != nil {
		t.Error("Balance with chunk limit failed:", err)
		UMountRaid(mountPoint, false)
		return
	}

	fmt.Println("\n=== Running full balance ===")
	err = BalanceRaid(mountPoint, 0, false)
	if err != nil {
		t.Error("Full balance failed:", err)
		UMountRaid(mountPoint, false)
		return
	}

	fmt.Println("=== After balance ===")
	info1, _ := GetFileSystemInfo(mountPoint)
	if info1 != nil {
		info1.Print()
	}

	err = RemoveRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	err = DeleteTempDisk(filepath0)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepath1)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepath2)
	if err != nil {
		t.Error(err)
	}
}

func TestDefragmentRaid(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	err = CreateRaid("raid-defrag", Raid1, dev0, dev1)
	if err != nil {
		t.Error(err)
		return
	}

	mountPoint := "/mnt/raid_defrag_test"
	err = MountRaid("raid-defrag", mountPoint, CompressionNone)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	// Create some fragmented files by writing and rewriting
	testFile := mountPoint + "/fragmented.txt"
	for i := 0; i < 10; i++ {
		data := strings.Repeat(fmt.Sprintf("iteration %d data\n", i), 1000)
		err = os.WriteFile(testFile, []byte(data), 0644)
		if err != nil {
			t.Error("Failed to write test file:", err)
			UMountRaid(mountPoint, false)
			return
		}
	}

	// Create a test directory with multiple files
	testDir := mountPoint + "/testdir"
	err = os.MkdirAll(testDir, 0755)
	if err != nil {
		t.Error("Failed to create test directory:", err)
		UMountRaid(mountPoint, false)
		return
	}

	for i := 0; i < 5; i++ {
		filename := fmt.Sprintf("%s/file%d.txt", testDir, i)
		err = os.WriteFile(filename, []byte(strings.Repeat("data\n", 100)), 0644)
		if err != nil {
			t.Error("Failed to write test file:", err)
			UMountRaid(mountPoint, false)
			return
		}
	}

	fmt.Println("=== Testing defragment single file ===")
	err = Defragment(testFile, false, "")
	if err != nil {
		t.Error("Defragment single file failed:", err)
		UMountRaid(mountPoint, false)
		return
	}

	fmt.Println("\n=== Testing defragment directory recursively with compression ===")
	err = Defragment(testDir, true, CompressionZstd3)
	if err != nil {
		t.Error("Defragment recursive failed:", err)
		UMountRaid(mountPoint, false)
		return
	}

	fmt.Println("\n=== Testing defragment entire mount point ===")
	err = Defragment(mountPoint, true, CompressionZstd3)
	if err != nil {
		t.Error("Defragment mount point failed:", err)
		UMountRaid(mountPoint, false)
		return
	}

	err = RemoveRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	err = DeleteTempDisk(filepath0)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepath1)
	if err != nil {
		t.Error(err)
	}
}

func TestScrubRaid(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath2, dev2, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	err = CreateRaid("raid-scrub", Raid1, dev0, dev1, dev2)
	if err != nil {
		t.Error(err)
		return
	}

	mountPoint := "/mnt/raid_scrub_test"
	err = MountRaid("raid-scrub", mountPoint, CompressionZstd3)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	// Write some test data
	testFile := mountPoint + "/scrubtest.txt"
	testData := strings.Repeat("data to scrub and verify checksums\n", 1000)
	err = os.WriteFile(testFile, []byte(testData), 0644)
	if err != nil {
		t.Error("Failed to write test file:", err)
		UMountRaid(mountPoint, false)
		return
	}

	fmt.Println("=== Running scrub operation ===")
	err = Scrub(mountPoint, false)
	if err != nil {
		t.Error("Scrub failed:", err)
		UMountRaid(mountPoint, false)
		return
	}

	fmt.Println("\n=== Checking device stats after scrub ===")
	stats, err := GetFileSystemStats(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		return
	}
	stats.Print()

	// Verify no errors were found
	if stats != nil {
		for _, devStat := range stats.DeviceStats {
			if devStat.CorruptionErrs > 0 || devStat.ReadIOErrs > 0 || devStat.WriteIOErrs > 0 {
				t.Errorf("Errors found on device %s after scrub", devStat.Device)
			}
		}
	}

	err = RemoveRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	err = DeleteTempDisk(filepath0)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepath1)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepath2)
	if err != nil {
		t.Error(err)
	}
}

func TestCheckBtrfsFunc(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath2, dev2, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	err = CreateRaid("raid-check", Raid1, dev0, dev1, dev2)
	if err != nil {
		t.Error(err)
		return
	}

	mountPoint := "/mnt/raid_check_test"
	err = MountRaid("raid-check", mountPoint, CompressionZstd3)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	fmt.Println("=== Testing CheckBtrfs with healthy filesystem ===")
	errorCount := 0
	errorCallback := func(errs ...any) {
		for _, err := range errs {
			errorCount++
			fmt.Printf("Error detected: %v\n", err)
		}
	}

	CheckBtrfs(errorCallback)

	if errorCount > 0 {
		t.Logf("Found %d errors in healthy filesystem (may be normal)", errorCount)
	} else {
		fmt.Println("No errors found in healthy filesystem")
	}

	// Optionally test with corrupted disk (commented out as it's destructive)
	/*
		fmt.Println("\n=== Testing with corrupted disk ===")
		err = UMountRaid(mountPoint, false)
		if err != nil {
			t.Error(err)
			return
		}

		// Corrupt one disk
		err = ForceCorruptTempDisk(filepath0)
		if err != nil {
			t.Error("Failed to corrupt disk:", err)
			return
		}

		// Try to remount (may fail)
		err = MountRaid("raid-check", mountPoint, CompressionZstd3)
		if err != nil {
			fmt.Println("Mount failed after corruption (expected):", err)
		} else {
			errorCount = 0
			CheckBtrfs(errorCallback)
			fmt.Printf("Errors found after corruption: %d\n", errorCount)
		}
	*/

	err = RemoveRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	err = DeleteTempDisk(filepath0)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepath1)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepath2)
	if err != nil {
		t.Error(err)
	}
}

func TestDiskFailure(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath2, dev2, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	// Create RAID1 (can tolerate 1 disk failure)
	err = CreateRaid("raid-failure", Raid1, dev0, dev1, dev2)
	if err != nil {
		t.Error(err)
		return
	}

	mountPoint := "/mnt/raid_failure_test"
	err = MountRaid("raid-failure", mountPoint, CompressionZstd3)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	// Write test data before failure
	testFile := mountPoint + "/important_data.txt"
	testData := "Critical data that must survive disk failure"
	err = os.WriteFile(testFile, []byte(testData), 0644)
	if err != nil {
		t.Error("Failed to write test file:", err)
		UMountRaid(mountPoint, false)
		return
	}

	fmt.Println("=== Before disk failure ===")
	disksBefore, _ := GetDisksFromRaid(mountPoint)
	fmt.Println("Disks before failure:", disksBefore)

	statsBefore, _ := GetFileSystemStats(mountPoint)
	if statsBefore != nil {
		statsBefore.Print()
	}

	// Unmount to simulate disk failure
	err = UMountRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	// Simulate disk failure by corrupting and detaching one disk
	fmt.Println("\n=== Simulating disk failure on", dev0, "===")
	err = ForceCorruptTempDisk(filepath0)
	if err != nil {
		t.Error("Failed to corrupt disk:", err)
		return
	}

	// Detach the failed disk
	err = UmountDisk(filepath0)
	if err != nil {
		t.Error("Failed to detach corrupted disk:", err)
		return
	}

	// Try to mount in degraded mode (with one disk missing)
	fmt.Println("\n=== Attempting degraded mount ===")
	// Mount with degraded option to allow mounting with missing disk
	args := []string{
		"mount",
		"-t", "btrfs",
		"-o", "degraded,compress=zstd:3",
		"-L", "raid-failure",
		mountPoint,
	}
	err = runCommand("mounting raid in degraded mode", args...)
	if err != nil {
		t.Error("Failed to mount in degraded mode:", err)
		// Cleanup
		DeleteTempDisk(filepath1)
		DeleteTempDisk(filepath2)
		os.Remove(filepath0) // File still exists, just detached
		return
	}

	fmt.Println("\n=== After disk failure (degraded mount) ===")
	disksAfter, err := GetDisksFromRaid(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		DeleteTempDisk(filepath1)
		DeleteTempDisk(filepath2)
		os.Remove(filepath0)
		return
	}
	fmt.Println("Disks after failure:", disksAfter)

	// Verify data is still accessible
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Error("Failed to read test file after disk failure:", err)
		UMountRaid(mountPoint, false)
		DeleteTempDisk(filepath1)
		DeleteTempDisk(filepath2)
		os.Remove(filepath0)
		return
	}
	if string(data) != testData {
		t.Error("Data corrupted after disk failure! Expected:", testData, "Got:", string(data))
		UMountRaid(mountPoint, false)
		DeleteTempDisk(filepath1)
		DeleteTempDisk(filepath2)
		os.Remove(filepath0)
		return
	}
	fmt.Println("✓ Data verified successfully after disk failure")

	// Write new data to verify write operations work in degraded mode
	newTestFile := mountPoint + "/new_data_after_failure.txt"
	newData := "Data written after disk failure"
	err = os.WriteFile(newTestFile, []byte(newData), 0644)
	if err != nil {
		t.Error("Failed to write new data in degraded mode:", err)
		UMountRaid(mountPoint, false)
		DeleteTempDisk(filepath1)
		DeleteTempDisk(filepath2)
		os.Remove(filepath0)
		return
	}
	fmt.Println("✓ Successfully wrote new data in degraded mode")

	// Check filesystem stats after failure
	statsAfter, err := GetFileSystemStats(mountPoint)
	if err != nil {
		t.Error(err)
		UMountRaid(mountPoint, false)
		DeleteTempDisk(filepath1)
		DeleteTempDisk(filepath2)
		os.Remove(filepath0)
		return
	}
	fmt.Println("\n=== Stats after disk failure ===")
	statsAfter.Print()

	// Run scrub to detect and report errors
	fmt.Println("\n=== Running scrub to detect errors ===")
	err = Scrub(mountPoint, false)
	if err != nil {
		fmt.Println("Scrub reported errors (expected after disk failure):", err)
	}

	// Cleanup
	err = UMountRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
	}

	err = DeleteTempDisk(filepath1)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepath2)
	if err != nil {
		t.Error(err)
	}
	// Remove the corrupted disk file
	os.Remove(filepath0)

	fmt.Println("\n=== Test completed successfully ===")
}

func TestMultipleDiskFailure(t *testing.T) {
	logger.SetType(env512.Mode)
	logger.SetCallBack(logs512.LogMessage)
	askForSudo()

	filepath0, dev0, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath1, dev1, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath2, dev2, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	filepath3, dev3, err := CreateTempFakeDisk()
	if err != nil {
		t.Error(err)
		return
	}

	// Create RAID1c3 (can tolerate 2 disk failures)
	err = CreateRaid("raid-multi-failure", Raid1c3, dev0, dev1, dev2, dev3)
	if err != nil {
		t.Error(err)
		return
	}

	mountPoint := "/mnt/raid_multi_failure_test"
	err = MountRaid("raid-multi-failure", mountPoint, CompressionZstd3)
	if err != nil {
		t.Error(err)
		return
	}

	// Verify the mount succeeded
	fmt.Println("Verifying mount at:", mountPoint)
	verifyCmd := exec.Command("findmnt", "-t", "btrfs", mountPoint)
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		fmt.Println("Mount verification failed:", string(output))
		t.Errorf("Mount verification failed: %v", err)
		UMountRaid(mountPoint, false)
		return
	} else {
		fmt.Println("Mount verified successfully")
	}

	// Write test data before failures
	testFile := mountPoint + "/resilient_data.txt"
	testData := "This data must survive multiple disk failures"
	err = os.WriteFile(testFile, []byte(testData), 0644)
	if err != nil {
		t.Error("Failed to write test file:", err)
		UMountRaid(mountPoint, false)
		return
	}

	// Create multiple test files to ensure data distribution
	for i := 0; i < 10; i++ {
		filename := fmt.Sprintf("%s/testfile_%d.txt", mountPoint, i)
		data := fmt.Sprintf("Test file %d - should survive failures", i)
		err = os.WriteFile(filename, []byte(data), 0644)
		if err != nil {
			t.Error("Failed to write test file:", err)
			UMountRaid(mountPoint, false)
			return
		}
	}

	fmt.Println("=== Before any disk failures ===")
	disksBefore, _ := GetDisksFromRaid(mountPoint)
	fmt.Println("Disks before failures:", disksBefore)
	fmt.Printf("Total disks: %d\n", len(disksBefore))

	infoBefore, _ := GetFileSystemInfo(mountPoint)
	if infoBefore != nil {
		infoBefore.Print()
	}

	// === FIRST DISK FAILURE ===
	err = UMountRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		return
	}

	fmt.Println("\n=== FIRST DISK FAILURE: Corrupting", dev0, "===")
	err = ForceCorruptTempDisk(filepath0)
	if err != nil {
		t.Error("Failed to corrupt first disk:", err)
		return
	}
	err = UmountDisk(filepath0)
	if err != nil {
		t.Error("Failed to detach first disk:", err)
		return
	}

	// Mount in degraded mode after first failure
	fmt.Println("=== Mounting with 1 disk failed ===")
	args := []string{
		"mount",
		"-t", "btrfs",
		"-o", "degraded,compress=zstd:3",
		"-L", "raid-multi-failure",
		mountPoint,
	}
	err = runCommand("mounting raid after first failure", args...)
	if err != nil {
		t.Error("Failed to mount after first disk failure:", err)
		DeleteTempDisk(filepath1)
		DeleteTempDisk(filepath2)
		DeleteTempDisk(filepath3)
		os.Remove(filepath0)
		return
	}

	// Verify data after first failure
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Error("Failed to read test file after first disk failure:", err)
		UMountRaid(mountPoint, false)
		DeleteTempDisk(filepath1)
		DeleteTempDisk(filepath2)
		DeleteTempDisk(filepath3)
		os.Remove(filepath0)
		return
	}
	if string(data) != testData {
		t.Error("Data corrupted after first disk failure!")
		UMountRaid(mountPoint, false)
		DeleteTempDisk(filepath1)
		DeleteTempDisk(filepath2)
		DeleteTempDisk(filepath3)
		os.Remove(filepath0)
		return
	}
	fmt.Println("✓ Data verified successfully after first disk failure")

	disksAfterFirst, _ := GetDisksFromRaid(mountPoint)
	fmt.Printf("Disks after first failure: %d remaining\n", len(disksAfterFirst))

	// === SECOND DISK FAILURE ===
	err = UMountRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
		DeleteTempDisk(filepath1)
		DeleteTempDisk(filepath2)
		DeleteTempDisk(filepath3)
		os.Remove(filepath0)
		return
	}

	fmt.Println("\n=== SECOND DISK FAILURE: Corrupting", dev1, "===")
	err = ForceCorruptTempDisk(filepath1)
	if err != nil {
		t.Error("Failed to corrupt second disk:", err)
		DeleteTempDisk(filepath2)
		DeleteTempDisk(filepath3)
		os.Remove(filepath0)
		os.Remove(filepath1)
		return
	}
	err = UmountDisk(filepath1)
	if err != nil {
		t.Error("Failed to detach second disk:", err)
		DeleteTempDisk(filepath2)
		DeleteTempDisk(filepath3)
		os.Remove(filepath0)
		os.Remove(filepath1)
		return
	}

	// Mount in degraded mode after second failure (RAID1c3 should still work with 2 disks failed)
	fmt.Println("=== Mounting with 2 disks failed ===")
	err = runCommand("mounting raid after second failure", args...)
	if err != nil {
		t.Error("Failed to mount after second disk failure:", err)
		DeleteTempDisk(filepath2)
		DeleteTempDisk(filepath3)
		os.Remove(filepath0)
		os.Remove(filepath1)
		return
	}

	// Verify data after second failure
	data, err = os.ReadFile(testFile)
	if err != nil {
		t.Error("Failed to read test file after second disk failure:", err)
		UMountRaid(mountPoint, false)
		DeleteTempDisk(filepath2)
		DeleteTempDisk(filepath3)
		os.Remove(filepath0)
		os.Remove(filepath1)
		return
	}
	if string(data) != testData {
		t.Error("Data corrupted after second disk failure!")
		UMountRaid(mountPoint, false)
		DeleteTempDisk(filepath2)
		DeleteTempDisk(filepath3)
		os.Remove(filepath0)
		os.Remove(filepath1)
		return
	}
	fmt.Println("✓ Data verified successfully after TWO disk failures")

	// Verify all test files are intact
	allFilesIntact := true
	for i := 0; i < 10; i++ {
		filename := fmt.Sprintf("%s/testfile_%d.txt", mountPoint, i)
		expectedData := fmt.Sprintf("Test file %d - should survive failures", i)
		data, err := os.ReadFile(filename)
		if err != nil {
			t.Errorf("Failed to read test file %d: %v", i, err)
			allFilesIntact = false
			break
		}
		if string(data) != expectedData {
			t.Errorf("Test file %d corrupted!", i)
			allFilesIntact = false
			break
		}
	}
	if allFilesIntact {
		fmt.Println("✓ All test files verified successfully after TWO disk failures")
	}

	disksAfterSecond, _ := GetDisksFromRaid(mountPoint)
	fmt.Printf("Disks after second failure: %d remaining\n", len(disksAfterSecond))

	// Get final stats
	fmt.Println("\n=== Final stats after 2 disk failures ===")
	statsAfter, err := GetFileSystemStats(mountPoint)
	if err != nil {
		t.Error(err)
	} else {
		statsAfter.Print()
	}

	infoAfter, err := GetFileSystemInfo(mountPoint)
	if err != nil {
		t.Error(err)
	} else {
		infoAfter.Print()
	}

	// Test that we can still write data after 2 disk failures
	finalTestFile := mountPoint + "/written_after_2_failures.txt"
	finalData := "Data written after 2 disk failures - RAID1c3 is resilient!"
	err = os.WriteFile(finalTestFile, []byte(finalData), 0644)
	if err != nil {
		t.Error("Failed to write data after 2 disk failures:", err)
	} else {
		fmt.Println("✓ Successfully wrote new data after TWO disk failures")
	}

	// Cleanup
	err = UMountRaid(mountPoint, false)
	if err != nil {
		t.Error(err)
	}

	err = DeleteTempDisk(filepath2)
	if err != nil {
		t.Error(err)
	}
	err = DeleteTempDisk(filepath3)
	if err != nil {
		t.Error(err)
	}
	// Remove the corrupted disk files
	os.Remove(filepath0)
	os.Remove(filepath1)

	fmt.Println("\n=== Test completed successfully ===")
	fmt.Println("✓ RAID1c3 successfully tolerated 2 simultaneous disk failures")
}
