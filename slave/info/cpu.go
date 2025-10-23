package info

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/sensors"
)

type CPUInfoStruct struct{}

var CPUInfo CPUInfoStruct

var (
	coreSensorIndexRegexp = regexp.MustCompile(`(?i)(?:core|cpu|tccd)[^0-9]*?(\d+)`)
	packageSensorRegexp   = regexp.MustCompile(`(?i)(?:package[_\s-]?id|tctl|tdie)`)
)

func (c *CPUInfoStruct) GetCPUModel() (map[string]int, error) {
	info, err := cpu.Info()
	if err != nil {
		return nil, err
	}

	modelCores := make(map[string]int)

	for _, ci := range info {
		key := fmt.Sprintf("%s|%s", ci.ModelName, ci.PhysicalID)
		modelCores[key] += int(ci.Cores)
	}

	return modelCores, nil
}

func (c *CPUInfoStruct) GetCpuTemps() ([]sensors.TemperatureStat, error) {
	temps, err := sensors.SensorsTemperatures()
	if err != nil {
		return nil, err
	}

	var coreTemps []sensors.TemperatureStat

	var cpuRes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^(coretemp|k10temp|zenpower|zenpower3)`), // CPU drivers
		regexp.MustCompile(`(?i)(^|[_\s-])package(_?id)?[_\s-]*\d+`),     // Package id N
		regexp.MustCompile(`(?i)(^|[_\s-])core[_\s-]*\d+`),               // Core N
		regexp.MustCompile(`(?i)(^|[_\s-])tctl$`),                        // AMD Tctl
		regexp.MustCompile(`(?i)(^|[_\s-])tdie$`),                        // AMD Tdie
		regexp.MustCompile(`(?i)(^|[_\s-])tccd\d+$`),                     // AMD CCD temps
		// Uncomment if you want to include generic CPU thermal zones (non-Intel/AMD PCs)
		// regexp.MustCompile(`(?i)^(cpu[-_]?thermal)$`),
	}

	for _, temp := range temps {
		for _, cpuSensor := range cpuRes {
			if cpuSensor.MatchString(temp.SensorKey) {
				coreTemps = append(coreTemps, temp)
				goto Next
			}
		}
	Next:
	}

	return coreTemps, nil
}

// per core
func (c *CPUInfoStruct) GetCPUUsage() ([]float64, error) {
	usage, err := cpu.Percent(1*time.Second, true)
	if err != nil {
		return nil, err
	}
	return usage, nil
}

type Core struct {
	Usage float64
	Temp  float64
}

type CPUCoreInfo struct {
	Cores []Core
}

func (c *CPUInfoStruct) GetCPUInfo() (*CPUCoreInfo, error) {
	// Get per-core CPU usage, try to degrade gracefully on error
	usage, err := c.GetCPUUsage()
	if err != nil {
		return nil, err
	}

	temps, tempErr := c.GetCpuTemps()
	if tempErr != nil {
		temps = nil // Keep going with usage info even if temp sensors fail
	}

	coreTemps := make(map[int]float64, len(temps))
	var packageTemp float64
	var packageTempSet bool

	for _, temp := range temps {
		if packageSensorRegexp.MatchString(temp.SensorKey) {
			if !packageTempSet || temp.Temperature > packageTemp {
				packageTemp = temp.Temperature
				packageTempSet = true
			}
			continue
		}

		matches := coreSensorIndexRegexp.FindStringSubmatch(temp.SensorKey)
		if len(matches) != 2 {
			continue
		}

		idx, convErr := strconv.Atoi(matches[1])
		if convErr != nil {
			continue
		}

		if existing, ok := coreTemps[idx]; !ok || temp.Temperature > existing {
			coreTemps[idx] = temp.Temperature
		}
	}

	cores := make([]Core, len(usage))
	for i, usageVal := range usage {
		core := Core{
			Usage: usageVal,
		}

		if temp, ok := coreTemps[i]; ok {
			core.Temp = temp
		} else if packageTempSet {
			core.Temp = packageTemp
		}

		cores[i] = core
	}

	return &CPUCoreInfo{Cores: cores}, tempErr
}

type StressTestCpuStruct struct{}

func (c *CPUInfoStruct) StressTestCPU(ctx context.Context, timeSeconds, nvcpu int) error {
	if _, err := exec.LookPath("stress-ng"); err != nil {
		return fmt.Errorf("stress-ng not found in PATH. Please install it (e.g., sudo apt-get install -y stress-ng): %w", err)
	}

	args := []string{
		"--cpu", strconv.Itoa(nvcpu),
		"--timeout", fmt.Sprintf("%ds", timeSeconds),
		"--metrics-brief",
	}

	cmd := exec.CommandContext(ctx, "stress-ng", args...)

	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		// Context was canceled or deadline exceeded; ensure we return that explicitly.
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("stress-ng failed: %w\n--- stress-ng output ---\n%s", err, string(out))
	}

	return nil
}
