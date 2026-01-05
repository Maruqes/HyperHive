package virsh

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slave/env512"
	"strconv"
	"strings"
	"sync"
	"time"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"github.com/Maruqes/512SvMan/logger"
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
	Format      string `json:"format"`
	VirtualSize uint64 `json:"virtual-size"`
	ActualSize  uint64 `json:"actual-size"`
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
		info, err := readQemuImgInfo(path)
		if err != nil {
			return "", err
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
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("disk path is empty")
	}

	// ensure parent dir exists before any operation
	if err := ensureParentDirExists(path); err != nil {
		return "", err
	}

	// If file already exists, detect its format and ensure perms
	if _, err := os.Stat(path); err == nil {
		fmtStr, err := DetectDiskFormat(path)
		if err != nil {
			return "", err
		}
		if err := ensureDiskPermissions(path); err != nil {
			return "", err
		}
		return strings.ToLower(fmtStr), nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}

	// File does not exist: must have a size to create it
	if sizeGB <= 0 {
		return "", fmt.Errorf("disk %s does not exist and sizeGB <= 0", path)
	}

	// Choose format based on raw flag
	createFmt := "qcow2"
	args := []string{"create", "-f", createFmt}
	args = append(args, path, fmt.Sprintf("%dG", sizeGB))

	cmd := exec.Command("qemu-img", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return "", fmt.Errorf("qemu-img create %s: %s", path, msg)
		}
		return "", fmt.Errorf("qemu-img create %s: %w", path, err)
	}

	if err := ensureDiskPermissions(path); err != nil {
		return "", err
	}
	return createFmt, nil
}

func readQemuImgInfo(path string) (*qiInfo, error) {
	cmd := exec.Command("qemu-img", "info", "--output=json", path)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("qemu-img info %s: %s", path, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("qemu-img info %s: %w", path, err)
	}

	var info qiInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("parse qemu-img info json: %w", err)
	}
	return &info, nil
}

func ensureDiskPermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat disk %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("disk %s is a directory", path)
	}

	if info.Mode().Perm() != 0o777 {
		if err := os.Chmod(path, 0o777); err != nil {
			return fmt.Errorf("chmod disk %s: %w", path, err)
		}
	}

	var uid int
	var gid int

	// Use configured QEMU UID/GID when provided, otherwise fall back to the current process values.
	if s := strings.TrimSpace(env512.Qemu_UID); s != "" {
		u, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("parse qemu uid: %w", err)
		}
		uid = u
	} else {
		uid = os.Geteuid()
	}

	if s := strings.TrimSpace(env512.Qemu_GID); s != "" {
		g, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("parse qemu gid: %w", err)
		}
		gid = g
	} else {
		gid = os.Getegid()
	}

	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown disk %s: %w", path, err)
	}

	if err := ensureDirTreePermissions(filepath.Dir(path)); err != nil {
		return err
	}
	return nil
}

func ensureDirTreePermissions(path string) error {
	cleaned := filepath.Clean(path)
	root := string(filepath.Separator)

	for cleaned != "" && cleaned != "." && cleaned != root {
		if err := ensureDirPermissions(cleaned); err != nil {
			return err
		}

		next := filepath.Dir(cleaned)
		if next == cleaned {
			break
		}
		cleaned = next
	}
	return nil
}

func ensureDirPermissions(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("directory path is empty")
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat dir %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}

	if info.Mode().Perm() != 0o777 {
		if err := os.Chmod(path, 0o777); err != nil {
			return fmt.Errorf("chmod dir %s: %w", path, err)
		}
	}
	return nil
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

func vncPortFromDomainXML(xmlData string) (int, error) {
	type graphics struct {
		Type string `xml:"type,attr"`
		Port string `xml:"port,attr"`
	}
	type devices struct {
		Graphics []graphics `xml:"graphics"`
	}
	type domain struct {
		Devices devices `xml:"devices"`
	}

	var d domain
	if err := xml.Unmarshal([]byte(xmlData), &d); err != nil {
		return 0, fmt.Errorf("parse domain xml: %w", err)
	}
	for _, g := range d.Devices.Graphics {
		if strings.EqualFold(strings.TrimSpace(g.Type), "vnc") {
			portStr := strings.TrimSpace(g.Port)
			if portStr == "" {
				return 0, nil
			}
			port, err := strconv.Atoi(portStr)
			if err != nil {
				return 0, fmt.Errorf("convert vnc port %q: %w", portStr, err)
			}
			return port, nil
		}
	}
	return 0, nil
}

func vncPasswordFromDomainXML(xmlData string) (string, error) {
	if strings.TrimSpace(xmlData) == "" {
		return "", nil
	}

	type graphics struct {
		Type   string `xml:"type,attr"`
		Passwd string `xml:"passwd,attr"`
	}
	type devices struct {
		Graphics []graphics `xml:"graphics"`
	}
	type domain struct {
		Devices devices `xml:"devices"`
	}

	var d domain
	if err := xml.Unmarshal([]byte(xmlData), &d); err != nil {
		return "", fmt.Errorf("parse domain xml: %w", err)
	}
	for _, g := range d.Devices.Graphics {
		if strings.EqualFold(strings.TrimSpace(g.Type), "vnc") {
			passwd := strings.TrimSpace(g.Passwd)
			if passwd != "" {
				return passwd, nil
			}
		}
	}
	return "", nil
}

func spicePortFromDomainXML(xmlData string) (int, error) {
	type graphics struct {
		Type string `xml:"type,attr"`
		Port string `xml:"port,attr"`
	}
	type devices struct {
		Graphics []graphics `xml:"graphics"`
	}
	type domain struct {
		Devices devices `xml:"devices"`
	}

	var d domain
	if err := xml.Unmarshal([]byte(xmlData), &d); err != nil {
		return 0, fmt.Errorf("parse domain xml: %w", err)
	}
	for _, g := range d.Devices.Graphics {
		if strings.EqualFold(strings.TrimSpace(g.Type), "spice") {
			portStr := strings.TrimSpace(g.Port)
			if portStr == "" || portStr == "-1" {
				return 0, nil
			}
			port, err := strconv.Atoi(portStr)
			if err != nil {
				return 0, fmt.Errorf("convert spice port %q: %w", portStr, err)
			}
			return port, nil
		}
	}
	return 0, nil
}

