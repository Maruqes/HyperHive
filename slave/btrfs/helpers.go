package btrfs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
)

type MinDisk struct {
	Path       string  `json:"path"`       // /dev/sda
	Name       string  `json:"name"`       // sda
	Model      string  `json:"model"`      // "Samsung SSD 860 EVO"
	Vendor     string  `json:"vendor"`     // "Samsung"
	Serial     string  `json:"serial"`     // "S3Z8NX0K123456A"
	Rotational bool    `json:"rotational"` // true = HDD, false = SSD
	SizeGB     float64 `json:"sizeGb"`     // tamanho em GB
	Mounted    bool    `json:"mounted"`    // está em uso/montado
	ByID       string  `json:"byId"`       // /dev/disk/by-id/ata-...
	Transport  string  `json:"transport"`  // sata, nvme, usb, virtio, ...
	PCIPath    string  `json:"pciPath"`    // /sys/block/sda/device
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

func parseInt64(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}

	// 1) tentar como número
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}

	// 2) tentar como string
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		// retirar espaços só para o caso
		s = strings.TrimSpace(s)
		if v, err2 := strconv.ParseInt(s, 10, 64); err2 == nil {
			return v
		}
	}

	return 0
}

func parseBoolFromInt(raw json.RawMessage) bool {
	n := parseInt64(raw)
	return n == 1
}

// devolve o primeiro /dev/disk/by-id/* que aponta para este device (ex: "sda")
func findByID(name string) string {
	entries, err := os.ReadDir("/dev/disk/by-id")
	if err != nil {
		return ""
	}

	for _, e := range entries {
		fullPath := filepath.Join("/dev/disk/by-id", e.Name())
		target, err := os.Readlink(fullPath)
		if err != nil {
			continue
		}
		// target costuma ser algo tipo "../../sda"
		if strings.Contains(target, name) {
			return fullPath
		}
	}

	return ""
}

