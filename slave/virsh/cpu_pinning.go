package virsh

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
	libvirt "libvirt.org/go/libvirt"
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

// ─── CPU Pinning ─────────────────────────────────────────────────────────────

// PhysicalCore represents a unique physical core and its HT sibling(s)
type PhysicalCore struct {
	CoreIndex  int   // index within the socket (0-based, sorted)
	PhysicalID int   // the lowest CPU ID in the sibling group (the "real" core)
	Siblings   []int // all CPU IDs sharing this core (physical + HT threads)
}

// CPUPinningConfig defines which physical cores to pin a VM to.
// RangeStart and RangeEnd refer to physical core indices (0-based),
// NOT logical CPU IDs. For example, if the socket has 28 physical cores,
// the valid range is 0..27. If HyperThreading is true, the HT siblings
// of each selected core are also included.
type CPUPinningConfig struct {
	RangeStart     int  `json:"range_start"`     // first physical core index (inclusive)
	RangeEnd       int  `json:"range_end"`       // last physical core index (inclusive)
	HyperThreading bool `json:"hyper_threading"` // include HT siblings
	SocketID       int  `json:"socket_id"`       // which socket (default 0)
}

// VCPUPin represents one vcpu→cpuset mapping for libvirt
type VCPUPin struct {
	VCPU   int    // virtual CPU index
	CPUSet string // host CPU set (e.g. "0,28" or "0")
}

// GetPhysicalCores extracts the unique physical cores from a CPUSocket,
// deduplicating by sibling group. Returns them sorted by the lowest
// CPU ID in each group.
func GetPhysicalCores(socket CPUSocket) []PhysicalCore {
	// Group CPUs by their sibling set (use the min sibling as key)
	seen := make(map[int]bool)
	var cores []PhysicalCore

	for _, cpu := range socket.CPUs {
		// The "physical" core is identified by the smallest sibling ID
		minSib := cpu.Siblings[0]
		for _, s := range cpu.Siblings {
			if s < minSib {
				minSib = s
			}
		}
		if seen[minSib] {
			continue
		}
		seen[minSib] = true

		sibs := make([]int, len(cpu.Siblings))
		copy(sibs, cpu.Siblings)
		sort.Ints(sibs)

		cores = append(cores, PhysicalCore{
			PhysicalID: minSib,
			Siblings:   sibs,
		})
	}

	// Sort by PhysicalID and assign CoreIndex
	sort.Slice(cores, func(i, j int) bool {
		return cores[i].PhysicalID < cores[j].PhysicalID
	})
	for i := range cores {
		cores[i].CoreIndex = i
	}

	return cores
}

