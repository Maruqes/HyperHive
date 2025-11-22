package btrfs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
)

type MinDisk struct {
	Path       string // /dev/sda
	Name       string // sda
	Model      string // "Samsung SSD 860 EVO"
	Vendor     string // "Samsung"
	Serial     string // "S3Z8NX0K123456A"
	Rotational bool   // true = HDD, false = SSD
	SizeGB     float64
	Mounted    bool
	ByID       string // /dev/disk/by-id/ata-Samsung_SSD
	Transport  string // sata, nvme, usb, virtio
	PCIPath    string // /sys/block/sda/device
}

// BtrfsDevice represents a physical device that is part of a BTRFS filesystem.
type BtrfsDevice struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Label      string `json:"label"`
	UUID       string `json:"uuid"`
	UUIDSub    string `json:"uuid_sub"`
	SizeBytes  int64  `json:"size_bytes"`
	MountPoint string `json:"mount_point"`
	Mounted    bool   `json:"mounted"`
}

func findByID(name string) string {
	files, _ := os.ReadDir("/dev/disk/by-id")
	for _, f := range files {
		fullPath := "/dev/disk/by-id/" + f.Name()
		target, _ := os.Readlink(fullPath)
		if strings.Contains(target, name) {
			return fullPath
		}
	}
	return ""
}

// if test i will return also loop for testing
func GetAllDisks(test bool) ([]MinDisk, error) {
	cmd := exec.Command(
		"lsblk", "-d", "-J",
		"-o", "NAME,PATH,MODEL,VENDOR,SERIAL,SIZE,ROTA,TYPE,TRAN",
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("lsblk failed: %w", err)
	}

	var parsed struct {
		Blockdevices []struct {
			Name   string `json:"name"`
			Path   string `json:"path"`
			Model  string `json:"model"`
			Vendor string `json:"vendor"`
			Serial string `json:"serial"`
			Size   int64  `json:"size"`
			Rota   int    `json:"rota"`
			Type   string `json:"type"`
			Tran   string `json:"tran"`
		} `json:"blockdevices"`
	}

	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("json parse error: %w", err)
	}

	var disks []MinDisk
	for _, d := range parsed.Blockdevices {

		if d.Type != "disk" && !test {
			continue
		}

		// /dev/disk/by-id
		byID := findByID(d.Name)

		disks = append(disks, MinDisk{
			Path:       d.Path,
			Name:       d.Name,
			Model:      d.Model,
			Vendor:     d.Vendor,
			Serial:     d.Serial,
			Rotational: d.Rota == 1,
			SizeGB:     float64(d.Size) / (1024 * 1024 * 1024),
			Mounted:    isMounted(d.Path),
			ByID:       byID,
			Transport:  d.Tran,
			PCIPath:    "/sys/block/" + d.Name + "/device",
		})
	}

	return disks, nil
}

//findmnt -t btrfs -o TARGET,SOURCE,FSTYPE,OPTIONS -J
//sudo btrfs --format json device stats /mnt/point
//sudo btrfs --format json filesystem df /mnt/point

type FileSystem struct {
	Target        string        `json:"target"`
	Source        string        `json:"source"`
	FSType        string        `json:"fstype"`
	Options       string        `json:"options"`
	UUID          string        `json:"uuid"`
	Label         string        `json:"label,omitempty"`
	Compression   string        `json:"compression"`
	RaidType      string        `json:"raid_type,omitempty"`
	MaxSpace      int64         `json:"max_space"`       // Total space in bytes (from btrfs fi df)
	UsedSpace     int64         `json:"used_space"`      // Used space in bytes (from btrfs fi df)
	RealMaxSpace  int64         `json:"real_max_space"`  // Real device size (from btrfs fi usage)
	RealUsedSpace int64         `json:"real_used_space"` // Real used space (from btrfs fi usage)
	Devices       []BtrfsDevice `json:"devices,omitempty"`
	Children      []FileSystem  `json:"children,omitempty"`
	Mounted       bool          `json:"mounted"`
}

