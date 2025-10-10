package virsh

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slave/env512"
	"strconv"
	"strings"
	"time"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	libvirt "libvirt.org/go/libvirt"
)

func ensureDirExists(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("directory path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory %s does not exist", path)
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

func ensureParentDirExists(path string) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	return ensureDirExists(dir)
}

func ensureFileExists(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("file path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file %s does not exist", path)
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	return nil
}

// qemu-img --output=json minimal struct
type qiInfo struct {
	Format string `json:"format"`
}

// DetectDiskFormat returns "qcow2" or "raw" (or other qemu formats if present).
// If the file doesn't exist, it infers from the extension.
func DetectDiskFormat(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("disk path is empty")
	}
	if err := ensureParentDirExists(path); err != nil {
		return "", err
	}

	if _, err := os.Stat(path); err == nil {
		cmd := exec.Command("qemu-img", "info", "--output=json", path)
		out, err := cmd.Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return "", fmt.Errorf("qemu-img info %s: %s", path, strings.TrimSpace(string(exitErr.Stderr)))
			}
			return "", fmt.Errorf("qemu-img info %s: %w", path, err)
		}
		var info qiInfo
		if err := json.Unmarshal(out, &info); err != nil {
			return "", fmt.Errorf("parse qemu-img info json: %w", err)
		}
		if info.Format == "" {
			return "", fmt.Errorf("could not detect disk format for %s", path)
		}
		return strings.ToLower(info.Format), nil
	}

	// Not exists: infer by extension
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".qcow2":
		return "qcow2", nil
	case ".img", ".raw":
		return "raw", nil
	default:
		// default to qcow2 if unknown
		return "qcow2", nil
	}
}

// EnsureDiskAndDetectFormat creates the disk if missing (using detected format)
// and returns the resulting format (e.g. "qcow2" or "raw").
func EnsureDiskAndDetectFormat(path string, sizeGB int) (string, error) {
	fmtStr, err := DetectDiskFormat(path)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(path); err == nil {
		return fmtStr, nil
	}

	if sizeGB <= 0 {
		return "", fmt.Errorf("disk %s does not exist and sizeGB <= 0", path)
	}

	// Create with chosen format
	args := []string{"create", "-f", fmtStr, path, fmt.Sprintf("%dG", sizeGB)}
	cmd := exec.Command("qemu-img", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return "", fmt.Errorf("qemu-img create %s: %s", path, msg)
		}
		return "", fmt.Errorf("qemu-img create %s: %w", path, err)
	}
	return fmtStr, nil
}

// WriteDomainXMLToDisk saves the domain XML alongside the disk image.
func WriteDomainXMLToDisk(vmName, xml, diskPath string) (string, error) {
	if strings.TrimSpace(vmName) == "" {
		return "", fmt.Errorf("vm name is empty")
	}
	if strings.TrimSpace(diskPath) == "" {
		return "", fmt.Errorf("disk path is empty")
	}

	if err := ensureParentDirExists(diskPath); err != nil {
		return "", err
	}

	dir := filepath.Dir(diskPath)
	if dir == "" {
		dir = "."
	}

	out := filepath.Join(dir, fmt.Sprintf("%s.xml", vmName))
	if err := os.WriteFile(out, []byte(strings.TrimSpace(xml)+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("write xml %s: %w", out, err)
	}
	return out, nil
}

func diskPathFromDomainXML(xmlData string) (string, error) {
	type diskSource struct {
		File string `xml:"file,attr"`
	}
	type disk struct {
		Device string     `xml:"device,attr"`
		Source diskSource `xml:"source"`
	}
	type devices struct {
		Disks []disk `xml:"disk"`
	}
	type domain struct {
		Devices devices `xml:"devices"`
	}

	var d domain
	if err := xml.Unmarshal([]byte(xmlData), &d); err != nil {
		return "", fmt.Errorf("parse domain xml: %w", err)
	}
	for _, disk := range d.Devices.Disks {
		device := strings.TrimSpace(disk.Device)
		if device == "" {
			device = "disk"
		}
		if device == "disk" && strings.TrimSpace(disk.Source.File) != "" {
			return strings.TrimSpace(disk.Source.File), nil
		}
	}
	return "", nil
}

func RestartLibvirt() error {
	cmd := exec.Command("systemctl", "restart", "libvirtd")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("restart libvirtd: %s", msg)
		}
		return fmt.Errorf("restart libvirtd: %w", err)
	}
	return nil
}

