package btrfs

import (
	"fmt"
	"os"
	"os/exec"
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

func CreateTempFakeDisk() (string, error) {
	//create a file with 2G in tmp
	randuuid := uuid.New()
	filepath := "/var/tmp/" + randuuid.String() + ".img"
	err := runCommand("creating random file 2g", "truncate", "-s", "2G", filepath)
	if err != nil {
		return "", err
	}
	logger.Info("CREATED: " + filepath)

	// Use MountDisk to mount the file as a loop device
	err = MountDisk(filepath)
	if err != nil {
		os.Remove(filepath)
		return "", err
	}

	//pode ver com "losetup -a"
	//sudo losetup -d /dev/loop0      remove esse
	//sudo losetup -D   		      remove tudo :D
	return filepath, nil
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

func TestRaid0(t *testing.T) {
}