func printFileSystem(fs FileSystem, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Printf("%s- %s\n", indent, fs.Target)
	fmt.Printf("%s  source     : %s\n", indent, fs.Source)
	if fs.FSType != "" {
		fmt.Printf("%s  type       : %s\n", indent, fs.FSType)
	}
	if fs.UUID != "" {
		fmt.Printf("%s  uuid       : %s\n", indent, fs.UUID)
	}
	if fs.Label != "" {
		fmt.Printf("%s  label      : %s\n", indent, fs.Label)
	}
	if fs.Options != "" {
		fmt.Printf("%s  options    : %s\n", indent, fs.Options)
	}
	fmt.Printf("%s  mounted    : %t\n", indent, fs.Mounted)
	if fs.Compression != "" {
		fmt.Printf("%s  compression: %s\n", indent, fs.Compression)
	}
	if fs.RaidType != "" {
		fmt.Printf("%s  raid type  : %s\n", indent, fs.RaidType)
	}
	if fs.MaxSpace > 0 {
		fmt.Printf("%s  max space  : %s\n", indent, formatBytes(fs.MaxSpace))
		fmt.Printf("%s  used space : %s\n", indent, formatBytes(fs.UsedSpace))
		freeSpace := fs.MaxSpace - fs.UsedSpace
		fmt.Printf("%s  free space : %s\n", indent, formatBytes(freeSpace))
		if fs.MaxSpace > 0 {
			usedPercent := float64(fs.UsedSpace) * 100 / float64(fs.MaxSpace)
			fmt.Printf("%s  usage      : %.2f%%\n", indent, usedPercent)
		}
	}
	if fs.RealMaxSpace > 0 {
		fmt.Printf("%s  real max   : %s\n", indent, formatBytes(fs.RealMaxSpace))
		fmt.Printf("%s  real used  : %s\n", indent, formatBytes(fs.RealUsedSpace))
		realFreeSpace := fs.RealMaxSpace - fs.RealUsedSpace
		fmt.Printf("%s  real free  : %s\n", indent, formatBytes(realFreeSpace))
		if fs.RealMaxSpace > 0 {
			realUsedPercent := float64(fs.RealUsedSpace) * 100 / float64(fs.RealMaxSpace)
			fmt.Printf("%s  real usage : %.2f%%\n", indent, realUsedPercent)
		}
	}
	for _, child := range fs.Children {
		printFileSystem(child, depth+1)
	}
	if len(fs.Devices) > 0 {
		fmt.Printf("%s  devices:\n", indent)
		for _, dev := range fs.Devices {
			fmt.Printf("%s    - %s (%s) size=%s mounted=%t\n",
				indent,
				dev.Path,
				dev.Type,
				formatBytes(dev.SizeBytes),
				dev.Mounted,
			)
		}
	}
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

func (f *FileSystem) Print() {
	printFileSystem(*f, 0)
}

type FindMntOutput struct {
	FileSystems []FileSystem `json:"filesystems"`
}

type lsblkResponse struct {
	BlockDevices []lsblkDevice `json:"blockdevices"`
}

type lsblkDevice struct {
	Name       string        `json:"name"`
	Type       string        `json:"type"`
	Size       int64         `json:"size"`
	FSType     *string       `json:"fstype"`
	Path       *string       `json:"path"`
	Label      *string       `json:"label"`
	MountPoint *string       `json:"mountpoint"`
	UUID       *string       `json:"uuid"`
	Children   []lsblkDevice `json:"children"`
}

type usageStats struct {
	maxBytes    int64
	usedBytes   int64
	raidProfile string
}

func GetAllFileSystems() (*FindMntOutput, error) {
	findmntOutput, err := runFindmnt()
	if err != nil {
		return nil, err
	}
	if findmntOutput == nil {
		findmntOutput = &FindMntOutput{}
	}

	devicesByUUID, labelsByUUID, sizeByUUID, err := collectBtrfsDevices()
	if err != nil {
		return nil, err
	}

	usageByUUID := make(map[string]usageStats)
	seenUUID := make(map[string]bool)

	// Enrich existing mount information with device/usage data
	walkFileSystems(findmntOutput.FileSystems, func(fs *FileSystem) {
		fs.Mounted = true
		fs.Compression = extractCompressionFromOptions(fs.Options)
		if fs.UUID == "" {
			return
		}

		seenUUID[fs.UUID] = true

		if label := labelsByUUID[fs.UUID]; label != "" && fs.Label == "" {
			fs.Label = label
		}

		if fs.Label == "" && fs.Target != "" {
			fs.Label = getBtrfsLabel(fs.Target)
		}

		if devs, ok := devicesByUUID[fs.UUID]; ok {
			fs.Devices = append([]BtrfsDevice(nil), devs...)
			if total := sizeByUUID[fs.UUID]; total > 0 {
				fs.RealMaxSpace = total
			}
		}

		if _, ok := usageByUUID[fs.UUID]; !ok && fs.Target != "" {
			if usage, err := filesystemUsage(fs.Target); err == nil {
				usageByUUID[fs.UUID] = usage
			} else {
				logger.Error(fmt.Sprintf("failed to inspect filesystem %s: %v", fs.Target, err))
			}
		}

		if usage, ok := usageByUUID[fs.UUID]; ok {
			fs.MaxSpace = usage.maxBytes
			fs.UsedSpace = usage.usedBytes
			if fs.RealMaxSpace == 0 {
				fs.RealMaxSpace = usage.maxBytes
			}
			fs.RealUsedSpace = usage.usedBytes
			if fs.RaidType == "" {
				fs.RaidType = usage.raidProfile
			}
		}

		if fs.RaidType == "" {
			fs.RaidType = detectRaidProfile(fs.Target, fs.Devices)
		}
	})

	// Append devices that are not currently mounted anywhere
	for uuid, devs := range devicesByUUID {
		if seenUUID[uuid] {
			continue
		}

		var target string
		mounted := false
		for _, dev := range devs {
			mp := strings.TrimSpace(dev.MountPoint)
			if mp != "" {
				target = mp
				mounted = true
				break
			}
		}
		if mounted {
			mounted = isMountPoint(target)
		}

		source := aggregateDeviceSources(devs)
		if source == "" && len(devs) > 0 {
			source = devs[0].Path
		}

		fs := FileSystem{
			Target:        target,
			Source:        source,
			FSType:        "btrfs",
			UUID:          uuid,
			Label:         labelsByUUID[uuid],
			Compression:   "",
			Mounted:       mounted,
			MaxSpace:      0,
			UsedSpace:     0,
			RealMaxSpace:  sizeByUUID[uuid],
			RealUsedSpace: 0,
			Devices:       append([]BtrfsDevice(nil), devs...),
		}

		if fs.Label == "" && fs.Target != "" {
			fs.Label = getBtrfsLabel(fs.Target)
		}

		if usage, ok := usageByUUID[uuid]; ok {
			fs.MaxSpace = usage.maxBytes
			fs.UsedSpace = usage.usedBytes
			if fs.RealMaxSpace == 0 {
				fs.RealMaxSpace = usage.maxBytes
			}
			fs.RealUsedSpace = usage.usedBytes
			if fs.RaidType == "" {
				fs.RaidType = usage.raidProfile
			}
		}

		if fs.RaidType == "" {
			fs.RaidType = detectRaidProfile(fs.Target, fs.Devices)
		}

		findmntOutput.FileSystems = append(findmntOutput.FileSystems, fs)
	}

	return findmntOutput, nil
}

func GetFileSystemByMountPoint(mountPoint string) (*FileSystem, error) {
	mountPoint = strings.TrimSpace(mountPoint)
	if mountPoint == "" {
		return nil, fmt.Errorf("mount point is required")
	}

	allFS, err := GetAllFileSystems()
	if err != nil {
		return nil, err
	}

	var found *FileSystem
	walkFileSystems(allFS.FileSystems, func(fs *FileSystem) {
		if found != nil {
			return
		}
		if fs.Target == mountPoint {
			found = cloneFileSystem(fs)
		}
	})

	if found == nil {
		return nil, fmt.Errorf("mount point %s not found", mountPoint)
	}

	return found, nil
}

func getBtrfsLabel(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}

	cmd := exec.Command("btrfs", "filesystem", "label", target)
	output, err := cmd.Output()
	if err != nil {
		logger.Error(fmt.Sprintf("failed to read btrfs label for %s: %v", target, err))
		return ""
	}
	return strings.TrimSpace(string(output))
}

