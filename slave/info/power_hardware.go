package info

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
)

type PowerHardwareStruct struct{}

var PowerHardware PowerHardwareStruct

type CpuSocket struct {
	Model string
	Cores int
}

type PowerDisk struct {
	Name string
	Kind string
}

type PowerHardwareInfo struct {
	Sockets []*CpuSocket
	RAMMB   int64
	Disks   []*PowerDisk
}

func (p *PowerHardwareStruct) GetPowerHardware() (*PowerHardwareInfo, error) {
	cpuInfo, err := cpu.Info()
	if err != nil {
		return nil, fmt.Errorf("cpu info: %w", err)
	}

	ramMB, err := MemInfo.GetMemTotalMB()
	if err != nil {
		return nil, fmt.Errorf("memory total: %w", err)
	}

	disks, err := listPowerDisks("/sys/block")
	if err != nil {
		return nil, fmt.Errorf("list power disks: %w", err)
	}

	return &PowerHardwareInfo{
		Sockets: socketsFromCPUInfo(cpuInfo),
		RAMMB:   int64(ramMB),
		Disks:   disks,
	}, nil
}

type cpuSocketGroup struct {
	model    string
	coreIDs  map[string]struct{}
	maxCores int
}

func socketsFromCPUInfo(infos []cpu.InfoStat) []*CpuSocket {
	groups := make(map[string]*cpuSocketGroup)

	for _, ci := range infos {
		g, ok := groups[ci.PhysicalID]
		if !ok {
			g = &cpuSocketGroup{coreIDs: make(map[string]struct{})}
			groups[ci.PhysicalID] = g
		}

		if g.model == "" {
			g.model = ci.ModelName
		}
		if ci.CoreID != "" {
			g.coreIDs[ci.CoreID] = struct{}{}
		}
		if int(ci.Cores) > g.maxCores {
			g.maxCores = int(ci.Cores)
		}
	}

	physicalIDs := make([]string, 0, len(groups))
	for id := range groups {
		physicalIDs = append(physicalIDs, id)
	}
	sort.Slice(physicalIDs, func(i, j int) bool {
		return comparePhysicalID(physicalIDs[i], physicalIDs[j]) < 0
	})

	sockets := make([]*CpuSocket, 0, len(physicalIDs))
	for _, id := range physicalIDs {
		g := groups[id]

		cores := len(g.coreIDs)
		if cores == 0 {
			cores = g.maxCores
		}

		sockets = append(sockets, &CpuSocket{
			Model: g.model,
			Cores: cores,
		})
	}

	return sockets
}

func comparePhysicalID(a, b string) int {
	ai, aErr := strconv.Atoi(a)
	bi, bErr := strconv.Atoi(b)
	if aErr == nil && bErr == nil {
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(a, b)
}

var skippedBlockPrefixes = []string{"loop", "ram", "dm-", "zram", "sr", "md", "nbd"}

func listPowerDisks(sysBlockDir string) ([]*PowerDisk, error) {
	entries, err := os.ReadDir(sysBlockDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", sysBlockDir, err)
	}

	disks := make([]*PowerDisk, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if skipBlockDevice(name) {
			continue
		}

		kind := ""
		if strings.HasPrefix(name, "nvme") {
			kind = "nvme"
		} else {
			raw, err := os.ReadFile(filepath.Join(sysBlockDir, name, "queue", "rotational"))
			if err != nil {
				continue
			}

			switch strings.TrimSpace(string(raw)) {
			case "1":
				kind = "hdd"
			case "0":
				kind = "ssd"
			default:
				continue
			}
		}

		disks = append(disks, &PowerDisk{Name: name, Kind: kind})
	}

	return disks, nil
}

func skipBlockDevice(name string) bool {
	for _, prefix := range skippedBlockPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