// GetAllDisks devolve todos os discos físicos visíveis via lsblk,
// marcando também os que já estão em uso por BTRFS ou montados.
//
// - test = true → não filtra por TYPE (permite ver "loop", etc.)
// - test = false → só TYPE == "disk"
func GetAllDisks(test bool) ([]MinDisk, error) {
	// Map of devices that belong to any BTRFS filesystem
	btrfsInUse := make(map[string]bool)
	if devsByUUID, _, _, err := collectBtrfsDevices(); err == nil {
		for _, devs := range devsByUUID {
			for _, d := range devs {
				path := strings.TrimSpace(d.Path)
				if path != "" {
					btrfsInUse[path] = true
				}
			}
		}
	} else {
		logger.Debug(fmt.Sprintf("collectBtrfsDevices failed: %v", err))
	}

	// lsblk in JSON with SIZE in bytes (-b) to list all block devices
	cmd := exec.Command("lsblk", "-d", "-b", "-J", "-o", "NAME,PATH,MODEL,VENDOR,SERIAL,SIZE,ROTA,TYPE,TRAN")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list disks with lsblk: %w", err)
	}

	var parsed struct {
		Blockdevices []struct {
			Name   string          `json:"name"`
			Path   string          `json:"path"`
			Model  string          `json:"model"`
			Vendor string          `json:"vendor"`
			Serial string          `json:"serial"`
			Size   json.RawMessage `json:"size"` // can be number or string
			Rota   json.RawMessage `json:"rota"`
			Type   string          `json:"type"`
			Tran   string          `json:"tran"`
		} `json:"blockdevices"`
	}

	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, fmt.Errorf("json parse error: %w", err)
	}

	var disks []MinDisk
	for _, d := range parsed.Blockdevices {
		if d.Type != "disk" && !test {
			continue
		}

		path := d.Path
		if strings.TrimSpace(path) == "" {
			path = "/dev/" + d.Name
		}

		sizeBytes := parseInt64(d.Size)
		sizeGB := float64(sizeBytes) / (1024 * 1024 * 1024)

		mounted := btrfsInUse[path] || isMounted(path)

		md := MinDisk{
			Path:       path,
			Name:       d.Name,
			Model:      strings.TrimSpace(d.Model),
			Vendor:     strings.TrimSpace(d.Vendor),
			Serial:     strings.TrimSpace(d.Serial),
			Rotational: parseBoolFromInt(d.Rota),
			SizeGB:     sizeGB,
			Mounted:    mounted,
			ByID:       findByID(d.Name),
			Transport:  strings.TrimSpace(d.Tran),
			PCIPath:    "/sys/block/" + d.Name + "/device",
		}

		disks = append(disks, md)
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
	// Step 1: parse /proc/mounts to get mounted btrfs filesystems (Target, Source, FSType, Options)
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/mounts: %w", err)
	}

	var result FindMntOutput
	seenUUID := make(map[string]bool)

	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// format: source target fstype options ...
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}
		src := parts[0]
		target := parts[1]
		fstype := parts[2]
		options := parts[3]

		if fstype != "btrfs" {
			continue
		}

		fs := FileSystem{
			Target:      target,
			Source:      src,
			FSType:      fstype,
			Options:     options,
			Mounted:     true,
			Devices:     []BtrfsDevice{},
			Compression: extractCompressionFromOptions(options),
		}

		// Enrich mounted filesystem with btrfs commands
		if info, err := GetFileSystemInfo(target); err == nil && info != nil {
			// compute totals
			var total, used int64
			for _, bg := range info.FilesystemDF {
				total += bg.Total
				used += bg.Used
			}
			fs.MaxSpace = total
			fs.UsedSpace = used
			fs.RealMaxSpace = total
			fs.RealUsedSpace = used
			fs.RaidType = extractRaidProfile(info)
		} else if err != nil {
			logger.Error(fmt.Sprintf("GetFileSystemInfo failed for %s: %v", target, err))
		}

		if binfo, err := GetBtrfsFilesystemInfo(target); err == nil && binfo != nil {
			fs.UUID = binfo.UUID
			if fs.Label == "" {
				fs.Label = binfo.Label
			}
			var total uint64
			for _, d := range binfo.Devices {
				total += d.Size
				dev := BtrfsDevice{
					Name:       filepath.Base(d.Path),
					Path:       d.Path,
					Type:       "",
					Label:      "",
					UUID:       "",
					SizeBytes:  int64(d.Size),
					MountPoint: "",
					Mounted:    d.Path != "MISSING",
				}
				fs.Devices = append(fs.Devices, dev)
			}
			fs.RealMaxSpace = int64(total)
		} else if err != nil {
			logger.Error(fmt.Sprintf("GetBtrfsFilesystemInfo failed for %s: %v", target, err))
		}

		if fs.UUID != "" {
			seenUUID[fs.UUID] = true
		}
		result.FileSystems = append(result.FileSystems, fs)
	}

	// Step 2: include filesystems known to the kernel from `btrfs filesystem show` that are not mounted
	cmd := exec.Command("btrfs", "filesystem", "show")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// if there's no output, just return what we have
		if len(bytes.TrimSpace(out)) == 0 {
			return &result, nil
		}
		return nil, fmt.Errorf("btrfs filesystem show failed: %w: %s", err, string(out))
	}

	lines := strings.Split(string(out), "\n")
	var cur *FileSystem

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			if cur != nil {
				// if not seen, append
				if cur.UUID == "" || !seenUUID[cur.UUID] {
					result.FileSystems = append(result.FileSystems, *cur)
				}
				cur = nil
			}
			continue
		}

		if strings.HasPrefix(line, "Label:") || strings.Contains(line, "uuid:") {
			if cur != nil {
				if cur.UUID == "" || !seenUUID[cur.UUID] {
					result.FileSystems = append(result.FileSystems, *cur)
				}
			}
			cur = &FileSystem{FSType: "btrfs", Devices: []BtrfsDevice{}}
			if strings.HasPrefix(line, "Label:") {
				parts := strings.Split(line, "uuid:")
				if len(parts) == 2 {
					labelPart := strings.TrimSpace(strings.TrimPrefix(parts[0], "Label:"))
					cur.Label = strings.Trim(labelPart, "' ")
					cur.UUID = strings.TrimSpace(parts[1])
				} else if strings.Contains(line, "uuid:") {
					p := strings.Split(line, "uuid:")
					cur.UUID = strings.TrimSpace(p[len(p)-1])
				}
			} else if strings.Contains(line, "uuid:") {
				parts := strings.Split(line, "uuid:")
				cur.UUID = strings.TrimSpace(parts[len(parts)-1])
			}
			continue
		}

		if strings.HasPrefix(line, "devid") && cur != nil {
			parts := strings.Fields(line)
			if len(parts) < 8 {
				continue
			}
			path := parts[len(parts)-1]
			if path == "MISSING" {
				continue
			}
			sizeBytes := int64(parseSize(parts[3]))
			dev := BtrfsDevice{
				Name:      filepath.Base(path),
				Path:      path,
				SizeBytes: sizeBytes,
				Mounted:   false,
			}
			cur.Devices = append(cur.Devices, dev)
			cur.RealMaxSpace += sizeBytes
			continue
		}

		if strings.HasPrefix(line, "Total devices") && cur != nil {
			parts := strings.Fields(line)
			for i := 0; i < len(parts)-1; i++ {
				if parts[i] == "used" && i+1 < len(parts) {
					cur.RealUsedSpace = int64(parseSize(parts[i+1]))
				}
			}
			continue
		}
	}

	if cur != nil {
		if cur.UUID == "" || !seenUUID[cur.UUID] {
			result.FileSystems = append(result.FileSystems, *cur)
		}
	}

	// For mounted filesystems, ensure RaidType and Compression were filled (best-effort)
	for i := range result.FileSystems {
		if result.FileSystems[i].Mounted {
			if result.FileSystems[i].RaidType == "" {
				// try to get raid from filesystem df
				if info, err := GetFileSystemInfo(result.FileSystems[i].Target); err == nil && info != nil {
					result.FileSystems[i].RaidType = extractRaidProfile(info)
				}
			}
			if result.FileSystems[i].Compression == "" {
				result.FileSystems[i].Compression = extractCompressionFromOptions(result.FileSystems[i].Options)
			}
		}
	}

	return &result, nil
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