func cloneFileSystem(fs *FileSystem) *FileSystem {
	if fs == nil {
		return nil
	}
	copy := *fs
	if len(fs.Children) > 0 {
		copy.Children = make([]FileSystem, len(fs.Children))
		for i := range fs.Children {
			copy.Children[i] = *cloneFileSystem(&fs.Children[i])
		}
	}
	if len(fs.Devices) > 0 {
		copy.Devices = append([]BtrfsDevice(nil), fs.Devices...)
	}
	return &copy
}

func walkFileSystems(list []FileSystem, fn func(*FileSystem)) {
	for i := range list {
		fn(&list[i])
		if len(list[i].Children) > 0 {
			walkFileSystems(list[i].Children, fn)
		}
	}
}

func filesystemUsage(mountPoint string) (usageStats, error) {
	var stats usageStats
	mountPoint = strings.TrimSpace(mountPoint)
	if mountPoint == "" {
		return stats, fmt.Errorf("mount point is required")
	}

	info, err := GetFileSystemInfo(mountPoint)
	if err != nil {
		return stats, err
	}

	if info == nil {
		return stats, fmt.Errorf("no filesystem info for %s", mountPoint)
	}

	var total, used int64
	for _, bg := range info.FilesystemDF {
		total += bg.Total
		used += bg.Used
	}

	return usageStats{
		maxBytes:    total,
		usedBytes:   used,
		raidProfile: extractRaidProfile(info),
	}, nil
}

