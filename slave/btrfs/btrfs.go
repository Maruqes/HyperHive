package btrfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

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

	err := cmd.Run()

	cmdStr := strings.Join(cmd.Args, " ")
	stdoutStr := strings.TrimSpace(stdoutBuf.String())
	stderrStr := strings.TrimSpace(stderrBuf.String())

	if err != nil {
		exitInfo := err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitInfo = fmt.Sprintf("exit code %d", exitErr.ExitCode())
		}

		logger.Error(fmt.Sprintf("%s failed (cmd=%s)", desc, cmdStr))
		if exitInfo != "" {
			logger.Error(desc + " exit info: " + exitInfo)
		}
		if stderrStr != "" {
			logger.Error(desc + " stderr: " + stderrStr)
		}
		if stdoutStr != "" {
			logger.Error(desc + " stdout: " + stdoutStr)
		}

		details := []string{fmt.Sprintf("cmd=%s", cmdStr)}
		if exitInfo != "" {
			details = append(details, "exit="+exitInfo)
		}
		if stderrStr != "" {
			details = append(details, "stderr="+stderrStr)
		}
		if stdoutStr != "" {
			details = append(details, "stdout="+stdoutStr)
		}

		return fmt.Errorf("%s failed (%s): %w", desc, strings.Join(details, "; "), err)
	}

	logger.Info(fmt.Sprintf("%s succeeded (cmd=%s)", desc, cmdStr))
	return nil
}

func InstallBTRFS() error {
	logger.Info("Installing btrfs-progs on Fedora")
	return runCommand("Install btrfs-progs", "sudo", "dnf", "install", "-y", "btrfs-progs")
}

// DUPLICATE ON MASTER
// DUPLICATE ON MASTER
// DUPLICATE ON MASTER
type raidType struct {
	sType string // perfil de dados (-d)
	sMeta string // perfil de metadados (-m)
	c     int    // numero minimo de discos
}

//DUPLICATE ON MASTER
//DUPLICATE ON MASTER
//DUPLICATE ON MASTER

var (
	Raid0 = raidType{
		sType: "raid0",
		sMeta: "single",
		c:     2,
	}

	Raid1 = raidType{
		sType: "raid1",
		sMeta: "raid1",
		c:     2,
	}

	Raid1c3 = raidType{
		sType: "raid1c3",
		sMeta: "raid1c3",
		c:     3,
	}

	Raid1c4 = raidType{
		sType: "raid1c4",
		sMeta: "raid1c4",
		c:     4,
	}
)

// Compression constants for BTRFS mount options
const (
	CompressionNone   = ""        // No compression (default)
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

const mountUsageCheckTimeout = 10 * time.Second

var errMountUsageCheckUnavailable = errors.New("mount usage check unavailable")

func doesDiskExist(disk string) bool {
	_, err := os.Stat(disk)
	return err == nil
}

// saber se disco esta montado
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

// saber se a PASTA esta montada (diferente da funcao de cima hehe)
func isMountPoint(path string) bool {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		logger.Error("Failed to read /proc/mounts: " + err.Error())
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[1] == path {
			return true
		}
	}
	return false
}

func filterLsofOutput(output string) string {
	lines := strings.Split(output, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "lsof:") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func collectMountUsageDetails(target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mountUsageCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "lsof", "+D", target)
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("timeout while inspecting open files under %s", target)
	}

	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errMountUsageCheckUnavailable
		}
		if _, ok := err.(*exec.ExitError); !ok {
			return "", fmt.Errorf("lsof failed for %s: %w", target, err)
		}
	}

	details := filterLsofOutput(string(output))
	if details == "" {
		return "", nil
	}

	return details, nil
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

