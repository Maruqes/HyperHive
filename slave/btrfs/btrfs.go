package btrfs

import (
	"bytes"
	"encoding/json"
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

// Compression constants for BTRFS mount options
const (
	CompressionNone   = ""        // No compression (default)
	CompressionNo     = "no"      // Explicitly disable compression
	CompressionLZO    = "lzo"     // Fast compression, moderate ratio
	CompressionZlib   = "zlib"    // Highest compression ratio, slowest
	CompressionZlib1  = "zlib:1"  // Zlib level 1 (fastest)
	CompressionZlib3  = "zlib:3"  // Zlib level 3 (default)
	CompressionZlib9  = "zlib:9"  // Zlib level 9 (maximum compression)
	CompressionZstd   = "zstd"    // Recommended: best balance of speed/ratio
	CompressionZstd1  = "zstd:1"  // Zstd level 1 (fastest)
	CompressionZstd3  = "zstd:3"  // Zstd level 3 (default, recommended)
	CompressionZstd9  = "zstd:9"  // Zstd level 9 (high compression)
	CompressionZstd15 = "zstd:15" // Zstd level 15 (maximum compression)
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
		if len(fields) == 0 {
			continue
		}
		if deviceMatchesDisk(fields[0], disk) {
			return true
		}
	}
	return false
}

func deviceMatchesDisk(device, disk string) bool {
	if device == disk {
		return true
	}

	if !strings.HasPrefix(device, disk) {
		return false
	}

	suffix := device[len(disk):]
	if suffix == "" {
		return true
	}

	if suffix[0] == 'p' {
		suffix = suffix[1:]
	}

	if suffix == "" {
		return false
	}

	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
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

func CreateRaid(name string, raid raidType, disks ...string) error {
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
		"-L", name,
		"-f",
	}, disks...)

	return runCommand("creating raid", args...)
}

/*
gpt explanation ->

BTRFS Compression Options:

- "zlib" (levels 1-9, default 3):
  - Highest compression ratio (~45-50% space savings)
  - Slowest compression speed
  - Best for: archival data, rarely accessed files, maximum space savings
  - CPU usage: High

- "lzo":
  - Moderate compression ratio (~35-40% space savings)
  - Fast compression/decompression
  - Best for: general purpose, good balance of speed and compression
  - CPU usage: Low to moderate

- "zstd" (levels 1-15, default 3):
  - Excellent compression ratio (~40-45% space savings)
  - Very fast compression/decompression (faster than zlib, similar to lzo)
  - Best for: modern systems, recommended for most use cases
  - CPU usage: Low to moderate (very efficient)
  - Note: Requires Linux kernel 4.14+

- "no" or empty string:
  - No compression (default)
  - Fastest I/O performance
  - Best for: already compressed data (videos, images), maximum performance

Performance comparison (approximate):
  Speed:       lzo > zstd > zlib
  Compression: zlib > zstd > lzo
  Recommended: zstd (best overall balance)
*/

func MountRaid(name string, mountPoint string, compression string) error {
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}

	var opts []string
	compression = strings.TrimSpace(compression)
	if compression != "" && compression != "no" {
		opts = append(opts, "compress="+compression)
	}

	args := []string{
		"mount",
		"-t", "btrfs",
	}

	if len(opts) > 0 {
		args = append(args, "-o", strings.Join(opts, ","))
	}

	args = append(args, "-L", name, mountPoint)

	return runCommand("mounting raid", args...)
}

type MinDisk struct {
	Path    string // /dev/sdx
	Mounted bool
}

func GetAllDisks() ([]MinDisk, error) {
	cmd := exec.Command("lsblk", "-d", "-n", "-o", "NAME,TYPE")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list disks: %w", err)
	}

	var disks []MinDisk
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		diskType := fields[1]

		if diskType != "disk" {
			continue
		}

		path := "/dev/" + name
		mounted := isMounted(path)

		disks = append(disks, MinDisk{
			Path:    path,
			Mounted: mounted,
		})
	}

	return disks, nil
}

//findmnt -t btrfs -o TARGET,SOURCE,FSTYPE,OPTIONS -J
//sudo btrfs --format json device stats /mnt/point
//sudo btrfs --format json filesystem df /mnt/point

type FileSystem struct {
	Target   string       `json:"target"`
	Source   string       `json:"source"`
	FSType   string       `json:"fstype"`
	Options  string       `json:"options"`
	Children []FileSystem `json:"children,omitempty"`
}

type FindMntOutput struct {
	FileSystems []FileSystem `json:"filesystems"`
}

func GetAllFileSystems() (*FindMntOutput, error) {
	cmd := exec.Command("findmnt", "-t", "btrfs", "-o", "TARGET,SOURCE,FSTYPE,OPTIONS", "-J")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get filesystems: %w", err)
	}

	var result FindMntOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal filesystems: %w", err)
	}

	return &result, nil
}

type DeviceStats struct {
	Header struct {
		Version string `json:"version"`
	} `json:"__header"`
	DeviceStats []struct {
		Device         string `json:"device"`
		DevID          int    `json:"devid"`
		WriteIOErrs    int    `json:"write_io_errs"`
		ReadIOErrs     int    `json:"read_io_errs"`
		FlushIOErrs    int    `json:"flush_io_errs"`
		CorruptionErrs int    `json:"corruption_errs"`
		GenerationErrs int    `json:"generation_errs"`
	} `json:"device-stats"`
}

func GetFileSystemStats(mountPoint string) (*DeviceStats, error) {
	cmd := exec.Command("btrfs", "--format", "json", "device", "stats", mountPoint)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get device stats: %w", err)
	}

	var stats DeviceStats
	if err := json.Unmarshal(output, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal device stats: %w", err)
	}

	return &stats, nil
}

type FilesystemDF struct {
	Header struct {
		Version string `json:"version"`
	} `json:"__header"`
	FilesystemDF []struct {
		BgType    string `json:"bg-type"`
		BgProfile string `json:"bg-profile"`
		Total     int64  `json:"total"`
		Used      int64  `json:"used"`
	} `json:"filesystem-df"`
}

func GetFileSystemInfo(mountPoint string) (*FilesystemDF, error) {
	cmd := exec.Command("btrfs", "--format", "json", "filesystem", "df", mountPoint)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get filesystem info: %w", err)
	}

	var info FilesystemDF
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal filesystem info: %w", err)
	}

	return &info, nil
}
