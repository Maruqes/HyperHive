package smartdisk

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slave/extra"
	"strings"
)

// SelfTestType defines which SMART test to trigger.
type SelfTestType string

const (
	SelfTestShort    SelfTestType = "short"
	SelfTestExtended SelfTestType = "long" // "long" is the smartctl flag for extended tests
)

var devicePathPattern = regexp.MustCompile(`^/dev/(sd[a-z][a-z0-9]*|hd[a-z][a-z0-9]*|vd[a-z][a-z0-9]*|xvd[a-z][a-z0-9]*|nvme[0-9]+n[0-9]+(p[0-9]+)?|mmcblk[0-9]+(p[0-9]+)?|loop[0-9]+|disk/by-id/[A-Za-z0-9._:-]+|disk/by-path/[A-Za-z0-9._:-]+)$`)

func validateDevicePath(device string) (string, error) {
	device = strings.TrimSpace(device)
	if device == "" {
		return "", errors.New("device must not be empty")
	}
	if !devicePathPattern.MatchString(device) {
		return "", fmt.Errorf("device path not allowed: %s", device)
	}
	return device, nil
}

// SmartDiskInfo exposes the most relevant SMART health indicators for a device.
type SmartDiskInfo struct {
	Device          string `json:"device"`
	Model           string `json:"model"`
	Serial          string `json:"serial"`
	Firmware        string `json:"firmware"`
	CapacityBytes   int64  `json:"capacity_bytes"`
	PowerOnHours    int64  `json:"power_on_hours"`
	PowerCycleCount int64  `json:"power_cycle_count"`
	TemperatureC    int64  `json:"temperature_c"`
	TemperatureMax  int64  `json:"temperature_max"`
	TemperatureMin  int64  `json:"temperature_min"`
	SmartPassed     bool   `json:"smart_passed"`
	// Critical physical problem indicators
	ReallocatedSectors    int64 `json:"reallocated_sectors"`
	ReallocatedEventCount int64 `json:"reallocated_event_count"`
	PendingSectors        int64 `json:"pending_sectors"`
	OfflineUncorrectable  int64 `json:"offline_uncorrectable"`
	RawReadErrorRate      int64 `json:"raw_read_error_rate"`
	SeekErrorRate         int64 `json:"seek_error_rate"`
	SpinRetryCount        int64 `json:"spin_retry_count"`
	SpinUpTime            int64 `json:"spin_up_time_ms"`
	StartStopCount        int64 `json:"start_stop_count"`
	LoadCycleCount        int64 `json:"load_cycle_count"`
	CRCErrorCount         int64 `json:"crc_error_count"`
	UncorrectableReadErrs int64 `json:"uncorrectable_read_errors"`
	CommandTimeouts       int64 `json:"command_timeouts"`
	WriteErrorRate        int64 `json:"write_error_rate"`
	EndToEndErrors        int64 `json:"end_to_end_errors"`
	ReportedUncorrectable int64 `json:"reported_uncorrectable"`
	HighFlyWrites         int64 `json:"high_fly_writes"`
	AirflowTemperatureC   int64 `json:"airflow_temperature_c"`
	HardwareECCRecovered  int64 `json:"hardware_ecc_recovered"`
	// NVMe specific
	MediaErrors             int64 `json:"media_errors"`
	PercentageUsed          int64 `json:"percentage_used"`
	AvailableSpare          int64 `json:"available_spare"`
	AvailableSpareThreshold int64 `json:"available_spare_threshold"`
	CriticalWarning         int64 `json:"critical_warning"`
	DataUnitsRead           int64 `json:"data_units_read"`
	DataUnitsWritten        int64 `json:"data_units_written"`
	HostReadCommands        int64 `json:"host_read_commands"`
	HostWriteCommands       int64 `json:"host_write_commands"`
	UnsafeShutdowns         int64 `json:"unsafe_shutdowns"`
	// Error logs
	ErrorLogCount    int64            `json:"error_log_count"`
	DeviceErrorCount int64            `json:"device_error_count"`
	LastATAErrors    []ATAErrorEntry  `json:"last_ata_errors,omitempty"`
	LastNVMeErrors   []NVMeErrorEntry `json:"last_nvme_errors,omitempty"`
	SelfTests        []SelfTestResult `json:"self_tests"`
	// Health assessment
	HealthStatus          string `json:"health_status"`         // "healthy", "warning", "critical", "failing"
	PhysicalProblemRisk   string `json:"physical_problem_risk"` // "none", "low", "medium", "high", "critical"
	RecommendedAction     string `json:"recommended_action"`
	rawSmartctlParseError error  `json:"-"`
}

