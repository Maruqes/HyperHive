package info

import (
	"fmt"
	"regexp"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/sensors"
)

type CPUInfoStruct struct{}

var CPUInfo CPUInfoStruct

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
