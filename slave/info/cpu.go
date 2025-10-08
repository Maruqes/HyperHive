package info

import (
	"flag"
	"fmt"
	"time"

	"github.com/klauspost/cpuid/v2"
	"github.com/shirou/gopsutil/v4/cpu"
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

//sudo dnf install lm_sensors lm_sensors-devel gcc

func (c *CPUInfoStruct) GetCpuTemps() ([]float64, error) {
	panic("unimplemented")
	return nil, nil
}

// per core
func (c *CPUInfoStruct) GetCPUUsage() ([]float64, error) {
	usage, err := cpu.Percent(1*time.Second, true)
	if err != nil {
		return nil, err
	}
	return usage, nil
}

// CPUS TEM DE SER OS MESMOS NO MESMO NO, nao pode ter um servidor com 2 cpus diferentes e5-2680v3 e e5-2680v4
// use https://github.com/klauspost/cpuid
func (c *CPUInfoStruct) GetCPUInfo() (cpuid.CPUInfo, error) {
	cpuid.Flags()
	flag.Parse()
	cpuid.Detect()
	//get all features
	return cpuid.CPU, nil
}
