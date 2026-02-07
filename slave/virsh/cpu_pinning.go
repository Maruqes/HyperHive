package virsh

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
)

// CPUInfo represents a logical CPU with its HT sibling pair
type CPUInfo struct {
	CPUID    int   // logical CPU id
	Siblings []int // list of sibling CPU ids (includes itself + HT pair)
}

// CPUSocket represents a physical CPU socket and all its cores
type CPUSocket struct {
	SocketID int
	CPUs     []CPUInfo
}

// GetCPUSockets reads /sys/devices/system/cpu to discover sockets,
// cores, and hyperthreading siblings. Returns a slice of CPUSocket
// sorted by SocketID, each containing its CPUs sorted by CPUID.
func GetCPUSockets() ([]CPUSocket, error) {
	basePath := "/sys/devices/system/cpu"

	entries, err := os.ReadDir(basePath)
	if err != nil {
		logger.Debugf("ERROR", fmt.Sprintf("cpu_pinning: failed to read %s: %v", basePath, err))
		return nil, fmt.Errorf("failed to read cpu directory: %w", err)
	}

	// Map socket_id -> list of CPUInfo
	socketMap := make(map[int][]CPUInfo)

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "cpu") {
			continue
		}
		cpuIDStr := strings.TrimPrefix(name, "cpu")
		cpuID, err := strconv.Atoi(cpuIDStr)
		if err != nil {
			continue // skip non-numeric entries like cpufreq, cpuidle
		}

		topologyPath := filepath.Join(basePath, name, "topology")

		// Read physical_package_id (socket)
		socketID, err := readIntFromFile(filepath.Join(topologyPath, "physical_package_id"))
		if err != nil {
			// CPU might be offline or topology not available
			continue
		}

		// Read thread_siblings_list to get HT pairs
		siblings, err := parseCPUList(filepath.Join(topologyPath, "thread_siblings_list"))
		if err != nil {
			// Fallback: just use the CPU itself
			siblings = []int{cpuID}
		}

		socketMap[socketID] = append(socketMap[socketID], CPUInfo{
			CPUID:    cpuID,
			Siblings: siblings,
		})
	}

	// Build sorted result
	var sockets []CPUSocket
	for sid, cpus := range socketMap {
		sort.Slice(cpus, func(i, j int) bool {
			return cpus[i].CPUID < cpus[j].CPUID
		})
		sockets = append(sockets, CPUSocket{
			SocketID: sid,
			CPUs:     cpus,
		})
	}
	sort.Slice(sockets, func(i, j int) bool {
		return sockets[i].SocketID < sockets[j].SocketID
	})

	logger.Debugf("INFO", fmt.Sprintf("cpu_pinning: found %d socket(s)", len(sockets)))
	return sockets, nil
}

// PrintCPUSockets is a helper that prints the CPU topology in a readable format
func PrintCPUSockets(sockets []CPUSocket) {
	for _, s := range sockets {
		fmt.Printf("Socket %d:\n", s.SocketID)
		for _, cpu := range s.CPUs {
			sibStrs := make([]string, len(cpu.Siblings))
			for i, sib := range cpu.Siblings {
				sibStrs[i] = strconv.Itoa(sib)
			}
			fmt.Printf("  CPU %d → %s\n", cpu.CPUID, strings.Join(sibStrs, ","))
		}
	}
}

// readIntFromFile reads a file and parses its content as an integer
func readIntFromFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// parseCPUList reads a file containing a CPU list (e.g. "0,28" or "0-3,8-11")
// and returns the individual CPU IDs
func parseCPUList(path string) ([]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return expandCPUList(strings.TrimSpace(string(data)))
}

// expandCPUList expands a CPU list string like "0,28" or "0-3,8-11" into
// individual CPU IDs
func expandCPUList(list string) ([]int, error) {
	var result []int
	parts := strings.Split(list, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			start, err := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid range start: %s", part)
			}
			end, err := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid range end: %s", part)
			}
			for i := start; i <= end; i++ {
				result = append(result, i)
			}
		} else {
			val, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid cpu id: %s", part)
			}
			result = append(result, val)
		}
	}
	sort.Ints(result)
	return result, nil
}
