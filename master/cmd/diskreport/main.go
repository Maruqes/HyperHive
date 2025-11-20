package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func main() {
	var devices multiFlag
	var runBadblocks bool
	var jsonOut bool

	flag.Var(&devices, "device", "Disk device name (`sda`, `nvme0n1`) or path (`/dev/sda`). Repeatable.")
	flag.BoolVar(&runBadblocks, "surface-scan", false, "Run a read-only `badblocks -sv` surface scan on each disk (requires root).")
	flag.BoolVar(&jsonOut, "json", false, "Emit the report as JSON instead of plain text.")
	flag.Parse()

	if runBadblocks && os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "warning: surface scan requires root privileges; continuing but it may fail")
	}

	detected, err := detectDisks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to list disks: %v\n", err)
		os.Exit(1)
	}

	targets, err := resolveTargets(detected, devices)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to resolve targets: %v\n", err)
		os.Exit(1)
	}

	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "no disks found")
		os.Exit(1)
	}

	var reports []*DiskReport
	for _, disk := range targets {
		report, err := buildDiskReport(disk, runBadblocks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %s: %v\n", disk.Path, err)
			continue
		}
		reports = append(reports, report)
	}

	if len(reports) == 0 {
		fmt.Fprintln(os.Stderr, "no disk reports were produced")
		os.Exit(1)
	}

	if jsonOut {
		buf, err := json.MarshalIndent(reports, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to marshal JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(buf))
		return
	}

	printReport(reports)
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			*m = append(*m, part)
		}
	}
	return nil
}

type diskDevice struct {
	Name      string
	Path      string
	Model     string
	SizeBytes uint64
}

type DiskReport struct {
	Path                 string           `json:"path"`
	Name                 string           `json:"name,omitempty"`
	Model                string           `json:"model,omitempty"`
	Serial               string           `json:"serial,omitempty"`
	Firmware             string           `json:"firmware,omitempty"`
	SizeBytes            uint64           `json:"size_bytes,omitempty"`
	SizeHuman            string           `json:"size,omitempty"`
	CapacityRaw          string           `json:"capacity_raw,omitempty"`
	SectorSize           string           `json:"sector_size,omitempty"`
	Rotation             string           `json:"rotation,omitempty"`
	SmartSupport         string           `json:"smart_support,omitempty"`
	SmartEnabled         bool             `json:"smart_enabled"`
	SmartStatus          string           `json:"smart_status,omitempty"`
	TemperatureCelsius   int              `json:"temperature_celsius,omitempty"`
	ReallocatedSectors   uint64           `json:"reallocated_sectors,omitempty"`
	PendingSectors       uint64           `json:"pending_sectors,omitempty"`
	OfflineUncorrectable uint64           `json:"offline_uncorrectable,omitempty"`
	SelfTests            []SelfTestEntry  `json:"self_tests,omitempty"`
	Badblocks            *BadblocksResult `json:"badblocks,omitempty"`
	Notes                []string         `json:"notes,omitempty"`
}

