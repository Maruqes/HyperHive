package virsh

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	coreIsolationArgIsolcpus = "isolcpus"
	coreIsolationArgNohzFull = "nohz_full"
	coreIsolationArgRcuNocbs = "rcu_nocbs"
)

type HostCoreIsolationSocketSelection struct {
	SocketID    int
	CoreIndices []int
}

type HostCoreIsolationRequest struct {
	Sockets []HostCoreIsolationSocketSelection
}

type HostCoreIsolationSocketState struct {
	SocketID            int
	TotalPhysicalCores  int
	MaxIsolatableCores  int
	IsolatedCoreIndices []int
}

type HostCoreIsolationState struct {
	Enabled        bool
	RebootRequired bool
	Message        string

	Isolcpus       string
	NohzFull       string
	RcuNocbs       string
	ActiveIsolcpus string

	ConfiguredCPUs []int
	ActiveCPUs     []int

	Sockets []HostCoreIsolationSocketState
}

type hostIsolationKernelArgs struct {
	IsolcpusRaw string
	NohzFullRaw string
	RcuNocbsRaw string

	IsolcpusNorm string
	NohzFullNorm string
	RcuNocbsNorm string

	ConfiguredCPUs []int
}

func GetHostCoreIsolation() (*HostCoreIsolationState, error) {
	configuredArgs, err := getDefaultKernelArgs()
	if err != nil {
		return nil, err
	}
	activeArgs, err := getActiveKernelArgs()
	if err != nil {
		return nil, err
	}

	configured, err := parseHostIsolationKernelArgs(configuredArgs)
	if err != nil {
		return nil, err
	}
	active, err := parseHostIsolationKernelArgs(activeArgs)
	if err != nil {
		return nil, err
	}

	sockets, err := GetCPUSockets()
	if err != nil {
		return nil, err
	}
	socketStates := buildHostCoreIsolationSocketStates(sockets, configured.ConfiguredCPUs)

	enabled := configured.IsolcpusRaw != "" || configured.NohzFullRaw != "" || configured.RcuNocbsRaw != ""
	rebootRequired := configured.IsolcpusNorm != active.IsolcpusNorm ||
		configured.NohzFullNorm != active.NohzFullNorm ||
		configured.RcuNocbsNorm != active.RcuNocbsNorm

	state := &HostCoreIsolationState{
		Enabled:        enabled,
		RebootRequired: rebootRequired,
		Isolcpus:       configured.IsolcpusRaw,
		NohzFull:       configured.NohzFullRaw,
		RcuNocbs:       configured.RcuNocbsRaw,
		ActiveIsolcpus: active.IsolcpusRaw,
		ConfiguredCPUs: append([]int(nil), configured.ConfiguredCPUs...),
		ActiveCPUs:     append([]int(nil), active.ConfiguredCPUs...),
		Sockets:        socketStates,
	}
	return state, nil
}

func SetHostCoreIsolation(req HostCoreIsolationRequest) (*HostCoreIsolationState, error) {
	sockets, err := GetCPUSockets()
	if err != nil {
		return nil, err
	}

	selectedCPUs, err := buildValidatedHostIsolationCPUSet(req, sockets)
	if err != nil {
		return nil, err
	}
	if len(selectedCPUs) == 0 {
		return nil, fmt.Errorf("no cores selected for isolation; use remove endpoint to clear isolation")
	}

	if !hasBinary("grubby") {
		return nil, fmt.Errorf("grubby is required")
	}

	cpuList := compressCPUList(selectedCPUs)
	if err := clearHostCoreIsolationKernelArgs(); err != nil {
		return nil, err
	}
	argsValue := fmt.Sprintf("%s=%s %s=%s %s=%s",
		coreIsolationArgIsolcpus, cpuList,
		coreIsolationArgNohzFull, cpuList,
		coreIsolationArgRcuNocbs, cpuList,
	)
	if err := runCmdDiscardOutputMaybeSudo("grubby", "--update-kernel=ALL", "--args", argsValue); err != nil {
		return nil, err
	}

	state, err := GetHostCoreIsolation()
	if err != nil {
		return nil, err
	}
	state.Message = "host core isolation updated"
	if state.RebootRequired {
		state.Message += " (reboot required)"
	}
	return state, nil
}

func RemoveHostCoreIsolation() (*HostCoreIsolationState, error) {
	if !hasBinary("grubby") {
		return nil, fmt.Errorf("grubby is required")
	}

	if err := clearHostCoreIsolationKernelArgs(); err != nil {
		return nil, err
	}

	state, err := GetHostCoreIsolation()
	if err != nil {
		return nil, err
	}
	state.Message = "host core isolation removed"
	if state.RebootRequired {
		state.Message += " (reboot required)"
	}
	return state, nil
}