// ValidateCPUPinningConfig checks that:
//   - The socket exists
//   - RangeStart <= RangeEnd
//   - RangeEnd < number of physical cores in the socket
//   - Each physical core has a valid HT sibling mapping
func ValidateCPUPinningConfig(config CPUPinningConfig, sockets []CPUSocket) error {
	if config.RangeStart < 0 {
		return fmt.Errorf("range_start must be >= 0, got %d", config.RangeStart)
	}
	if config.RangeEnd < config.RangeStart {
		return fmt.Errorf("range_end (%d) must be >= range_start (%d)", config.RangeEnd, config.RangeStart)
	}

	// Find the requested socket
	var found bool
	var socket CPUSocket
	for _, s := range sockets {
		if s.SocketID == config.SocketID {
			socket = s
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("socket %d not found (available: %v)", config.SocketID, socketIDs(sockets))
	}

	physCores := GetPhysicalCores(socket)
	physCount := len(physCores)

	if physCount == 0 {
		return fmt.Errorf("socket %d has no physical cores", config.SocketID)
	}

	if config.RangeEnd >= physCount {
		return fmt.Errorf("range_end (%d) exceeds physical core count (%d), max allowed is %d",
			config.RangeEnd, physCount, physCount-1)
	}

	// Validate HT sibling consistency
	if config.HyperThreading {
		for i := config.RangeStart; i <= config.RangeEnd; i++ {
			core := physCores[i]
			if len(core.Siblings) < 2 {
				return fmt.Errorf("physical core %d (CPU %d) has no HT sibling — HyperThreading requested but not available",
					i, core.PhysicalID)
			}
		}
	}

	return nil
}

// BuildVCPUPins creates the vcpu-to-cpuset mappings for the given config.
// Without HT: one vCPU per physical core, pinned to that core only.
// With HT: two vCPUs per physical core — one pinned to the physical thread,
//
//	one pinned to the HT sibling. This matches the guest topology threads='2'.
func BuildVCPUPins(config CPUPinningConfig, sockets []CPUSocket) ([]VCPUPin, error) {
	if err := ValidateCPUPinningConfig(config, sockets); err != nil {
		return nil, err
	}

	var socket CPUSocket
	for _, s := range sockets {
		if s.SocketID == config.SocketID {
			socket = s
			break
		}
	}

	physCores := GetPhysicalCores(socket)
	selectedCores := physCores[config.RangeStart : config.RangeEnd+1]

	var pins []VCPUPin
	vcpuIdx := 0

	for _, core := range selectedCores {
		if config.HyperThreading {
			// Each sibling gets its own vCPU, pinned 1:1
			// This gives the guest a proper threads='2' topology
			for _, sib := range core.Siblings {
				pins = append(pins, VCPUPin{
					VCPU:   vcpuIdx,
					CPUSet: strconv.Itoa(sib),
				})
				vcpuIdx++
			}
		} else {
			// Pin this vCPU only to the physical core (first/lowest sibling)
			pins = append(pins, VCPUPin{
				VCPU:   vcpuIdx,
				CPUSet: strconv.Itoa(core.PhysicalID),
			})
			vcpuIdx++
		}
	}

	return pins, nil
}

// VCPUCount returns the total number of vCPUs for a given pinning config.
// Without HT: number of physical cores in range.
// With HT: number of physical cores * 2 (each core has 2 threads).
func VCPUCount(config CPUPinningConfig) int {
	numCores := config.RangeEnd - config.RangeStart + 1
	if config.HyperThreading {
		return numCores * 2
	}
	return numCores
}

// BuildCPUTopologyXML generates the <cpu> block with the correct <topology> for pinning.
// With HT:    <topology sockets='1' cores='N' threads='2'/>
// Without HT: <topology sockets='1' cores='N' threads='1'/>
func BuildCPUTopologyXML(config CPUPinningConfig, indent string) string {
	numCores := config.RangeEnd - config.RangeStart + 1
	threads := 1
	if config.HyperThreading {
		threads = 2
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s<cpu mode='host-passthrough' check='none' migratable='on'>\n", indent))
	sb.WriteString(fmt.Sprintf("%s  <topology sockets='1' cores='%d' threads='%d'/>\n", indent, numCores, threads))
	sb.WriteString(fmt.Sprintf("%s</cpu>", indent))
	return sb.String()
}

// BuildVCPUTagXML generates the <vcpu> tag for the given pinning config.
func BuildVCPUTagXML(config CPUPinningConfig, indent string) string {
	count := VCPUCount(config)
	return fmt.Sprintf("%s<vcpu placement='static'>%d</vcpu>", indent, count)
}

// BuildFullPinningXML generates the complete XML blocks needed for CPU pinning:
// <vcpu>, <cputune>, and <cpu> with <topology>.
// Useful for previewing what will be applied to a VM.
func BuildFullPinningXML(config CPUPinningConfig, sockets []CPUSocket, indent string) (string, error) {
	pins, err := BuildVCPUPins(config, sockets)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(BuildVCPUTagXML(config, indent))
	sb.WriteString("\n")
	sb.WriteString(BuildCPUTuneXML(pins, indent))
	sb.WriteString("\n")
	sb.WriteString(BuildCPUTopologyXML(config, indent))
	return sb.String(), nil
}

// BuildCPUTuneXML generates the <cputune> XML block for libvirt.
// Emulatorpin is set to the union of all pinned CPUs.
func BuildCPUTuneXML(pins []VCPUPin, indent string) string {
	var sb strings.Builder
	sb.WriteString(indent + "<cputune>\n")
	for _, pin := range pins {
		sb.WriteString(fmt.Sprintf("%s  <vcpupin vcpu='%d' cpuset='%s'/>\n", indent, pin.VCPU, pin.CPUSet))
	}

	allCPUs := collectAllCPUs(pins)
	if len(allCPUs) > 0 {
		sb.WriteString(fmt.Sprintf("%s  <emulatorpin cpuset='%s'/>\n", indent, formatCPUSet(allCPUs)))
	}

	sb.WriteString(indent + "</cputune>")
	return sb.String()
}

// ApplyCPUPinning applies CPU pinning to a VM by modifying its libvirt XML definition.
// The VM must be shut off.
func ApplyCPUPinning(vmName string, config CPUPinningConfig) error {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return fmt.Errorf("vm name is empty")
	}

	// Get host topology
	sockets, err := GetCPUSockets()
	if err != nil {
		return fmt.Errorf("get cpu topology: %w", err)
	}

	// Build pin mappings (validates config internally)
	pins, err := BuildVCPUPins(config, sockets)
	if err != nil {
		return fmt.Errorf("build vcpu pins: %w", err)
	}

	if len(pins) == 0 {
		return fmt.Errorf("no vcpu pins generated")
	}

	// Connect to libvirt
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("lookup vm %q: %w", vmName, err)
	}
	defer dom.Free()

	// Check VM is shut off
	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}
	if state != libvirt.DOMAIN_SHUTOFF {
		return fmt.Errorf("vm %q must be shut off before applying cpu pinning (current state: %d)", vmName, state)
	}

	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return fmt.Errorf("get xml: %w", err)
	}

	// Update vcpu count to match pins (with HT: cores*2, without: cores)
	vcpuCount := len(pins)
	updatedXML, err := updateVcpuTag(xmlDesc, vcpuCount)
	if err != nil {
		return fmt.Errorf("update vcpu count: %w", err)
	}

	// Update CPU topology (threads='2' if HT, threads='1' otherwise)
	numCores := config.RangeEnd - config.RangeStart + 1
	threadsPerCore := 1
	if config.HyperThreading {
		threadsPerCore = 2
	}
	updatedXML, err = updateCPUTopologyForPinning(updatedXML, numCores, threadsPerCore)
	if err != nil {
		return fmt.Errorf("update cpu topology: %w", err)
	}

	// Remove existing <cputune> if present
	updatedXML = removeCPUTuneBlock(updatedXML)

	// Insert new <cputune> block after </vcpu>
	cputuneXML := BuildCPUTuneXML(pins, "  ")
	updatedXML = insertCPUTuneBlock(updatedXML, cputuneXML)

	// Redefine the domain
	newDom, err := conn.DomainDefineXML(updatedXML)
	if err != nil {
		return fmt.Errorf("define xml: %w", err)
	}
	defer newDom.Free()

	logger.Debugf("INFO", fmt.Sprintf("cpu_pinning: applied %d vcpu pin(s) to vm %q (socket %d, cores %d-%d, ht=%v)",
		len(pins), vmName, config.SocketID, config.RangeStart, config.RangeEnd, config.HyperThreading))

	return nil
}