func wipeDiskSignatures(disk string) error {
	cmd := exec.Command("wipefs", "-a", disk)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wipefs failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func CreateRaid(name string, raid *raidType, disks ...string) (string, error) {
	if raid == nil {
		return "", fmt.Errorf("raid type is not valid")
	}
	for _, disk := range disks {
		if !doesDiskExist(disk) {
			return "", fmt.Errorf("disk %s does not exist", disk)
		}
		if isMounted(disk) {
			return "", fmt.Errorf("disk %s is already mounted", disk)
		}
		if isDuplicate(disk, disks...) {
			return "", fmt.Errorf("disk %s is duplicated", disk)
		}
	}

	if len(disks) < raid.c {
		return "", fmt.Errorf("amount of disks must be at least %d to use %s", raid.c, raid.sType)
	}

	for _, disk := range disks {
		if err := wipeDiskSignatures(disk); err != nil {
			return "", fmt.Errorf("failed to wipe signatures on %s: %w", disk, err)
		}
	}

	allFS, err := GetAllFileSystems()
	if err == nil && allFS != nil {
		for _, fs := range allFS.FileSystems {
			label := getBtrfsLabel(fs.Target)
			if label == name {
				return "", fmt.Errorf("a BTRFS filesystem with label '%s' already exists at %s", name, fs.Target)
			}
		}
	}

	args := append([]string{
		"mkfs.btrfs",
		"-d", raid.sType,
		"-m", raid.sMeta,
		"-L", name,
		"-f",
		"-K",
	}, disks...)

	if err := runCommand("creating raid", args...); err != nil {
		return "", err
	}

	// Get UUID of the newly created filesystem
	cmd := exec.Command("blkid", "-s", "UUID", "-o", "value", disks[0])
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get UUID from %s: %w (output: %s)", disks[0], err, strings.TrimSpace(string(output)))
	}

	uuid := strings.TrimSpace(string(output))
	if uuid == "" {
		return "", fmt.Errorf("failed to get UUID from %s: empty blkid output", disks[0])
	}
	return uuid, nil
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
type MountResult struct {
	Degraded bool
	Problems []string
}

func MountRaid(uuid string, mountPoint string, compression string) (*MountResult, error) {
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return nil, fmt.Errorf("uuid is required to mount a filesystem")
	}

	mountPoint = strings.TrimSpace(mountPoint)
	if mountPoint == "" {
		return nil, fmt.Errorf("mount point is required")
	}

	compression = strings.TrimSpace(compression)
	validCompressions := []string{
		CompressionNone,
		CompressionLZO,
		CompressionZlib,
		CompressionZlib1,
		CompressionZlib3,
		CompressionZlib9,
		CompressionZstd,
		CompressionZstd1,
		CompressionZstd3,
		CompressionZstd9,
		CompressionZstd15,
	}

	isValid := false
	for _, valid := range validCompressions {
		if compression == valid {
			isValid = true
			break
		}
	}
	if !isValid && compression != "" {
		return nil, fmt.Errorf("invalid compression type: %s", compression)
	}

	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		return nil, fmt.Errorf("failed to create mount point: %w", err)
	}

	if isMountPoint(mountPoint) {
		return nil, fmt.Errorf("mount point %s already has something mounted", mountPoint)
	}

	baseArgs := []string{
		"mount",
		"-t", "btrfs",
	}

	// função interna para montar com opções extra
	mountWithOpts := func(extraOpts []string) error {
		var opts []string
		if compression != "" {
			opts = append(opts, "compress="+compression)
		}
		opts = append(opts, extraOpts...)

		args := append([]string{}, baseArgs...)
		if len(opts) > 0 {
			args = append(args, "-o", strings.Join(opts, ","))
		}
		args = append(args, "-U", uuid, mountPoint)
		return runCommand("mounting raid", args...)
	}

	// 1) tentar montar normalmente
	normalErr := mountWithOpts(nil)
	if normalErr == nil {
		return &MountResult{Degraded: false, Problems: nil}, nil
	}

	// 2) fallback: tentar com degraded
	if degradedErr := mountWithOpts([]string{"degraded"}); degradedErr == nil {
		// aqui sabemos que montou mas está degradado
		return &MountResult{Degraded: true, Problems: []string{normalErr.Error()}}, nil
	} else {
		// se até com degraded falhar, provavelmente RAID0 partido ou perda demasiado grande
		return nil, fmt.Errorf(
			"failed to mount uuid %s at %s (compression=%q); normal attempt error: %v; degraded attempt error: %v",
			uuid,
			mountPoint,
			compression,
			normalErr,
			degradedErr,
		)
	}
}

