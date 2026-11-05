package services

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

//go:embed cpu-tdp-table.csv
var cpuTDPCSV []byte

const (
	energyIdleBaseW            = 30.0
	energyIdleCPUTDPFraction   = 0.15
	energyIdleRAMWPerGB        = 0.4
	energyIdlePerHDDW          = 7.0
	energyIdlePerSSDW          = 3.0
	energyIdlePerNVMeW         = 5.0
	energyLoadCPUTDPFraction   = 0.85
	energyUnknownBrandWPerCore = 8.0
	energyRAMMBPerGB           = 1024.0
	energyPercentScale         = 100.0

	energyBrandXeon  = "xeon"
	energyBrandEPYC  = "epyc"
	energyBrandRyzen = "ryzen"
	energyBrandCore  = "core"

	energyCSVVendorColumn   = 0
	energyCSVFamilyColumn   = 1
	energyCSVModelColumn    = 2
	energyCSVCoresColumn    = 3
	energyCSVTDPColumn      = 4
	energyCSVSourceColumn   = 5
	energyCSVExpectedFields = 6
)

// EnergySocket is one physical CPU socket as reported by the host.
type EnergySocket struct {
	Model string
	Cores int
}

// EnergyHardware is the host inventory used by the idle estimate.
type EnergyHardware struct {
	Sockets []EnergySocket
	RAMMB   int64
	HDDs    int
	SSDs    int
	NVMes   int
}

// EnergyVMLoad is the running state of one guest. CPUUsage is a percentage of
// the guest's own vCPU capacity.
type EnergyVMLoad struct {
	Name     string
	CPUUsage float64
	CPUCount int
	Running  bool
}

type cpuTDPRow struct {
	brand    string
	modelKey string
	cores    int
	tdpW     float64
}

var (
	cpuTableOnce    sync.Once
	cpuTableRows    []cpuTDPRow
	cpuTableMedians map[string]float64
)

// EstimateCPUTDP returns the summed package TDP of the sockets. generic is true
// when any socket was not found in the table and a fallback was used.
func EstimateCPUTDP(sockets []EnergySocket) (watts float64, generic bool) {
	ensureCPUTDPTable()
	for _, socket := range sockets {
		cores := nonNegative(socket.Cores)
		brand := brandForModel(socket.Model)
		if brand != "" {
			if row, ok := lookupCPURow(socket.Model, brand); ok {
				watts += row.tdpW
				continue
			}
			if median, ok := cpuTableMedians[brand]; ok {
				watts += median * float64(cores)
				generic = true
				continue
			}
		}
		watts += energyUnknownBrandWPerCore * float64(cores)
		generic = true
	}
	return watts, generic
}

// EstimateHostWatts splits the host estimate into idle and CPU load. hostCPU is
// a 0..1 fraction and is clamped to that range.
func EstimateHostWatts(hw EnergyHardware, hostCPU float64) (idleW, loadW float64, generic bool) {
	cpuTDP, generic := EstimateCPUTDP(hw.Sockets)
	ramGB := float64(hw.RAMMB) / energyRAMMBPerGB
	idleW = energyIdleBaseW +
		energyIdleCPUTDPFraction*cpuTDP +
		energyIdleRAMWPerGB*ramGB +
		energyIdlePerHDDW*float64(hw.HDDs) +
		energyIdlePerSSDW*float64(hw.SSDs) +
		energyIdlePerNVMeW*float64(hw.NVMes)
	loadW = energyLoadCPUTDPFraction * cpuTDP * clampFraction(hostCPU)
	return idleW, loadW, generic
}

// MeanCPUUsageFraction converts per-core usage percentages into a clamped
// 0..1 host fraction. An empty slice means zero.
func MeanCPUUsageFraction(perCorePercents []float64) float64 {
	if len(perCorePercents) == 0 {
		return 0
	}
	var sum float64
	for _, percent := range perCorePercents {
		sum += percent
	}
	return clampFraction(sum / float64(len(perCorePercents)) / energyPercentScale)
}

// SplitVMWatts attributes the CPU load watts across running VMs in proportion
// to CPUUsage*CPUCount. Every VM in the input appears in the result; a stopped
// VM, or any VM when no running weight exists, gets zero.
func SplitVMWatts(loadW float64, vms []EnergyVMLoad) map[string]float64 {
	weights := make([]float64, len(vms))
	var totalWeight float64
	for i, vm := range vms {
		if !vm.Running {
			continue
		}
		usage := vm.CPUUsage
		if usage < 0 {
			usage = 0
		}
		weight := usage * float64(nonNegative(vm.CPUCount))
		weights[i] = weight
		totalWeight += weight
	}

	out := make(map[string]float64, len(vms))
	for i, vm := range vms {
		if totalWeight <= 0 {
			out[vm.Name] = 0
			continue
		}
		out[vm.Name] = loadW * weights[i] / totalWeight
	}
	return out
}

