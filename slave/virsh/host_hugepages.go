package virsh

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	hostHugePagesArgDefaultSize = "default_hugepagesz"
	hostHugePagesArgSize        = "hugepagesz"
	hostHugePagesArgCount       = "hugepages"
)

type HostHugePagesRequest struct {
	PageSize  string
	PageCount int
}

type HostHugePagesState struct {
	Enabled        bool
	RebootRequired bool
	Message        string

	PageSize        string
	PageCount       int
	ActivePageSize  string
	ActivePageCount int

	DefaultHugepagesz       string
	Hugepagesz              string
	Hugepages               string
	ActiveDefaultHugepagesz string
	ActiveHugepagesz        string
	ActiveHugepages         string
}

type hostHugePagesKernelArgs struct {
	DefaultHugepageszRaw string
	HugepageszRaw        string
	HugepagesRaw         string

	DefaultHugepageszNorm string
	HugepageszNorm        string
	HugepagesNorm         string

	PageSize  string
	PageCount int
	HasCount  bool
}

type procHugePagesInfo struct {
	PageSizeKB int
	PageCount  int
}

func GetHostHugePages() (*HostHugePagesState, error) {
	configuredArgs, err := getDefaultKernelArgs()
	if err != nil {
		return nil, err
	}
	activeArgs, err := getActiveKernelArgs()
	if err != nil {
		return nil, err
	}

	configured, err := parseHostHugePagesKernelArgs(configuredArgs)
	if err != nil {
		return nil, err
	}
	active, err := parseHostHugePagesKernelArgs(activeArgs)
	if err != nil {
		return nil, err
	}

	procInfo, err := readProcHugePagesInfo()
	if err != nil {
		return nil, err
	}

	enabled := configured.DefaultHugepageszRaw != "" || configured.HugepageszRaw != "" || configured.HugepagesRaw != ""
	rebootRequired := configured.DefaultHugepageszNorm != active.DefaultHugepageszNorm ||
		configured.HugepageszNorm != active.HugepageszNorm ||
		configured.HugepagesNorm != active.HugepagesNorm

	activePageSize := firstNonEmpty(active.PageSize, formatHugePageSizeKB(procInfo.PageSizeKB))
	activePageCount := procInfo.PageCount
	if active.HasCount {
		activePageCount = active.PageCount
	}

	state := &HostHugePagesState{
		Enabled:                 enabled,
		RebootRequired:          rebootRequired,
		PageSize:                configured.PageSize,
		PageCount:               configured.PageCount,
		ActivePageSize:          activePageSize,
		ActivePageCount:         activePageCount,
		DefaultHugepagesz:       configured.DefaultHugepageszRaw,
		Hugepagesz:              configured.HugepageszRaw,
		Hugepages:               configured.HugepagesRaw,
		ActiveDefaultHugepagesz: active.DefaultHugepageszRaw,
		ActiveHugepagesz:        active.HugepageszRaw,
		ActiveHugepages:         active.HugepagesRaw,
	}
	return state, nil
}

func SetHostHugePages(req HostHugePagesRequest) (*HostHugePagesState, error) {
	if !hasBinary("grubby") {
		return nil, fmt.Errorf("grubby is required")
	}

	pageSize, err := normalizeHugePageSize(req.PageSize)
	if err != nil {
		return nil, err
	}
	if pageSize == "" {
		return nil, fmt.Errorf("page_size is required")
	}
	if req.PageCount <= 0 {
		return nil, fmt.Errorf("page_count must be > 0")
	}

	if err := clearHostHugePagesKernelArgs(); err != nil {
		return nil, err
	}

	argsValue := fmt.Sprintf("%s=%s %s=%s %s=%d",
		hostHugePagesArgDefaultSize, pageSize,
		hostHugePagesArgSize, pageSize,
		hostHugePagesArgCount, req.PageCount,
	)
	if err := runCmdDiscardOutputMaybeSudo("grubby", "--update-kernel=ALL", "--args", argsValue); err != nil {
		return nil, err
	}

	state, err := GetHostHugePages()
	if err != nil {
		return nil, err
	}
	state.Message = "host hugepages updated"
	if state.RebootRequired {
		state.Message += " (reboot required)"
	}
	return state, nil
}

func RemoveHostHugePages() (*HostHugePagesState, error) {
	if !hasBinary("grubby") {
		return nil, fmt.Errorf("grubby is required")
	}

	if err := clearHostHugePagesKernelArgs(); err != nil {
		return nil, err
	}

	state, err := GetHostHugePages()
	if err != nil {
		return nil, err
	}
	state.Message = "host hugepages removed"
	if state.RebootRequired {
		state.Message += " (reboot required)"
	}
	return state, nil
}

func clearHostHugePagesKernelArgs() error {
	return runCmdDiscardOutputMaybeSudo(
		"grubby",
		"--update-kernel=ALL",
		"--remove-args",
		fmt.Sprintf("%s %s %s", hostHugePagesArgDefaultSize, hostHugePagesArgSize, hostHugePagesArgCount),
	)
}