// # qemu:///system  => system libvirtd (/etc/libvirt/qemu.conf)
// set-> remote_display_port_min = 12000
//
//	remote_display_port_max = 12999
func SetVNCPorts(minPort, maxPort int) error {
	if minPort < 5900 || maxPort > 65535 || minPort > maxPort {
		return fmt.Errorf("invalid remote display port range %d-%d", minPort, maxPort)
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("SetVNCPorts requires root privileges")
	}

	const configPath = "/etc/libvirt/qemu.conf"

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", configPath, err)
	}

	content := string(data)
	minLine := fmt.Sprintf("remote_display_port_min = %d", minPort)
	maxLine := fmt.Sprintf("remote_display_port_max = %d", maxPort)

	minPattern := regexp.MustCompile(`(?i)^\s*#?\s*remote_display_port_min\s*=`)
	maxPattern := regexp.MustCompile(`(?i)^\s*#?\s*remote_display_port_max\s*=`)

	trimmed := strings.TrimRight(content, "\n")
	var lines []string
	if trimmed != "" {
		lines = strings.Split(trimmed, "\n")
	}

	var out []string
	minApplied := false
	maxApplied := false
	for _, line := range lines {
		switch {
		case minPattern.MatchString(line):
			if !minApplied {
				out = append(out, minLine)
				minApplied = true
			}
		case maxPattern.MatchString(line):
			if !maxApplied {
				out = append(out, maxLine)
				maxApplied = true
			}
		default:
			out = append(out, line)
		}
	}

	if !minApplied {
		out = append(out, minLine)
		minApplied = true
	}
	if !maxApplied {
		out = append(out, maxLine)
		maxApplied = true
	}

	newContent := strings.Join(out, "\n")
	if newContent != "" {
		newContent += "\n"
	}

	if newContent == content {
		return nil
	}

	info, err := os.Stat(configPath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", configPath, err)
	}
	if err := os.WriteFile(configPath, []byte(newContent), info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	if err := RestartLibvirt(); err != nil {
		return fmt.Errorf("restart libvirt after updating %s: %w", configPath, err)
	}

	return nil
}
func domainStateToString(state libvirt.DomainState) grpcVirsh.VmState {
	switch state {
	case libvirt.DOMAIN_NOSTATE:
		return grpcVirsh.VmState_NOSTATE
	case libvirt.DOMAIN_RUNNING:
		return grpcVirsh.VmState_RUNNING
	case libvirt.DOMAIN_BLOCKED:
		return grpcVirsh.VmState_BLOCKED
	case libvirt.DOMAIN_PAUSED:
		return grpcVirsh.VmState_PAUSED
	case libvirt.DOMAIN_SHUTDOWN:
		return grpcVirsh.VmState_SHUTDOWN
	case libvirt.DOMAIN_SHUTOFF:
		return grpcVirsh.VmState_SHUTOFF
	case libvirt.DOMAIN_CRASHED:
		return grpcVirsh.VmState_CRASHED
	case libvirt.DOMAIN_PMSUSPENDED:
		return grpcVirsh.VmState_PMSUSPENDED
	default:
		return grpcVirsh.VmState_UNKNOWN
	}
}

func getMemStats(dom *libvirt.Domain) (totalKiB, usedKiB uint64, err error) {
	const maxStats = 16
	stats, err := dom.MemoryStats(maxStats, 0)
	if err != nil {
		return 0, 0, fmt.Errorf("MemoryStats: %w", err)
	}

	var actualBalloon, rss, unused uint64
	for _, s := range stats {
		switch s.Tag {
		case int32(libvirt.DOMAIN_MEMORY_STAT_ACTUAL_BALLOON):
			actualBalloon = s.Val
		case int32(libvirt.DOMAIN_MEMORY_STAT_RSS):
			rss = s.Val
		case int32(libvirt.DOMAIN_MEMORY_STAT_UNUSED):
			unused = s.Val
		}
	}

	if actualBalloon == 0 {
		if info, err2 := dom.GetInfo(); err2 == nil {
			actualBalloon = uint64(info.Memory)
		}
	}

	var used uint64
	switch {
	case unused > 0 && unused <= actualBalloon:
		used = actualBalloon - unused
	case rss > 0:
		used = rss
	default:
		used = actualBalloon
	}

	return actualBalloon, used, nil
}