func UMountRaid(target string, force bool) error {
	args := []string{"umount"}

	if force {
		args = append(args, "-f")
	}

	args = append(args, target)

	err := runCommand("unmounting raid", args...)
	return err
}

func RemoveRaid(targetMountPoint string, force bool) error {
	//umount an do wipefs on all disks
	//get info
	devs, err := GetDisksFromRaid(targetMountPoint)
	if err != nil {
		return err
	}

	//umount
	err = UMountRaid(targetMountPoint, force)
	if err != nil {
		return err
	}

	//check if its mounted
	mounted := isMountPoint(targetMountPoint)
	if mounted {
		return fmt.Errorf("raid should not be mounted at this point")
	}

	//wipefs disks
	for _, disk := range devs {
		if err := runCommand("wiping disk "+disk, "wipefs", "-a", disk); err != nil {
			return fmt.Errorf("failed to wipe disk %s: %w", disk, err)
		}
	}

	return nil
}

// findmnt -rn -S UUID=755aaa3a-750b-410b-878f-b25c8f58e784 -o TARGET     onde esta montade para desmontar
// sudo wipefs -a /dev/device     apaga device
func RemoveRaidUUID(uuid string) error {
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return fmt.Errorf("uuid is required")
	}

	devPaths, mountPoints, err := collectUUIDDeviceInfo(uuid)
	if err != nil {
		return err
	}
	if len(devPaths) == 0 {
		return fmt.Errorf("no block devices found for uuid %s", uuid)
	}

	mountPoints = appendMountPointFromFindmnt(uuid, mountPoints)

	removed, err := removeViaKnownMounts(uuid, mountPoints)
	if err != nil {
		return err
	}
	if removed {
		return nil
	}

	if err := ensureFilesystemNotMounted(uuid, mountPoints); err != nil {
		return err
	}

	return wipeDevicesByUUID(uuid, devPaths)
}

func collectUUIDDeviceInfo(uuid string) ([]string, []string, error) {
	devicesByUUID, _, _, err := collectBtrfsDevices()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to collect btrfs devices: %w", err)
	}

	var (
		devPaths    []string
		mountPoints []string
	)
	for candidate, devs := range devicesByUUID {
		if !strings.EqualFold(candidate, uuid) {
			continue
		}
		for _, d := range devs {
			if path := strings.TrimSpace(d.Path); path != "" {
				devPaths = appendIfMissing(devPaths, path)
			}
			if mp := strings.TrimSpace(d.MountPoint); mp != "" {
				mountPoints = appendIfMissing(mountPoints, mp)
			}
		}
	}

	return devPaths, mountPoints, nil
}

func appendMountPointFromFindmnt(uuid string, mountPoints []string) []string {
	findmntCmd := exec.Command("findmnt", "-rn", "-S", "UUID="+uuid, "-o", "TARGET")
	out, err := findmntCmd.Output()
	if err != nil {
		return mountPoints
	}

	mountOut := strings.TrimSpace(string(out))
	if mountOut == "" {
		return mountPoints
	}

	lines := strings.Split(mountOut, "\n")
	target := strings.TrimSpace(lines[0])
	if target == "" {
		return mountPoints
	}

	return appendIfMissing(mountPoints, target)
}

func removeViaKnownMounts(uuid string, mountPoints []string) (bool, error) {
	for _, target := range mountPoints {
		target = strings.TrimSpace(target)
		if target == "" || !isMountPoint(target) {
			continue
		}

		logger.Info("Found mount point " + target + " for uuid " + uuid + "; removing raid via mount point")
		if err := RemoveRaid(target, false); err != nil {
			if isMountPoint(target) {
				return false, fmt.Errorf("failed to remove raid at %s: %w", target, err)
			}
			logger.Error("RemoveRaid via mount point failed, attempting device-based removal: " + err.Error())
			continue
		}

		return true, nil
	}

	return false, nil
}