type SelfTestEntry struct {
	Num         int    `json:"num"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	Remaining   string `json:"remaining,omitempty"`
	Lifetime    string `json:"lifetime,omitempty"`
	LBA         string `json:"lba,omitempty"`
}

type BadblocksResult struct {
	Ran      bool   `json:"ran"`
	Success  bool   `json:"success"`
	Duration string `json:"duration,omitempty"`
	Message  string `json:"message,omitempty"`
	Error    string `json:"error,omitempty"`
}

func resolveTargets(detected []diskDevice, requested []string) ([]diskDevice, error) {
	if len(requested) == 0 {
		return detected, nil
	}

	lookup := make(map[string]diskDevice)
	for _, d := range detected {
		lookup[d.Path] = d
		lookup[d.Name] = d
	}

	var targets []diskDevice
	for _, raw := range requested {
		path := normalizeDevicePath(raw)
		if d, ok := lookup[path]; ok {
			targets = appendIfMissing(targets, d)
			continue
		}

		if !filepath.IsAbs(raw) {
			if d, ok2 := lookup[raw]; ok2 {
				targets = appendIfMissing(targets, d)
				continue
			}
		}

		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("device %q not found (%v)", raw, err)
		}

		targets = appendIfMissing(targets, diskDevice{Path: path})
	}

	return targets, nil
}

func appendIfMissing(list []diskDevice, entry diskDevice) []diskDevice {
	for _, item := range list {
		if item.Path == entry.Path {
			return list
		}
	}
	return append(list, entry)
}

func normalizeDevicePath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if filepath.IsAbs(raw) {
		return raw
	}
	return "/dev/" + raw
}

func detectDisks() ([]diskDevice, error) {
	cmd := exec.Command("lsblk", "-J", "-b", "-o", "NAME,TYPE,SIZE,MODEL")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("lsblk failed: %w", err)
	}

	var lsblk struct {
		Blockdevices []struct {
			Name  string          `json:"name"`
			Type  string          `json:"type"`
			Size  json.RawMessage `json:"size"`
			Model string          `json:"model"`
		} `json:"blockdevices"`
	}

	if err := json.Unmarshal(out, &lsblk); err != nil {
		return nil, fmt.Errorf("parse lsblk JSON: %w", err)
	}

	var disks []diskDevice
	for _, entry := range lsblk.Blockdevices {
		if entry.Type != "disk" {
			continue
		}

		size := uint64(0)
		if len(entry.Size) > 0 {
			size, _ = strconv.ParseUint(strings.Trim(string(entry.Size), `"`), 10, 64)
		}

		disks = append(disks, diskDevice{
			Name:      entry.Name,
			Path:      "/dev/" + entry.Name,
			Model:     strings.TrimSpace(entry.Model),
			SizeBytes: size,
		})
	}

	return disks, nil
}

func buildDiskReport(disk diskDevice, runBadblocks bool) (*DiskReport, error) {
	report := &DiskReport{
		Path:      disk.Path,
		Name:      disk.Name,
		Model:     disk.Model,
		SizeBytes: disk.SizeBytes,
		SizeHuman: formatBytes(disk.SizeBytes),
	}

	infoOut, infoErr := runSmartctl(disk.Path, "-i")
	if infoErr != nil && errors.Is(infoErr, exec.ErrNotFound) {
		return nil, infoErr
	}
	if infoErr != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("smartctl -i: %v", infoErr))
	}
	populateInfo(report, infoOut)

	healthOut, healthErr := runSmartctl(disk.Path, "-H")
	if healthErr != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("smartctl -H: %v", healthErr))
	}
	report.SmartStatus = parseHealth(healthOut)

	attrsOut, attrsErr := runSmartctl(disk.Path, "-A")
	if attrsErr != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("smartctl -A: %v", attrsErr))
	}
	parseAttributes(report, attrsOut)

	selftestOut, selftestErr := runSmartctl(disk.Path, "-l", "selftest")
	if selftestErr != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("smartctl -l selftest: %v", selftestErr))
	}
	report.SelfTests = parseSelfTestLog(selftestOut)

	if runBadblocks {
		badblocks, err := runBadblocksScan(disk.Path)
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("badblocks: %v", err))
		} else {
			report.Badblocks = badblocks
		}
	}

	return report, nil
}

func runSmartctl(device string, args ...string) (string, error) {
	cmdArgs := append(args, device)
	cmd := exec.Command("smartctl", cmdArgs...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	output := buf.String()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("smartctl not installed: %w", err)
		}
		return output, fmt.Errorf("smartctl %s: %w", strings.Join(args, " "), err)
	}
	return output, nil
}