func clearHostCoreIsolationKernelArgs() error {
	return runCmdDiscardOutputMaybeSudo(
		"grubby",
		"--update-kernel=ALL",
		"--remove-args",
		fmt.Sprintf("%s %s %s", coreIsolationArgIsolcpus, coreIsolationArgNohzFull, coreIsolationArgRcuNocbs),
	)
}

func buildValidatedHostIsolationCPUSet(req HostCoreIsolationRequest, sockets []CPUSocket) ([]int, error) {
	if len(req.Sockets) == 0 {
		return nil, fmt.Errorf("at least one socket selection is required")
	}

	socketByID := make(map[int]CPUSocket, len(sockets))
	for _, s := range sockets {
		socketByID[s.SocketID] = s
	}

	seenSockets := make(map[int]bool, len(req.Sockets))
	var selectedLogicalCPUs []int

	for _, sel := range req.Sockets {
		if seenSockets[sel.SocketID] {
			return nil, fmt.Errorf("socket %d provided more than once", sel.SocketID)
		}
		seenSockets[sel.SocketID] = true

		socket, ok := socketByID[sel.SocketID]
		if !ok {
			return nil, fmt.Errorf("socket %d not found", sel.SocketID)
		}

		physCores := GetPhysicalCores(socket)
		totalPhysCores := len(physCores)
		if totalPhysCores == 0 {
			return nil, fmt.Errorf("socket %d has no physical cores", sel.SocketID)
		}

		maxIsolatable := totalPhysCores / 2
		coreIndices := normalizeCoreIndices(sel.CoreIndices)
		if len(coreIndices) == 0 {
			return nil, fmt.Errorf("socket %d must include at least one physical core index", sel.SocketID)
		}
		if len(coreIndices) > maxIsolatable {
			return nil, fmt.Errorf(
				"socket %d exceeds isolation limit: requested %d physical cores, max allowed is %d (50%% of %d)",
				sel.SocketID, len(coreIndices), maxIsolatable, totalPhysCores,
			)
		}

		for _, idx := range coreIndices {
			if idx < 0 || idx >= totalPhysCores {
				return nil, fmt.Errorf(
					"socket %d core index %d out of range (valid: 0..%d)",
					sel.SocketID, idx, totalPhysCores-1,
				)
			}
			// Isolate the whole physical core: all siblings (HT threads) are included.
			for _, cpuID := range physCores[idx].Siblings {
				selectedLogicalCPUs = append(selectedLogicalCPUs, cpuID)
			}
		}
	}

	return uniqueSortedInts(selectedLogicalCPUs), nil
}

func buildHostCoreIsolationSocketStates(sockets []CPUSocket, isolatedCPUs []int) []HostCoreIsolationSocketState {
	isolatedSet := make(map[int]bool, len(isolatedCPUs))
	for _, cpu := range isolatedCPUs {
		isolatedSet[cpu] = true
	}

	states := make([]HostCoreIsolationSocketState, 0, len(sockets))
	for _, sock := range sockets {
		physCores := GetPhysicalCores(sock)
		state := HostCoreIsolationSocketState{
			SocketID:           sock.SocketID,
			TotalPhysicalCores: len(physCores),
			MaxIsolatableCores: len(physCores) / 2,
		}

		for _, core := range physCores {
			if len(core.Siblings) == 0 {
				continue
			}
			allSiblingsIsolated := true
			for _, sib := range core.Siblings {
				if !isolatedSet[sib] {
					allSiblingsIsolated = false
					break
				}
			}
			if allSiblingsIsolated {
				state.IsolatedCoreIndices = append(state.IsolatedCoreIndices, core.CoreIndex)
			}
		}
		states = append(states, state)
	}

	return states
}

func normalizeCoreIndices(indices []int) []int {
	if len(indices) == 0 {
		return nil
	}
	out := append([]int(nil), indices...)
	sort.Ints(out)
	dst := 0
	for i, v := range out {
		if i == 0 || v != out[i-1] {
			out[dst] = v
			dst++
		}
	}
	return out[:dst]
}

func uniqueSortedInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	out := append([]int(nil), values...)
	sort.Ints(out)
	dst := 0
	for i, v := range out {
		if i == 0 || v != out[i-1] {
			out[dst] = v
			dst++
		}
	}
	return out[:dst]
}