// RemoveCPUPinning removes the <cputune> block from a VM's XML definition.
func RemoveCPUPinning(vmName string) error {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return fmt.Errorf("vm name is empty")
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("lookup vm %q: %w", vmName, err)
	}
	defer dom.Free()

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}
	if state != libvirt.DOMAIN_SHUTOFF {
		return fmt.Errorf("vm %q must be shut off before removing cpu pinning", vmName)
	}

	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return fmt.Errorf("get xml: %w", err)
	}

	updatedXML := removeCPUTuneBlock(xmlDesc)
	if updatedXML == xmlDesc {
		return nil // nothing to remove
	}

	// Reset topology threads back to 1, keeping current vcpu count as cores
	vcpuRe := regexp.MustCompile(`<vcpu[^>]*>(\d+)</vcpu>`)
	if m := vcpuRe.FindStringSubmatch(updatedXML); m != nil {
		vcpuCount, _ := strconv.Atoi(m[1])
		if vcpuCount > 0 {
			updatedXML, _ = updateCPUTopologyForPinning(updatedXML, vcpuCount, 1)
		}
	}

	newDom, err := conn.DomainDefineXML(updatedXML)
	if err != nil {
		return fmt.Errorf("define xml: %w", err)
	}
	defer newDom.Free()

	logger.Debugf("INFO", fmt.Sprintf("cpu_pinning: removed cpu pinning from vm %q", vmName))
	return nil
}

// CPUPinningResult holds the full parsed CPU pinning state from a VM's XML.
type CPUPinningResult struct {
	HasPinning     bool
	Pins           []VCPUPin
	HyperThreading bool
	RangeStart     int
	RangeEnd       int
	SocketID       int
}