func networkFromDomainXML(xmlData string) (string, error) {
	type ifaceSource struct {
		Network string `xml:"network,attr"`
		Bridge  string `xml:"bridge,attr"`
		Dev     string `xml:"dev,attr"`
	}
	type iface struct {
		Type   string      `xml:"type,attr"`
		Source ifaceSource `xml:"source"`
	}
	type devices struct {
		Interfaces []iface `xml:"interface"`
	}
	type domain struct {
		Devices devices `xml:"devices"`
	}

	var d domain
	if err := xml.Unmarshal([]byte(xmlData), &d); err != nil {
		return "", fmt.Errorf("parse domain xml: %w", err)
	}
	for _, iface := range d.Devices.Interfaces {
		ifaceType := strings.ToLower(strings.TrimSpace(iface.Type))
		switch ifaceType {
		case "network":
			if v := strings.TrimSpace(iface.Source.Network); v != "" {
				return v, nil
			}
		case "bridge":
			if v := strings.TrimSpace(iface.Source.Bridge); v != "" {
				return v, nil
			}
		case "direct":
			if v := strings.TrimSpace(iface.Source.Dev); v != "" {
				return v, nil
			}
		default:
			if v := strings.TrimSpace(iface.Source.Network); v != "" {
				return v, nil
			}
			if v := strings.TrimSpace(iface.Source.Bridge); v != "" {
				return v, nil
			}
			if v := strings.TrimSpace(iface.Source.Dev); v != "" {
				return v, nil
			}
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

func getMemStats(dom *libvirt.Domain) (totalKiB, usedKiB, rssKiB uint64, err error) {
	const maxStats = 16
	stats, err := dom.MemoryStats(maxStats, 0)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("MemoryStats: %w", err)
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

	if rss == 0 {
		rss = used
	}

	return actualBalloon, used, rss, nil
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

type DiskInfo struct {
	Path   string
	SizeGB int64 // virtual capacity in GB (fallback: allocated GB)
	// AllocatedGB is the currently allocated/occupied size on disk in GB (if available)
	AllocatedGB int64
}

func GetPrimaryDiskInfo(dom *libvirt.Domain) (*DiskInfo, error) {
	xmlStr, err := dom.GetXMLDesc(0)
	if err != nil {
		return nil, fmt.Errorf("xml: %w", err)
	}

	type domainXML struct {
		Devices struct {
			Disks []struct {
				Device string `xml:"device,attr"` // "disk", "cdrom"
				Source struct {
					File string `xml:"file,attr"` // e.g., /var/lib/libvirt/images/foo.qcow2
					Dev  string `xml:"dev,attr"`  // e.g., /dev/vg/vol
					Name string `xml:"name,attr"` // network/ceph cases
				} `xml:"source"`
				Target struct {
					Dev string `xml:"dev,attr"` // e.g., vda, sda
				} `xml:"target"`
				Driver struct {
					Type string `xml:"type,attr"`
					Name string `xml:"name,attr"`
				} `xml:"driver"`
			} `xml:"disk"`
		} `xml:"devices"`
	}

	var d domainXML
	if err := xml.Unmarshal([]byte(xmlStr), &d); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	var srcPath string
	var targetDev string
	for _, disk := range d.Devices.Disks {
		if disk.Device != "disk" {
			continue
		}
		if disk.Source.File != "" {
			srcPath = disk.Source.File
		} else if disk.Source.Dev != "" {
			srcPath = disk.Source.Dev
		} else {
			continue
		}
		targetDev = strings.TrimSpace(disk.Target.Dev)
		if srcPath != "" {
			break
		}
	}
	if srcPath == "" {
		return nil, fmt.Errorf("no disk source path found")
	}

	type blockInfo struct {
		Capacity   uint64
		Allocation uint64
		Physical   uint64
	}
	var bi *blockInfo

	blockInfoKey := strings.TrimSpace(targetDev)
	if blockInfoKey == "" {
		blockInfoKey = srcPath
	}
	if info, err := dom.GetBlockInfo(blockInfoKey, 0); err == nil {
		// info has fields: Capacity, Allocation, Physical (bytes)
		bi = &blockInfo{
			Capacity:   info.Capacity,
			Allocation: info.Allocation,
			Physical:   info.Physical,
		}
	}

	// Prefer to return Capacity as SizeGB and Allocation as AllocatedGB when available
	if bi != nil && bi.Capacity > 0 {
		sizeGB := int64((bi.Capacity + (1 << 30) - 1) / (1 << 30))
		allocGB := int64(0)
		if bi.Allocation > 0 {
			allocGB = int64((bi.Allocation + (1 << 30) - 1) / (1 << 30))
		}
		return &DiskInfo{Path: srcPath, SizeGB: sizeGB, AllocatedGB: allocGB}, nil
	}

	// If libvirt couldn't provide block info, try qemu-img which reports the virtual capacity
	// even for sparse images (e.g. qcow2).
	if !strings.HasPrefix(srcPath, "/dev/") {
		if info, err := readQemuImgInfo(srcPath); err == nil && info.VirtualSize > 0 {
			sizeGB := int64((info.VirtualSize + (1 << 30) - 1) / (1 << 30))
			allocBytes := info.ActualSize
			if allocBytes == 0 {
				if st, statErr := os.Stat(srcPath); statErr == nil {
					allocBytes = uint64(st.Size())
				}
			}
			allocGB := int64(0)
			if allocBytes > 0 {
				allocGB = int64((allocBytes + (1 << 30) - 1) / (1 << 30))
			}
			return &DiskInfo{Path: srcPath, SizeGB: sizeGB, AllocatedGB: allocGB}, nil
		}
	}

	if strings.HasPrefix(srcPath, "/dev/") {
		return &DiskInfo{Path: srcPath, SizeGB: 0, AllocatedGB: 0}, nil
	}
	st, err := os.Stat(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Disk image file is missing on the host; return a zero-sized DiskInfo
			// so callers can continue and treat missing files as non-fatal.
			return &DiskInfo{Path: srcPath, SizeGB: 0, AllocatedGB: 0}, nil
		}
		return nil, fmt.Errorf("stat %s: %w", srcPath, err)
	}
	allocGB := int64((st.Size() + (1 << 30) - 1) / (1 << 30))
	return &DiskInfo{Path: srcPath, SizeGB: allocGB, AllocatedGB: allocGB}, nil
}

func isUsefulIP(ip string) bool {
	p := net.ParseIP(ip)
	if p == nil {
		return false
	}
	// skip loopback and link-local (keeps private + public)
	if p.IsLoopback() || p.IsLinkLocalUnicast() || p.IsLinkLocalMulticast() {
		return false
	}
	return true
}

func addUnique(ips *[]string, seen map[string]struct{}, ip string) {
	if !isUsefulIP(ip) {
		return
	}
	if _, ok := seen[ip]; ok {
		return
	}
	seen[ip] = struct{}{}
	*ips = append(*ips, ip)
}

func ipsForDomain(dom *libvirt.Domain) ([]string, []string) {
	var ips []string
	seen := map[string]struct{}{}
	var warns []string

	try := func(src libvirt.DomainInterfaceAddressesSource) bool {
		ifAddrs, err := dom.ListAllInterfaceAddresses(src)
		if err != nil {
			warns = append(warns, fmt.Sprintf("iface addresses (%v): %v", src, err))
			return false
		}
		for _, ifa := range ifAddrs {
			for _, a := range ifa.Addrs {
				addUnique(&ips, seen, a.Addr)
			}
		}
		return len(ips) > 0
	}

	// tiny backoff helps right after boot
	for attempt := 0; attempt < 3; attempt++ {
		if try(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_AGENT) || try(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	return ips, warns
}

func definedResourcesFromDomainXML(xmlDesc string) (int32, int32, error) {
	xmlDesc = strings.TrimSpace(xmlDesc)
	if xmlDesc == "" {
		return 0, 0, fmt.Errorf("domain xml is empty")
	}

	type domainResources struct {
		Memory struct {
			Unit  string `xml:"unit,attr"`
			Value string `xml:",chardata"`
		} `xml:"memory"`
		VCPU struct {
			Value string `xml:",chardata"`
		} `xml:"vcpu"`
	}

	var dom domainResources
	if err := xml.Unmarshal([]byte(xmlDesc), &dom); err != nil {
		return 0, 0, fmt.Errorf("unmarshal domain xml: %w", err)
	}

	var definedCPUs int32
	if cpuStr := strings.TrimSpace(dom.VCPU.Value); cpuStr != "" {
		parsed, err := strconv.ParseInt(cpuStr, 10, 32)
		if err != nil {
			return 0, 0, fmt.Errorf("parse vcpu value %q: %w", cpuStr, err)
		}
		definedCPUs = int32(parsed)
	}

	var definedMemMB int32
	if memStr := strings.TrimSpace(dom.Memory.Value); memStr != "" {
		memValue, err := strconv.ParseInt(memStr, 10, 64)
		if err != nil {
			return definedCPUs, 0, fmt.Errorf("parse memory value %q: %w", memStr, err)
		}
		unit := strings.ToLower(strings.TrimSpace(dom.Memory.Unit))
		if unit == "" {
			unit = "kib"
		}

		var memInMB int64
		switch unit {
		case "kib", "kb", "k":
			memInMB = memValue / 1024
		case "mib", "mb", "m":
			memInMB = memValue
		case "gib", "gb", "g":
			memInMB = memValue * 1024
		case "tib", "tb", "t":
			memInMB = memValue * 1024 * 1024
		default:
			return definedCPUs, 0, fmt.Errorf("unsupported memory unit %q", dom.Memory.Unit)
		}

		definedMemMB = clampToInt32(memInMB)
	}

	return definedCPUs, definedMemMB, nil
}

func clampToInt32(val int64) int32 {
	const maxInt32 = int64(1<<31 - 1)
	const minInt32 = -1 << 31
	if val > maxInt32 {
		return int32(maxInt32)
	}
	if val < minInt32 {
		return int32(minInt32)
	}
	return int32(val)
}

func GetVMByName(name string) (*grpcVirsh.Vm, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("vm name is empty")
	}

	connURI := "qemu:///system"
	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		doms, listErr := conn.ListAllDomains(0)
		if listErr != nil {
			return nil, fmt.Errorf("lookup: %w", err)
		}
		foundIdx := -1
		for i := range doms {
			domName, nameErr := doms[i].GetName()
			if nameErr == nil && strings.EqualFold(strings.TrimSpace(domName), name) {
				foundIdx = i
				break
			}
		}
		for i := range doms {
			if i == foundIdx {
				continue
			}
			_ = doms[i].Free()
		}
		if foundIdx < 0 {
			return nil, fmt.Errorf("lookup: %w", err)
		}
		dom = &doms[foundIdx]
	}
	defer dom.Free()

	// Get state
	state := libvirt.DOMAIN_NOSTATE
	if s, _, err := dom.GetState(); err != nil {
		logger.Error(fmt.Sprintf("%s: state: %v", name, err))
	} else {
		state = s
	}

	// Parse XML for VNC port
	xmlDesc := ""
	if xml, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_SECURE); err != nil {
		logger.Error(fmt.Sprintf("%s: xml secure: %v", name, err))
		if xml, err = dom.GetXMLDesc(0); err != nil {
			logger.Error(fmt.Sprintf("%s: xml: %v", name, err))
		} else {
			xmlDesc = xml
		}
	} else {
		xmlDesc = xml
	}

	port := 0
	spicePort := 0
	var (
		networkName string
		vncPassword string
	)
	if xmlDesc != "" {
		if p, err := vncPortFromDomainXML(xmlDesc); err != nil {
			logger.Error(fmt.Sprintf("%s: vnc port: %v", name, err))
		} else {
			port = p
		}
		if spice, err := spicePortFromDomainXML(xmlDesc); err != nil {
			logger.Error(fmt.Sprintf("%s: spice port: %v", name, err))
		} else {
			spicePort = spice
		}
		if netName, err := networkFromDomainXML(xmlDesc); err != nil {
			logger.Error(fmt.Sprintf("%s: network: %v", name, err))
		} else {
			networkName = netName
		}
		if pwd, err := vncPasswordFromDomainXML(xmlDesc); err != nil {
			logger.Error(fmt.Sprintf("%s: vnc password: %v", name, err))
		} else {
			vncPassword = pwd
		}
	}

	var usedMemMB int32
	var cpuPct int32
	var vcpuCount int32
	var totalMemMB int32
	var realHostMemUsageMB int32
	// Only do live sampling when actually running
	if state == libvirt.DOMAIN_RUNNING {
		if totalKiB, usedKiB, rssKiB, err := getMemStats(dom); err == nil {
			totalMemMB = int32(totalKiB / 1024)
			usedMemMB = int32(usedKiB / 1024)
			realHostMemUsageMB = int32(rssKiB / 1024)
		}
		if pct, vcpus, err := cpuPercentOver(dom, 500*time.Millisecond); err == nil {
			cpuPct = int32(pct + 0.5)
			vcpuCount = int32(vcpus)
		}
	}

	//get diskSize and DiskPath
	diskInfo := &DiskInfo{}
	if di, err := GetPrimaryDiskInfo(dom); err != nil {
		logger.Error(fmt.Sprintf("%s: get disk info: %v", name, err))
	} else if di != nil {
		diskInfo = di
	}

	ips, _ := ipsForDomain(dom)

	cpuXML := ""
	if xml, err := GetVmCPUXML(name); err != nil {
		if fallback := extractCPUXML(xmlDesc); fallback != "" {
			cpuXML = fallback
		} else {
			logger.Error(fmt.Sprintf("%s: cpu xml: %v", name, err))
		}
	} else {
		cpuXML = xml
	}

	var definedCPUs int32
	var definedMemMB int32
	if parsedCPUs, parsedMemMB, err := definedResourcesFromDomainXML(xmlDesc); err != nil {
		logger.Error(fmt.Sprintf("%s: parse defined resources: %v", name, err))
	} else {
		definedCPUs = parsedCPUs
		definedMemMB = parsedMemMB
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
		DiskSizeGB:           int32(diskInfo.SizeGB),
		DiskPath:             diskInfo.Path,
		Ip:                   ips,
		Network:              networkName,
		VNCPassword:          vncPassword,
		CPUXML:               cpuXML,
		DefinedCPUS:          definedCPUs,
		DefinedRam:           definedMemMB,
		SpritePort:           strconv.Itoa(spicePort),
		AllocatedGb:          int32(diskInfo.AllocatedGB),
		RealHostMemUsage:     realHostMemUsageMB,
	}
	return info, nil
}

func GetAllVMs() ([]*grpcVirsh.Vm, []string, error) {
	connURI := "qemu:///system"
	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	doms, err := conn.ListAllDomains(0)
	if err != nil {
		return nil, nil, fmt.Errorf("list domains: %w", err)
	}

	type vmResult struct {
		idx      int
		vm       *grpcVirsh.Vm
		warnings []string
	}

	resCh := make(chan vmResult, len(doms))
	var wg sync.WaitGroup

	for i := range doms {
		dom := doms[i]
		idx := i
		wg.Add(1)
		go func(idx int, dom libvirt.Domain) {
			defer wg.Done()

			var warns []string

			name, err := dom.GetName()
			if err != nil {
				warns = append(warns, fmt.Sprintf("get name: %v", err))
				resCh <- vmResult{idx: idx, warnings: warns}
				dom.Free()
				return
			}

			info := &grpcVirsh.Vm{
				MachineName: env512.MachineName,
				Name:        name,
				NovncPort:   "0",
				State:       grpcVirsh.VmState_UNKNOWN,
			}

			state := libvirt.DOMAIN_NOSTATE
			stateKnown := false
			if s, _, err := dom.GetState(); err != nil {
				warns = append(warns, fmt.Sprintf("%s: state: %v", name, err))
			} else {
				state = s
				stateKnown = true
				info.State = domainStateToString(state)
			}

			var (
				totalMemMB         int32
				usedMemMB          int32
				cpuPct             int32
				vcpuCount          int32
				realHostMemUsageMB int32
			)

			if stateKnown && state == libvirt.DOMAIN_RUNNING {
				if totalKiB, usedKiB, rssKiB, err := getMemStats(&dom); err == nil {
					totalMemMB = int32(totalKiB / 1024)
					usedMemMB = int32(usedKiB / 1024)
					realHostMemUsageMB = int32(rssKiB / 1024)
				}
				if pct, vcpus, err := cpuPercentOver(&dom, 500*time.Millisecond); err == nil {
					cpuPct = int32(pct + 0.5)
					vcpuCount = int32(vcpus)
				}
			}

			xmlDesc := ""
			if xml, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_SECURE); err != nil {
				warns = append(warns, fmt.Sprintf("%s: xml: %v", name, err))
			} else {
				xmlDesc = xml
			}

			port := 0
			spicePort := 0
			networkName := ""
			vncPassword := ""
			cpuXML := ""
			if xmlDesc != "" {
				if p, err := vncPortFromDomainXML(xmlDesc); err != nil {
					warns = append(warns, fmt.Sprintf("%s: parse vnc port: %v", name, err))
				} else {
					port = p
				}
				if spice, err := spicePortFromDomainXML(xmlDesc); err != nil {
					warns = append(warns, fmt.Sprintf("%s: parse spice port: %v", name, err))
				} else {
					spicePort = spice
				}
				if netName, err := networkFromDomainXML(xmlDesc); err != nil {
					warns = append(warns, fmt.Sprintf("%s: parse network: %v", name, err))
				} else {
					networkName = netName
				}
				if pwd, err := vncPasswordFromDomainXML(xmlDesc); err != nil {
					warns = append(warns, fmt.Sprintf("%s: parse vnc password: %v", name, err))
				} else {
					vncPassword = pwd
				}
				cpuXML = extractCPUXML(xmlDesc)
				if parsedCPUs, parsedMemMB, err := definedResourcesFromDomainXML(xmlDesc); err != nil {
					warns = append(warns, fmt.Sprintf("%s: defined resources: %v", name, err))
				} else {
					info.DefinedCPUS = parsedCPUs
					info.DefinedRam = parsedMemMB
				}
			}

			diskInfoPath := ""
			diskSizeGB := int32(0)
			var diskInfo *DiskInfo
			if di, err := GetPrimaryDiskInfo(&dom); err != nil {
				warns = append(warns, fmt.Sprintf("%s: get disk info: %v", name, err))
				logger.Error("failed to get diskInfo err: " + err.Error())
			} else if di != nil {
				diskInfo = di
				diskInfoPath = diskInfo.Path
				diskSizeGB = int32(diskInfo.SizeGB)
			}

			ips, _ := ipsForDomain(&dom)

			info.NovncPort = strconv.Itoa(port)
			info.CpuCount = vcpuCount
			info.MemoryMB = totalMemMB
			info.CurrentCpuUsage = cpuPct
			info.CurrentMemoryUsageMB = usedMemMB
			info.RealHostMemUsage = realHostMemUsageMB
			info.DiskSizeGB = diskSizeGB
			info.DiskPath = diskInfoPath
			info.Ip = ips
			info.Network = networkName
			info.VNCPassword = vncPassword
			info.CPUXML = cpuXML
			info.SpritePort = strconv.Itoa(spicePort)
			if diskInfo != nil {
				info.AllocatedGb = int32(diskInfo.AllocatedGB)
			} else {
				info.AllocatedGb = 0
			}

			resCh <- vmResult{idx: idx, vm: info, warnings: warns}
			dom.Free()
		}(idx, dom)
	}

	wg.Wait()
	close(resCh)

	allWarnings := make([]string, 0)
	vmByIdx := make([]*grpcVirsh.Vm, len(doms))
	for res := range resCh {
		allWarnings = append(allWarnings, res.warnings...)
		if res.vm != nil {
			vmByIdx[res.idx] = res.vm
		}
	}

	vms := make([]*grpcVirsh.Vm, 0, len(doms))
	for _, vm := range vmByIdx {
		if vm != nil {
			vms = append(vms, vm)
		}
	}

	return vms, allWarnings, nil
}

func EditVm(name string, newCPU, newMemMiB int, newDiskSizeGB int) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("vm name is empty")
	}
	if newCPU < 0 {
		return fmt.Errorf("newCPU must be non-negative")
	}
	if newMemMiB < 0 {
		return fmt.Errorf("newMemMiB must be non-negative")
	}

	targetDiskGB := newDiskSizeGB
	if targetDiskGB < 0 {
		return fmt.Errorf("newDiskSizeGB must be non-negative")
	}

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

	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return fmt.Errorf("get xml: %w", err)
	}

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}
	if state != libvirt.DOMAIN_SHUTOFF {
		stateLabel := domainStateToString(state).String()
		return fmt.Errorf("vm %s must be shut off before editing (state %s)", name, stateLabel)
	}

	effectiveCPU := newCPU
	effectiveMemMiB := newMemMiB
	if effectiveCPU <= 0 || effectiveMemMiB <= 0 {
		definedCPU, definedMemMB, err := definedResourcesFromDomainXML(xmlDesc)
		if err != nil {
			return fmt.Errorf("resolve existing resources: %w", err)
		}
		if effectiveCPU <= 0 {
			if definedCPU <= 0 {
				return fmt.Errorf("current cpu count not found in domain xml")
			}
			effectiveCPU = int(definedCPU)
		}
		if effectiveMemMiB <= 0 {
			if definedMemMB <= 0 {
				return fmt.Errorf("current memory size not found in domain xml")
			}
			effectiveMemMiB = int(definedMemMB)
		}
	}

	nodeInfo, err := conn.GetNodeInfo()
	if err != nil {
		return fmt.Errorf("node info: %w", err)
	}
	hostMemMiB := nodeInfo.Memory / 1024
	if hostMemMiB == 0 {
		return fmt.Errorf("host reported zero memory")
	}
	if uint64(effectiveMemMiB) > hostMemMiB {
		return fmt.Errorf("requested memory %d MiB exceeds host capacity %d MiB", effectiveMemMiB, hostMemMiB)
	}

	updatedXML, err := mutateDomainXMLResources(xmlDesc, effectiveCPU, effectiveMemMiB)
	if err != nil {
		return err
	}

	if targetDiskGB > 0 {
		diskPath, err := diskPathFromDomainXML(xmlDesc)
		if err != nil {
			return fmt.Errorf("detect disk path: %w", err)
		}
		if strings.TrimSpace(diskPath) == "" {
			return fmt.Errorf("domain xml does not specify a disk image path")
		}
		if err := ensureDiskSizeAtLeast(diskPath, targetDiskGB); err != nil {
			return err
		}
	}

	if updatedXML == xmlDesc {
		return nil
	}

	newDom, err := conn.DomainDefineXML(updatedXML)
	if err != nil {
		return fmt.Errorf("define: %w", err)
	}
	defer newDom.Free()

	return nil
}

