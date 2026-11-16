package pci

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Maruqes/512SvMan/logger"
	libvirt "libvirt.org/go/libvirt"
)

// A GPU is in exactly one mode. HyperHive owns the driver binding of every function in the
// GPU's IOMMU group and pins it with driver_override, so nothing rebinds it behind our back.
const (
	GPUModeNone = "none" // no driver on any function
	GPUModeHost = "host" // vendor driver on the GPU, native drivers on its companions
	GPUModeVM   = "vm"   // vfio-pci on every function, hostdevs on one VM
)

const (
	vfioDriver       = "vfio-pci"
	nativeDriver     = "" // no override: the kernel picks the device's own driver
	gpuCommandTime   = 60 * time.Second
	gpuBindWait      = 5 * time.Second
	persistencedComm = "nvidia-persiste" // /proc/<pid>/comm is cut at 15 characters
)

// Overridden in tests.
var (
	sysfsPCI = "/sys/bus/pci"
	procRoot = "/proc"
)

// Serializes mode changes: two switches of the same group must never interleave.
var gpuModeMu sync.Mutex

// Host drivers by PCI vendor, tried in order. Nouveau and radeon are never used.
var hostDriversByVendor = map[string][]string{
	"0x10de": {"nvidia"},
	"0x1002": {"amdgpu"},
	"0x8086": {"i915", "xe"},
}

// Modules to unload, in order, once no device is bound to the vendor driver.
var vendorModules = map[string][]string{
	"nvidia": {"nvidia_uvm", "nvidia_drm", "nvidia_modeset", "nvidia"},
	"amdgpu": {"amdgpu"},
	"i915":   {"i915"},
	"xe":     {"xe"},
}

var hostdevBlockPattern = regexp.MustCompile(`(?s)<hostdev\b[^>]*>.*?</hostdev>`)