func ensureFilesystemNotMounted(uuid string, mountPoints []string) error {
	for _, target := range mountPoints {
		if isMountPoint(target) {
			return fmt.Errorf("filesystem %s is still mounted at %s; aborting wipe", uuid, target)
		}
	}
	return nil
}

func wipeDevicesByUUID(uuid string, devPaths []string) error {
	var lastErr error
	for _, disk := range devPaths {
		if !doesDiskExist(disk) {
			logger.Error("device does not exist: " + disk)
			lastErr = fmt.Errorf("device not found: %s", disk)
			continue
		}

		logger.Info("Wiping device " + disk + " for uuid " + uuid)
		if err := runCommand("wiping disk "+disk, "wipefs", "-a", disk); err != nil {
			logger.Error("failed to wipe " + disk + ": " + err.Error())
			if lastErr == nil {
				lastErr = err
			}
		}
	}

	if lastErr != nil {
		return fmt.Errorf("some devices failed to be wiped: %w", lastErr)
	}
	return nil
}

func appendIfMissing(list []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return list
	}
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

func AddDiskToRaid(diskPath string, target string) error {
	//check if is really a disk
	if !doesDiskExist(diskPath) {
		return fmt.Errorf("%s", "diskpath its not a disk -> "+diskPath)
	}

	//check if is not mounted anywhere
	if isMounted(diskPath) {
		return fmt.Errorf("%s", "disk path is already mounted somewhere -> "+diskPath)
	}

	// Add the disk to the BTRFS filesystem
	logger.Info("Adding disk " + diskPath + " to BTRFS filesystem at " + target)
	if err := runCommand("add disk to raid", "btrfs", "device", "add", diskPath, target); err != nil {
		return fmt.Errorf("failed to add disk %s to raid at %s: %w", diskPath, target, err)
	}

	logger.Info("Successfully added disk " + diskPath + " to raid")
	return nil
}

// RemoveDisk removes a disk from a BTRFS RAID array
// diskPath: the device path to remove (e.g., /dev/sdb)
// target: the mount point of the BTRFS filesystem
func RemoveDisk(diskPath string, target string) error {
	if !doesDiskExist(diskPath) {
		return fmt.Errorf("disk does not exist: %s", diskPath)
	}

	//check if its mounted
	mounted := isMountPoint(target)
	if !mounted {
		return fmt.Errorf("target mount point does not exist: %s", target)
	}

	devs, err := GetDisksFromRaid(target)
	if err != nil {
		return err
	}

	diskFound := false
	for _, dev := range devs {
		if dev == diskPath {
			diskFound = true
			break
		}
	}

	if !diskFound {
		return fmt.Errorf("disk %s is not part of the BTRFS filesystem at %s", diskPath, target)
	}

	stats, err := GetFileSystemStats(target)
	if err != nil {
		return err
	}

	currentDiskCount := len(stats.DeviceStats)
	if currentDiskCount <= 1 {
		return fmt.Errorf("cannot remove disk: filesystem must have at least one disk remaining")
	}

	logger.Info("Removing disk " + diskPath + " from BTRFS filesystem at " + target)
	logger.Info("This operation may take a while as data is being relocated...")

	if err = runCommand("remove disk from raid", "btrfs", "device", "remove", diskPath, target); err != nil {
		return fmt.Errorf("failed to remove disk %s from raid at %s: %w", diskPath, target, err)
	}

	logger.Info("Successfully removed disk " + diskPath + " from raid")
	logger.Info("You can now wipe the disk with: wipefs -a " + diskPath)

	return nil
}

