package nfs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type FolderMount struct {
	FolderPath string // shared folder
	Source     string // nfs source (ip:/path)
	Target     string // local mount point
}

func InstallNFS() error {
	if err := runCommand("install nfs-utils", "sudo", "dnf", "-y", "install", "nfs-utils"); err != nil {
		return err
	}
	// Ensure the NFS server services are available when acting as a host
	if err := runCommand("enable nfs-server", "sudo", "systemctl", "enable", "--now", "nfs-server"); err != nil {
		return err
	}
	return nil
}

// CreateSharedFolder creates a directory and ensures it is exported via /etc/exports.
func CreateSharedFolder(folder FolderMount) error {
	path := strings.TrimSpace(folder.FolderPath)
	if path == "" {
		return fmt.Errorf("folder path is required")
	}

	if err := runCommand("create share directory", "sudo", "mkdir", "-p", path); err != nil {
		return err
	}

	entry := fmt.Sprintf("%s *(rw,sync,no_subtree_check,no_root_squash)", path)
	cmdStr := fmt.Sprintf("touch /etc/exports && (grep -Fxq %q /etc/exports || echo %q >> /etc/exports)", entry, entry)
	if err := runCommand("update /etc/exports", "sudo", "bash", "-lc", cmdStr); err != nil {
		return err
	}

	if err := runCommand("refresh nfs exports", "sudo", "exportfs", "-ra"); err != nil {
		return err
	}

	return nil
}

func RemoveSharedFolder(folder FolderMount) error {
	path := strings.TrimSpace(folder.FolderPath)
	if path == "" {
		return fmt.Errorf("folder path is required")
	}

	entry := fmt.Sprintf("%s *(rw,sync,no_subtree_check,no_root_squash)", path)
	cmdStr := fmt.Sprintf("sed -i.bak '/^%s$/d' /etc/exports && rm -f /etc/exports.bak", entry)
	if err := runCommand("update /etc/exports", "sudo", "bash", "-lc", cmdStr); err != nil {
		return err
	}

	if err := runCommand("refresh nfs exports", "sudo", "exportfs", "-ra"); err != nil {
		return err
	}

	if err := runCommand("remove share directory", "sudo", "rm", "-rf", path); err != nil {
		return err
	}

	return nil
}

// MountSharedFolder mounts an exported folder from the network.
func MountSharedFolder(folder FolderMount) error {
	source := strings.TrimSpace(folder.Source)
	target := strings.TrimSpace(folder.Target)
	if source == "" {
		return fmt.Errorf("source is required")
	}
	if target == "" {
		return fmt.Errorf("target is required")
	}

	if err := runCommand("ensure mount directory", "sudo", "mkdir", "-p", target); err != nil {
		return err
	}

	mounted, err := isMounted(target)
	if err != nil {
		return err
	}
	if mounted {
		return nil
	}

	if err := runCommand("mount nfs share", "sudo", "mount", "-t", "nfs", source, target); err != nil {
		return err
	}

	return nil
}

func UnmountSharedFolder(folder FolderMount) error {
	target := strings.TrimSpace(folder.Target)
	if target == "" {
		return fmt.Errorf("target is required")
	}

	mounted, err := isMounted(target)
	if err != nil {
		return err
	}
	if !mounted {
		return nil
	}

	if err := runCommand("unmount nfs share", "sudo", "umount", target); err != nil {
		return err
	}

	return nil
}

func runCommand(desc string, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s: no command provided", desc)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", desc, err)
	}
	return nil
}

func isMounted(target string) (bool, error) {
	cmd := exec.Command("findmnt", "-rn", "--target", target)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("findmnt %s: %w", target, err)
	}
	return true, nil
}
