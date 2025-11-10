package btrfs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
)

/*
btrfs->
raid0-> juta varios discos e faz parecer q é um so, SE UM FALHAR PERDEMOS TUDO

RAID1 — espelhamento total (redundância clássica), é tudo clonado 1 vez, se 1 disco falar, pode ter qualuqer numero de discos

raid1c2 tolera falha de 1 disco-> o raid1 normal   ficas com 50% do espaço
raid1c3 tolera falha de 2 disco                              33%
raid1c4 tolera falha de 3 disco                              25%
*/

func runCommand(desc string, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s: no command provided", desc)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	if err := cmd.Run(); err != nil {
		stdoutStr := strings.TrimSpace(stdoutBuf.String())
		stderrStr := strings.TrimSpace(stderrBuf.String())
		logger.Error(desc + " failed: " + err.Error())
		if stderrStr != "" {
			logger.Error(desc + " stderr: " + stderrStr)
		}
		if stdoutStr != "" {
			logger.Error(desc + " stdout: " + stdoutStr)
		}

		var details []string
		if stderrStr != "" {
			details = append(details, "stderr: "+stderrStr)
		}
		if stdoutStr != "" {
			details = append(details, "stdout: "+stdoutStr)
		}
		if len(details) > 0 {
			return fmt.Errorf("%s: %s: %w", desc, strings.Join(details, "; "), err)
		}
		return fmt.Errorf("%s: %w", desc, err)
	}
	logger.Info(desc + " succeeded")
	return nil
}

func InstallBTRFS() error {
	logger.Info("Installing btrfs-progs on Fedora")
	return runCommand("Install btrfs-progs", "sudo", "dnf", "install", "-y", "btrfs-progs")
}

type raidType struct {
	sType string // perfil de dados (-d)
	sMeta string // perfil de metadados (-m)
	c     int    // numero minimo de discos
}

var (
	Raid0   = raidType{"raid0", "raid1c2", 2}
	Raid1c2 = raidType{"raid1c2", "raid1c2", 2}
	Raid1c3 = raidType{"raid1c3", "raid1c3", 3}
	Raid1c4 = raidType{"raid1c4", "raid1c4", 4}
)

func doesDiskExist(disk string) bool {
	_, err := os.Stat(disk)
	return err == nil
}

func isMounted(disk string) bool {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		logger.Error("Failed to read /proc/mounts: " + err.Error())
		return false
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 1 && fields[0] == disk {
			return true
		}
	}
	return false
}

func isDuplicate(disk string, disks ...string) bool {
	count := 0
	for _, d := range disks {
		if d == disk {
			count++
			if count > 1 {
				return true
			}
		}
	}
	return false
}

func CreateRaid(raid raidType, disks ...string) error {
	for _, disk := range disks {
		if !doesDiskExist(disk) {
			return fmt.Errorf("disk %s does not exist", disk)
		}
		if isMounted(disk) {
			return fmt.Errorf("disk %s is already mounted", disk)
		}
		if isDuplicate(disk, disks...) {
			return fmt.Errorf("disk %s is duplicated", disk)
		}
	}

	if len(disks) < raid.c {
		return fmt.Errorf("amount of disks must be at least %d to use %s", raid.c, raid.sType)
	}

	args := append([]string{
		"mkfs.btrfs",
		"-d", raid.sType,
		"-m", raid.sMeta,
		"-f",
	}, disks...)

	return runCommand("creating raid", args...)
}

//findmnt -t btrfs -o TARGET,SOURCE,FSTYPE,OPTIONS -J
//sudo btrfs device stats /mnt/point
//sudo btrfs filesystem df /mnt/point

// func GetRaid