func populateInfo(report *DiskReport, output string) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "Device Model", "Model Number":
			if report.Model == "" {
				report.Model = value
			} else if !strings.Contains(report.Model, value) {
				report.Model = fmt.Sprintf("%s / %s", report.Model, value)
			}
		case "Serial Number":
			report.Serial = value
		case "Firmware Version":
			report.Firmware = value
		case "User Capacity":
			report.CapacityRaw = value
			if bytesCount, human := parseCapacity(value); bytesCount > 0 {
				report.SizeBytes = bytesCount
				report.SizeHuman = human
			}
		case "Sector Size":
			report.SectorSize = value
		case "Rotation Rate":
			report.Rotation = value
		case "SMART support is":
			report.SmartSupport = value
			report.SmartEnabled = strings.Contains(strings.ToLower(value), "enabled")
		case "Current Drive Temperature":
			if temp, err := strconv.Atoi(strings.TrimSuffix(value, " C")); err == nil {
				report.TemperatureCelsius = temp
			}
		}
	}
}

func parseHealth(output string) string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "SMART overall-health") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "unknown"
}

func parseAttributes(report *DiskReport, output string) {
	var (
		reallocated bool
		pending     bool
		offline     bool
		tempSet     bool
	)

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "ID#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		name := fields[1]
		raw := fields[len(fields)-1]
		value, _ := strconv.ParseUint(raw, 10, 64)

		switch name {
		case "Reallocated_Sector_Ct":
			report.ReallocatedSectors = value
			reallocated = true
		case "Current_Pending_Sector":
			report.PendingSectors = value
			pending = true
		case "Offline_Uncorrectable":
			report.OfflineUncorrectable = value
			offline = true
		case "Temperature_Celsius":
			report.TemperatureCelsius = int(value)
			tempSet = true
		}
	}

	if !tempSet && report.TemperatureCelsius == 0 {
		// fallback to 0 to differentiate from not set
		report.TemperatureCelsius = 0
	}
	if !reallocated {
		report.ReallocatedSectors = 0
	}
	if !pending {
		report.PendingSectors = 0
	}
	if !offline {
		report.OfflineUncorrectable = 0
	}
}

func parseSelfTestLog(output string) []SelfTestEntry {
	scanner := bufio.NewScanner(strings.NewReader(output))
	columns := []string{"Num", "Test_Description", "Status", "Remaining", "LifeTime", "LBA_of_first_error"}
	var starts []int
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Test_Description") && strings.Contains(line, "LifeTime") {
			st, ok := columnStarts(line, columns)
			if ok {
				starts = st
				break
			}
		}
	}

	if len(starts) == 0 {
		return nil
	}

	var tests []SelfTestEntry
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !(unicode.IsDigit(rune(trimmed[0])) || strings.HasPrefix(trimmed, "#")) {
			continue
		}
		values := extractColumns(line, starts, columns)
		if len(values) < len(columns) {
			continue
		}
		numText := strings.TrimPrefix(values[0], "#")
		num, _ := strconv.Atoi(numText)
		tests = append(tests, SelfTestEntry{
			Num:         num,
			Description: values[1],
			Status:      values[2],
			Remaining:   values[3],
			Lifetime:    values[4],
			LBA:         values[5],
		})
	}

	// keep the last 5 tests for readability
	if len(tests) > 5 {
		return tests[len(tests)-5:]
	}
	return tests
}

func columnStarts(header string, columns []string) ([]int, bool) {
	starts := make([]int, 0, len(columns))
	offset := 0
	for _, name := range columns {
		idx := strings.Index(header[offset:], name)
		if idx == -1 {
			return nil, false
		}
		starts = append(starts, offset+idx)
		offset += idx + len(name)
	}
	return starts, true
}

func extractColumns(line string, starts []int, columns []string) []string {
	lineLen := len(line)
	values := make([]string, len(columns))
	for i := 0; i < len(columns); i++ {
		start := starts[i]
		end := lineLen
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		if start >= lineLen {
			continue
		}
		if end > lineLen {
			end = lineLen
		}
		values[i] = strings.TrimSpace(line[start:end])
	}
	return values
}

