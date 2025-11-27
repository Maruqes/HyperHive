package btrfs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Maruqes/512SvMan/logger"
)

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

type DeviceStat struct {
	Device         string `json:"device"`
	DevID          int    `json:"devid"`
	WriteIOErrs    int    `json:"write_io_errs"`
	ReadIOErrs     int    `json:"read_io_errs"`
	FlushIOErrs    int    `json:"flush_io_errs"`
	CorruptionErrs int    `json:"corruption_errs"`
	GenerationErrs int    `json:"generation_errs"`
	BalanceStatus  string `json:"balance_status"`

	FSUUID          string `json:"fs_uuid,omitempty"`
	FSLabel         string `json:"fs_label,omitempty"`
	DeviceSizeBytes uint64 `json:"device_size_bytes,omitempty"`
	DeviceUsedBytes uint64 `json:"device_used_bytes,omitempty"`
	DeviceMissing   bool   `json:"device_missing,omitempty"`
}

type DeviceStats struct {
	Header struct {
		Version string `json:"version"`
	} `json:"__header"`

	// Info de FS ao nível global (útil se quiseres)
	FSUUID       string `json:"fs_uuid,omitempty"`
	FSLabel      string `json:"fs_label,omitempty"`
	TotalDevices int    `json:"total_devices,omitempty"`

	DeviceStats []DeviceStat `json:"device-stats"`
}

type BtrfsDeviceInfo struct {
	DevID   int64  `json:"devid"`
	Size    uint64 `json:"size_bytes"`
	Used    uint64 `json:"used_bytes"`
	Path    string `json:"path"`
	Missing bool   `json:"missing"`
}

type BtrfsFilesystemInfo struct {
	Label        string            `json:"label"`
	UUID         string            `json:"uuid"`
	TotalDevices int               `json:"total_devices"`
	FSBytesUsed  uint64            `json:"fs_bytes_used"`
	Devices      []BtrfsDeviceInfo `json:"devices"`
}

