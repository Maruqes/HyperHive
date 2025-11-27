package smartdisk

import (
	"fmt"
	"slave/btrfs"
	"slave/extra"
	"strings"
	"time"

	"github.com/Maruqes/512SvMan/logger"
)

//checks if last smart test was sucessfull

func CheckSmartTestNot() {
	disks, err := btrfs.GetAllDisks(false)
	if err != nil {
		logger.Errorf("Could not get all disks in CheckSmartTestNot: %v", err)
		return
	}

	for _, disk := range disks {
		info, err := GetSmartInfo(disk.Path)
		if err != nil {
			logger.Errorf("GetSmartInfo(%s) failed: %v", disk.Path, err)
			continue
		}
		if info == nil {
			logger.Errorf("GetSmartInfo(%s) returned nil info", disk.Path)
			continue
		}

		checkSmartDiskProblems(disk.Path, info)
	}
}

// checkSmartDiskProblems inspects SMART data and emits notifications for any issues.
func checkSmartDiskProblems(device string, info *SmartDiskInfo) {
	notify := func(title, msg string, critical bool) {
		// one place to guarantee both notification + log
		logger.Errorf("[%s] %s", device, msg)
		extra.SendNotifications(title, msg, "/", critical)
	}

	// --- Overall health / SMART pass ---

	if strings.EqualFold(info.HealthStatus, "warning") {
		notify(
			"SMART warning",
			fmt.Sprintf("%s is in SMART warning state (health_status=%q).", device, info.HealthStatus),
			false,
		)
	}

	if strings.EqualFold(info.HealthStatus, "critical") || strings.EqualFold(info.HealthStatus, "failing") {
		notify(
			"SMART critical health",
			fmt.Sprintf("%s is in SMART %q state.", device, info.HealthStatus),
			true,
		)
	}

	if !info.SmartPassed {
		notify(
			"SMART overall health FAILED",
			fmt.Sprintf("%s reported SMART overall-health = FAILED.", device),
			true,
		)
	}

	// --- Classic HDD attributes ---

	if info.ReallocatedSectors > 0 {
		critical := info.ReallocatedSectors > 100 // simple heuristic
		notify(
			"SMART reallocated sectors",
			fmt.Sprintf("%s has %d reallocated sectors (possible physical media damage).", device, info.ReallocatedSectors),
			critical,
		)
	}

	if info.PendingSectors > 0 {
		notify(
			"SMART pending sectors",
			fmt.Sprintf("%s has %d pending sectors waiting for reallocation.", device, info.PendingSectors),
			true,
		)
	}

	if info.OfflineUncorrectable > 0 {
		notify(
			"SMART offline uncorrectable",
			fmt.Sprintf("%s has %d offline uncorrectable sectors.", device, info.OfflineUncorrectable),
			true,
		)
	}

	if info.CRCErrorCount > 0 {
		// mostly cable / connection; usually não crítico mas importante
		critical := info.CRCErrorCount > 1000
		notify(
			"SMART interface CRC errors",
			fmt.Sprintf("%s has %d CRC interface errors (check SATA/SAS cable and connectors).", device, info.CRCErrorCount),
			critical,
		)
	}

	if info.CommandTimeouts > 0 {
		notify(
			"SMART command timeouts",
			fmt.Sprintf("%s has %d command timeouts recorded.", device, info.CommandTimeouts),
			false,
		)
	}

	if info.EndToEndErrors > 0 {
		notify(
			"SMART end-to-end errors",
			fmt.Sprintf("%s has %d end-to-end data path errors.", device, info.EndToEndErrors),
			true,
		)
	}

	if info.ReportedUncorrectable > 0 {
		notify(
			"SMART reported uncorrectable",
			fmt.Sprintf("%s has %d reported uncorrectable errors.", device, info.ReportedUncorrectable),
			true,
		)
	}

	if info.UncorrectableReadErrs > 0 {
		notify(
			"SMART uncorrectable read errors",
			fmt.Sprintf("%s has %d uncorrectable read errors.", device, info.UncorrectableReadErrs),
			true,
		)
	}

	if info.HighFlyWrites > 0 {
		notify(
			"SMART high fly writes",
			fmt.Sprintf("%s has %d high-fly writes (possible head flying height issue).", device, info.HighFlyWrites),
			false,
		)
	}

	// Estes são vendor-specific; aqui só logamos se forem muito anormais (>0)
	if info.RawReadErrorRate > 0 {
		notify(
			"SMART raw read error rate",
			fmt.Sprintf("%s reports raw read error rate = %d (interpretation is vendor specific).", device, info.RawReadErrorRate),
			false,
		)
	}

	if info.SeekErrorRate > 0 {
		notify(
			"SMART seek error rate",
			fmt.Sprintf("%s reports seek error rate = %d (interpretation is vendor specific).", device, info.SeekErrorRate),
			false,
		)
	}

	if info.SpinRetryCount > 0 {
		notify(
			"SMART spin retry count",
			fmt.Sprintf("%s needed %d spin retries (possible spindle/motor issue).", device, info.SpinRetryCount),
			true,
		)
	}

	if info.HardwareECCRecovered > 0 {
		// normalmente estes números são altos; só um one-liner informativo
		notify(
			"SMART ECC recovered",
			fmt.Sprintf("%s reports hardware ECC recovered = %d (may be normal for some vendors).", device, info.HardwareECCRecovered),
			false,
		)
	}

	// --- NVMe-specific checks ---

	if info.MediaErrors > 0 {
		notify(
			"NVMe media errors",
			fmt.Sprintf("%s has %d NVMe media errors.", device, info.MediaErrors),
			true,
		)
	}

	if info.PercentageUsed >= 95 {
		critical := info.PercentageUsed >= 100
		notify(
			"NVMe wear level high",
			fmt.Sprintf("%s reports percentage_used = %d%% (near or beyond endurance).", device, info.PercentageUsed),
			critical,
		)
	}

	if info.AvailableSpareThreshold > 0 && info.AvailableSpare <= info.AvailableSpareThreshold {
		notify(
			"NVMe spare below threshold",
			fmt.Sprintf("%s available_spare=%d%% is at/below threshold %d%%.", device, info.AvailableSpare, info.AvailableSpareThreshold),
			true,
		)
	}

	if info.CriticalWarning != 0 {
		notify(
			"NVMe critical warning",
			fmt.Sprintf("%s has NVMe critical_warning flag set (0x%x).", device, info.CriticalWarning),
			true,
		)
	}

	if info.UnsafeShutdowns > 0 {
		notify(
			"NVMe unsafe shutdowns",
			fmt.Sprintf("%s detected %d unsafe shutdowns.", device, info.UnsafeShutdowns),
			false,
		)
	}

	// --- Error logs ---

	if info.ErrorLogCount > 0 || info.DeviceErrorCount > 0 {
		notify(
			"SMART error log entries",
			fmt.Sprintf("%s has %d SMART error log entries (device_error_count=%d).", device, info.ErrorLogCount, info.DeviceErrorCount),
			false,
		)
	}

	if len(info.LastATAErrors) > 0 {
		notify(
			"Recent ATA errors",
			fmt.Sprintf("%s has %d recent ATA errors in SMART log.", device, len(info.LastATAErrors)),
			true,
		)
	}

	if len(info.LastNVMeErrors) > 0 {
		notify(
			"Recent NVMe errors",
			fmt.Sprintf("%s has %d recent NVMe errors in SMART log.", device, len(info.LastNVMeErrors)),
			true,
		)
	}

	// --- Temperature sanity check ---

	if info.TemperatureC >= 55 {
		critical := info.TemperatureC >= 65
		notify(
			"Disk temperature high",
			fmt.Sprintf("%s is at %d°C (recommended < 50–55°C).", device, info.TemperatureC),
			critical,
		)
	}

	// --- Last self-test status ---

	if len(info.SelfTests) != 0 {
		last := info.SelfTests[0] // assuming most recent first

		// se tiveres um helper containsInProgress, podes reutilizar
		isRunning := false
		statusLower := strings.ToLower(last.Status)
		if strings.Contains(statusLower, "in progress") || strings.Contains(statusLower, "running") {
			isRunning = true
		}

		// Só avaliamos tests concluídos
		if !isRunning && last.RemainingPercent == 0 {
			if !strings.Contains(statusLower, "completed without error") {
				notify(
					"SMART self-test failed",
					fmt.Sprintf("%s last SMART self-test finished with status %q.", device, last.Status),
					true,
				)
			}
		}
	}
}
func StartSmartTestChecker() {
	go func() {
		for {
			CheckSmartTestNot()
			time.Sleep(6 * time.Hour)
		}
	}()
}