func GetMaxMemory(name string) (int, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return 0, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	if strings.TrimSpace(name) != "" {
		dom, err := conn.LookupDomainByName(name)
		if err != nil {
			return 0, fmt.Errorf("lookup: %w", err)
		}
		dom.Free()
	}

	nodeInfo, err := conn.GetNodeInfo()
	if err != nil {
		return 0, fmt.Errorf("node info: %w", err)
	}

	totalMiB := nodeInfo.Memory / 1024
	if totalMiB == 0 {
		return 0, fmt.Errorf("host reported zero memory")
	}

	maxInt := uint64(^uint(0) >> 1)
	if totalMiB > maxInt {
		totalMiB = maxInt
	}

	return int(totalMiB), nil
}

func mutateDomainXMLResources(xmlDesc string, newCPU, newMemMiB int) (string, error) {
	memKiB := uint64(newMemMiB) * 1024

	var changed bool
	var ok bool

	before := xmlDesc
	xmlDesc, ok = replaceTagWithLine(xmlDesc, "memory", fmt.Sprintf("<memory unit='KiB'>%d</memory>", memKiB))
	if !ok {
		return "", fmt.Errorf("memory element not found in domain xml")
	}
	if xmlDesc == "" {
		return "", fmt.Errorf("memory replacement produced empty xml")
	}
	if xmlDesc != before {
		changed = true
	}

	before = xmlDesc
	xmlDesc, ok = replaceTagWithLine(xmlDesc, "currentMemory", fmt.Sprintf("<currentMemory unit='KiB'>%d</currentMemory>", memKiB))
	if !ok {
		var inserted bool
		xmlDesc, inserted = insertAfterTag(xmlDesc, "memory", fmt.Sprintf("<currentMemory unit='KiB'>%d</currentMemory>", memKiB))
		if !inserted {
			return "", fmt.Errorf("failed to add currentMemory element to domain xml")
		}
		changed = true
	} else if xmlDesc != before {
		changed = true
	}

	//cpus needs to update
	//<vcpu placement='static'>12</vcpu>
	//cputune number of  vcpupin
	//topology

	updatedVcpuXML, err := updateVcpuTag(xmlDesc, newCPU)
	if err != nil {
		return "", err
	}
	if updatedVcpuXML != xmlDesc {
		changed = true
		xmlDesc = updatedVcpuXML
	}

	cputuneXML, err := updateCputuneBlock(xmlDesc, newCPU)
	if err != nil {
		return "", err
	}
	if cputuneXML != xmlDesc {
		changed = true
		xmlDesc = cputuneXML
	}

	topologyXML, err := updateDomainCPUTopology(xmlDesc, newCPU)
	if err != nil {
		return "", err
	}
	if topologyXML != xmlDesc {
		changed = true
		xmlDesc = topologyXML
	}

	if !changed {
		return xmlDesc, nil
	}

	return xmlDesc, nil
}