func GetBtrfsFilesystemInfo(mountpoint string) (*BtrfsFilesystemInfo, error) {
	out, err := exec.Command("btrfs", "filesystem", "show", "-m", mountpoint).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("btrfs filesystem show failed: %w: %s", err, string(out))
	}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	fsInfo := &BtrfsFilesystemInfo{}
	var devices []BtrfsDeviceInfo

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Header: Label + UUID
		if strings.HasPrefix(line, "Label:") {
			// Label: 'test'  uuid: e0607e63-...
			parts := strings.Split(line, "uuid:")
			if len(parts) == 2 {
				labelPart := strings.TrimSpace(strings.TrimPrefix(parts[0], "Label:"))
				fsInfo.Label = strings.Trim(labelPart, "' ")
				fsInfo.UUID = strings.TrimSpace(parts[1])
			}
			continue
		}

		// Total devices + FS bytes used
		if strings.Contains(line, "Total devices") {
			// Total devices 3 FS bytes used 24.84GiB
			parts := strings.Fields(line)
			if len(parts) >= 7 {
				total, _ := strconv.Atoi(parts[2]) // "3"
				fsInfo.TotalDevices = total
				fsInfo.FSBytesUsed = parseSize(parts[6]) // "24.84GiB"
			}
			continue
		}

		// ✅ Device lines
		if strings.HasPrefix(line, "devid") {
			// devid 1 size 3.64TiB used 27.00GiB path /dev/sdb
			parts := strings.Fields(line)
			if len(parts) < 8 {
				// formato estranho, ignora
				continue
			}

			devID, _ := strconv.ParseInt(parts[1], 10, 64) // "1"
			sizeBytes := parseSize(parts[3])               // "3.64TiB"
			usedBytes := parseSize(parts[5])               // "27.00GiB"
			path := parts[7]                               // "/dev/sdb" ou "MISSING"
			missing := path == "MISSING"

			devices = append(devices, BtrfsDeviceInfo{
				DevID:   devID,
				Size:    sizeBytes,
				Used:    usedBytes,
				Path:    path,
				Missing: missing,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	fsInfo.Devices = devices
	return fsInfo, nil
}

func parseSize(s string) uint64 {
	s = strings.TrimSpace(s)
	multipliers := map[string]float64{
		"TiB": 1024 * 1024 * 1024 * 1024,
		"GiB": 1024 * 1024 * 1024,
		"MiB": 1024 * 1024,
		"KiB": 1024,
		"B":   1,
	}

	for unit, mul := range multipliers {
		if strings.HasSuffix(s, unit) {
			numStr := strings.TrimSuffix(s, unit)
			num, _ := strconv.ParseFloat(numStr, 64)
			return uint64(num * mul)
		}
	}

	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}

func (d *DeviceStats) Print() {
	if d == nil {
		fmt.Println("No device stats available.")
		return
	}

	fmt.Printf("BTRFS device stats (version %s)\n", d.Header.Version)
	if d.FSUUID != "" {
		fmt.Printf("  FS Label: %s  UUID: %s  TotalDevices: %d\n", d.FSLabel, d.FSUUID, d.TotalDevices)
	}

	if len(d.DeviceStats) == 0 {
		fmt.Println("  <no devices>")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DEVICE\tDEVID\tSIZE_BYTES\tUSED_BYTES\tMISSING\tWRITE_ERRS\tREAD_ERRS\tFLUSH_ERRS\tCORRUPTION_ERRS\tGENERATION_ERRS\tBALANCE_STATUS")
	for _, stat := range d.DeviceStats {
		fmt.Fprintf(
			w,
			"%s\t%d\t%d\t%d\t%v\t%d\t%d\t%d\t%d\t%d\t%s\n",
			stat.Device,
			stat.DevID,
			stat.DeviceSizeBytes,
			stat.DeviceUsedBytes,
			stat.DeviceMissing,
			stat.WriteIOErrs,
			stat.ReadIOErrs,
			stat.FlushIOErrs,
			stat.CorruptionErrs,
			stat.GenerationErrs,
			stat.BalanceStatus,
		)
	}
	w.Flush()
}

func GetFileSystemStats(mountPoint string) (*DeviceStats, error) {
	cmd := exec.Command("btrfs", "--format", "json", "device", "stats", mountPoint)
	output, err := cmd.Output()
	if err != nil {
		// Try to get stderr for better error message
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("failed to get device stats: %w, stderr: %s", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to get device stats: %w", err)
	}

	var stats DeviceStats
	if err := json.Unmarshal(output, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal device stats: %w (output: %s)", err, string(output))
	}

	// Balance status (global por FS)
	balanceStatus, _ := GetBalanceStatus(mountPoint)
	for i := range stats.DeviceStats {
		stats.DeviceStats[i].BalanceStatus = balanceStatus
	}

	// info do `btrfs filesystem show -m`
	fsInfo, err := GetBtrfsFilesystemInfo(mountPoint)
	if err == nil {
		stats.FSUUID = fsInfo.UUID
		stats.FSLabel = fsInfo.Label
		stats.TotalDevices = fsInfo.TotalDevices

		devByID := make(map[int64]BtrfsDeviceInfo, len(fsInfo.Devices))
		for _, d := range fsInfo.Devices {
			devByID[d.DevID] = d
		}

		for i := range stats.DeviceStats {
			ds := &stats.DeviceStats[i]
			if dev, ok := devByID[int64(ds.DevID)]; ok {
				ds.FSUUID = fsInfo.UUID
				ds.FSLabel = fsInfo.Label
				ds.DeviceSizeBytes = dev.Size
				ds.DeviceUsedBytes = dev.Used
				ds.DeviceMissing = dev.Missing
			}
		}
	} else {
		logger.Error("nao deu para ver o btrfs filesystem show -m")
	}
	return &stats, nil
}

func GetBalanceStatus(mountPoint string) (string, error) {
	cmd := exec.Command("btrfs", "balance", "status", mountPoint)
	output, err := cmd.CombinedOutput()
	status := parseBalanceStatusText(string(output))

	if err != nil {
		// Some environments return non-zero even while providing useful status text.
		// If we have a parsed status, surface it instead of hiding it behind the error.
		if status != "" {
			return status, nil
		}

		return "", fmt.Errorf(
			"failed to get balance status for %s: %w (output: %s)",
			mountPoint,
			err,
			strings.TrimSpace(string(output)),
		)
	}

	return status, nil
}

// parseBalanceStatusText extracts a useful balance status string from the CLI output.
// When progress information is present (second line), it returns that line;
// otherwise it returns the first non-empty line.
func parseBalanceStatusText(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}

	if len(cleaned) == 0 {
		return ""
	}

	// Prefer the progress line when available
	if len(cleaned) > 1 {
		return cleaned[len(cleaned)-1]
	}

	return cleaned[0]
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

type ScrubStatus struct {
	UUID          string   `json:"uuid"`
	Path          string   `json:"path"`                   // o mountpoint/dispositivo que passaste
	Status        string   `json:"status"`                 // running / finished / aborted / ...
	StartedAt     string   `json:"started_at"`             // string crua do btrfs
	Duration      string   `json:"duration"`               // ex: "0:01:23"
	TimeLeft      string   `json:"time_left"`              // ex: "0:05:40"
	TotalToScrub  string   `json:"total_to_scrub"`         // ex: "500.00GiB"
	BytesScrubbed string   `json:"bytes_scrubbed"`         // ex: "150.00GiB  (30.00%)"
	Rate          string   `json:"rate"`                   // ex: "1.14GiB/s"
	ErrorSummary  string   `json:"error_summary"`          // ex: "no errors found"
	PercentDone   *float64 `json:"percent_done,omitempty"` // 30.00, se conseguir extrair
}

func GetScrubStats(mntPoint string) (ScrubStatus, error) {
	var status ScrubStatus
	status.Path = mntPoint

	cmd := exec.Command("btrfs", "scrub", "status", mntPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return status, fmt.Errorf(
			"failed to get scrub status for %s: %w (output: %s)",
			mntPoint,
			err,
			strings.TrimSpace(string(output)),
		)
	}

	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch {
		case strings.HasPrefix(key, "UUID"):
			status.UUID = value

		case strings.HasPrefix(key, "Scrub started"):
			status.StartedAt = value

		case strings.HasPrefix(key, "Status"):
			status.Status = strings.ToLower(value)

		case strings.HasPrefix(key, "Duration"):
			status.Duration = value

		case strings.HasPrefix(key, "Time left"):
			status.TimeLeft = value

		case strings.HasPrefix(key, "Total to scrub"):
			status.TotalToScrub = value

		case strings.HasPrefix(key, "Bytes scrubbed"):
			status.BytesScrubbed = value
			// tentar extrair "(30.00%)" -> 30.00
			if p, ok := extractPercent(value); ok {
				status.PercentDone = &p
			}

		case strings.HasPrefix(key, "Rate"):
			status.Rate = value

		case strings.HasPrefix(key, "Error summary"):
			status.ErrorSummary = value
		}
	}

	return status, nil
}

func extractPercent(s string) (float64, bool) {
	start := strings.Index(s, "(")
	end := strings.Index(s, "%")
	if start == -1 || end == -1 || end <= start+1 {
		return 0, false
	}

	inner := s[start+1 : end] // "30.00"
	inner = strings.TrimSpace(inner)

	v, err := strconv.ParseFloat(inner, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

type btrfsErrorEvent struct {
	Target    string `json:"target"`
	Device    string `json:"device"`
	DevID     int    `json:"devid"`
	ErrorType string `json:"error_type"`
	Count     int    `json:"count"`
}

func CheckBtrfs(callErrs func(...any)) {

	raids, _ := GetAllFileSystems()
	if raids == nil || len(raids.FileSystems) == 0 {
		return
	}

	for _, raid := range raids.FileSystems {
		if !raid.Mounted || strings.TrimSpace(raid.Target) == "" {
			continue
		}

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
}

func CheckBtrfsGoLoop(callErrs func(...any)) {

	go func() {
		for {
			CheckBtrfs(callErrs)
			time.Sleep(30 * time.Second)
		}
	}()
}