// SelfTestResult captures the outcome of recent SMART self-tests.
type SelfTestResult struct {
	Type             string `json:"type"`
	Status           string `json:"status"`
	Passed           bool   `json:"passed"`
	RemainingPercent int64  `json:"remaining_percent"`
	LifetimeHours    int64  `json:"lifetime_hours"`
}

// RunSelfTest triggers a SMART self-test (short or extended). smartctl returns
// a non-zero exit code when the device reports problems, so the caller should
// inspect the returned error message for context.
func RunSelfTest(device string, test SelfTestType) (err error) {
	var output []byte
	// record test flag early so defer can include it even if validation fails
	testFlag := string(test)
	if test == SelfTestExtended {
		testFlag = "long"
	}

	defer func() {
		title := "SMART self-test"
		success := err == nil
		var msg string
		outStr := strings.TrimSpace(string(output))
		if success {
			// don't include smartctl output on success
			msg = fmt.Sprintf("Self-test %s started on %s.", testFlag, device)
		} else {
			// include smartctl output on failure when available
			if outStr != "" {
				msg = fmt.Sprintf("Self-test %s failed to start on %s: %v. smartctl output: %s", testFlag, device, err, outStr)
			} else {
				msg = fmt.Sprintf("Self-test %s failed to start on %s: %v.", testFlag, device, err)
			}
		}
		// last param: critical/urgent flag (true for failures)
		extra.SendNotifications(title, msg, "/", !success)
	}()

	if device, err = validateDevicePath(device); err != nil {
		return err
	}

	cmd := exec.Command("smartctl", "-t", testFlag, device)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("smartctl self-test (%s): %w: %s", testFlag, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// GetSmartInfo collects SMART details for a device and returns a summarized view.
// The function still returns parsed information when smartctl exits non-zero
// (common if the drive reports failing health); in that case the error is
// attached to SmartDiskInfo.rawSmartctlParseError and also returned.
func GetSmartInfo(device string) (*SmartDiskInfo, error) {
	var err error
	if device, err = validateDevicePath(device); err != nil {
		return nil, err
	}

	resp, parseErr := runSmartctlJSON(device)
	if resp == nil {
		return nil, parseErr
	}

	info := summarizeSMART(device, resp)
	info.rawSmartctlParseError = parseErr
	return info, parseErr
}

// === Helpers ===

func runSmartctlJSON(device string) (*smartctlResponse, error) {
	cmd := exec.Command("smartctl", "-a", "-j", device)
	output, err := cmd.CombinedOutput()
	if len(output) == 0 && err != nil {
		return nil, fmt.Errorf("smartctl -a -j %s: %w", device, err)
	}

	var resp smartctlResponse
	if unmarshalErr := json.Unmarshal(output, &resp); unmarshalErr != nil {
		if err != nil {
			return nil, fmt.Errorf("smartctl parse error: %w (cmd error: %v)", unmarshalErr, err)
		}
		return nil, fmt.Errorf("smartctl parse error: %w", unmarshalErr)
	}

	if err != nil {
		// Return response plus context that smartctl exited non-zero.
		return &resp, fmt.Errorf("smartctl reported issues for %s: %w: %s", device, err, strings.TrimSpace(string(output)))
	}

	return &resp, nil
}

func summarizeSMART(device string, resp *smartctlResponse) *SmartDiskInfo {
	info := &SmartDiskInfo{
		Device:   device,
		Model:    resp.ModelName,
		Serial:   resp.SerialNumber,
		Firmware: resp.FirmwareVersion,
		CapacityBytes: func() int64 {
			if resp.UserCapacity != nil {
				return resp.UserCapacity.Bytes
			}
			return 0
		}(),
		PowerOnHours: resp.PowerOnTime.Hours,
		SmartPassed:  resp.SmartStatus.Passed,
	}

	// Temperature extraction
	if resp.Temperature != nil {
		info.TemperatureC = resp.Temperature.Current
		if resp.Temperature.Max != 0 {
			info.TemperatureMax = resp.Temperature.Max
		}
		if resp.Temperature.Min != 0 {
			info.TemperatureMin = resp.Temperature.Min
		}
	} else if resp.NVMESmartHealth != nil {
		info.TemperatureC = resp.NVMESmartHealth.Temperature
	}

	// ATA/SATA disk attributes
	if resp.ATASMARTAttributes != nil {
		// Critical physical indicators
		info.ReallocatedSectors = getAttrValue(resp.ATASMARTAttributes.Table, []int{5}, "reallocated_sector_ct", "reallocated sectors")
		info.ReallocatedEventCount = getAttrValue(resp.ATASMARTAttributes.Table, []int{196}, "reallocated_event_count")
		info.PendingSectors = getAttrValue(resp.ATASMARTAttributes.Table, []int{197}, "current_pending_sector")
		info.OfflineUncorrectable = getAttrValue(resp.ATASMARTAttributes.Table, []int{198}, "offline_uncorrectable")
		info.RawReadErrorRate = getAttrValue(resp.ATASMARTAttributes.Table, []int{1}, "raw_read_error_rate", "read_error_rate")
		info.SeekErrorRate = getAttrValue(resp.ATASMARTAttributes.Table, []int{7}, "seek_error_rate")
		info.SpinRetryCount = getAttrValue(resp.ATASMARTAttributes.Table, []int{10}, "spin_retry_count")
		info.WriteErrorRate = getAttrValue(resp.ATASMARTAttributes.Table, []int{200}, "write_error_rate", "multi_zone_error_rate")
		info.EndToEndErrors = getAttrValue(resp.ATASMARTAttributes.Table, []int{184}, "end_to_end_error")
		info.ReportedUncorrectable = getAttrValue(resp.ATASMARTAttributes.Table, []int{187}, "reported_uncorrect", "uncorrectable_error_cnt")
		info.HighFlyWrites = getAttrValue(resp.ATASMARTAttributes.Table, []int{189}, "high_fly_writes")
		info.HardwareECCRecovered = getAttrValue(resp.ATASMARTAttributes.Table, []int{195}, "hardware_ecc_recovered")

		// Connection/interface errors
		info.CRCErrorCount = getAttrValue(resp.ATASMARTAttributes.Table, []int{199}, "udma_crc_error_count", "crc_error_count")
		info.UncorrectableReadErrs = getAttrValue(resp.ATASMARTAttributes.Table, []int{187}, "uncorrectable_error_cnt", "reported_uncorrectable_errors")
		info.CommandTimeouts = getAttrValue(resp.ATASMARTAttributes.Table, []int{188}, "command_timeout")

		// Usage statistics
		info.PowerCycleCount = getAttrValue(resp.ATASMARTAttributes.Table, []int{12}, "power_cycle_count")
		info.StartStopCount = getAttrValue(resp.ATASMARTAttributes.Table, []int{4}, "start_stop_count")
		info.LoadCycleCount = getAttrValue(resp.ATASMARTAttributes.Table, []int{193}, "load_cycle_count")
		info.SpinUpTime = getAttrValue(resp.ATASMARTAttributes.Table, []int{3}, "spin_up_time")

		// Temperature from attributes if not already set
		if info.TemperatureC == 0 {
			info.TemperatureC = getAttrValue(resp.ATASMARTAttributes.Table, []int{194}, "temperature_celsius", "airflow_temperature_cel")
		}
		info.AirflowTemperatureC = getAttrValue(resp.ATASMARTAttributes.Table, []int{190}, "airflow_temperature_cel")
	}

	// ATA error logs
	if resp.ATASMARTErrorLog != nil {
		info.ErrorLogCount = resp.ATASMARTErrorLog.Summary.Count
		info.DeviceErrorCount = resp.ATASMARTErrorLog.Summary.DeviceErrorCount
		info.LastATAErrors = extractATAErrors(resp.ATASMARTErrorLog)
	}

	// NVMe specific attributes
	if resp.NVMESmartHealth != nil {
		info.MediaErrors = resp.NVMESmartHealth.MediaErrors
		info.ErrorLogCount = resp.NVMESmartHealth.NumErrLogEntries
		info.PercentageUsed = resp.NVMESmartHealth.PercentageUsed
		info.AvailableSpare = resp.NVMESmartHealth.AvailableSpare
		info.AvailableSpareThreshold = resp.NVMESmartHealth.AvailableSpareThreshold
		info.CriticalWarning = resp.NVMESmartHealth.CriticalWarning
		info.DataUnitsRead = resp.NVMESmartHealth.DataUnitsRead
		info.DataUnitsWritten = resp.NVMESmartHealth.DataUnitsWritten
		info.HostReadCommands = resp.NVMESmartHealth.HostReadCommands
		info.HostWriteCommands = resp.NVMESmartHealth.HostWriteCommands
		info.UnsafeShutdowns = resp.NVMESmartHealth.UnsafeShutdowns
		info.PowerCycleCount = resp.NVMESmartHealth.PowerCycles

		if info.PowerOnHours == 0 && resp.NVMESmartHealth.PowerOnHours > 0 {
			info.PowerOnHours = resp.NVMESmartHealth.PowerOnHours
		}
	}

	// NVMe error logs
	if resp.NVMeErrorLog != nil {
		info.LastNVMeErrors = extractNVMeErrors(resp.NVMeErrorLog)
	}

	info.SelfTests = extractSelfTests(resp)

	// Calculate health status and risk assessment
	assessHealth(info)

	return info
}

func extractSelfTests(resp *smartctlResponse) []SelfTestResult {
	var results []SelfTestResult

	// 1) Estado ATA atual (ata_smart_data.self_test.status)
	if resp.ATASMARTData != nil {
		st := resp.ATASMARTData.SelfTest.Status
		if st.String != "" {
			results = append(results, SelfTestResult{
				Type:   "current",
				Status: st.String,
				// Se remaining == 0 e a string disser "completed", podemos marcar como passed
				Passed: st.RemainingPercent == 0 &&
					strings.Contains(strings.ToLower(st.String), "completed"),
				RemainingPercent: st.RemainingPercent,
				LifetimeHours:    0, // não vem neste field
			})
		}
	}

	// 2) Log histórico ATA (como já tinhas)
	if resp.ATASMARTSelfTestLog != nil && resp.ATASMARTSelfTestLog.Standard != nil {
		for _, entry := range resp.ATASMARTSelfTestLog.Standard.Table {
			results = append(results, SelfTestResult{
				Type:             entry.Type.String,
				Status:           entry.Status.String,
				Passed:           entry.Status.Passed,
				RemainingPercent: entry.RemainingPercent,
				LifetimeHours:    entry.LifetimeHours,
			})
		}
	}

	// 3) NVMe (como já tinhas)
	if resp.NVMeSelfTestLog != nil {
		for _, entry := range resp.NVMeSelfTestLog.Data {
			results = append(results, SelfTestResult{
				Type:             entry.Desc,
				Status:           entry.Status.String,
				Passed:           entry.Status.Passed,
				RemainingPercent: entry.Remaining,
				LifetimeHours:    entry.LifeTimeHours,
			})
		}
	}

	return results
}

func extractATAErrors(log *ataSmartErrorLog) []ATAErrorEntry {
	if log == nil || log.Standard == nil {
		return nil
	}

	var out []ATAErrorEntry
	for _, entry := range log.Standard.Table {
		out = append(out, ATAErrorEntry{
			ErrorNumber:    entry.ErrorNumber,
			LifetimeHours:  entry.LifetimeHours,
			LBA:            entry.LBA,
			LBAFirstError:  entry.LBAFirstError,
			Status:         entry.Status.String,
			ErrorMessage:   entry.Error.String,
			Operation:      entry.Command.Name,
			SectorCount:    entry.Command.SectorCount,
			PoweredUpHours: entry.PowerUpTimeHours,
		})
		if len(out) >= 5 { // keep it short; newest first in smartctl output
			break
		}
	}
	return out
}

func extractNVMeErrors(log *nvmeErrorLog) []NVMeErrorEntry {
	if log == nil || len(log.Entries) == 0 {
		return nil
	}

	var out []NVMeErrorEntry
	for _, e := range log.Entries {
		entry := NVMeErrorEntry(e)
		out = append(out, entry)
		if len(out) >= 5 {
			break
		}
	}
	return out
}

// assessHealth analyzes SMART data and determines health status and physical problem risk
func assessHealth(info *SmartDiskInfo) {
	// Count critical indicators
	criticalCount := 0
	warningCount := 0

	// Check for critical physical problems
	if info.ReallocatedSectors > 0 {
		criticalCount++
	}
	if info.PendingSectors > 0 {
		criticalCount += 2 // More serious
	}
	if info.OfflineUncorrectable > 0 {
		criticalCount += 2
	}
	if info.ReportedUncorrectable > 0 {
		criticalCount++
	}
	if info.UncorrectableReadErrs > 0 {
		criticalCount++
	}

	// Check for warning indicators
	if info.ReallocatedEventCount > 0 {
		warningCount++
	}
	if info.RawReadErrorRate > 1000 {
		warningCount++
	}
	if info.SeekErrorRate > 1000 {
		warningCount++
	}
	if info.SpinRetryCount > 0 {
		warningCount++
	}
	if info.CommandTimeouts > 0 {
		warningCount++
	}
	if info.CRCErrorCount > 10 {
		warningCount++
	}
	if info.EndToEndErrors > 0 {
		warningCount++
	}
	if info.HighFlyWrites > 0 {
		warningCount++
	}

	// Temperature checks
	if info.TemperatureC > 60 {
		warningCount++
	}
	if info.TemperatureC > 70 {
		criticalCount++
	}

	// NVMe specific checks
	if info.MediaErrors > 0 {
		criticalCount += 2
	}
	if info.CriticalWarning > 0 {
		criticalCount++
	}
	if info.AvailableSpare < info.AvailableSpareThreshold && info.AvailableSpareThreshold > 0 {
		warningCount++
	}
	if info.PercentageUsed > 90 {
		warningCount++
	}

	// Error log checks
	if info.DeviceErrorCount > 0 {
		warningCount++
	}
	if info.ErrorLogCount > 100 {
		warningCount++
	}

	var guidance []string
	if info.TemperatureC > 60 {
		if info.TemperatureC > 70 {
			guidance = append(guidance, "immediately cool disk and reduce workload (temperature critical)")
		} else {
			guidance = append(guidance, "improve airflow or reduce load (temperature high)")
		}
	}
	if info.CRCErrorCount > 0 || info.CommandTimeouts > 0 || info.DeviceErrorCount > 0 {
		guidance = append(guidance, "check/reseat data and power cables or move to a stable port (interface errors seen)")
	}

	// Determine health status
	if !info.SmartPassed {
		info.HealthStatus = "failing"
		info.PhysicalProblemRisk = "critical"
		info.RecommendedAction = "IMMEDIATE BACKUP AND DISK REPLACEMENT REQUIRED - Drive is failing"
	} else if criticalCount > 0 {
		if criticalCount >= 3 {
			info.HealthStatus = "critical"
			info.PhysicalProblemRisk = "critical"
			info.RecommendedAction = "URGENT: Backup immediately and replace disk - Multiple critical physical problems detected"
		} else {
			info.HealthStatus = "warning"
			info.PhysicalProblemRisk = "high"
			info.RecommendedAction = "Backup data immediately and monitor closely - Physical problems detected"
		}
	} else if warningCount >= 3 {
		info.HealthStatus = "warning"
		info.PhysicalProblemRisk = "medium"
		info.RecommendedAction = "Backup data and monitor - Multiple warning indicators present"
	} else if warningCount > 0 {
		info.HealthStatus = "caution"
		info.PhysicalProblemRisk = "low"
		info.RecommendedAction = "Monitor regularly - Some warning indicators detected"
	} else {
		info.HealthStatus = "healthy"
		info.PhysicalProblemRisk = "none"
		info.RecommendedAction = "Normal operation - Continue regular monitoring"
	}

	// Override if self-tests failed
	for _, test := range info.SelfTests {
		if !test.Passed && test.RemainingPercent == 0 {
			if info.HealthStatus == "healthy" {
				info.HealthStatus = "warning"
				info.PhysicalProblemRisk = "medium"
				info.RecommendedAction = "Self-test failed - Run extended diagnostics"
			}
			break
		}
	}

	if len(guidance) > 0 && info.RecommendedAction != "" {
		info.RecommendedAction = fmt.Sprintf("%s; %s", info.RecommendedAction, strings.Join(guidance, "; "))
	} else if len(guidance) > 0 {
		info.RecommendedAction = strings.Join(guidance, "; ")
	}
}

func getAttrValue(table []ataAttribute, ids []int, names ...string) int64 {
	namesNormalized := make([]string, len(names))
	for i, n := range names {
		namesNormalized[i] = strings.ToLower(n)
	}

	for _, attr := range table {
		for _, id := range ids {
			if attr.ID == id {
				return attr.Raw.Value
			}
		}
		name := strings.ToLower(attr.Name)
		for _, n := range namesNormalized {
			if n == "" {
				continue
			}
			if name == n {
				return attr.Raw.Value
			}
		}
	}
	return 0
}

// === JSON wiring ===

type smartctlResponse struct {
	ModelName             string               `json:"model_name"`
	SerialNumber          string               `json:"serial_number"`
	FirmwareVersion       string               `json:"firmware_version"`
	UserCapacity          *userCapacity        `json:"user_capacity"`
	PowerOnTime           powerOnTime          `json:"power_on_time"`
	Temperature           *temperatureInfo     `json:"temperature"`
	SmartStatus           smartStatus          `json:"smart_status"`
	ATASMARTAttributes    *ataSmartAttributes  `json:"ata_smart_attributes"`
	ATASMARTErrorLog      *ataSmartErrorLog    `json:"ata_smart_error_log"`
	ATASMARTSelfTestLog   *ataSmartSelfTestLog `json:"ata_smart_self_test_log"`
	ATASMARTData          *ataSmartData        `json:"ata_smart_data"` // <-- NOVO
	NVMESmartHealth       *nvmeSmartHealth     `json:"nvme_smart_health_information_log"`
	NVMeSelfTestLog       *nvmeSelfTestLog     `json:"nvme_self_test_log"`
	NVMeErrorLog          *nvmeErrorLog        `json:"nvme_error_log"`
	AdditionalTemperature *int64               `json:"-"` // reserved for future fields
}

type userCapacity struct {
	Bytes int64 `json:"bytes"`
}

type powerOnTime struct {
	Hours int64 `json:"hours"`
}

type temperatureInfo struct {
	Current int64 `json:"current"`
	Max     int64 `json:"max"`
	Min     int64 `json:"min"`
}

type smartStatus struct {
	Passed bool `json:"passed"`
}

type ataSmartAttributes struct {
	Table []ataAttribute `json:"table"`
}

type ataAttribute struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Raw  struct {
		Value int64  `json:"value"`
		Str   string `json:"string"`
	} `json:"raw"`
}

type ataSmartErrorLog struct {
	Summary struct {
		Count            int64 `json:"count"`
		DeviceErrorCount int64 `json:"device_error_count"`
	} `json:"summary"`
	Standard *struct {
		Table []ataErrorLogEntry `json:"table"`
	} `json:"standard"`
}

type ataSmartSelfTestLog struct {
	Standard *struct {
		Table []ataSelfTestEntry `json:"table"`
	} `json:"standard"`
}
type ataSmartData struct {
	SelfTest struct {
		Status struct {
			Value            int64  `json:"value"`
			String           string `json:"string"`
			RemainingPercent int64  `json:"remaining_percent"`
		} `json:"status"`
	} `json:"self_test"`
}

type ataSelfTestEntry struct {
	Type struct {
		String string `json:"string"`
	} `json:"type"`
	Status struct {
		String string `json:"string"`
		Passed bool   `json:"passed"`
	} `json:"status"`
	RemainingPercent int64 `json:"remaining_percent"`
	LifetimeHours    int64 `json:"lifetime_hours"`
}

// ATAErrorEntry is a condensed view of ATA error log rows.
type ATAErrorEntry struct {
	ErrorNumber    int64  `json:"error_number"`
	LifetimeHours  int64  `json:"lifetime_hours"`
	LBA            int64  `json:"lba"`
	LBAFirstError  int64  `json:"lba_first_error"`
	Status         string `json:"status"`
	ErrorMessage   string `json:"error_message"`
	Operation      string `json:"operation"`
	SectorCount    int64  `json:"sector_count"`
	PoweredUpHours int64  `json:"powered_up_hours"`
}

type ataErrorLogEntry struct {
	ErrorNumber   int64 `json:"error_number"`
	LifetimeHours int64 `json:"lifetime_hours"`
	LBA           int64 `json:"lba"`
	LBAFirstError int64 `json:"lba_first_error"`
	Status        struct {
		String string `json:"string"`
	} `json:"status"`
	Error struct {
		String string `json:"string"`
	} `json:"error"`
	Command struct {
		Name        string `json:"name"`
		SectorCount int64  `json:"sector_count"`
	} `json:"command"`
	PowerUpTimeHours int64 `json:"power_up_time_hours"`
}

type nvmeSmartHealth struct {
	Temperature             int64 `json:"temperature"`
	MediaErrors             int64 `json:"media_errors"`
	NumErrLogEntries        int64 `json:"num_err_log_entries"`
	PowerOnHours            int64 `json:"power_on_hours"`
	PercentageUsed          int64 `json:"percentage_used"`
	AvailableSpare          int64 `json:"available_spare"`
	AvailableSpareThreshold int64 `json:"available_spare_threshold"`
	CriticalWarning         int64 `json:"critical_warning"`
	DataUnitsRead           int64 `json:"data_units_read"`
	DataUnitsWritten        int64 `json:"data_units_written"`
	HostReadCommands        int64 `json:"host_read_commands"`
	HostWriteCommands       int64 `json:"host_write_commands"`
	UnsafeShutdowns         int64 `json:"unsafe_shutdowns"`
	PowerCycles             int64 `json:"power_cycles"`
}

type nvmeSelfTestLog struct {
	Data []nvmeSelfTestEntry `json:"data"`
}

type nvmeSelfTestEntry struct {
	Desc          string `json:"desc"`
	LifeTimeHours int64  `json:"life_time_hours"`
	Remaining     int64  `json:"remaining"`
	Status        struct {
		String string `json:"string"`
		Passed bool   `json:"passed"`
	} `json:"status"`
}

// NVMeErrorEntry captures NVMe error log entries.
type NVMeErrorEntry struct {
	ErrorCount   int64  `json:"error_count"`
	SQID         int64  `json:"sqid"`
	CID          int64  `json:"cid"`
	StatusField  int64  `json:"status_field"`
	ParamErrLoc  int64  `json:"param_error_location"`
	LBA          int64  `json:"lba"`
	NSID         int64  `json:"nsid"`
	VS           int64  `json:"vs"`
	Trk          int64  `json:"trk"`
	Message      string `json:"message"`
	SubmissionTS int64  `json:"submission_timestamp"`
}

type nvmeErrorLog struct {
	Entries []nvmeErrorEntry `json:"entries"`
}

type nvmeErrorEntry struct {
	ErrorCount   int64  `json:"error_count"`
	SQID         int64  `json:"sqid"`
	CID          int64  `json:"cid"`
	StatusField  int64  `json:"status_field"`
	ParamErrLoc  int64  `json:"parameter_error_location"`
	LBA          int64  `json:"lba"`
	NSID         int64  `json:"nsid"`
	VS           int64  `json:"vs"`
	Trk          int64  `json:"trk"`
	Message      string `json:"message"`
	SubmissionTS int64  `json:"submission_timestamp"`
}
