package virsh

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

func buildCPUTuneXML(vcpuCount int) (string, error) {
	if vcpuCount <= 0 {
		return "", nil
	}

	hostCPUs, err := detectOnlineCPUs()
	if err != nil {
		return "", err
	}
	if len(hostCPUs) == 0 {
		return "", fmt.Errorf("no online host CPUs detected")
	}

	var pinPool []int
	emulatorCPUs := hostCPUs

	if len(hostCPUs) > vcpuCount {
		pinPool = append([]int(nil), hostCPUs[1:]...)
		if len(pinPool) == 0 {
			pinPool = append([]int(nil), hostCPUs...)
		}
		emulatorCPUs = hostCPUs[:1]
	} else {
		pinPool = append([]int(nil), hostCPUs...)
	}

	var vcpuPins []string
	for vcpu := 0; vcpu < vcpuCount; vcpu++ {
		hostCPU := pinPool[vcpu%len(pinPool)]
		vcpuPins = append(vcpuPins, fmt.Sprintf("    <vcpupin vcpu='%d' cpuset='%d'/>", vcpu, hostCPU))
	}

	emulatorSet := formatCPUSet(emulatorCPUs)
	if emulatorSet == "" {
		emulatorSet = formatCPUSet(hostCPUs)
	}
	iothreadSet := emulatorSet

	shares := vcpuCount * 1024
	if shares < 1024 {
		shares = 1024
	}

	var b strings.Builder
	b.WriteString("  <cputune>\n")
	for _, line := range vcpuPins {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("    <emulatorpin cpuset='%s'/>\n", emulatorSet))
	b.WriteString(fmt.Sprintf("    <iothreadpin iothread='1' cpuset='%s'/>\n", iothreadSet))
	b.WriteString(fmt.Sprintf("    <shares>%d</shares>\n", shares))
	b.WriteString("  </cputune>")

	return b.String(), nil
}

func detectOnlineCPUs() ([]int, error) {
	const cpuOnlinePath = "/sys/devices/system/cpu/online"

	data, err := os.ReadFile(cpuOnlinePath)
	if err == nil {
		cpus, parseErr := parseCPUSet(strings.TrimSpace(string(data)))
		if parseErr != nil {
			return nil, parseErr
		}
		if len(cpus) > 0 {
			return cpus, nil
		}
	}

	count := runtime.NumCPU()
	if count <= 0 {
		return nil, fmt.Errorf("runtime reported no CPUs")
	}
	cpus := make([]int, count)
	for i := range cpus {
		cpus[i] = i
	}
	return cpus, nil
}

func parseCPUSet(spec string) ([]int, error) {
	if spec == "" {
		return nil, nil
	}

	var cpus []int
	parts := strings.Split(spec, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			if len(bounds) != 2 {
				return nil, fmt.Errorf("invalid cpu range: %s", part)
			}
			start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid cpu range start %q: %w", bounds[0], err)
			}
			end, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid cpu range end %q: %w", bounds[1], err)
			}
			if end < start {
				return nil, fmt.Errorf("invalid cpu range %s", part)
			}
			for cpu := start; cpu <= end; cpu++ {
				cpus = append(cpus, cpu)
			}
			continue
		}

		value, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid cpu value %q: %w", part, err)
		}
		cpus = append(cpus, value)
	}

	if len(cpus) == 0 {
		return cpus, nil
	}
	sort.Ints(cpus)
	deduped := cpus[:1]
	for _, cpu := range cpus[1:] {
		if cpu != deduped[len(deduped)-1] {
			deduped = append(deduped, cpu)
		}
	}
	return deduped, nil
}

func formatCPUSet(cpus []int) string {
	if len(cpus) == 0 {
		return ""
	}
	sorted := append([]int(nil), cpus...)
	sort.Ints(sorted)

	var parts []string
	start := sorted[0]
	prev := sorted[0]

	for i := 1; i < len(sorted); i++ {
		current := sorted[i]
		if current == prev+1 {
			prev = current
			continue
		}
		parts = append(parts, renderCPURange(start, prev))
		start = current
		prev = current
	}
	parts = append(parts, renderCPURange(start, prev))
	return strings.Join(parts, ",")
}

func renderCPURange(start, end int) string {
	if start == end {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}