func parseHostHugePagesKernelArgs(args map[string]string) (*hostHugePagesKernelArgs, error) {
	result := &hostHugePagesKernelArgs{
		DefaultHugepageszRaw: strings.TrimSpace(args[hostHugePagesArgDefaultSize]),
		HugepageszRaw:        strings.TrimSpace(args[hostHugePagesArgSize]),
		HugepagesRaw:         strings.TrimSpace(args[hostHugePagesArgCount]),
	}

	var err error
	result.DefaultHugepageszNorm, err = normalizeHugePageSize(result.DefaultHugepageszRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s value %q: %w", hostHugePagesArgDefaultSize, result.DefaultHugepageszRaw, err)
	}
	result.HugepageszNorm, err = normalizeHugePageSize(result.HugepageszRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s value %q: %w", hostHugePagesArgSize, result.HugepageszRaw, err)
	}
	result.HugepagesNorm, result.PageCount, result.HasCount, err = normalizeHugePagesCount(result.HugepagesRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s value %q: %w", hostHugePagesArgCount, result.HugepagesRaw, err)
	}

	result.PageSize = firstNonEmpty(result.DefaultHugepageszNorm, result.HugepageszNorm)
	return result, nil
}

func normalizeHugePageSize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	parts := strings.Fields(raw)
	if len(parts) > 2 {
		return "", fmt.Errorf("expected format like 2M or 1048576 kB")
	}

	var numberStr string
	var unitStr string
	if len(parts) == 1 {
		numberStr, unitStr = splitHugePageSizeToken(parts[0])
	} else {
		numberStr = parts[0]
		unitStr = parts[1]
	}
	if numberStr == "" || unitStr == "" {
		return "", fmt.Errorf("expected format like 2M or 1G")
	}

	n, err := strconv.Atoi(numberStr)
	if err != nil {
		return "", fmt.Errorf("invalid size number: %w", err)
	}
	if n <= 0 {
		return "", fmt.Errorf("size must be > 0")
	}

	unit := strings.ToUpper(strings.TrimSpace(unitStr))
	unit = strings.TrimSuffix(unit, "IB")
	unit = strings.TrimSuffix(unit, "B")
	switch unit {
	case "K":
		return formatHugePageSizeKB(n), nil
	case "M":
		return formatHugePageSizeKB(n * 1024), nil
	case "G":
		return formatHugePageSizeKB(n * 1024 * 1024), nil
	default:
		return "", fmt.Errorf("unsupported size unit %q (use K, M, or G)", unitStr)
	}
}

func splitHugePageSizeToken(tok string) (number string, unit string) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return "", ""
	}
	i := 0
	for i < len(tok) && tok[i] >= '0' && tok[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(tok) {
		return "", ""
	}
	return tok[:i], tok[i:]
}

func formatHugePageSizeKB(kb int) string {
	if kb <= 0 {
		return ""
	}
	if kb%(1024*1024) == 0 {
		return fmt.Sprintf("%dG", kb/(1024*1024))
	}
	if kb%1024 == 0 {
		return fmt.Sprintf("%dM", kb/1024)
	}
	return fmt.Sprintf("%dK", kb)
}

func normalizeHugePagesCount(raw string) (norm string, count int, has bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, false, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return "", 0, false, err
	}
	if v < 0 {
		return "", 0, false, fmt.Errorf("must be >= 0")
	}
	return strconv.Itoa(v), v, true, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func readProcHugePagesInfo() (*procHugePagesInfo, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	defer f.Close()

	info := &procHugePagesInfo{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "HugePages_Total:"):
			raw := strings.TrimSpace(strings.TrimPrefix(line, "HugePages_Total:"))
			v, err := strconv.Atoi(raw)
			if err != nil {
				return nil, fmt.Errorf("parse HugePages_Total from /proc/meminfo: %w", err)
			}
			info.PageCount = v
		case strings.HasPrefix(line, "Hugepagesize:"):
			raw := strings.TrimSpace(strings.TrimPrefix(line, "Hugepagesize:"))
			size, err := normalizeHugePageSize(raw)
			if err != nil {
				return nil, fmt.Errorf("parse Hugepagesize from /proc/meminfo: %w", err)
			}
			if size == "" {
				continue
			}
			// Convert normalized size back to kB so formatting remains consistent.
			kbStr, unit := splitHugePageSizeToken(size)
			kb, err := strconv.Atoi(kbStr)
			if err != nil {
				return nil, fmt.Errorf("parse normalized hugepage size: %w", err)
			}
			switch strings.ToUpper(unit) {
			case "K":
				info.PageSizeKB = kb
			case "M":
				info.PageSizeKB = kb * 1024
			case "G":
				info.PageSizeKB = kb * 1024 * 1024
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	return info, nil
}