// SetGPUMode puts a GPU and its whole IOMMU group in the given mode. It is idempotent and never
// kills anything: it refuses while a running VM or a host process holds the GPU.
//
// Order: checks, then drivers, then VM definitions. A failure after the checks can leave the
// group without a driver or a VM definition not yet updated; calling it again repairs both.
func SetGPUMode(ctx context.Context, gpuRef, mode, vmName string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	vmName = strings.TrimSpace(vmName)
	switch mode {
	case GPUModeNone, GPUModeHost:
		vmName = ""
	case GPUModeVM:
		if vmName == "" {
			return fmt.Errorf("vm name is required for gpu mode vm")
		}
	default:
		return fmt.Errorf("invalid gpu mode %q: use none, host or vm", mode)
	}

	address, err := ParsePCIAddress(gpuRef)
	if err != nil {
		return err
	}
	if err := ensureHostPCIIsGPU(address); err != nil {
		return err
	}

	gpuModeMu.Lock()
	defer gpuModeMu.Unlock()

	group, err := relatedPCIDevicesForGPU(address)
	if err != nil {
		return err
	}
	inGroup := make(map[string]struct{}, len(group))
	for _, dev := range group {
		inGroup[dev.String()] = struct{}{}
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	vms, err := listGroupVMs(conn, inGroup)
	if err != nil {
		return err
	}

	var target *libvirt.Domain
	targetName := ""
	targetOff := true
	targetDevices := map[string]VMPCIDevice{}
	if mode == GPUModeVM {
		target, err = conn.LookupDomainByName(vmName)
		if err != nil {
			return fmt.Errorf("vm %s not found: %w", vmName, err)
		}
		defer target.Free()
		targetName = canonicalDomainName(target, vmName)
		if targetOff, err = domainIsOff(target); err != nil {
			return fmt.Errorf("vm %s: %w", targetName, err)
		}
		devices, err := domainPCIDevices(target, targetName)
		if err != nil {
			return err
		}
		targetDevices = makeVMPCIDeviceMap(filterVMPCIDevicesByAddress(devices, inGroup))
	}

	for _, vm := range vms {
		if !vm.off && vm.name != targetName {
			return fmt.Errorf("gpu %s is in use by running vm %s: shut it down first", address, vm.name)
		}
	}

	wanted := make(map[string][]string, len(group))
	for _, dev := range group {
		if wanted[dev.String()], err = wantedDrivers(dev, mode); err != nil {
			return err
		}
	}

	if !targetOff {
		// A running VM cannot receive a GPU. It is fine only if it already has it.
		for _, dev := range group {
			current, err := readCurrentPCIDriver(dev)
			if err != nil {
				return err
			}
			if _, ok := targetDevices[dev.String()]; !ok || current != vfioDriver {
				return fmt.Errorf("vm %s must be shut down to receive gpu %s", targetName, address)
			}
		}
	}

	left, err := ensureGroupIsFree(ctx, address, group, wanted)
	if err != nil {
		return err
	}

	for _, dev := range group {
		if err := applyDriver(ctx, dev, wanted[dev.String()]); err != nil {
			return fmt.Errorf("set gpu %s to mode %s: %w", address, mode, err)
		}
	}
	unloadUnusedModules(ctx, left)

	var errs []error
	for _, vm := range vms {
		if vm.name == targetName {
			continue
		}
		if err := removeHostdevs(conn, vm); err != nil {
			errs = append(errs, err)
		}
	}
	if mode == GPUModeVM {
		if err := ensureUnmanagedHostdevs(conn, target, targetName, group, inGroup, targetDevices); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("gpu %s is bound for mode %s but vm definitions were not updated: %w", address, mode, errors.Join(errs...))
	}
	return nil
}

type groupVM struct {
	name    string
	off     bool
	devices []VMPCIDevice // hostdevs of this VM that belong to the group
}

// listGroupVMs returns the VMs whose definition references a device of the group. Any VM
// that cannot be read is an error: a mode change must not guess who owns the GPU.
func listGroupVMs(conn *libvirt.Connect, inGroup map[string]struct{}) ([]groupVM, error) {
	domains, err := conn.ListAllDomains(0)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	defer freeDomains(domains)

	var vms []groupVM
	for i := range domains {
		name, err := domains[i].GetName()
		if err != nil {
			return nil, fmt.Errorf("get vm name: %w", err)
		}
		devices, err := domainPCIDevices(&domains[i], name)
		if err != nil {
			return nil, err
		}
		devices = filterVMPCIDevicesByAddress(devices, inGroup)
		if len(devices) == 0 {
			continue
		}
		off, err := domainIsOff(&domains[i])
		if err != nil {
			return nil, fmt.Errorf("vm %s: %w", name, err)
		}
		vms = append(vms, groupVM{name: name, off: off, devices: devices})
	}
	return vms, nil
}

func domainIsOff(dom *libvirt.Domain) (bool, error) {
	state, _, err := dom.GetState()
	if err != nil {
		return false, fmt.Errorf("get vm state: %w", err)
	}
	return state == libvirt.DOMAIN_SHUTOFF, nil
}

// wantedDrivers lists the drivers a function may end on, in the order to try them.
func wantedDrivers(dev PCIAddress, mode string) ([]string, error) {
	switch mode {
	case GPUModeVM:
		return []string{vfioDriver}, nil
	case GPUModeNone:
		return []string{driverNone}, nil
	}
	if !classLooksLikeGPU(readSysfsValue(dev, "class")) {
		return []string{nativeDriver}, nil
	}
	vendor := strings.ToLower(readSysfsValue(dev, "vendor"))
	drivers := hostDriversByVendor[vendor]
	if len(drivers) == 0 {
		return nil, fmt.Errorf("no host driver is known for gpu %s (vendor %s): only NVIDIA, AMD and Intel are supported", dev, emptyFallback(vendor, "unknown"))
	}
	return drivers, nil
}

// keepsDriver reports whether a function already has a driver the mode accepts.
func keepsDriver(current string, wanted []string) bool {
	if wanted[0] == nativeDriver {
		return current != "" && !isVFIODriver(current)
	}
	return slices.Contains(wanted, current)
}

// ensureGroupIsFree refuses when a driver about to be unbound is still in use. It returns
// the host drivers that will be left. The only thing it ever stops is nvidia-persistenced.
func ensureGroupIsFree(ctx context.Context, address PCIAddress, group []PCIAddress, wanted map[string][]string) ([]string, error) {
	var left, hostPaths []string
	leavesVFIO := false
	for _, dev := range group {
		current, err := readCurrentPCIDriver(dev)
		if err != nil {
			return nil, err
		}
		if current == "" || keepsDriver(current, wanted[dev.String()]) {
			continue
		}
		if isVFIODriver(current) {
			leavesVFIO = true
			continue
		}
		left = appendUnique(left, current)
		hostPaths = append(hostPaths, hostDevicePaths(dev, current)...)
	}

	if leavesVFIO {
		// Unbinding vfio-pci from an open device blocks until it is released.
		groupLink, err := os.Readlink(devicePath(address, "iommu_group"))
		if err != nil {
			return nil, fmt.Errorf("read iommu group of gpu %s: %w", address, err)
		}
		holders, err := deviceHolders([]string{filepath.Join("/dev/vfio", filepath.Base(groupLink))})
		if err != nil {
			return nil, err
		}
		if len(holders) > 0 {
			return nil, fmt.Errorf("gpu %s is in use by a virtual machine process: %s", address, strings.Join(holders, ", "))
		}
	}

	if len(hostPaths) == 0 {
		return left, nil
	}
	holders, err := deviceHolders(hostPaths)
	if err != nil {
		return nil, err
	}
	if others := withoutPersistenced(holders); len(others) > 0 {
		return nil, fmt.Errorf("gpu %s is in use on the host by %s: stop these processes first", address, strings.Join(others, ", "))
	}
	if len(holders) > 0 {
		if err := runGPUCommand(ctx, "systemctl", "stop", "nvidia-persistenced"); err != nil {
			return nil, fmt.Errorf("gpu %s is held by nvidia-persistenced, which could not be stopped: %w", address, err)
		}
		if holders, err = deviceHolders(hostPaths); err != nil {
			return nil, err
		}
		if len(holders) > 0 {
			return nil, fmt.Errorf("gpu %s is in use on the host by %s: stop these processes first", address, strings.Join(holders, ", "))
		}
	}
	return left, nil
}

func withoutPersistenced(holders []string) []string {
	var others []string
	for _, holder := range holders {
		if !strings.Contains(holder, "("+persistencedComm) {
			others = append(others, holder)
		}
	}
	return others
}

// hostDevicePaths lists the device nodes a host process opens to use this function.
func hostDevicePaths(dev PCIAddress, driver string) []string {
	var paths []string
	for _, pattern := range []string{"card*", "renderD*"} {
		nodes, _ := filepath.Glob(devicePath(dev, "drm", pattern))
		for _, node := range nodes {
			paths = append(paths, filepath.Join("/dev/dri", filepath.Base(node)))
		}
	}
	if driver == "nvidia" {
		// shortcut: /dev/nvidiactl and /dev/nvidia-uvm are shared by every NVIDIA GPU, so a
		// process using another NVIDIA GPU also blocks this one; map minors per GPU if a
		// host ever runs two NVIDIA GPUs in different modes.
		nodes, _ := filepath.Glob("/dev/nvidia*")
		paths = append(paths, nodes...)
	}
	return paths
}

// deviceHolders returns "pid(command)" for every process with one of the device nodes open.
func deviceHolders(paths []string) ([]string, error) {
	devices := map[uint64]struct{}{}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.Mode()&os.ModeCharDevice == 0 {
			continue
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			devices[uint64(stat.Rdev)] = struct{}{}
		}
	}
	if len(devices) == 0 {
		return nil, nil
	}

	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	var pids []int
	for _, entry := range entries {
		if pid, err := strconv.Atoi(entry.Name()); err == nil {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)

	var holders []string
	for _, pid := range pids {
		fdDir := filepath.Join(procRoot, strconv.Itoa(pid), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // the process ended, or it is a kernel thread
		}
		for _, fd := range fds {
			fdPath := filepath.Join(fdDir, fd.Name())
			// Only stat device nodes: a stat through a dead network mount could hang.
			if link, err := os.Readlink(fdPath); err != nil || !strings.HasPrefix(link, "/dev/") {
				continue
			}
			info, err := os.Stat(fdPath)
			if err != nil || info.Mode()&os.ModeCharDevice == 0 {
				continue
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				continue
			}
			if _, held := devices[uint64(stat.Rdev)]; held {
				comm, _ := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "comm"))
				holders = append(holders, fmt.Sprintf("%d(%s)", pid, strings.TrimSpace(string(comm))))
				break
			}
		}
	}
	return holders, nil
}

