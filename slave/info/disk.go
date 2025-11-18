package info

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
	"golang.org/x/sys/unix"
)

type DiskInfoStruct struct{}

var DiskInfo DiskInfoStruct

type DiskStruct struct {
	Device      string   // Device name (e.g., /dev/sda1)
	MountPoint  string   // Mount point path (e.g., /)
	Fstype      string   // File system type (e.g., ext4, xfs, ntfs)
	Total       uint64   // Total size in bytes
	Free        uint64   // Free space in bytes
	Used        uint64   // Used space in bytes
	UsedPercent float64  // Used percentage
	Opts        []string // Mount options
}

func (d *DiskInfoStruct) GetDisks() ([]DiskStruct, error) {
	partitions, err := disk.Partitions(false) // false = exclude pseudo filesystems
	if err != nil {
		return nil, err
	}

	var disks []DiskStruct

	for _, partition := range partitions {
		usage, err := disk.Usage(partition.Mountpoint)
		if err != nil {
			// Skip partitions we can't read
			continue
		}

		diskInfo := DiskStruct{
			Device:      partition.Device,
			MountPoint:  partition.Mountpoint,
			Fstype:      partition.Fstype,
			Total:       usage.Total,
			Free:        usage.Free,
			Used:        usage.Used,
			UsedPercent: usage.UsedPercent,
			Opts:        partition.Opts,
		}

		disks = append(disks, diskInfo)
	}

	return disks, nil
}

// GetDiskUsage returns the usage percentage for each disk partition
func (d *DiskInfoStruct) GetDiskUsage() (map[string]float64, error) {
	partitions, err := disk.Partitions(false)
	if err != nil {
		return nil, err
	}

	usage := make(map[string]float64)

	for _, partition := range partitions {
		usageStats, err := disk.Usage(partition.Mountpoint)
		if err != nil {
			continue
		}
		usage[partition.Device] = usageStats.UsedPercent
	}

	return usage, nil
}

// DiskIOStruct contains disk I/O statistics
type DiskIOStruct struct {
	Device           string // Device name
	ReadCount        uint64 // Number of read operations
	WriteCount       uint64 // Number of write operations
	ReadBytes        uint64 // Bytes read
	WriteBytes       uint64 // Bytes written
	ReadTime         uint64 // Time spent reading (ms)
	WriteTime        uint64 // Time spent writing (ms)
	IopsInProgress   uint64 // I/O operations in progress
	IoTime           uint64 // Time spent doing I/Os (ms)
	WeightedIO       uint64 // Weighted time spent doing I/Os (ms)
	MergedReadCount  uint64 // Number of merged reads
	MergedWriteCount uint64 // Number of merged writes
}

// DiskCacheStruct captures cache pressure metrics that are tied to a device.
type DiskCacheStruct struct {
	Device         string `json:"device"`
	DirtyKB        uint64 `json:"dirty_kb"`
	WritebackKB    uint64 `json:"writeback_kb"`
	WritebackTmpKB uint64 `json:"writeback_tmp_kb"`
}

// NFSServerCacheStats exposes NFS daemon cache counters.
type NFSServerCacheStats struct {
	ReplyCache map[string]uint64 `json:"reply_cache"`
	FileCache  map[string]uint64 `json:"file_cache"`
}

// GetDiskIOUsage returns I/O statistics for each disk
func (d *DiskInfoStruct) GetDiskIOUsage() ([]DiskIOStruct, error) {
	ioCounters, err := disk.IOCounters()
	if err != nil {
		return nil, err
	}

	var diskIOs []DiskIOStruct

	for device, io := range ioCounters {
		diskIO := DiskIOStruct{
			Device:           device,
			ReadCount:        io.ReadCount,
			WriteCount:       io.WriteCount,
			ReadBytes:        io.ReadBytes,
			WriteBytes:       io.WriteBytes,
			ReadTime:         io.ReadTime,
			WriteTime:        io.WriteTime,
			IopsInProgress:   io.IopsInProgress,
			IoTime:           io.IoTime,
			WeightedIO:       io.WeightedIO,
			MergedReadCount:  io.MergedReadCount,
			MergedWriteCount: io.MergedWriteCount,
		}
		diskIOs = append(diskIOs, diskIO)
	}

	return diskIOs, nil
}

// GetDiskCacheStats gathers backing-device cache counters (Dirty/Writeback) per disk when supported by the kernel.
func (d *DiskInfoStruct) GetDiskCacheStats(disks []DiskStruct) []DiskCacheStruct {
	cacheStats := make([]DiskCacheStruct, 0, len(disks))

	for _, diskEntry := range disks {
		cacheEntry := DiskCacheStruct{Device: diskEntry.Device}
		bdiPath, err := deviceToBDI(diskEntry.Device)
		if err != nil {
			cacheStats = append(cacheStats, cacheEntry)
			continue
		}

		base := filepath.Join("/sys/class/bdi", bdiPath)
		cacheEntry.DirtyKB = readUintFileToKB(filepath.Join(base, "dirty"))
		cacheEntry.WritebackKB = readUintFileToKB(filepath.Join(base, "writeback"))
		cacheEntry.WritebackTmpKB = readUintFileToKB(filepath.Join(base, "writeback_tmp"))
		cacheStats = append(cacheStats, cacheEntry)
	}

	return cacheStats
}