func runBadblocksScan(device string) (*BadblocksResult, error) {
	start := time.Now()
	cmd := exec.Command("badblocks", "-sv", device)
	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
	err := cmd.Run()
	duration := time.Since(start)

	result := &BadblocksResult{
		Ran:      true,
		Duration: duration.String(),
	}

	tail := tailString(buf.String(), 5)
	if err != nil {
		result.Success = false
		result.Error = strings.TrimSpace(tail)
		return result, fmt.Errorf("badblocks scan failed: %s", tail)
	}

	result.Success = true
	result.Message = tail
	return result, nil
}

func tailString(input string, lines int) string {
	if input == "" {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(input))
	var buffer []string
	for scanner.Scan() {
		buffer = append(buffer, scanner.Text())
		if len(buffer) > lines {
			buffer = buffer[1:]
		}
	}
	return strings.Join(buffer, "\n")
}

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := uint64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

func parseCapacity(value string) (uint64, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, ""
	}
	parts := strings.Split(value, "bytes")
	if len(parts) == 0 {
		return 0, ""
	}
	digits := strings.ReplaceAll(parts[0], ",", "")
	digits = strings.TrimSpace(digits)
	size, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, ""
	}
	human := ""
	if len(parts) > 1 {
		human = strings.TrimSpace(parts[1])
	}
	if human == "" {
		human = formatBytes(size)
	}
	return size, human
}

func printReport(reports []*DiskReport) {
	fmt.Printf("Disk Health Report (%s)\n", time.Now().Format(time.RFC3339))
	fmt.Println(strings.Repeat("=", 60))
	for _, report := range reports {
		fmt.Printf("Device: %s\n", report.Path)
		if report.Model != "" {
			fmt.Printf("  Model: %s\n", report.Model)
		}
		if report.Serial != "" {
			fmt.Printf("  Serial: %s\n", report.Serial)
		}
		fmt.Printf("  Size: %s (%d bytes)\n", report.SizeHuman, report.SizeBytes)
		if report.SectorSize != "" {
			fmt.Printf("  Sector size: %s\n", report.SectorSize)
		}
		if report.Rotation != "" {
			fmt.Printf("  Rotation: %s\n", report.Rotation)
		}
		if report.SmartSupport != "" {
			fmt.Printf("  SMART support: %s\n", report.SmartSupport)
		}
		fmt.Printf("  SMART enabled: %t\n", report.SmartEnabled)
		fmt.Printf("  SMART health: %s\n", report.SmartStatus)
		fmt.Printf("  Temperature: %d C\n", report.TemperatureCelsius)
		fmt.Printf("  Reallocated sectors: %d\n", report.ReallocatedSectors)
		fmt.Printf("  Pending sectors: %d\n", report.PendingSectors)
		fmt.Printf("  Offline uncorrectable: %d\n", report.OfflineUncorrectable)
		if len(report.SelfTests) > 0 {
			fmt.Println("  Recent self tests:")
			for _, test := range report.SelfTests {
				fmt.Printf("    #%d %-25s %-25s Remaining:%s Lifetime:%s LBA:%s\n",
					test.Num, test.Description, test.Status, test.Remaining, test.Lifetime, test.LBA)
			}
		}
		if report.Badblocks != nil {
			status := "completed"
			if !report.Badblocks.Success {
				status = "errors detected"
			}
			fmt.Printf("  Badblocks scan: %s (duration %s)\n", status, report.Badblocks.Duration)
			if report.Badblocks.Message != "" {
				fmt.Printf("    %s\n", report.Badblocks.Message)
			}
			if report.Badblocks.Error != "" {
				fmt.Printf("    %s\n", report.Badblocks.Error)
			}
		}
		if len(report.Notes) > 0 {
			fmt.Println("  Notes:")
			for _, note := range report.Notes {
				fmt.Printf("    - %s\n", note)
			}
		}
		fmt.Println(strings.Repeat("-", 60))
	}
}
