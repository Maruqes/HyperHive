package info

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/shirou/gopsutil/v4/disk"
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