func replaceTagWithLine(xmlStr, tag, replacement string) (string, bool) {
	pattern := regexp.MustCompile(`(?m)([ \t]*)<` + tag + `\b[^>]*>[^<]*</` + tag + `>`)
	replaced := false
	result := pattern.ReplaceAllStringFunc(xmlStr, func(match string) string {
		replaced = true
		indent := extractLeadingWhitespace(match)
		return indent + replacement
	})
	return result, replaced
}

func insertAfterTag(xmlStr, anchorTag, newLine string) (string, bool) {
	pattern := regexp.MustCompile(`(?m)([ \t]*)<` + anchorTag + `\b[^>]*>[^<]*</` + anchorTag + `>`)
	matchIndexes := pattern.FindStringSubmatchIndex(xmlStr)
	if matchIndexes == nil {
		return xmlStr, false
	}

	indent := ""
	if len(matchIndexes) >= 4 {
		indent = xmlStr[matchIndexes[2]:matchIndexes[3]]
	}

	insertPos := matchIndexes[1]
	var builder strings.Builder
	builder.Grow(len(xmlStr) + len(newLine) + len(indent) + 1)
	builder.WriteString(xmlStr[:insertPos])
	builder.WriteString("\n")
	builder.WriteString(indent)
	builder.WriteString(newLine)
	builder.WriteString(xmlStr[insertPos:])
	return builder.String(), true
}