// applyDriver pins one function to a driver and binds it. A function that already has an
// accepted driver is only pinned, never unbound: unbinding vfio-pci under a running VM hangs.
func applyDriver(ctx context.Context, dev PCIAddress, wanted []string) error {
	current, err := readCurrentPCIDriver(dev)
	if err != nil {
		return err
	}

	switch wanted[0] {
	case nativeDriver:
		if err := setDriverOverride(dev, ""); err != nil {
			return err
		}
		if keepsDriver(current, wanted) {
			return nil
		}
		if err := unbindDriver(dev, current); err != nil {
			return err
		}
		// A companion function with no driver installed simply stays unbound.
		if err := probeDriver(dev); err != nil {
			logger.Warnf("probe native driver for pci %s: %v", dev, err)
		}
		return nil
	case driverNone:
		if err := setDriverOverride(dev, driverNone); err != nil {
			return err
		}
		if err := unbindDriver(dev, current); err != nil {
			return err
		}
		return waitForDriver(ctx, dev, "")
	}

	var lastErr error
	for _, driver := range wanted {
		if current != driver {
			// Dry run: a missing driver must fail before the device is touched.
			if err := runGPUCommand(ctx, "modprobe", "-n", driver); err != nil {
				release, _ := os.ReadFile(filepath.Join(procRoot, "sys/kernel/osrelease"))
				lastErr = fmt.Errorf("driver %s is not available for kernel %s; HyperHive does not install GPU drivers, install it on the host yourself: %w",
					driver, strings.TrimSpace(string(release)), err)
				continue
			}
		}
		if err := setDriverOverride(dev, driver); err != nil {
			return err
		}
		if current != driver {
			if err := unbindDriver(dev, current); err != nil {
				return err
			}
			// Load only now: nvidia refuses to load ("No such device") unless it finds a
			// free GPU pinned to it. Loading binds the device; the probe covers a module
			// that was already loaded.
			if err := runGPUCommand(ctx, "modprobe", driver); err != nil {
				lastErr = fmt.Errorf("driver %s failed to load: %w", driver, err)
				continue
			}
			if current, err = readCurrentPCIDriver(dev); err != nil {
				return err
			}
		}
		if current != driver {
			if err := probeDriver(dev); err != nil {
				logger.Warnf("probe driver %s for pci %s: %v", driver, dev, err)
			}
		}
		if err := waitForDriver(ctx, dev, driver); err != nil {
			lastErr = err
			continue
		}
		if driver == "nvidia" {
			// CUDA needs it; the display path does not, so a failure is only a warning.
			if err := runGPUCommand(ctx, "modprobe", "nvidia_uvm"); err != nil {
				logger.Warnf("load nvidia_uvm: %v", err)
			}
		}
		return nil
	}
	return lastErr
}

