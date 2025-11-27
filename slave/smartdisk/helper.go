package smartdisk

import (
	"fmt"
	"slave/btrfs"
	"slave/extra"
	"strings"
	"time"

	"github.com/Maruqes/512SvMan/logger"
)

// checks if last smart test was sucessfull
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

func checkSmartDiskProblems(device string, info *SmartDiskInfo) {
	// Helper: envia notificação + log de erro.
	notify := func(title, msg string, critical bool) {
		logger.Errorf("[%s] %s", device, msg)
		extra.SendNotifications(title, msg, "/", critical)
	}

	// Check summary info (model/serial/firmware/capacity) – info only.
	logger.Infof("[%s] SMART summary: model=%q serial=%q fw=%q capacity=%dB power_on=%dh cycles=%d",
		device, info.Model, info.Serial, info.Firmware,
		info.CapacityBytes, info.PowerOnHours, info.PowerCycleCount,
	)

	// Check parse errors from smartctl.
	if info.rawSmartctlParseError != nil {
		notify(
			"SMART parse error",
			fmt.Sprintf("%s had a smartctl parse error: %v.", device, info.rawSmartctlParseError),
			false,
		)
	}

	if info.HealthStatus != "" {
		switch strings.ToLower(info.HealthStatus) {
		case "failing", "critical":
			// One-liner: disk is in critical/failing state, show recommended action.
			notify(
				"SMART critical health",
				fmt.Sprintf("%s is in %q state (risk=%q). %s",
					device, info.HealthStatus, info.PhysicalProblemRisk, info.RecommendedAction),
				true,
			)

		case "warning":
			// One-liner: warning health, high/medium risk, show recommended action.
			notify(
				"SMART warning health",
				fmt.Sprintf("%s is in %q state (risk=%q). %s",
					device, info.HealthStatus, info.PhysicalProblemRisk, info.RecommendedAction),
				true, // warning but still serious enough to be "critical" notification
			)

		case "caution":
			// One-liner: caution state, low risk, monitor.
			notify(
				"SMART caution health",
				fmt.Sprintf("%s is in %q state (risk=%q). %s",
					device, info.HealthStatus, info.PhysicalProblemRisk, info.RecommendedAction),
				false,
			)

		case "healthy":
			// Optional: just log as info, no notification.
			logger.Infof("[%s] SMART health is healthy (risk=%q). %s",
				device, info.PhysicalProblemRisk, info.RecommendedAction)
		}
	} else if !info.SmartPassed {
		// Fallback if HealthStatus was not filled, but SmartPassed is false.
		notify(
			"SMART overall health FAILED",
			fmt.Sprintf("%s reported SMART overall-health = FAILED.", device),
			true,
		)
	}

	// Check physical problem risk assessment.
	if info.PhysicalProblemRisk != "" {
		risk := strings.ToLower(info.PhysicalProblemRisk)

		// Check medium risk level.
		if risk == "medium" {
			notify(
				"SMART physical risk (medium)",
				fmt.Sprintf("%s physical_problem_risk=%q. Recommended: %s", device, info.PhysicalProblemRisk, info.RecommendedAction),
				false,
			)
		}

		// Check high/critical risk level.
		if risk == "high" || risk == "critical" {
			notify(
				"SMART physical risk (high/critical)",
				fmt.Sprintf("%s physical_problem_risk=%q. Recommended: %s", device, info.PhysicalProblemRisk, info.RecommendedAction),
				true,
			)
		}
	}

	// Check SMART overall-passed flag.
	if !info.SmartPassed {
		notify(
			"SMART overall health FAILED",
			fmt.Sprintf("%s reported SMART overall-health = FAILED.", device),
			true,
		)
	}

	// Check very high power-on hours (old drive).
	if info.PowerOnHours > 60000 {
		notify(
			"High power-on hours",
			fmt.Sprintf("%s has %d power-on hours (drive is heavily used).", device, info.PowerOnHours),
			false,
		)
	}

	// Check very high power-cycle count (many start/stop cycles).
	if info.PowerCycleCount > 20000 {
		notify(
			"High power-cycle count",
			fmt.Sprintf("%s has %d power cycles (possible mechanical wear).", device, info.PowerCycleCount),
			false,
		)
	}

	// Check current operating temperature.
	if info.TemperatureC >= 55 {
		critical := info.TemperatureC >= 65
		notify(
			"Disk temperature high",
			fmt.Sprintf("%s is at %d°C (recommended < 50–55°C).", device, info.TemperatureC),
			critical,
		)
	}

	// Check historical max temperature.
	if info.TemperatureMax >= 60 {
		notify(
			"Disk max temperature high",
			fmt.Sprintf("%s reached a historical max temperature of %d°C.", device, info.TemperatureMax),
			false,
		)
	}

	// Check suspiciously low min temperature (could indicate wrong sensors).
	if info.TemperatureMin <= 0 && info.TemperatureMin != 0 {
		notify(
			"Disk min temperature abnormal",
			fmt.Sprintf("%s reports a minimum temperature of %d°C.", device, info.TemperatureMin),
			false,
		)
	}

	// Check airflow temperature if present.
	if info.AirflowTemperatureC >= 55 {
		notify(
			"Disk airflow temperature high",
			fmt.Sprintf("%s airflow temperature is %d°C.", device, info.AirflowTemperatureC),
			false,
		)
	}

	// Check reallocated sectors count.
	if info.ReallocatedSectors > 0 {
		critical := info.ReallocatedSectors > 100
		notify(
			"SMART reallocated sectors",
			fmt.Sprintf("%s has %d reallocated sectors (possible physical media damage).", device, info.ReallocatedSectors),
			critical,
		)
	}

	// Check reallocated event count (how many times reallocation happened).
	if info.ReallocatedEventCount > 0 {
		notify(
			"SMART reallocation events",
			fmt.Sprintf("%s has %d reallocation events recorded.", device, info.ReallocatedEventCount),
			false,
		)
	}

	// Check pending sectors waiting for reallocation.
	if info.PendingSectors > 0 {
		notify(
			"SMART pending sectors",
			fmt.Sprintf("%s has %d pending sectors waiting for reallocation.", device, info.PendingSectors),
			true,
		)
	}

	// Check offline uncorrectable sectors.
	if info.OfflineUncorrectable > 0 {
		notify(
			"SMART offline uncorrectable",
			fmt.Sprintf("%s has %d offline uncorrectable sectors.", device, info.OfflineUncorrectable),
			true,
		)
	}

	// Check raw read error rate (vendor specific).
	if info.RawReadErrorRate > 0 {
		notify(
			"SMART raw read error rate",
			fmt.Sprintf("%s reports raw read error rate = %d (vendor specific).", device, info.RawReadErrorRate),
			false,
		)
	}

	// Check seek error rate (vendor specific).
	if info.SeekErrorRate > 0 {
		notify(
			"SMART seek error rate",
			fmt.Sprintf("%s reports seek error rate = %d (vendor specific).", device, info.SeekErrorRate),
			false,
		)
	}

	// Check spin retry count (problems spinning up).
	if info.SpinRetryCount > 0 {
		notify(
			"SMART spin retry count",
			fmt.Sprintf("%s needed %d spin retries (possible spindle/motor issue).", device, info.SpinRetryCount),
			true,
		)
	}

	// Check spin-up time (very slow spin-up).
	if info.SpinUpTime > 8000 && info.SpinUpTime != 0 { // >8s em ms
		notify(
			"SMART slow spin-up time",
			fmt.Sprintf("%s has spin-up time of %d ms (drive spins up slowly).", device, info.SpinUpTime),
			false,
		)
	}

	// Check high start/stop count.
	if info.StartStopCount > 20000 {
		notify(
			"SMART high start/stop count",
			fmt.Sprintf("%s has %d start/stop cycles (mechanical wear risk).", device, info.StartStopCount),
			false,
		)
	}

	// Check high load cycle count (excessive head parking).
	if info.LoadCycleCount > 300000 {
		notify(
			"SMART high load cycle count",
			fmt.Sprintf("%s has %d load/unload cycles (possible head parking issue).", device, info.LoadCycleCount),
			false,
		)
	}

	// Check CRC error count (cable / link problems).
	if info.CRCErrorCount > 0 {
		critical := info.CRCErrorCount > 1000
		notify(
			"SMART interface CRC errors",
			fmt.Sprintf("%s has %d CRC interface errors (check SATA/SAS cable and connectors).", device, info.CRCErrorCount),
			critical,
		)
	}

	// Check command timeouts (communication issues).
	if info.CommandTimeouts > 0 {
		notify(
			"SMART command timeouts",
			fmt.Sprintf("%s has %d command timeouts recorded.", device, info.CommandTimeouts),
			false,
		)
	}

	// Check write error rate (vendor specific).
	if info.WriteErrorRate > 0 {
		notify(
			"SMART write error rate",
			fmt.Sprintf("%s reports write error rate = %d (vendor specific).", device, info.WriteErrorRate),
			false,
		)
	}

	// Check end-to-end errors in controller path.
	if info.EndToEndErrors > 0 {
		notify(
			"SMART end-to-end errors",
			fmt.Sprintf("%s has %d end-to-end data path errors.", device, info.EndToEndErrors),
			true,
		)
	}

	// Check reported uncorrectable errors.
	if info.ReportedUncorrectable > 0 {
		notify(
			"SMART reported uncorrectable",
			fmt.Sprintf("%s has %d reported uncorrectable errors.", device, info.ReportedUncorrectable),
			true,
		)
	}

	// Check uncorrectable read errors.
	if info.UncorrectableReadErrs > 0 {
		notify(
			"SMART uncorrectable read errors",
			fmt.Sprintf("%s has %d uncorrectable read errors.", device, info.UncorrectableReadErrs),
			true,
		)
	}

	// Check high-fly writes (head flying height issues).
	if info.HighFlyWrites > 0 {
		notify(
			"SMART high-fly writes",
			fmt.Sprintf("%s has %d high-fly writes (possible head flying height issue).", device, info.HighFlyWrites),
			false,
		)
	}

	// Check ECC recovered count (info – normalmente alto).
	if info.HardwareECCRecovered > 0 {
		notify(
			"SMART ECC recovered",
			fmt.Sprintf("%s reports hardware ECC recovered = %d (often normal for some vendors).", device, info.HardwareECCRecovered),
			false,
		)
	}

	// Check NVMe media errors.
	if info.MediaErrors > 0 {
		notify(
			"NVMe media errors",
			fmt.Sprintf("%s has %d NVMe media errors.", device, info.MediaErrors),
			true,
		)
	}

	// Check NVMe wear level percentage used.
	if info.PercentageUsed >= 95 {
		critical := info.PercentageUsed >= 100
		notify(
			"NVMe wear level high",
			fmt.Sprintf("%s reports percentage_used = %d%% (near or beyond endurance).", device, info.PercentageUsed),
			critical,
		)
	}

	// Check NVMe available spare below threshold.
	if info.AvailableSpareThreshold > 0 && info.AvailableSpare <= info.AvailableSpareThreshold && info.AvailableSpare != 0 {
		notify(
			"NVMe spare below threshold",
			fmt.Sprintf("%s available_spare=%d%% is at/below threshold %d%%.", device, info.AvailableSpare, info.AvailableSpareThreshold),
			true,
		)
	}

	// Check NVMe critical_warning flag.
	if info.CriticalWarning != 0 {
		notify(
			"NVMe critical warning",
			fmt.Sprintf("%s has NVMe critical_warning flag set (0x%x).", device, info.CriticalWarning),
			true,
		)
	}

	// Check unsafe shutdowns count.
	if info.UnsafeShutdowns > 0 {
		notify(
			"NVMe unsafe shutdowns",
			fmt.Sprintf("%s detected %d unsafe shutdowns.", device, info.UnsafeShutdowns),
			false,
		)
	}

	// DataUnitsRead/DataUnitsWritten/Host*Commands são mais estatísticas que problemas:
	// aqui só fazemos uma sanity check muito simples (ex.: zero com disco antigo).
	if info.PowerOnHours > 1000 && info.DataUnitsRead == 0 && info.DataUnitsWritten == 0 {
		notify(
			"NVMe lifetime stats suspicious",
			fmt.Sprintf("%s has 0 DataUnitsRead/Written after %dh on time.", device, info.PowerOnHours),
			false,
		)
	}

	// Check SMART error log counts.
	if info.ErrorLogCount > 0 || info.DeviceErrorCount > 0 {
		notify(
			"SMART error log entries",
			fmt.Sprintf("%s has %d SMART error log entries (device_error_count=%d).", device, info.ErrorLogCount, info.DeviceErrorCount),
			false,
		)
	}

	// Check presence of recent ATA errors.
	if len(info.LastATAErrors) > 0 {
		notify(
			"Recent ATA errors",
			fmt.Sprintf("%s has %d recent ATA errors in SMART log.", device, len(info.LastATAErrors)),
			true,
		)
	}

	// Check presence of recent NVMe errors.
	if len(info.LastNVMeErrors) > 0 {
		notify(
			"Recent NVMe errors",
			fmt.Sprintf("%s has %d recent NVMe errors in SMART log.", device, len(info.LastNVMeErrors)),
			true,
		)
	}

		// Check last SMART self-test result.
		if len(info.SelfTests) != 0 {
			// Assume slice is ordered from most recent to oldest.
			last := info.SelfTests[0]
			statusLower := strings.ToLower(last.Status)
			// Check if last test is still running.
			isRunning := strings.Contains(statusLower, "in progress") || strings.Contains(statusLower, "running")

			// Check last completed test if not running.
			if !isRunning && last.RemainingPercent == 0 {
				if !strings.Contains(statusLower, "completed without error") {
					notify(
						"SMART self-test failed",
						fmt.Sprintf("%s last SMART self-test finished with status %q at %dh power-on.", device, last.Status, last.LifetimeHours),
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