func updateVcpuTag(xmlStr string, newCPU int) (string, error) {
	pattern := regexp.MustCompile(`(?m)([ \t]*)<vcpu([^>]*)>[^<]*</vcpu>`)
	updated := false
	result := pattern.ReplaceAllStringFunc(xmlStr, func(match string) string {
		submatches := pattern.FindStringSubmatch(match)
		if len(submatches) != 3 {
			return match
		}
		updated = true
		indent := submatches[1]
		attrs := setAttributeString(submatches[2], "current", strconv.Itoa(newCPU))
		return fmt.Sprintf("%s<vcpu%s>%d</vcpu>", indent, attrs, newCPU)
	})
	if !updated {
		return "", fmt.Errorf("vcpu element not found in domain xml")
	}
	return result, nil
}

func updateCputuneBlock(xmlStr string, newCPU int) (string, error) {
	newTune, err := buildCPUTuneXML(newCPU)
	if err != nil {
		return "", fmt.Errorf("rebuild cputune: %w", err)
	}
	if strings.TrimSpace(newTune) == "" {
		return xmlStr, nil
	}

	pattern := regexp.MustCompile(`(?ms)([ \t]*)<cputune>.*?</cputune>`)
	replaced := false
	result := pattern.ReplaceAllStringFunc(xmlStr, func(match string) string {
		replaced = true
		indent := extractLeadingWhitespace(match)
		return indentBlock(newTune, indent)
	})
	if replaced {
		return result, nil
	}

	indent := findIndentForTag(xmlStr, "vcpu")
	if indent == "" {
		indent = "  "
	}
	insertion := indentBlock(newTune, indent)

	if idx := strings.Index(xmlStr, "</vcpu>"); idx != -1 {
		var builder strings.Builder
		builder.Grow(len(xmlStr) + len(insertion) + 1)
		builder.WriteString(xmlStr[:idx+len("</vcpu>")])
		builder.WriteString("\n")
		builder.WriteString(insertion)
		builder.WriteString(xmlStr[idx+len("</vcpu>"):])
		return builder.String(), nil
	}

	if idx := strings.LastIndex(xmlStr, "</domain>"); idx != -1 {
		var builder strings.Builder
		builder.Grow(len(xmlStr) + len(insertion) + 2)
		builder.WriteString(xmlStr[:idx])
		builder.WriteString("\n")
		builder.WriteString(insertion)
		builder.WriteString("\n")
		builder.WriteString(xmlStr[idx:])
		return builder.String(), nil
	}

	return "", fmt.Errorf("unable to insert cputune block into domain xml")
}