// ReplaceDisk swaps an existing device in a BTRFS filesystem for a new one.
// oldDiskPath: device currently in the filesystem (e.g., /dev/sdb)
// newDiskPath: unused device that will take over (e.g., /dev/sdc)
// target: mount point of the BTRFS filesystem
func ReplaceDisk(oldDiskPath string, newDiskPath string, target string) error {
	if oldDiskPath == newDiskPath {
		return fmt.Errorf("old and new disk paths must not be the same :D")
	}

	if _, err := os.Stat(target); os.IsNotExist(err) {
		return fmt.Errorf("target mount point does not exist: %s", target)
	}

	if !doesDiskExist(oldDiskPath) {
		return fmt.Errorf("disk does not exist: %s", oldDiskPath)
	}

	if !doesDiskExist(newDiskPath) {
		return fmt.Errorf("disk does not exist: %s", newDiskPath)
	}

	if isMounted(newDiskPath) {
		return fmt.Errorf("new disk is mounted elsewhere: %s", newDiskPath)
	}

	devs, err := GetDisksFromRaid(target)
	if err != nil {
		return err
	}

	oldFound := false
	for _, dev := range devs {
		if dev == oldDiskPath {
			oldFound = true
			break
		}
	}

	if !oldFound {
		return fmt.Errorf("disk %s is not part of the BTRFS filesystem at %s", oldDiskPath, target)
	}

	for _, dev := range devs {
		if dev == newDiskPath {
			return fmt.Errorf("new disk %s is already part of filesystem at %s", newDiskPath, target)
		}
	}

	logger.Info("Replacing disk " + oldDiskPath + " with " + newDiskPath + " in filesystem at " + target)
	logger.Info("This will relocate data from the old disk to the new disk; it may take some time")

	// Use 'start -B' to run in foreground and wait for completion
	if err = runCommand("replace disk in raid", "btrfs", "device", "replace", "start", "-B", oldDiskPath, newDiskPath, target); err != nil {
		return fmt.Errorf("failed to replace disk %s with %s at %s: %w", oldDiskPath, newDiskPath, target, err)
	}

	logger.Info("Disk replacement completed successfully")

	// Balance after replacing a disk to ensure optimal data distribution
	logger.Info("Starting balance operation to optimize data distribution...")
	if err := BalanceRaid(BalanceRaidReq{
		MountPoint: target,
		Force:      false,
		Filters: struct {
			DataUsageMax     int32 `json:"dataUsageMax"`
			MetadataUsageMax int32 `json:"metadataUsageMax"`
		}{
			DataUsageMax:     100,
			MetadataUsageMax: 100,
		},
		ConvertToCurrentRaid: true,
	}); err != nil {
		logger.Error("Balance operation failed: " + err.Error())
		logger.Info("You may need to manually run BalanceRaid() later")
		logger.Info("You can now wipe the old disk with: wipefs -a " + oldDiskPath)
		return fmt.Errorf("disk replaced successfully but balance failed: %w", err)
	}

	logger.Info("Balance completed successfully")
	logger.Info("You can now wipe the old disk with: wipefs -a " + oldDiskPath)

	return nil
}

func validateMountPoint(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("target mount point is required")
	}

	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("target mount point does not exist: %s", target)
		}
		return "", fmt.Errorf("failed to access target mount point: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("target mount point is not a directory: %s", target)
	}

	return target, nil
}

func ChangeRaidLevel(target string, raid raidType) error {
	target, err := validateMountPoint(target)
	if err != nil {
		return err
	}

	stats, err := GetFileSystemStats(target)
	if err != nil {
		return fmt.Errorf("failed to inspect filesystem: %w", err)
	}
	if stats == nil || len(stats.DeviceStats) == 0 {
		return fmt.Errorf("no devices detected for filesystem at %s", target)
	}

	deviceCount := len(stats.DeviceStats)
	if deviceCount < raid.c {
		return fmt.Errorf("filesystem has %d device(s); %s requires at least %d", deviceCount, raid.sType, raid.c)
	}

	args := []string{
		"btrfs", "balance", "start",
		"-f",
		"-v",
		"-dconvert=" + raid.sType,
		"-mconvert=" + raid.sMeta,
		target,
	}

	if err := runCommand("changing raid level", args...); err != nil {
		return fmt.Errorf("failed to change raid level to data=%s meta=%s at %s: %w", raid.sType, raid.sMeta, target, err)
	}

	logger.Info("Monitor progress with: btrfs balance status " + target)
	return nil
}