// GetNFSServerCacheStats returns the NFS server reply/file cache counters when the kernel exposes them.
func (d *DiskInfoStruct) GetNFSServerCacheStats() *NFSServerCacheStats {
	replyStats, err := parseKeyValueFile("/proc/fs/nfsd/reply_cache_stats")
	if err != nil && !os.IsNotExist(err) {
		return nil
	}
	fileStats, err := parseKeyValueFile("/proc/fs/nfsd/filecache")
	if err != nil && !os.IsNotExist(err) {
		return nil
	}

	if len(replyStats) == 0 && len(fileStats) == 0 {
		return nil
	}

	return &NFSServerCacheStats{
		ReplyCache: replyStats,
		FileCache:  fileStats,
	}
}

func deviceToBDI(device string) (string, error) {
	var stat unix.Stat_t
	if err := unix.Stat(device, &stat); err != nil {
		return "", err
	}

	return fmt.Sprintf("%d:%d", unix.Major(stat.Rdev), unix.Minor(stat.Rdev)), nil
}

func readUintFileToKB(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}

	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}

	return value / 1024
}

func parseKeyValueFile(path string) (map[string]uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	result := make(map[string]uint64)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		fields := strings.Fields(parts[1])
		if len(fields) == 0 {
			continue
		}

		value, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		result[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func getSystemCacheStats() *DiskCacheStruct {
	values, err := parseMeminfoFields([]string{"Dirty", "Writeback", "WritebackTmp"})
	if err != nil || len(values) == 0 {
		return nil
	}

	return &DiskCacheStruct{
		Device:         "system",
		DirtyKB:        values["Dirty"],
		WritebackKB:    values["Writeback"],
		WritebackTmpKB: values["WritebackTmp"],
	}
}

func parseMeminfoFields(fields []string) (map[string]uint64, error) {
	fieldSet := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		fieldSet[f] = struct{}{}
	}

	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := make(map[string]uint64)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		field := strings.TrimSuffix(parts[0], ":")
		if _, ok := fieldSet[field]; !ok {
			continue
		}

		value, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return nil, err
		}
		values[field] = value
		if len(values) == len(fieldSet) {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return values, nil
}

// returns write/read/err
func (d *DiskInfoStruct) GetWriteReadSpeed(folderMount string) (float64, float64, error) {
	//check if folder exists
	_, err := os.Stat(folderMount)
	if err != nil {
		return 0, 0, err
	}

	getInfo := func(typeRW string) (float64, error) {
		if typeRW != "read" && typeRW != "write" {
			return 0, fmt.Errorf("typeRW needs to be write or read")
		}
		cmd := exec.Command("fio",
			"--name=test",
			"--ioengine=sync",
			"--rw="+typeRW,
			"--bs=1M",
			"--size=1G",
			"--directory="+folderMount,
			"--runtime=30s",
			"--time_based",
			"--output-format=json",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return 0, fmt.Errorf("fio failed: %w\n--- fio output ---\n%s", err, string(out))
		}

		//out is a json

		type JobResult struct {
			Read struct {
				BwBytes uint64 `json:"bw_bytes"`
			} `json:"read"`
			Write struct {
				BwBytes uint64 `json:"bw_bytes"`
			} `json:"write"`
		}

		type FioOutput struct {
			Jobs []JobResult `json:"jobs"`
		}

		var fioResult FioOutput
		if err := json.Unmarshal(out, &fioResult); err != nil {
			return 0, fmt.Errorf("failed to parse fio output: %w", err)
		}

		if len(fioResult.Jobs) == 0 {
			return 0, fmt.Errorf("no jobs found in fio output")
		}

		var bwBytes uint64
		if typeRW == "read" {
			bwBytes = fioResult.Jobs[0].Read.BwBytes
		} else {
			bwBytes = fioResult.Jobs[0].Write.BwBytes
		}

		// Delete the test file created by fio
		testFile := folderMount + "/test.0.0"
		os.Remove(testFile)

		return float64(bwBytes) / (1024 * 1024), nil
	}

	read, err := getInfo("read")
	if err != nil {
		return 0, 0, err
	}

	write, err := getInfo("write")
	if err != nil {
		return 0, 0, err
	}

	return write, read, nil
}

// DiskSummary groups together disk inventory, usage, and IO stats.
type DiskSummary struct {
	Disks          []DiskStruct         `json:"disks"`
	Usage          map[string]float64   `json:"usage"`
	IO             []DiskIOStruct       `json:"io"`
	Cache          []DiskCacheStruct    `json:"cache"`
	SystemCache    *DiskCacheStruct     `json:"system_cache"`
	NFSServerCache *NFSServerCacheStats `json:"nfs_server_cache"`
}

// GetDiskSummary returns a consolidated view of disk information by calling
// GetDisks, GetDiskUsage, and GetDiskIOUsage. It returns the first error encountered.
func (d *DiskInfoStruct) GetDiskSummary() (*DiskSummary, error) {
	disks, err := d.GetDisks()
	if err != nil {
		return nil, err
	}

	usage, err := d.GetDiskUsage()
	if err != nil {
		return nil, err
	}

	ioStats, err := d.GetDiskIOUsage()
	if err != nil {
		return nil, err
	}

	cacheStats := d.GetDiskCacheStats(disks)
	systemCache := getSystemCacheStats()
	nfsCache := d.GetNFSServerCacheStats()

	return &DiskSummary{
		Disks:          disks,
		Usage:          usage,
		IO:             ioStats,
		Cache:          cacheStats,
		SystemCache:    systemCache,
		NFSServerCache: nfsCache,
	}, nil
}
