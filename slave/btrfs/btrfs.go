package btrfs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
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
	if compression != "" {
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

func UMountRaid(target string, force bool) error {
	args := []string{"umount"}

	if force {
		args = append(args, "-f")
	}

	args = append(args, target)

	return runCommand("unmounting raid", args...)
}

func RemoveRaid(target string, force bool) error {
	//umount an do wipefs on all disks
	//get info
	devs, err := GetDisksFromRaid(target)
	if err != nil {
		return err
	}

	//umount
	err = UMountRaid(target, force)
	if err != nil {
		return err
	}

	//check if its mounted
	mounted := isMountPoint(target)
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
	err := runCommand("add disk to raid", "btrfs", "device", "add", diskPath, target)
	if err != nil {
		return fmt.Errorf("failed to add disk to raid: %w", err)
	}

	logger.Info("Successfully added disk " + diskPath + " to raid")

	// It's recommended to balance after adding a disk to distribute data
	logger.Info("Note: Consider running BalanceRaid() to redistribute data across all disks")

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
	if mounted {
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

	err = runCommand("remove disk from raid", "btrfs", "device", "remove", diskPath, target)
	if err != nil {
		return fmt.Errorf("failed to remove disk from raid: %w", err)
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

	err = runCommand("replace disk in raid", "btrfs", "device", "replace", "start", oldDiskPath, newDiskPath, target)
	if err != nil {
		return fmt.Errorf("failed to replace disk: %w", err)
	}

	logger.Info("Replacement command completed. Monitor progress with: btrfs device replace status " + target)
	logger.Info("After completion, consider wiping the old disk with: wipefs -a " + oldDiskPath)

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
		"-dconvert=" + raid.sType,
		"-mconvert=" + raid.sMeta,
		target,
	}

	if err := runCommand("changing raid level", args...); err != nil {
		return err
	}

	logger.Info("Monitor progress with: btrfs balance status " + target)
	return nil
}

// BalanceRaid redistributes data and metadata chunks across devices.
// When chunkLimit > 0, only that many chunks of each type are balanced per run.
// Set background to true to let the kernel continue the balance asynchronously.
func BalanceRaid(target string, chunkLimit int, background bool) error {
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

	args := []string{"btrfs", "balance", "start"}
	if chunkLimit > 0 {
		limit := fmt.Sprintf("%d", chunkLimit)
		args = append(args, "-dlimit="+limit, "-mlimit="+limit)
	}
	if background {
		args = append(args, "-b")
	} else {
		args = append(args, "-B")
	}
	args = append(args, target)

	if err := runCommand("balancing raid", args...); err != nil {
		return err
	}

	if background {
		logger.Info("Balance running in background; check progress with: btrfs balance status " + target)
	}
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
		args = append(args, "-c"+compression)
	}
	args = append(args, target)

	return runCommand("defragmenting", args...)
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
		return err
	}

	if background {
		logger.Info("Scrub running in background; check progress with: btrfs scrub status " + target)
	}
	return nil
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

func (f *FindMntOutput) Print() {
	if f == nil || len(f.FileSystems) == 0 {
		fmt.Println("No BTRFS filesystems found.")
		return
	}

	fmt.Println("BTRFS filesystems:")
	for i, fs := range f.FileSystems {
		printFileSystem(fs, 0)
		if i != len(f.FileSystems)-1 {
			fmt.Println()
		}
	}
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

func printFileSystem(fs FileSystem, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Printf("%s- %s\n", indent, fs.Target)
	fmt.Printf("%s  source : %s\n", indent, fs.Source)
	if fs.FSType != "" {
		fmt.Printf("%s  type   : %s\n", indent, fs.FSType)
	}
	if fs.Options != "" {
		fmt.Printf("%s  options: %s\n", indent, fs.Options)
	}
	for _, child := range fs.Children {
		printFileSystem(child, depth+1)
	}
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

func (d *DeviceStats) Print() {
	if d == nil {
		fmt.Println("No device stats available.")
		return
	}

	fmt.Printf("BTRFS device stats (version %s)\n", d.Header.Version)
	if len(d.DeviceStats) == 0 {
		fmt.Println("  <no devices>")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DEVICE\tDEVID\tWRITE_ERRS\tREAD_ERRS\tFLUSH_ERRS\tCORRUPTION_ERRS\tGENERATION_ERRS")
	for _, stat := range d.DeviceStats {
		fmt.Fprintf(
			w,
			"%s\t%d\t%d\t%d\t%d\t%d\t%d\n",
			stat.Device,
			stat.DevID,
			stat.WriteIOErrs,
			stat.ReadIOErrs,
			stat.FlushIOErrs,
			stat.CorruptionErrs,
			stat.GenerationErrs,
		)
	}
	w.Flush()
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

func (f *FilesystemDF) Print() {
	if f == nil {
		fmt.Println("No filesystem usage information available.")
		return
	}

	fmt.Printf("BTRFS filesystem usage (version %s)\n", f.Header.Version)
	if len(f.FilesystemDF) == 0 {
		fmt.Println("  <no allocation groups>")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "BG_TYPE\tPROFILE\tTOTAL\tUSED\tFREE")
	for _, bg := range f.FilesystemDF {
		free := bg.Total - bg.Used
		fmt.Fprintf(
			w,
			"%s\t%s\t%s\t%s\t%s\n",
			bg.BgType,
			bg.BgProfile,
			formatBytes(bg.Total),
			formatBytes(bg.Used),
			formatBytes(free),
		)
	}
	w.Flush()
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

func formatBytes(value int64) string {
	if value < 0 {
		return "-" + formatBytes(-value)
	}
	if value == 0 {
		return "0 B"
	}

	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	floatVal := float64(value)
	idx := 0
	for floatVal >= 1024 && idx < len(units)-1 {
		floatVal /= 1024
		idx++
	}

	if idx == 0 {
		return fmt.Sprintf("%d %s", value, units[idx])
	}

	return fmt.Sprintf("%.2f %s", floatVal, units[idx])
}

// GetDisksFromRaid returns the list of disk devices that are part of a BTRFS raid
// mounted at the given mountPoint
func GetDisksFromRaid(mountPoint string) ([]string, error) {
	// Use GetFileSystemStats which returns device stats with device paths
	stats, err := GetFileSystemStats(mountPoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get filesystem stats: %w", err)
	}

	if stats == nil || len(stats.DeviceStats) == 0 {
		return []string{}, nil
	}

	// Extract device paths from the stats
	disks := make([]string, 0, len(stats.DeviceStats))
	for _, deviceStat := range stats.DeviceStats {
		if deviceStat.Device != "" {
			disks = append(disks, deviceStat.Device)
		}
	}

	return disks, nil
}

func CheckBtrfsGoLoop(callErrs func(...any)) {
	type btrfsErrorEvent struct {
		Target    string `json:"target"`
		Device    string `json:"device"`
		DevID     int    `json:"devid"`
		ErrorType string `json:"error_type"`
		Count     int    `json:"count"`
	}

	go func() {
		for {
			raids, _ := GetAllFileSystems()
			if raids == nil || len(raids.FileSystems) == 0 {
				return
			}

			for _, raid := range raids.FileSystems {
				stats, err := GetFileSystemStats(raid.Target)
				if err != nil {
					callErrs(fmt.Errorf("failed to get stats for %s: %w", raid.Target, err))
					continue
				}
				if stats == nil {
					continue
				}

				for _, devStat := range stats.DeviceStats {
					metrics := map[string]int{
						"corruption_errs": devStat.CorruptionErrs,
						"flush_io_errs":   devStat.FlushIOErrs,
						"generation_errs": devStat.GenerationErrs,
						"read_io_errs":    devStat.ReadIOErrs,
						"write_io_errs":   devStat.WriteIOErrs,
					}

					for name, val := range metrics {
						if val != 0 {
							evt := btrfsErrorEvent{
								Target:    raid.Target,
								Device:    devStat.Device,
								DevID:     devStat.DevID,
								ErrorType: name,
								Count:     val,
							}
							// pass the event to caller; callers can accept either errors or structured events
							callErrs(evt)
						}
					}
				}
			}
			time.Sleep(30 * time.Second)
		}
	}()
}