func updateDomainCPUTopology(xmlStr string, vcpuCount int) (string, error) {
	if vcpuCount <= 0 {
		return xmlStr, nil
	}

	cpuBlockPattern := regexp.MustCompile(`(?ms)([ \t]*)<cpu\b[^>]*>.*?</cpu>`)
	if loc := cpuBlockPattern.FindStringIndex(xmlStr); loc != nil {
		block := xmlStr[loc[0]:loc[1]]
		updatedBlock, changed, err := updateTopologyInCPUBlock(block, vcpuCount)
		if err != nil {
			return "", err
		}
		if !changed {
			return xmlStr, nil
		}
		return xmlStr[:loc[0]] + updatedBlock + xmlStr[loc[1]:], nil
	}

	// Handle <cpu .../> by expanding it with a topology line.
	cpuSelfClosingPattern := regexp.MustCompile(`(?m)([ \t]*)<cpu\b([^>]*)/>`)
	replaced := false
	result := cpuSelfClosingPattern.ReplaceAllStringFunc(xmlStr, func(match string) string {
		if replaced {
			return match
		}
		sub := cpuSelfClosingPattern.FindStringSubmatch(match)
		if len(sub) != 3 {
			return match
		}
		replaced = true
		indent := sub[1]
		attrs := sub[2]
		topologyIndent := indent + "  "
		topology := fmt.Sprintf("%s<topology sockets='1' cores='%d' threads='1'/>", topologyIndent, vcpuCount)
		return fmt.Sprintf("%s<cpu%s>\n%s\n%s</cpu>", indent, attrs, topology, indent)
	})
	if replaced {
		return result, nil
	}

	// Some domain definitions omit <cpu>; that's ok.
	return xmlStr, nil
}

func updateTopologyInCPUBlock(cpuBlock string, vcpuCount int) (string, bool, error) {
	if vcpuCount <= 0 {
		return cpuBlock, false, nil
	}

	topologyPattern := regexp.MustCompile(`(?m)([ \t]*)<topology\b([^>]*)/>`)
	if topologyPattern.MatchString(cpuBlock) {
		changed := false
		updated := topologyPattern.ReplaceAllStringFunc(cpuBlock, func(match string) string {
			sub := topologyPattern.FindStringSubmatch(match)
			if len(sub) != 3 {
				return match
			}
			indent := sub[1]
			attrs := sub[2]
			newAttrs := updateTopologyAttrs(attrs, vcpuCount)
			if newAttrs != attrs {
				changed = true
			}
			return fmt.Sprintf("%s<topology%s/>", indent, newAttrs)
		})
		return updated, changed, nil
	}

	// Insert topology before </cpu>.
	lines := strings.Split(cpuBlock, "\n")
	if len(lines) == 0 {
		return cpuBlock, false, nil
	}
	cpuIndent := extractLeadingWhitespace(lines[0])
	topologyIndent := cpuIndent + "  "
	topologyLine := fmt.Sprintf("%s<topology sockets='1' cores='%d' threads='1'/>", topologyIndent, vcpuCount)

	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "</cpu>") {
			// Insert right before the closing tag line.
			lines = append(lines[:i], append([]string{topologyLine}, lines[i:]...)...)
			return strings.Join(lines, "\n"), true, nil
		}
	}

	return "", false, fmt.Errorf("cpu block missing closing </cpu> tag")
}