func extractCompressionFromOptions(options string) string {
	if options == "" {
		return ""
	}

	opts := strings.Split(options, ",")
	for _, opt := range opts {
		opt = strings.TrimSpace(opt)
		switch {
		case strings.HasPrefix(opt, "compress-force="):
			return strings.TrimPrefix(opt, "compress-force=") + " (forced)"
		case strings.HasPrefix(opt, "compress="):
			return strings.TrimPrefix(opt, "compress=")
		case opt == "compress":
			return "compress"
		}
	}
	return ""
}

func extractRaidProfile(info *FilesystemDF) string {
	if info == nil {
		return ""
	}

	var (
		bestProfile string
		largestData int64
	)

	for _, bg := range info.FilesystemDF {
		profile := strings.TrimSpace(strings.ToLower(bg.BgProfile))
		if profile == "" {
			continue
		}
		if strings.EqualFold(bg.BgType, "data") && bg.Total >= largestData {
			largestData = bg.Total
			bestProfile = profile
		}
	}

	if bestProfile != "" {
		return bestProfile
	}

	for _, bg := range info.FilesystemDF {
		profile := strings.TrimSpace(strings.ToLower(bg.BgProfile))
		if profile != "" {
			return profile
		}
	}

	return ""
}

func detectRaidProfile(target string, devs []BtrfsDevice) string {
	var paths []string
	if t := strings.TrimSpace(target); t != "" {
		paths = appendIfMissing(paths, t)
	}
	for _, dev := range devs {
		paths = appendIfMissing(paths, dev.Path)
	}

	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}

		usage, err := filesystemUsage(path)
		if err != nil {
			logger.Debug(fmt.Sprintf("failed to detect raid profile for %s: %v", path, err))
			continue
		}
		if usage.raidProfile != "" {
			return usage.raidProfile
		}
	}

	return ""
}

func runFindmnt() (*FindMntOutput, error) {
	cmd := exec.Command("findmnt", "-t", "btrfs", "-o", "TARGET,SOURCE,FSTYPE,OPTIONS,UUID", "-J")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				// No BTRFS filesystems currently mounted
				return &FindMntOutput{}, nil
			}
			return nil, fmt.Errorf("findmnt failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("findmnt failed: %w", err)
	}

	if len(bytes.TrimSpace(output)) == 0 {
		return &FindMntOutput{}, nil
	}

	var result FindMntOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal findmnt output: %w", err)
	}
	return &result, nil
}