// GetCPUPinning reads the current CPU pinning configuration from a VM's libvirt XML.
// Returns has_pinning=false if no <cputune> block is present.
func GetCPUPinning(vmName string) (*CPUPinningResult, error) {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return nil, fmt.Errorf("vm name is empty")
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return nil, fmt.Errorf("lookup vm %q: %w", vmName, err)
	}
	defer dom.Free()

	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return nil, fmt.Errorf("get xml: %w", err)
	}

	return parseCPUPinningFromXML(xmlDesc)
}

// parseCPUPinningFromXML extracts vcpupin entries and topology info from a VM XML string.
func parseCPUPinningFromXML(xmlStr string) (*CPUPinningResult, error) {
	result := &CPUPinningResult{}

	// Parse <cputune> pins
	cputuneRe := regexp.MustCompile(`(?ms)<cputune>(.*?)</cputune>`)
	match := cputuneRe.FindStringSubmatch(xmlStr)
	if match == nil {
		return result, nil
	}

	vcpupinRe := regexp.MustCompile(`<vcpupin\s+vcpu='(\d+)'\s+cpuset='([^']+)'/>`)
	pinMatches := vcpupinRe.FindAllStringSubmatch(match[1], -1)
	if len(pinMatches) == 0 {
		return result, nil
	}

	result.HasPinning = true
	result.Pins = make([]VCPUPin, 0, len(pinMatches))
	for _, pm := range pinMatches {
		vcpu, err := strconv.Atoi(pm[1])
		if err != nil {
			continue
		}
		result.Pins = append(result.Pins, VCPUPin{VCPU: vcpu, CPUSet: pm[2]})
	}

	// Parse <topology sockets='...' cores='...' threads='...'/> to detect HT
	topoRe := regexp.MustCompile(`<topology\s+[^/]*threads='(\d+)'[^/]*/>`)
	topoMatch := topoRe.FindStringSubmatch(xmlStr)
	if topoMatch != nil {
		threads, _ := strconv.Atoi(topoMatch[1])
		result.HyperThreading = threads >= 2
	}

	// Infer range from cpuset values: collect all unique host CPU IDs
	allCPUs := collectAllCPUs(result.Pins)
	if len(allCPUs) > 0 {
		// With HT, each physical core maps to 2 pins (core, core+N).
		// The range refers to physical core indices. Try to get from host topology.
		sockets, err := GetCPUSockets()
		if err == nil && len(sockets) > 0 {
			// Build a map: host CPU ID -> physical core index
			type coreMapping struct {
				CoreIndex int
				SocketID  int
			}
			cpuToCore := make(map[int]coreMapping)
			for _, sock := range sockets {
				physCores := GetPhysicalCores(sock)
				for _, pc := range physCores {
					for _, sib := range pc.Siblings {
						cpuToCore[sib] = coreMapping{CoreIndex: pc.CoreIndex, SocketID: sock.SocketID}
					}
				}
			}

			// Find min/max core indices and socket from the pinned CPUs
			minCore := -1
			maxCore := -1
			for _, cpuID := range allCPUs {
				if cm, ok := cpuToCore[cpuID]; ok {
					if minCore == -1 || cm.CoreIndex < minCore {
						minCore = cm.CoreIndex
					}
					if maxCore == -1 || cm.CoreIndex > maxCore {
						maxCore = cm.CoreIndex
					}
					result.SocketID = cm.SocketID
				}
			}
			if minCore >= 0 {
				result.RangeStart = minCore
				result.RangeEnd = maxCore
			}
		}
	}

	return result, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func socketIDs(sockets []CPUSocket) []int {
	ids := make([]int, len(sockets))
	for i, s := range sockets {
		ids[i] = s.SocketID
	}
	return ids
}

// updateCPUTopologyForPinning updates the <cpu><topology> block to match the pinning config.
// Sets sockets='1', cores=numCores, threads=threadsPerCore.
func updateCPUTopologyForPinning(xmlStr string, numCores, threadsPerCore int) (string, error) {
	vcpuCount := numCores * threadsPerCore

	// First try to update existing <topology> within <cpu> block
	cpuBlockPattern := regexp.MustCompile(`(?ms)([ \t]*)<cpu\b([^>]*)>.*?</cpu>`)
	if loc := cpuBlockPattern.FindStringIndex(xmlStr); loc != nil {
		block := xmlStr[loc[0]:loc[1]]

		// Replace topology attributes directly
		topologyPattern := regexp.MustCompile(`<topology\b[^/]*/>`)
		if topologyPattern.MatchString(block) {
			newTopology := fmt.Sprintf("<topology sockets='1' cores='%d' threads='%d'/>", numCores, threadsPerCore)
			updatedBlock := topologyPattern.ReplaceAllString(block, newTopology)
			return xmlStr[:loc[0]] + updatedBlock + xmlStr[loc[1]:], nil
		}

		// No topology line in cpu block — insert one before </cpu>
		closeCPU := strings.LastIndex(block, "</cpu>")
		if closeCPU >= 0 {
			indent := extractLeadingWhitespace(block)
			topologyLine := fmt.Sprintf("%s  <topology sockets='1' cores='%d' threads='%d'/>\n", indent, numCores, threadsPerCore)
			updatedBlock := block[:closeCPU] + topologyLine + block[closeCPU:]
			return xmlStr[:loc[0]] + updatedBlock + xmlStr[loc[1]:], nil
		}
	}

	// Handle self-closing <cpu .../>
	cpuSelfClosing := regexp.MustCompile(`(?m)([ \t]*)<cpu\b([^>]*)/>`)
	if loc := cpuSelfClosing.FindStringSubmatchIndex(xmlStr); loc != nil {
		indent := xmlStr[loc[2]:loc[3]]
		attrs := xmlStr[loc[4]:loc[5]]
		replacement := fmt.Sprintf("%s<cpu%s>\n%s  <topology sockets='1' cores='%d' threads='%d'/>\n%s</cpu>",
			indent, attrs, indent, numCores, threadsPerCore, indent)
		return xmlStr[:loc[0]] + replacement + xmlStr[loc[1]:], nil
	}

	// No <cpu> block at all — use the generic updateDomainCPUTopology as fallback
	return updateDomainCPUTopology(xmlStr, vcpuCount)
}

// removeCPUTuneBlock removes any existing <cputune>...</cputune> block from XML
func removeCPUTuneBlock(xmlStr string) string {
	// Match <cputune> ... </cputune> including newlines
	re := regexp.MustCompile(`(?ms)\s*<cputune>.*?</cputune>\s*`)
	return re.ReplaceAllString(xmlStr, "\n")
}

// insertCPUTuneBlock inserts the cputune XML block after the </vcpu> tag
func insertCPUTuneBlock(xmlStr string, cputuneXML string) string {
	// Try to insert after </vcpu>
	vcpuEnd := regexp.MustCompile(`(?m)(</vcpu>)`)
	if loc := vcpuEnd.FindStringIndex(xmlStr); loc != nil {
		return xmlStr[:loc[1]] + "\n" + cputuneXML + xmlStr[loc[1]:]
	}

	// Fallback: insert after <domain ...> opening tag
	domainTag := regexp.MustCompile(`(?m)(<domain[^>]*>)`)
	if loc := domainTag.FindStringIndex(xmlStr); loc != nil {
		return xmlStr[:loc[1]] + "\n" + cputuneXML + xmlStr[loc[1]:]
	}

	return xmlStr
}

// collectAllCPUs extracts all unique host CPU IDs from a set of VCPUPins
func collectAllCPUs(pins []VCPUPin) []int {
	seen := make(map[int]bool)
	for _, pin := range pins {
		parts := strings.Split(pin.CPUSet, ",")
		for _, p := range parts {
			if id, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
				seen[id] = true
			}
		}
	}
	result := make([]int, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Ints(result)
	return result
}

// formatCPUSet formats a sorted list of CPU IDs into a compact cpuset string
// e.g. [0,1,2,3,28,29,30,31] → "0-3,28-31"
func formatCPUSet(cpus []int) string {
	if len(cpus) == 0 {
		return ""
	}

	var parts []string
	start := cpus[0]
	end := cpus[0]

	for i := 1; i < len(cpus); i++ {
		if cpus[i] == end+1 {
			end = cpus[i]
		} else {
			if start == end {
				parts = append(parts, strconv.Itoa(start))
			} else {
				parts = append(parts, fmt.Sprintf("%d-%d", start, end))
			}
			start = cpus[i]
			end = cpus[i]
		}
	}
	if start == end {
		parts = append(parts, strconv.Itoa(start))
	} else {
		parts = append(parts, fmt.Sprintf("%d-%d", start, end))
	}

	return strings.Join(parts, ",")
}