func updateTopologyAttrs(attrs string, vcpuCount int) string {
	sockets, hasSockets := intAttr(attrs, "sockets")
	threads, hasThreads := intAttr(attrs, "threads")
	dies, hasDies := intAttr(attrs, "dies")
	clusters, hasClusters := intAttr(attrs, "clusters")

	if sockets <= 0 {
		sockets = 1
	}
	if threads <= 0 {
		threads = 1
	}
	if dies <= 0 {
		dies = 1
	}
	if clusters <= 0 {
		clusters = 1
	}

	factor := sockets * threads * dies * clusters
	if factor <= 0 {
		factor = 1
	}

	if vcpuCount%factor == 0 {
		cores := vcpuCount / factor
		attrs = setAttributeString(attrs, "cores", strconv.Itoa(cores))
		if !hasSockets {
			attrs = setAttributeString(attrs, "sockets", "1")
		}
		if !hasThreads {
			attrs = setAttributeString(attrs, "threads", "1")
		}
		return attrs
	}

	// If the current socket/thread layout can't represent the requested vCPU count,
	// fall back to a 1-socket / 1-thread topology.
	attrs = setAttributeString(attrs, "cores", strconv.Itoa(vcpuCount))
	attrs = setAttributeString(attrs, "sockets", "1")
	attrs = setAttributeString(attrs, "threads", "1")
	if hasDies {
		attrs = setAttributeString(attrs, "dies", "1")
	}
	if hasClusters {
		attrs = setAttributeString(attrs, "clusters", "1")
	}
	return attrs
}

func intAttr(attrs, key string) (int, bool) {
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(key) + `=['"]([^'"]+)['"]`)
	match := pattern.FindStringSubmatch(attrs)
	if len(match) != 2 {
		return 0, false
	}
	n, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, true
	}
	return n, true
}

func extractLeadingWhitespace(s string) string {
	i := 0
	for i < len(s) {
		if s[i] != ' ' && s[i] != '\t' {
			break
		}
		i++
	}
	return s[:i]
}

func setAttributeString(attrs, key, value string) string {
	if strings.TrimSpace(attrs) == "" {
		return fmt.Sprintf(" %s='%s'", key, value)
	}

	attrPattern := regexp.MustCompile(key + `=['"][^'"]*['"]`)
	if attrPattern.MatchString(attrs) {
		return attrPattern.ReplaceAllString(attrs, fmt.Sprintf("%s='%s'", key, value))
	}

	if last := attrs[len(attrs)-1]; last != ' ' && last != '\t' {
		return attrs + " " + fmt.Sprintf("%s='%s'", key, value)
	}
	return attrs + fmt.Sprintf("%s='%s'", key, value)
}

func indentBlock(block, indent string) string {
	lines := strings.Split(block, "\n")
	if len(lines) == 0 {
		return indent + block
	}

	minIndent := -1
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		leading := len(line) - len(trimmed)
		if minIndent == -1 || leading < minIndent {
			minIndent = leading
		}
	}
	if minIndent < 0 {
		minIndent = 0
	}

	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = ""
			continue
		}
		if len(line) >= minIndent {
			lines[i] = indent + line[minIndent:]
		} else {
			lines[i] = indent + line
		}
	}

	return strings.Join(lines, "\n")
}

func findIndentForTag(xmlStr, tag string) string {
	pattern := regexp.MustCompile(`(?m)([ \t]*)<` + tag + `\b`)
	if match := pattern.FindStringSubmatch(xmlStr); len(match) == 2 {
		return match[1]
	}
	return ""
}

func ensureDiskSizeAtLeast(path string, targetGB int) error {
	if targetGB <= 0 {
		return nil
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("disk path is empty")
	}

	info, err := readQemuImgInfo(path)
	if err != nil {
		return err
	}
	if info.VirtualSize == 0 {
		return fmt.Errorf("qemu-img info %s returned zero virtual size", path)
	}

	const gib = uint64(1024 * 1024 * 1024)
	if uint64(targetGB) > ^uint64(0)/gib {
		return fmt.Errorf("requested disk size is too large")
	}
	requestedBytes := uint64(targetGB) * gib
	if requestedBytes <= info.VirtualSize {
		return nil
	}

	format := strings.ToLower(strings.TrimSpace(info.Format))
	args := []string{"resize"}
	if format != "" {
		args = append(args, "-f", format)
	}
	args = append(args, path, fmt.Sprintf("%dG", targetGB))

	cmd := exec.Command("qemu-img", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("qemu-img resize %s: %s", path, msg)
		}
		return fmt.Errorf("qemu-img resize %s: %w", path, err)
	}

	updated, err := readQemuImgInfo(path)
	if err != nil {
		return fmt.Errorf("verify resize via qemu-img info %s: %w", path, err)
	}
	if updated.VirtualSize < requestedBytes {
		return fmt.Errorf("qemu-img resize %s did not take effect (have %d bytes, want %d bytes)", path, updated.VirtualSize, requestedBytes)
	}
	return nil
}

func RemoveIsoFromVM(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("vm name is empty")
	}

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

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}

	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		xmlDesc, err = dom.GetXMLDesc(0)
		if err != nil {
			return fmt.Errorf("xml: %w", err)
		}
	}

	diskPattern := regexp.MustCompile(`(?is)\n?[ \t]*<disk\b[^>]*device\s*=\s*['"]cdrom['"][^>]*>.*?</disk>\n?`)
	locs := diskPattern.FindAllStringIndex(xmlDesc, -1)
	if len(locs) == 0 {
		return nil
	}

	deviceXMLs := make([]string, 0, len(locs))
	for _, idx := range locs {
		deviceXMLs = append(deviceXMLs, strings.TrimSpace(xmlDesc[idx[0]:idx[1]]))
	}

	var builder strings.Builder
	builder.Grow(len(xmlDesc))
	prev := 0
	for _, idx := range locs {
		start, end := idx[0], idx[1]
		builder.WriteString(xmlDesc[prev:start])
		prev = end
	}
	builder.WriteString(xmlDesc[prev:])
	newXML := builder.String()

	bootPattern := regexp.MustCompile(`(?mi)^[ \t]*<boot\b[^>]*dev\s*=\s*['"]cdrom['"][^>]*/>\s*\n?`)
	newXML = bootPattern.ReplaceAllString(newXML, "")

	if strings.TrimSpace(newXML) == "" {
		return fmt.Errorf("removing ISO produced empty domain XML")
	}

	if state == libvirt.DOMAIN_RUNNING || state == libvirt.DOMAIN_BLOCKED || state == libvirt.DOMAIN_PAUSED {
		for _, deviceXML := range deviceXMLs {
			if err := dom.DetachDeviceFlags(deviceXML, libvirt.DOMAIN_DEVICE_MODIFY_LIVE); err != nil {
				return fmt.Errorf("detach cdrom: %w", err)
			}
		}
	}

	newDom, err := conn.DomainDefineXML(newXML)
	if err != nil {
		return fmt.Errorf("define: %w", err)
	}
	defer newDom.Free()

	return nil
}