// collectBtrfsDevices queries lsblk and returns BTRFS devices grouped by UUID,
// along with their labels and total sizes. Returns maps: devicesByUUID, labelsByUUID, totalSizesByUUID.
func collectBtrfsDevices() (map[string][]BtrfsDevice, map[string]string, map[string]int64, error) {
	cmd := exec.Command("lsblk", "-b", "--json", "-o", "NAME,TYPE,SIZE,FSTYPE,PATH,LABEL,MOUNTPOINT,UUID")
	output, err := cmd.Output()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("lsblk failed: %w", err)
	}

	var resp lsblkResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse lsblk output: %w", err)
	}

	devices := make(map[string][]BtrfsDevice)
	labels := make(map[string]string)
	totalSizes := make(map[string]int64)
	uuidSubCache := make(map[string]string)

	var walk func(lsblkDevice)
	walk = func(dev lsblkDevice) {
		fstype := strings.ToLower(strings.TrimSpace(dev.getFSType()))
		if fstype == "btrfs" {
			uuid := dev.getUUID()
			path := dev.getPath()
			if uuid != "" && path != "" {
				device := BtrfsDevice{
					Name:       dev.Name,
					Path:       path,
					Type:       dev.Type,
					Label:      dev.getLabel(),
					UUID:       uuid,
					SizeBytes:  dev.Size,
					MountPoint: dev.getMountPoint(),
					Mounted:    dev.getMountPoint() != "",
				}
				if cached, ok := uuidSubCache[path]; ok {
					device.UUIDSub = cached
				} else if sub, err := getUUIDSub(path); err == nil && sub != "" {
					device.UUIDSub = sub
					uuidSubCache[path] = sub
				}
				devices[uuid] = append(devices[uuid], device)
				totalSizes[uuid] += device.SizeBytes
				if device.Label != "" && labels[uuid] == "" {
					labels[uuid] = device.Label
				}
			}
		}
		for _, child := range dev.Children {
			walk(child)
		}
	}

	for _, dev := range resp.BlockDevices {
		walk(dev)
	}

	return devices, labels, totalSizes, nil
}

func aggregateDeviceSources(devs []BtrfsDevice) string {
	if len(devs) == 0 {
		return ""
	}
	paths := make([]string, 0, len(devs))
	seen := make(map[string]struct{})
	for _, dev := range devs {
		if dev.Path == "" {
			continue
		}
		if _, ok := seen[dev.Path]; ok {
			continue
		}
		seen[dev.Path] = struct{}{}
		paths = append(paths, dev.Path)
	}
	return strings.Join(paths, ",")
}

func (d lsblkDevice) getFSType() string {
	if d.FSType == nil {
		return ""
	}
	return *d.FSType
}

func (d lsblkDevice) getPath() string {
	if d.Path == nil {
		return ""
	}
	return strings.TrimSpace(*d.Path)
}

func (d lsblkDevice) getLabel() string {
	if d.Label == nil {
		return ""
	}
	return strings.TrimSpace(*d.Label)
}

func (d lsblkDevice) getMountPoint() string {
	if d.MountPoint == nil {
		return ""
	}
	return strings.TrimSpace(*d.MountPoint)
}

func (d lsblkDevice) getUUID() string {
	if d.UUID == nil {
		return ""
	}
	return strings.TrimSpace(*d.UUID)
}

func getUUIDSub(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}

	cmd := exec.Command("blkid", "-s", "UUID_SUB", "-o", "value", path)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// UUID_SUB is optional; missing info should not be fatal
			logger.Error(fmt.Sprintf("blkid UUID_SUB failed for %s: %s", path, strings.TrimSpace(string(exitErr.Stderr))))
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func GetMountPointFromUUID(fsid string) (string, error) {
	// Step 1: run `btrfs filesystem show`
	cmd := exec.Command("btrfs", "filesystem", "show", fsid)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to query btrfs fsid: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	var devices []string

	// Step 2: extract device paths
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "devid") {
			parts := strings.Fields(line)

			// procurar explicitamente pelo token "path"
			for i := 0; i < len(parts); i++ {
				if parts[i] == "path" && i+1 < len(parts) {
					devices = append(devices, parts[i+1]) // ex: /dev/loop8
					break
				}
			}
		}
	}

	if len(devices) == 0 {
		return "", fmt.Errorf("no devices found for FSID %s", fsid)
	}

	// Step 3: try findmnt for each device
	for _, dev := range devices {
		cmd2 := exec.Command("findmnt", "-n", "-o", "TARGET", "--source", dev)
		mpOut, err := cmd2.Output()
		if err != nil {
			continue // se um falhar tenta o próximo
		}

		mountPoint := strings.TrimSpace(string(mpOut))
		if mountPoint != "" {
			return mountPoint, nil
		}
	}

	return "", fmt.Errorf("filesystem %s is not mounted", fsid)
}