// BalanceRaidReq defines the input for a Btrfs balance operation.
type BalanceRaidReq struct {
	// MountPoint is the path where the Btrfs filesystem is mounted,
	// e.g. "/mnt/raidpool".
	MountPoint string `json:"mountPoint"`

	Filters struct {
		// DataUsageMax maps to `-dusage=<n>` (0–100).
		// Only data chunks with usage <= n% will be relocated.
		DataUsageMax int32 `json:"dataUsageMax"`

		// MetadataUsageMax maps to `-musage=<n>` (0–100).
		// Only metadata chunks with usage <= n% will be relocated.
		MetadataUsageMax int32 `json:"metadataUsageMax"`
	} `json:"filters"`

	// Force adds `-f` to the balance command.
	Force bool `json:"force"`

	// ConvertToCurrentRaid, if true, will add -dconvert=<profile> and
	// -mconvert=<profile> using the filesystem's current RaidType.
	ConvertToCurrentRaid bool `json:"convertToCurrentRaid"`
}

func raidTypeToBtrfsProfile(raidType string) (string, error) {
	rt := strings.ToLower(strings.TrimSpace(raidType))
	if rt == "" {
		// No raid type set — treat as "no-op".
		return "", nil
	}

	switch rt {
	case "single",
		"raid0",
		"raid1",
		"raid1c2", // sometimes explicit
		"raid1c3",
		"raid1c4",
		"raid5",
		"raid6",
		"dup":
		return rt, nil

	default:
		return "", fmt.Errorf("unsupported raid type %q for convert", raidType)
	}
}

// BalanceRaid balances a Btrfs filesystem using parameters from BalanceRaidReq.
//
// - Uses the provided mountpoint (must already be mounted).
// - Optionally applies -dusage and -musage filters.
// - Optionally applies -f (force).
// - Optionally re-applies the current RaidType as -dconvert/-mconvert.
//
// If no filters are set (>0) and ConvertToCurrentRaid is false,
// it behaves like a normal `btrfs balance start <mount>`.
func BalanceRaid(req BalanceRaidReq) error {
	mountPoint, err := validateMountPoint(strings.TrimSpace(req.MountPoint))
	if err != nil {
		return err
	}

	stats, err := GetFileSystemStats(mountPoint)
	if err != nil {
		return fmt.Errorf("failed to inspect filesystem: %w", err)
	}
	if stats == nil || len(stats.DeviceStats) == 0 {
		return fmt.Errorf("no devices detected for filesystem at %s", mountPoint)
	}

	fileSystem, err := GetFileSystemByMountPoint(mountPoint)
	if err != nil {
		return fmt.Errorf("failed to get filesystem: %w", err)
	}

	args := []string{"btrfs", "balance", "start"}

	// Force (-f)
	if req.Force {
		args = append(args, "-f")
	}

	// Optionally re-apply current RAID profile with -dconvert/-mconvert.
	if req.ConvertToCurrentRaid {
		profile, err := raidTypeToBtrfsProfile(fileSystem.RaidType)
		if err != nil {
			return err
		}
		if profile != "" {
			// Use same profile for data and metadata.
			// If in the future you track them separately (e.g. DataRaidType/MetaRaidType),
			// you can map them independently here.
			args = append(args,
				fmt.Sprintf("-dconvert=%s", profile),
				fmt.Sprintf("-mconvert=%s", profile),
			)
		}
	}

	// Clamp helper: 1–100 only; 0 = disabled (no flag).
	clampPercent := func(v int32) int32 {
		if v <= 0 {
			return 0
		}
		if v > 100 {
			return 100
		}
		return v
	}

	dataUsage := clampPercent(req.Filters.DataUsageMax)
	metaUsage := clampPercent(req.Filters.MetadataUsageMax)

	// Add usage filters if set.
	if dataUsage > 0 {
		args = append(args, fmt.Sprintf("-dusage=%d", dataUsage))
	}
	if metaUsage > 0 {
		args = append(args, fmt.Sprintf("-musage=%d", metaUsage))
	}

	// Finally, the mount point.
	args = append(args, mountPoint)

	if err := runCommand("balancing raid", args...); err != nil {
		return fmt.Errorf("failed to balance filesystem at %s: %w", mountPoint, err)
	}

	logger.Info("Balance operation started; check progress with: btrfs balance status " + mountPoint)
	return nil
}