func GetVmCPUXML(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("vm name is empty")
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return "", fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	// Get domain XML
	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return "", fmt.Errorf("xml: %w", err)
	}

	cpuxml := extractCPUXML(xmlDesc)
	return cpuxml, nil
}

func FreezeDisk(vmName string) error {
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
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}
	switch state {
	case libvirt.DOMAIN_RUNNING, libvirt.DOMAIN_BLOCKED:
		// proceed
	case libvirt.DOMAIN_PAUSED:
		return fmt.Errorf("vm %s: freeze requires the guest to be running (current state: %s)", vmName, domainStateToString(state).String())
	default:
		// If the domain is shut off or in another non-running state, there is nothing to freeze.
		return nil
	}

	if err := dom.FSFreeze(nil, 0); err != nil {
		var lerr libvirt.Error
		if errors.As(err, &lerr) {
			switch lerr.Code {
			case libvirt.ERR_AGENT_UNRESPONSIVE, libvirt.ERR_OPERATION_INVALID, libvirt.ERR_AGENT_UNSYNCED, libvirt.ERR_AGENT_COMMAND_FAILED, libvirt.ERR_AGENT_COMMAND_TIMEOUT:
				return fmt.Errorf("vm %s: guest agent not available for fsfreeze: %w", vmName, err)
			}
		}
		return fmt.Errorf("vm %s: fsfreeze failed: %w", vmName, err)
	}

	return nil
}

func UnFreezeDisk(vmName string) error {
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
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}
	switch state {
	case libvirt.DOMAIN_RUNNING, libvirt.DOMAIN_BLOCKED:
		// proceed
	case libvirt.DOMAIN_PAUSED:
		return fmt.Errorf("vm %s: cannot unfreeze while paused (resume the guest first)", vmName)
	default:
		return nil
	}

	if err := dom.FSThaw(nil, 0); err != nil {
		var lerr libvirt.Error
		if errors.As(err, &lerr) {
			switch lerr.Code {
			case libvirt.ERR_AGENT_UNRESPONSIVE, libvirt.ERR_OPERATION_INVALID, libvirt.ERR_AGENT_UNSYNCED, libvirt.ERR_AGENT_COMMAND_FAILED, libvirt.ERR_AGENT_COMMAND_TIMEOUT:
				return fmt.Errorf("vm %s: guest agent not available for fsthaw: %w", vmName, err)
			}
		}
		return fmt.Errorf("vm %s: fsthaw failed: %w", vmName, err)
	}

	return nil
}

func ChangeNetwork(vmName string, newNetwork string) error {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return fmt.Errorf("vm name is empty")
	}
	newNetwork = strings.TrimSpace(newNetwork)
	if newNetwork == "" {
		return fmt.Errorf("network name is empty")
	}
	// Validate that network is either "default" or "512rede"
	if newNetwork != "default" && newNetwork != "512rede" {
		return fmt.Errorf("network must be either 'default' or '512rede', got '%s'", newNetwork)
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	defer dom.Free()

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}

	// Get the current domain XML
	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return fmt.Errorf("xml: %w", err)
	}

	// Parse and modify the network in the XML
	newXML, err := updateNetworkInXML(xmlDesc, newNetwork)
	if err != nil {
		return fmt.Errorf("update network xml: %w", err)
	}

	if newXML == xmlDesc {
		return nil // No change needed
	}

	// If VM is running, update the live configuration first
	if state == libvirt.DOMAIN_RUNNING || state == libvirt.DOMAIN_BLOCKED || state == libvirt.DOMAIN_PAUSED {
		// Get the old interface XML for detaching
		oldIfaceXML, err := extractInterfaceXML(xmlDesc)
		if err != nil {
			return fmt.Errorf("extract old interface: %w", err)
		}

		// Get the new interface XML for attaching
		newIfaceXML, err := extractInterfaceXML(newXML)
		if err != nil {
			return fmt.Errorf("extract new interface: %w", err)
		}

		// Detach old interface (if exists)
		if oldIfaceXML != "" {
			if err := dom.DetachDeviceFlags(oldIfaceXML, libvirt.DOMAIN_DEVICE_MODIFY_LIVE); err != nil {
				// Log warning but continue - the interface might not exist
				logger.Error(fmt.Sprintf("warning: detach old interface: %v", err))
			}
		}

		// Attach new interface
		if newIfaceXML != "" {
			if err := dom.AttachDeviceFlags(newIfaceXML, libvirt.DOMAIN_DEVICE_MODIFY_LIVE); err != nil {
				return fmt.Errorf("attach new interface (live): %w", err)
			}
		}
	}

	// Update persistent configuration
	newDom, err := conn.DomainDefineXML(newXML)
	if err != nil {
		return fmt.Errorf("define: %w", err)
	}
	defer newDom.Free()

	return nil
}

func updateNetworkInXML(xmlDesc string, newNetwork string) (string, error) {
	if strings.TrimSpace(xmlDesc) == "" {
		return "", fmt.Errorf("domain xml is empty")
	}
	if strings.TrimSpace(newNetwork) == "" {
		return "", fmt.Errorf("network name is empty")
	}

	// Build the new interface XML based on the network type
	var newInterfaceXML string
	if newNetwork == "512rede" {
		newInterfaceXML = `<interface type='direct'>
  <source dev='512rede' mode='bridge'/>
  <model type='virtio'/>
</interface>`
	} else {
		newInterfaceXML = fmt.Sprintf(`<interface type='network'>
  <source network='%s'/>
  <model type='virtio'/>
</interface>`, newNetwork)
	}

	// Pattern to match any interface element (network, bridge, or direct)
	interfacePattern := regexp.MustCompile(`(?s)[ \t]*<interface\s+type=['"][^'"]*['"][^>]*>.*?</interface>`)

	// Find the first interface
	match := interfacePattern.FindString(xmlDesc)
	if match == "" {
		return "", fmt.Errorf("no network interface found in domain xml")
	}

	// Get the indentation from the original interface
	indent := extractLeadingWhitespace(match)

	// Indent the new interface XML to match
	indentedNewXML := indentBlock(newInterfaceXML, indent)

	// Replace only the first interface
	result := interfacePattern.ReplaceAllStringFunc(xmlDesc, func(m string) string {
		if m == match {
			return indentedNewXML
		}
		return m
	})

	return result, nil
}

func extractInterfaceXML(xmlDesc string) (string, error) {
	// Pattern to extract the first interface element
	ifacePattern := regexp.MustCompile(`(?s)<interface\s+type=['"][^'"]*['"][^>]*>.*?</interface>`)

	match := ifacePattern.FindString(xmlDesc)
	if match == "" {
		return "", nil // No interface found
	}

	return strings.TrimSpace(match), nil
}