func sampleCPUTime(dom *libvirt.Domain) (ns uint64, vcpus int, err error) {
	info, err := dom.GetInfo()
	if err != nil {
		return 0, 0, fmt.Errorf("GetInfo: %w", err)
	}
	return info.CpuTime, int(info.NrVirtCpu), nil
}

func cpuPercentOver(dom *libvirt.Domain, interval time.Duration) (percent float64, vcpus int, err error) {
	start := time.Now()

	t0, nVCPU, err := sampleCPUTime(dom)
	if err != nil {
		return 0, 0, err
	}
	time.Sleep(interval)
	t1, _, err := sampleCPUTime(dom)
	if err != nil {
		return 0, 0, err
	}

	if t1 < t0 || nVCPU <= 0 {
		return 0, nVCPU, nil
	}

	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		return 0, nVCPU, nil
	}

	deltaCPUSeconds := float64(t1-t0) / float64(time.Second)
	percent = (deltaCPUSeconds / (elapsed * float64(nVCPU))) * 100.0

	if percent < 0 {
		percent = 0
	}
	max := 100.0
	if percent > max {
		percent = max
	}
	return percent, nVCPU, nil
}

func GetVMByName(name string) (*grpcVirsh.Vm, error) {
	connURI := "qemu:///system"
	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return nil, fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	// Get state
	state, _, err := dom.GetState()
	if err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}

	// Parse XML for VNC port
	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return nil, fmt.Errorf("xml: %w", err)
	}

	port := 0
	if strings.Contains(xmlDesc, "graphics type='vnc'") {
		start := strings.Index(xmlDesc, "port='")
		if start != -1 {
			fmt.Sscanf(xmlDesc[start:], "port='%d'", &port)
		}
	}

	var usedMemMB int32
	var cpuPct int32
	var vcpuCount int32
	var totalMemMB int32
	// Only do live sampling when actually running
	if state == libvirt.DOMAIN_RUNNING {
		if totalKiB, usedKiB, err := getMemStats(dom); err == nil {
			totalMemMB = int32(totalKiB / 1024)
			usedMemMB = int32(usedKiB / 1024)
		}
		if pct, vcpus, err := cpuPercentOver(dom, 500*time.Millisecond); err == nil {
			cpuPct = int32(pct + 0.5)
			vcpuCount = int32(vcpus)
		}
	}

	info := &grpcVirsh.Vm{
		MachineName:          env512.MachineName,
		Name:                 name,
		State:                domainStateToString(state),
		NovncPort:            strconv.Itoa(port),
		CpuCount:             int32(vcpuCount),
		MemoryMB:             totalMemMB,
		CurrentCpuUsage:      int32(cpuPct),
		CurrentMemoryUsageMB: usedMemMB,
	}
	return info, nil
}