func ensureCPUTDPTable() {
	cpuTableOnce.Do(loadCPUTDPTable)
}

func loadCPUTDPTable() {
	cpuTableMedians = make(map[string]float64)
	records, err := csv.NewReader(bytes.NewReader(cpuTDPCSV)).ReadAll()
	if err != nil || len(records) == 0 {
		return
	}

	perCore := make(map[string][]float64)
	for _, record := range records[1:] {
		if len(record) < energyCSVExpectedFields {
			continue
		}
		cores, err := strconv.Atoi(strings.TrimSpace(record[energyCSVCoresColumn]))
		if err != nil {
			continue
		}
		tdp, err := strconv.ParseFloat(strings.TrimSpace(record[energyCSVTDPColumn]), 64)
		if err != nil {
			continue
		}
		brand := brandForFamily(record[energyCSVFamilyColumn])
		if brand == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(record[energyCSVModelColumn]))
		cpuTableRows = append(cpuTableRows, cpuTDPRow{
			brand:    brand,
			modelKey: key,
			cores:    cores,
			tdpW:     tdp,
		})
		if cores > 0 {
			perCore[brand] = append(perCore[brand], tdp/float64(cores))
		}
	}

	for brand, values := range perCore {
		sort.Float64s(values)
		middle := len(values) / 2
		if len(values)%2 == 1 {
			cpuTableMedians[brand] = values[middle]
			continue
		}
		cpuTableMedians[brand] = (values[middle-1] + values[middle]) / 2
	}
}

func brandForFamily(family string) string {
	switch {
	case strings.HasPrefix(family, "Core"):
		return energyBrandCore
	case strings.Contains(family, "EPYC"):
		return energyBrandEPYC
	case strings.Contains(family, "Ryzen"), strings.Contains(family, "Threadripper"):
		return energyBrandRyzen
	case strings.Contains(family, "Xeon"):
		return energyBrandXeon
	default:
		return ""
	}
}

func brandForModel(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, energyBrandXeon):
		return energyBrandXeon
	case strings.Contains(lower, energyBrandEPYC):
		return energyBrandEPYC
	case strings.Contains(lower, energyBrandRyzen), strings.Contains(lower, "threadripper"):
		return energyBrandRyzen
	case isIntelCoreModel(lower):
		return energyBrandCore
	default:
		return ""
	}
}

func isIntelCoreModel(lower string) bool {
	switch {
	case strings.Contains(lower, "i3-"), strings.Contains(lower, "i5-"),
		strings.Contains(lower, "i7-"), strings.Contains(lower, "i9-"):
		return true
	case !strings.Contains(lower, energyBrandCore):
		return false
	case strings.Contains(lower, "core(tm)"), strings.Contains(lower, "core i"):
		return true
	default:
		return strings.Contains(lower, "intel")
	}
}

func lookupCPURow(model, brand string) (cpuTDPRow, bool) {
	lower := strings.ToLower(model)
	best := -1
	bestLength := -1
	for i := range cpuTableRows {
		row := cpuTableRows[i]
		if row.brand != brand || len(row.modelKey) <= bestLength {
			continue
		}
		if !modelMatchesKey(lower, row.modelKey) {
			continue
		}
		best = i
		bestLength = len(row.modelKey)
	}
	if best < 0 {
		return cpuTDPRow{}, false
	}
	return cpuTableRows[best], true
}

func modelMatchesKey(model, key string) bool {
	for offset := 0; ; {
		index := strings.Index(model[offset:], key)
		if index < 0 {
			return false
		}
		index += offset
		end := index + len(key)
		beforeOK := index == 0 || !isModelWordRune(lastRune(model[:index]))
		afterOK := end == len(model) || !isModelWordRune(firstRune(model[end:]))
		if beforeOK && afterOK && !conflictsWithKeyVersion(model[end:], key) {
			return true
		}
		offset = index + 1
	}
}

// conflictsWithKeyVersion rejects a version-less table key when the CPU string
// carries an explicit " v<digit>" generation right after the match.
func conflictsWithKeyVersion(rest, key string) bool {
	if keyHasVersion(key) {
		return false
	}
	return len(rest) >= 3 && rest[0] == ' ' && rest[1] == 'v' && isASCIIDigit(rest[2])
}

func keyHasVersion(key string) bool {
	index := strings.LastIndex(key, " v")
	return index >= 0 && index+2 < len(key) && isASCIIDigit(key[index+2])
}

func lastRune(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

func isModelWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-'
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func clampFraction(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