func devicePath(dev PCIAddress, elem ...string) string {
	return filepath.Join(append([]string{sysfsPCI, "devices", dev.String()}, elem...)...)
}

func readSysfsValue(dev PCIAddress, name string) string {
	data, err := os.ReadFile(devicePath(dev, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// setDriverOverride pins the function to one driver name; an empty name clears the pin.
// The pin is lost on reboot and is set again when the master reconciles the slave.
func setDriverOverride(dev PCIAddress, driver string) error {
	if err := os.WriteFile(devicePath(dev, "driver_override"), []byte(driver+"\n"), 0); err != nil {
		return fmt.Errorf("pin pci %s to driver %q: %w", dev, driver, err)
	}
	return nil
}

func unbindDriver(dev PCIAddress, current string) error {
	if current == "" {
		return nil
	}
	if err := os.WriteFile(devicePath(dev, "driver", "unbind"), []byte(dev.String()), 0); err != nil {
		return fmt.Errorf("unbind pci %s from %s: %w", dev, current, err)
	}
	return nil
}

func probeDriver(dev PCIAddress) error {
	return os.WriteFile(filepath.Join(sysfsPCI, "drivers_probe"), []byte(dev.String()), 0)
}

// waitForDriver waits until the function is bound to the driver ("" = no driver).
func waitForDriver(ctx context.Context, dev PCIAddress, driver string) error {
	deadline := time.Now().Add(gpuBindWait)
	for {
		current, err := readCurrentPCIDriver(dev)
		if err != nil {
			return err
		}
		if current == driver {
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("pci %s: expected driver %q, got %q", dev, normalizeDriverDisplayName(driver), normalizeDriverDisplayName(current))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func runGPUCommand(ctx context.Context, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, gpuCommandTime)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// unloadUnusedModules removes a vendor driver the group just left when no other device
// uses it. Best effort: a module that stays loaded is harmless.
func unloadUnusedModules(ctx context.Context, left []string) {
	for _, driver := range left {
		modules, ok := vendorModules[driver]
		if !ok {
			continue
		}
		bound, err := os.ReadDir(filepath.Join(sysfsPCI, "drivers", driver))
		if err != nil {
			continue // not loaded
		}
		if slices.ContainsFunc(bound, func(entry os.DirEntry) bool { return fullBDFPattern.MatchString(entry.Name()) }) {
			continue
		}
		if err := runGPUCommand(ctx, "modprobe", append([]string{"-r"}, modules...)...); err != nil {
			logger.Warnf("unload unused gpu driver %s: %v", driver, err)
		}
	}
}

// removeHostdevs drops the group's hostdevs from a shut-off VM's definition.
func removeHostdevs(conn *libvirt.Connect, vm groupVM) error {
	dom, err := conn.LookupDomainByName(vm.name)
	if err != nil {
		return fmt.Errorf("lookup vm %s: %w", vm.name, err)
	}
	defer dom.Free()

	for _, device := range vm.devices {
		address, err := ParsePCIAddress(device.Address)
		if err != nil {
			return err
		}
		if err := dom.DetachDeviceFlags(buildHostDevXML(address, device.Managed), libvirt.DOMAIN_DEVICE_MODIFY_CONFIG); err != nil {
			return fmt.Errorf("remove pci %s from vm %s: %w", address, vm.name, err)
		}
	}
	return nil
}

// ensureUnmanagedHostdevs gives the VM a managed='no' hostdev for every function of the
// group. Only the stored definition changes; a running VM picks it up on its next boot.
func ensureUnmanagedHostdevs(conn *libvirt.Connect, dom *libvirt.Domain, vmName string, group []PCIAddress, inGroup map[string]struct{}, existing map[string]VMPCIDevice) error {
	rewrite := false
	for _, dev := range group {
		device, ok := existing[dev.String()]
		if ok {
			rewrite = rewrite || device.Managed
			continue
		}
		if err := dom.AttachDeviceFlags(buildHostDevXML(dev, false), libvirt.DOMAIN_DEVICE_MODIFY_CONFIG); err != nil {
			return fmt.Errorf("add pci %s to vm %s: %w", dev, vmName, err)
		}
	}
	if !rewrite {
		return nil
	}

	// Edit the definition in place so the hostdev keeps its guest address, ROM and boot order.
	// SECURE keeps the VNC password in the redefined XML.
	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE | libvirt.DOMAIN_XML_SECURE)
	if err != nil {
		return fmt.Errorf("get vm xml %s: %w", vmName, err)
	}
	updated, changed := unmanageHostdevs(xmlDesc, inGroup)
	if !changed {
		return nil
	}
	newDom, err := conn.DomainDefineXML(updated)
	if err != nil {
		return fmt.Errorf("set managed='no' on gpu hostdevs of vm %s: %w", vmName, err)
	}
	return newDom.Free()
}

// unmanageHostdevs switches the matching PCI hostdevs to managed='no' so libvirt never
// rebinds their driver when the VM starts or stops.
func unmanageHostdevs(domainXMLDesc string, addresses map[string]struct{}) (string, bool) {
	changed := false
	updated := hostdevBlockPattern.ReplaceAllStringFunc(domainXMLDesc, func(block string) string {
		var hostDev hostDevXML
		if err := xml.Unmarshal([]byte(block), &hostDev); err != nil || !strings.EqualFold(hostDev.Type, "pci") {
			return block
		}
		address, err := pciAddressFromNodeFields(
			hostDev.Source.Address.Domain, hostDev.Source.Address.Bus,
			hostDev.Source.Address.Slot, hostDev.Source.Address.Function)
		if err != nil {
			return block
		}
		if _, ok := addresses[address.String()]; !ok {
			return block
		}
		tagEnd := strings.IndexByte(block, '>')
		tag := block[:tagEnd]
		for _, quote := range []string{"'", `"`} {
			tag = strings.Replace(tag, "managed="+quote+"yes"+quote, "managed="+quote+"no"+quote, 1)
		}
		if tag == block[:tagEnd] {
			return block
		}
		changed = true
		return tag + block[tagEnd:]
	})
	return updated, changed
}

// actualGPUMode names the mode a GPU is really in, from its bound driver.
func actualGPUMode(driver string) string {
	switch {
	case isVFIODriver(driver):
		return GPUModeVM
	case isNoneDriver(driver):
		return GPUModeNone
	}
	return GPUModeHost
}