func GetAllVMs() ([]*grpcVirsh.Vm, error) {
	connURI := "qemu:///system"
	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	doms, err := conn.ListAllDomains(0)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}

	var vms []*grpcVirsh.Vm
	for _, dom := range doms {
		name, err := dom.GetName()
		if err != nil {
			dom.Free()
			return nil, fmt.Errorf("get name: %w", err)
		}

		state, _, err := dom.GetState()
		if err != nil {
			dom.Free()
			return nil, fmt.Errorf("state: %w", err)
		}

		xmlDesc, err := dom.GetXMLDesc(0)
		if err != nil {
			dom.Free()
			return nil, fmt.Errorf("xml: %w", err)
		}

		port := 0
		if strings.Contains(xmlDesc, "graphics type='vnc'") {
			start := strings.Index(xmlDesc, "port='")
			if start != -1 {
				fmt.Sscanf(xmlDesc[start:], "port='%d'", &port)
			}
		}

		var usedMemMB int32
		var cpuPct int32
		var vcpuCount int32
		var totalMemMB int32
		// Only do live sampling when actually running
		if state == libvirt.DOMAIN_RUNNING {
			if totalKiB, usedKiB, err := getMemStats(&dom); err == nil {
				totalMemMB = int32(totalKiB / 1024)
				usedMemMB = int32(usedKiB / 1024)
			}
			if pct, vcpus, err := cpuPercentOver(&dom, 500*time.Millisecond); err == nil {
				cpuPct = int32(pct + 0.5)
				vcpuCount = int32(vcpus)
			}
		}

		info := &grpcVirsh.Vm{
			MachineName:          env512.MachineName,
			Name:                 name,
			State:                domainStateToString(state),
			NovncPort:            strconv.Itoa(port),
			CpuCount:             int32(vcpuCount),
			MemoryMB:             totalMemMB,
			CurrentCpuUsage:      int32(cpuPct),
			CurrentMemoryUsageMB: usedMemMB,
		}
		vms = append(vms, info)
		dom.Free()
	}
	return vms, nil
}
func ShutdownVM(name string) error {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	// If paused, resume so the guest can actually react to shutdown.
	if st, _, _ := dom.GetState(); st == libvirt.DOMAIN_PAUSED {
		_ = dom.Resume()
	}

	// 1) Try guest agent (best; clean)
	// Requires qemu-guest-agent running inside the VM and a <channel> in XML.
	if err := dom.ShutdownFlags(libvirt.DOMAIN_SHUTDOWN_GUEST_AGENT); err != nil {
		// 2) Fallback to ACPI power button (what virsh shutdown does by default)
		_ = dom.ShutdownFlags(libvirt.DOMAIN_SHUTDOWN_ACPI_POWER_BTN)
	}

	// 3) Wait/poll until the VM actually turns off
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		st, _, err := dom.GetState()
		if err != nil {
			return fmt.Errorf("get state: %w", err)
		}
		if st == libvirt.DOMAIN_SHUTOFF {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}

	// 4) Still up? Return a clear error (your caller may choose to Destroy() then).
	return fmt.Errorf("graceful shutdown timed out (agent/ACPI ignored)")
}

func ForceShutdownVM(name string) error {
	connURI := "qemu:///system"
	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	if err := dom.Destroy(); err != nil {
		return fmt.Errorf("force shutdown: %w", err)
	}
	return nil
}

func StartVM(name string) error {
	connURI := "qemu:///system"
	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	if err := dom.Create(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return nil
}

func RemoveVM(name string) error {
	connURI := "qemu:///system"
	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return fmt.Errorf("xml: %w", err)
	}

	diskPath, err := diskPathFromDomainXML(xmlDesc)
	if err != nil {
		return fmt.Errorf("detect disk path: %w", err)
	}

	if err := dom.Destroy(); err != nil {
		return fmt.Errorf("force shutdown: %w", err)
	}

	//force remove
	if err := dom.UndefineFlags(libvirt.DOMAIN_UNDEFINE_MANAGED_SAVE | libvirt.DOMAIN_UNDEFINE_SNAPSHOTS_METADATA | libvirt.DOMAIN_UNDEFINE_NVRAM); err != nil {
		return fmt.Errorf("undefine: %w", err)
	}

	if diskPath != "" {
		if err := os.Remove(diskPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove disk %s: %w", diskPath, err)
		}

		xmlDir := filepath.Dir(diskPath)
		if xmlDir == "" {
			xmlDir = "."
		}
		xmlPath := filepath.Join(xmlDir, name+".xml")
		if err := os.Remove(xmlPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove xml %s: %w", xmlPath, err)
		}
	}

	return nil
}

func RestartVM(name string) error {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	// Force stop
	if err := dom.Destroy(); err != nil {
		return fmt.Errorf("destroy: %w", err)
	}

	// Wait until the VM is fully shut off
	for {
		state, _, err := dom.GetState()
		if err != nil {
			return fmt.Errorf("get state: %w", err)
		}
		if state == libvirt.DOMAIN_SHUTOFF {
			break
		}
		// short non-blocking poll (100ms is fine)
		time.Sleep(100 * time.Millisecond)
	}

	// Restart
	if err := dom.Create(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	return nil
}