// PauseBalance pauses a running balance operation on the target filesystem.
func PauseBalance(target string) error {
	target, err := validateMountPoint(target)
	if err != nil {
		return err
	}

	args := []string{"btrfs", "balance", "pause", target}
	if err := runCommand("pausing balance", args...); err != nil {
		return fmt.Errorf("failed to pause balance at %s: %w", target, err)
	}

	logger.Info("Balance paused at " + target)
	return nil
}

// ResumeBalance resumes a paused balance operation on the target filesystem.
func ResumeBalance(target string) error {
	target, err := validateMountPoint(target)
	if err != nil {
		return err
	}

	args := []string{"btrfs", "balance", "resume", target}
	if err := runCommand("resuming balance", args...); err != nil {
		return fmt.Errorf("failed to resume balance at %s: %w", target, err)
	}

	logger.Info("Balance resumed at " + target)
	return nil
}

// CancelBalance cancels a running or paused balance operation on the target filesystem.
func CancelBalance(target string) error {
	target, err := validateMountPoint(target)
	if err != nil {
		return err
	}

	args := []string{"btrfs", "balance", "cancel", target}
	if err := runCommand("canceling balance", args...); err != nil {
		return fmt.Errorf("failed to cancel balance at %s: %w", target, err)
	}

	logger.Info("Balance canceled at " + target)
	return nil
}

// Defragment rewrites fragmented files within the target path.
// When recursive is true, directories are processed recursively. compression selects an optional
// compression algorithm (e.g. "zstd" or "lzo"); pass empty string to disable compression.
func Defragment(target string, recursive bool, compression string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("target path is required")
	}

	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("target path does not exist: %s", target)
		}
		return fmt.Errorf("failed to access target path: %w", err)
	}

	args := []string{"btrfs", "filesystem", "defrag", "-v"}
	if recursive {
		args = append(args, "-r")
	}
	compression = strings.TrimSpace(compression)
	if compression != "" && compression != "no" {
		// btrfs defragment only accepts base algorithm names without levels
		// e.g., "zstd:3" becomes "zstd", "zlib:9" becomes "zlib"
		if idx := strings.Index(compression, ":"); idx != -1 {
			compression = compression[:idx]
		}
		// Also handle forced compression suffix
		compression = strings.TrimSuffix(compression, " (forced)")
		args = append(args, "-c"+compression)
	}
	args = append(args, target)

	if err := runCommand("defragmenting", args...); err != nil {
		return fmt.Errorf("failed to defragment %s: %w", target, err)
	}
	return nil
}

// Scrub verifies data and metadata checksums across devices, optionally running in background.
func Scrub(target string, background bool) error {
	target, err := validateMountPoint(target)
	if err != nil {
		return err
	}

	stats, err := GetFileSystemStats(target)
	if err != nil {
		return fmt.Errorf("failed to inspect filesystem: %w", err)
	}
	if stats == nil || len(stats.DeviceStats) == 0 {
		return fmt.Errorf("no devices detected for filesystem at %s", target)
	}

	args := []string{"btrfs", "scrub", "start"}
	if background {
		args = append(args, "-b")
	} else {
		args = append(args, "-B")
	}
	args = append(args, target)

	if err := runCommand("scrubbing raid", args...); err != nil {
		return fmt.Errorf("failed to scrub %s: %w", target, err)
	}

	if background {
		logger.Info("Scrub running in background; check progress with: btrfs scrub status " + target)
	}
	return nil
}