func compressCPUList(values []int) string {
	values = uniqueSortedInts(values)
	if len(values) == 0 {
		return ""
	}

	var parts []string
	start := values[0]
	prev := values[0]
	for i := 1; i < len(values); i++ {
		v := values[i]
		if v == prev+1 {
			prev = v
			continue
		}
		parts = append(parts, formatCPURange(start, prev))
		start = v
		prev = v
	}
	parts = append(parts, formatCPURange(start, prev))

	return strings.Join(parts, ",")
}

func formatCPURange(start, end int) string {
	if start == end {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}

func getDefaultKernelArgs() (map[string]string, error) {
	if !hasBinary("grubby") {
		return nil, fmt.Errorf("grubby is required")
	}

	out, err := runCmdOutputMaybeSudo("grubby", "--info=DEFAULT")
	if err != nil {
		return nil, err
	}

	argsLine, err := extractGrubbyArgsLine(out)
	if err != nil {
		return nil, err
	}
	return parseKernelArgs(argsLine), nil
}

func getActiveKernelArgs() (map[string]string, error) {
	data, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return nil, fmt.Errorf("read /proc/cmdline: %w", err)
	}
	return parseKernelArgs(strings.TrimSpace(string(data))), nil
}

func extractGrubbyArgsLine(out string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "args=") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "args="))
		if raw == "" {
			return "", nil
		}
		if unquoted, err := strconv.Unquote(raw); err == nil {
			return unquoted, nil
		}
		return strings.Trim(raw, `"`), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("parse grubby output: %w", err)
	}
	return "", fmt.Errorf("grubby output did not contain args=")
}

func parseKernelArgs(cmdline string) map[string]string {
	args := make(map[string]string)
	for _, tok := range strings.Fields(strings.TrimSpace(cmdline)) {
		if tok == "" {
			continue
		}
		if key, value, ok := strings.Cut(tok, "="); ok {
			args[strings.TrimSpace(key)] = strings.TrimSpace(value)
			continue
		}
		args[strings.TrimSpace(tok)] = ""
	}
	return args
}

func parseHostIsolationKernelArgs(args map[string]string) (*hostIsolationKernelArgs, error) {
	result := &hostIsolationKernelArgs{
		IsolcpusRaw: strings.TrimSpace(args[coreIsolationArgIsolcpus]),
		NohzFullRaw: strings.TrimSpace(args[coreIsolationArgNohzFull]),
		RcuNocbsRaw: strings.TrimSpace(args[coreIsolationArgRcuNocbs]),
	}

	var err error
	result.IsolcpusNorm, result.ConfiguredCPUs, err = normalizeKernelCPUArg(coreIsolationArgIsolcpus, result.IsolcpusRaw)
	if err != nil {
		return nil, err
	}
	result.NohzFullNorm, _, err = normalizeKernelCPUArg(coreIsolationArgNohzFull, result.NohzFullRaw)
	if err != nil {
		return nil, err
	}
	result.RcuNocbsNorm, _, err = normalizeKernelCPUArg(coreIsolationArgRcuNocbs, result.RcuNocbsRaw)
	if err != nil {
		return nil, err
	}

	if len(result.ConfiguredCPUs) == 0 {
		_, cpus, err := normalizeKernelCPUArg(coreIsolationArgNohzFull, result.NohzFullRaw)
		if err != nil {
			return nil, err
		}
		result.ConfiguredCPUs = cpus
	}
	if len(result.ConfiguredCPUs) == 0 {
		_, cpus, err := normalizeKernelCPUArg(coreIsolationArgRcuNocbs, result.RcuNocbsRaw)
		if err != nil {
			return nil, err
		}
		result.ConfiguredCPUs = cpus
	}

	return result, nil
}

func normalizeKernelCPUArg(argName, raw string) (string, []int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, nil
	}

	cpuListValue := raw
	if argName == coreIsolationArgIsolcpus {
		cpuListValue = extractIsolcpusCPUList(raw)
		if cpuListValue == "" {
			return "", nil, fmt.Errorf("invalid %s value %q: missing CPU list", argName, raw)
		}
	}

	cpus, err := expandCPUList(cpuListValue)
	if err != nil {
		return "", nil, fmt.Errorf("invalid %s value %q: %w", argName, raw, err)
	}
	cpus = uniqueSortedInts(cpus)
	return compressCPUList(cpus), cpus, nil
}

func extractIsolcpusCPUList(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parts := strings.Split(raw, ",")
	start := -1
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if containsASCIIDigit(p) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}

	return strings.Join(parts[start:], ",")
}

func containsASCIIDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}
