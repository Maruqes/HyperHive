package pci

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	libvirt "libvirt.org/go/libvirt"
)

var (
	fullBDFPattern     = regexp.MustCompile(`(?i)^([0-9a-f]{4}):([0-9a-f]{2}):([0-9a-f]{2})\.([0-7])$`)
	shortBDFPattern    = regexp.MustCompile(`(?i)^([0-9a-f]{2}):([0-9a-f]{2})\.([0-7])$`)
	nodeNamePattern    = regexp.MustCompile(`(?i)^pci_([0-9a-f]{4})_([0-9a-f]{2})_([0-9a-f]{2})_([0-7])$`)
	rawNodeNamePattern = regexp.MustCompile(`(?i)^([0-9a-f]{4})_([0-9a-f]{2})_([0-9a-f]{2})_([0-7])$`)
)

const driverNone = "none"

// PCIAddress identifies a PCI device using its domain:bus:slot.function.
type PCIAddress struct {
	Domain   uint16
	Bus      uint8
	Slot     uint8
	Function uint8
}

func (a PCIAddress) String() string {
	return fmt.Sprintf("%04x:%02x:%02x.%d", a.Domain, a.Bus, a.Slot, a.Function)
}

func (a PCIAddress) nodeDeviceName() string {
	return fmt.Sprintf("pci_%04x_%02x_%02x_%d", a.Domain, a.Bus, a.Slot, a.Function)
}

// HostPCIDevice is a host-visible PCI device with metadata useful for passthrough.
type HostPCIDevice struct {
	NodeName      string
	Path          string
	Address       string
	Domain        uint16
	Bus           uint8
	Slot          uint8
	Function      uint8
	Driver        string
	Vendor        string
	VendorID      string
	Product       string
	ProductID     string
	Class         string
	IOMMUGroup    int
	NUMANode      int
	IsGPU         bool
	ManagedByVFIO bool
	AttachedToVMs []string
}

// VMPCIDevice is a PCI hostdev configured on a VM.
type VMPCIDevice struct {
	Address  string
	Domain   uint16
	Bus      uint8
	Slot     uint8
	Function uint8
	Managed  bool
	Alias    string
}

// ParsePCIAddress accepts:
// - 0000:65:00.0
// - 65:00.0 (assumes domain 0000)
// - pci_0000_65_00_0
// - 0000_65_00_0
// - /sys/bus/pci/devices/0000:65:00.0
func ParsePCIAddress(raw string) (PCIAddress, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return PCIAddress{}, fmt.Errorf("pci address is empty")
	}
	if idx := strings.LastIndexByte(s, '/'); idx >= 0 {
		s = s[idx+1:]
	}
	s = strings.TrimSpace(s)

	if m := fullBDFPattern.FindStringSubmatch(s); len(m) == 5 {
		return pciAddressFromHexParts(m[1], m[2], m[3], m[4])
	}

	if m := shortBDFPattern.FindStringSubmatch(s); len(m) == 4 {
		return pciAddressFromHexParts("0000", m[1], m[2], m[3])
	}

	if m := nodeNamePattern.FindStringSubmatch(s); len(m) == 5 {
		return pciAddressFromHexParts(m[1], m[2], m[3], m[4])
	}

	if m := rawNodeNamePattern.FindStringSubmatch(s); len(m) == 5 {
		return pciAddressFromHexParts(m[1], m[2], m[3], m[4])
	}

	return PCIAddress{}, fmt.Errorf("invalid pci address format: %q", raw)
}

// ListHostPCIDevices lists host PCI devices and marks which VMs reference them.
func ListHostPCIDevices() ([]HostPCIDevice, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	attachments, err := listAllVMAttachments(conn)
	if err != nil {
		return nil, err
	}

	nodeDevices, err := conn.ListAllNodeDevices(libvirt.CONNECT_LIST_NODE_DEVICES_CAP_PCI_DEV)
	if err != nil {
		return nil, fmt.Errorf("list host pci devices: %w", err)
	}

	devices := make([]HostPCIDevice, 0, len(nodeDevices))
	for i := range nodeDevices {
		xmlDesc, err := nodeDevices[i].GetXMLDesc(0)
		_ = nodeDevices[i].Free()
		if err != nil {
			return nil, fmt.Errorf("get node device xml: %w", err)
		}

		device, err := parseHostPCIDevice(xmlDesc)
		if err != nil {
			return nil, err
		}
		if vmNames, ok := attachments[device.Address]; ok {
			device.AttachedToVMs = append(device.AttachedToVMs, vmNames...)
		}
		devices = append(devices, device)
	}

	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Address < devices[j].Address
	})

	return devices, nil
}

// ListAllPCIDevices is an alias for ListHostPCIDevices.
func ListAllPCIDevices() ([]HostPCIDevice, error) {
	return ListHostPCIDevices()
}

// ListHostPCIDevicesWithIOMMU lists only host PCI devices that belong to an IOMMU group.
func ListHostPCIDevicesWithIOMMU() ([]HostPCIDevice, error) {
	devices, err := ListHostPCIDevices()
	if err != nil {
		return nil, err
	}
	return filterPCIDevicesWithIOMMU(devices), nil
}

// ListHostGPUs returns host PCI devices likely to be graphics adapters.
func ListHostGPUs() ([]HostPCIDevice, error) {
	all, err := ListHostPCIDevices()
	if err != nil {
		return nil, err
	}

	out := make([]HostPCIDevice, 0, len(all))
	for _, dev := range all {
		if dev.IsGPU {
			out = append(out, dev)
		}
	}
	return out, nil
}

// ListHostGPUsWithIOMMU lists only host GPUs that belong to an IOMMU group.
func ListHostGPUsWithIOMMU() ([]HostPCIDevice, error) {
	devices, err := ListHostGPUs()
	if err != nil {
		return nil, err
	}
	return filterPCIDevicesWithIOMMU(devices), nil
}

func filterPCIDevicesWithIOMMU(devices []HostPCIDevice) []HostPCIDevice {
	out := make([]HostPCIDevice, 0, len(devices))
	for _, dev := range devices {
		if dev.IOMMUGroup >= 0 {
			out = append(out, dev)
		}
	}
	return out
}

// ListVMPCIDevices lists PCI hostdev entries configured in a VM definition.
func ListVMPCIDevices(vmName string) ([]VMPCIDevice, error) {
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
		return nil, fmt.Errorf("lookup vm %s: %w", vmName, err)
	}
	defer dom.Free()

	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		xmlDesc, err = dom.GetXMLDesc(0)
		if err != nil {
			return nil, fmt.Errorf("get vm xml %s: %w", vmName, err)
		}
	}

	return parseVMPCIDevices(xmlDesc)
}

// ListVMGPUs lists VM hostdev entries that correspond to host GPUs.
func ListVMGPUs(vmName string) ([]VMPCIDevice, error) {
	vmDevices, err := ListVMPCIDevices(vmName)
	if err != nil {
		return nil, err
	}

	hostGPUs, err := ListHostGPUs()
	if err != nil {
		return nil, err
	}

	gpuAddressSet := makeAddressSetFromHostDevices(hostGPUs)
	return filterVMPCIDevicesByAddress(vmDevices, gpuAddressSet), nil
}

// AttachPCIToVM adds a host PCI device to a VM using managed='yes'.
func AttachPCIToVM(vmName, pciRef string) error {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return fmt.Errorf("vm name is empty")
	}

	address, err := ParsePCIAddress(pciRef)
	if err != nil {
		return err
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("lookup vm %s: %w", vmName, err)
	}
	defer dom.Free()

	nodeDev, err := conn.LookupDeviceByName(address.nodeDeviceName())
	if err != nil {
		return fmt.Errorf("lookup host pci %s: %w", address.String(), err)
	}
	defer nodeDev.Free()

	targetVMName := vmName
	if actualName, nameErr := dom.GetName(); nameErr == nil {
		if actualName = strings.TrimSpace(actualName); actualName != "" {
			targetVMName = actualName
		}
	}

	attachments, err := listAllVMAttachments(conn)
	if err != nil {
		return err
	}
	if attachedVMs, ok := attachments[address.String()]; ok {
		alreadyAttachedToTarget, attachedElsewhere := partitionPCIAttachments(attachedVMs, targetVMName)
		if len(attachedElsewhere) > 0 {
			return fmt.Errorf(
				"pci %s is already attached to vm(s): %s",
				address.String(),
				strings.Join(attachedElsewhere, ","),
			)
		}
		if alreadyAttachedToTarget {
			return nil
		}
	}

	// Force driver to vfio-pci before attaching to VM
	if err := bindToVFIO(nodeDev, address); err != nil {
		return err
	}

	flags, err := domainDeviceFlags(dom)
	if err != nil {
		return err
	}

	if err := dom.AttachDeviceFlags(buildHostDevXML(address, true), flags); err != nil {
		return fmt.Errorf("attach pci %s to vm %s: %w", address.String(), vmName, err)
	}
	return nil
}

// AttachGPUToVM adds a host GPU PCI device to a VM using managed='yes'.
func AttachGPUToVM(vmName, gpuRef string) error {
	address, err := ParsePCIAddress(gpuRef)
	if err != nil {
		return err
	}

	if err := ensureHostPCIIsGPU(address); err != nil {
		return err
	}

	related, err := relatedPCIDevicesForGPU(address)
	if err != nil {
		return err
	}

	for _, dev := range related {
		if err := AttachPCIToVM(vmName, dev.String()); err != nil {
			return err
		}
	}

	return nil
}

func relatedPCIDevicesForGPU(address PCIAddress) ([]PCIAddress, error) {
	device, err := lookupHostPCIDevice(address)
	if err != nil {
		return nil, err
	}

	if device.IOMMUGroup < 0 {
		return []PCIAddress{address}, nil
	}

	hostDevices, err := ListHostPCIDevices()
	if err != nil {
		return nil, err
	}

	related := make([]PCIAddress, 0)
	seen := make(map[string]struct{})
	for _, dev := range hostDevices {
		if dev.IOMMUGroup != device.IOMMUGroup {
			continue
		}
		addr, err := ParsePCIAddress(dev.Address)
		if err != nil {
			continue
		}
		key := addr.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		related = append(related, addr)
	}

	if _, ok := seen[address.String()]; !ok {
		related = append(related, address)
	}

	sort.Slice(related, func(i, j int) bool {
		return related[i].String() < related[j].String()
	})

	return related, nil
}

func partitionPCIAttachments(attachedVMs []string, targetVM string) (bool, []string) {
	targetVM = strings.TrimSpace(targetVM)
	alreadyAttachedToTarget := false
	attachedElsewhere := make([]string, 0, len(attachedVMs))

	for _, vm := range attachedVMs {
		vm = strings.TrimSpace(vm)
		if vm == "" {
			continue
		}
		if vm == targetVM {
			alreadyAttachedToTarget = true
			continue
		}
		attachedElsewhere = appendUnique(attachedElsewhere, vm)
	}

	sort.Strings(attachedElsewhere)
	return alreadyAttachedToTarget, attachedElsewhere
}

// DetachPCIFromVM removes a host PCI device from a VM and attempts to return it to the host.
func DetachPCIFromVM(vmName, pciRef string) error {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return fmt.Errorf("vm name is empty")
	}

	address, err := ParsePCIAddress(pciRef)
	if err != nil {
		return err
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("lookup vm %s: %w", vmName, err)
	}
	defer dom.Free()

	flags, err := domainDeviceFlags(dom)
	if err != nil {
		return err
	}

	if err := dom.DetachDeviceFlags(buildHostDevXML(address, true), flags); err != nil {
		return fmt.Errorf("detach pci %s from vm %s: %w", address.String(), vmName, err)
	}

	return ReturnPCIToHost(address.String())
}

// DetachGPUFromVM removes a host GPU PCI device from a VM and restores driver state to none.
func DetachGPUFromVM(vmName, gpuRef string) error {
	address, err := ParsePCIAddress(gpuRef)
	if err != nil {
		return err
	}

	if err := ensureHostPCIIsGPU(address); err != nil {
		return err
	}

	if err := DetachPCIFromVM(vmName, address.String()); err != nil {
		return err
	}

	// Keep host GPU in neutral state after VM detach.
	return forcePCIDriverToNone(address)
}

// ReturnPCIToHost re-attaches a PCI device to the host driver (unbinds from vfio-pci).
func ReturnPCIToHost(pciRef string) error {
	address, err := ParsePCIAddress(pciRef)
	if err != nil {
		return err
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	nodeDev, err := conn.LookupDeviceByName(address.nodeDeviceName())
	if err != nil {
		return fmt.Errorf("lookup host pci %s: %w", address.String(), err)
	}
	defer nodeDev.Free()

	driver, err := getNodeDeviceDriver(nodeDev)
	if err == nil && !isNoneDriver(driver) && !isVFIODriver(driver) {
		// Already using host driver, nothing to do.
		return nil
	}

	// ReAttach unbinds from vfio-pci and restores the host side driver state.
	if err := nodeDev.ReAttach(); err != nil {
		return fmt.Errorf("reattach pci %s to host driver: %w", address.String(), err)
	}

	return nil
}

// ReturnGPUToHost unbinds a GPU PCI device from vfio and restores driver state to none.
func ReturnGPUToHost(gpuRef string) error {
	address, err := ParsePCIAddress(gpuRef)
	if err != nil {
		return err
	}

	if err := ensureHostPCIIsGPU(address); err != nil {
		return err
	}

	if err := ReturnPCIToHost(address.String()); err != nil {
		return err
	}

	// Keep host GPU in neutral state (driver none) when not assigned to a VM.
	return forcePCIDriverToNone(address)
}

// bindToVFIO detaches the PCI device from its current driver and binds it to vfio-pci.
// This is required before a device can be passed through to a VM.
func bindToVFIO(nodeDev *libvirt.NodeDevice, address PCIAddress) error {
	driver, err := getNodeDeviceDriver(nodeDev)
	if err == nil && isVFIODriver(driver) {
		// Already using vfio-pci driver
		return nil
	}

	// DetachFlags with "vfio-pci" driver name will unbind from current driver
	// and bind to vfio-pci
	if err := nodeDev.DetachFlags("vfio-pci", 0); err != nil {
		return fmt.Errorf("bind pci %s to vfio-pci: %w", address.String(), err)
	}

	driver, err = getNodeDeviceDriver(nodeDev)
	if err != nil {
		return fmt.Errorf("verify pci %s driver after vfio bind: %w", address.String(), err)
	}
	if !isVFIODriver(driver) {
		return fmt.Errorf("verify pci %s driver after vfio bind: expected vfio-pci, got %q", address.String(), normalizeDriverDisplayName(driver))
	}

	return nil
}

func domainDeviceFlags(dom *libvirt.Domain) (libvirt.DomainDeviceModifyFlags, error) {
	state, _, err := dom.GetState()
	if err != nil {
		return 0, fmt.Errorf("get vm state: %w", err)
	}

	flags := libvirt.DOMAIN_DEVICE_MODIFY_CONFIG
	switch state {
	case libvirt.DOMAIN_RUNNING, libvirt.DOMAIN_BLOCKED, libvirt.DOMAIN_PAUSED, libvirt.DOMAIN_PMSUSPENDED:
		flags |= libvirt.DOMAIN_DEVICE_MODIFY_LIVE
	}

	return flags, nil
}

func buildHostDevXML(address PCIAddress, managed bool) string {
	managedStr := "no"
	if managed {
		managedStr = "yes"
	}
	return fmt.Sprintf(
		"<hostdev mode='subsystem' type='pci' managed='%s'><source><address domain='0x%04x' bus='0x%02x' slot='0x%02x' function='0x%x'/></source></hostdev>",
		managedStr,
		address.Domain,
		address.Bus,
		address.Slot,
		address.Function,
	)
}

func listAllVMAttachments(conn *libvirt.Connect) (map[string][]string, error) {
	domains, err := conn.ListAllDomains(0)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	defer freeDomains(domains)

	attachments := make(map[string][]string)
	for i := range domains {
		name, err := domains[i].GetName()
		if err != nil {
			continue
		}

		xmlDesc, err := domains[i].GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
		if err != nil {
			xmlDesc, err = domains[i].GetXMLDesc(0)
			if err != nil {
				continue
			}
		}

		vmDevices, err := parseVMPCIDevices(xmlDesc)
		if err != nil {
			continue
		}
		for _, dev := range vmDevices {
			attachments[dev.Address] = appendUnique(attachments[dev.Address], name)
		}
	}

	return attachments, nil
}

func parseHostPCIDevice(xmlDesc string) (HostPCIDevice, error) {
	var node nodeDeviceXML
	if err := xml.Unmarshal([]byte(xmlDesc), &node); err != nil {
		return HostPCIDevice{}, fmt.Errorf("parse node device xml: %w", err)
	}

	address, err := pciAddressFromNodeFields(node.Capability.Domain, node.Capability.Bus, node.Capability.Slot, node.Capability.Function)
	if err != nil {
		return HostPCIDevice{}, err
	}

	iommuGroup := -1
	if g := strings.TrimSpace(node.Capability.IOMMUGroup.Number); g != "" {
		if parsed, err := parseXMLPCIComponent(g, 32); err == nil {
			iommuGroup = int(parsed)
		}
	}

	numaNode := -1
	if n := strings.TrimSpace(node.Capability.NUMA.Node); n != "" {
		if parsed, err := parseXMLPCIComponent(n, 32); err == nil {
			numaNode = int(parsed)
		}
	}

	device := HostPCIDevice{
		NodeName:      strings.TrimSpace(node.Name),
		Path:          strings.TrimSpace(node.Path),
		Address:       address.String(),
		Domain:        address.Domain,
		Bus:           address.Bus,
		Slot:          address.Slot,
		Function:      address.Function,
		Driver:        normalizeDriverDisplayName(node.Driver.Name),
		Vendor:        strings.TrimSpace(node.Capability.Vendor.Text),
		VendorID:      strings.TrimSpace(node.Capability.Vendor.ID),
		Product:       strings.TrimSpace(node.Capability.Product.Text),
		ProductID:     strings.TrimSpace(node.Capability.Product.ID),
		Class:         strings.TrimSpace(node.Capability.Class),
		IOMMUGroup:    iommuGroup,
		NUMANode:      numaNode,
		IsGPU:         classLooksLikeGPU(node.Capability.Class),
		ManagedByVFIO: isVFIODriver(node.Driver.Name),
	}

	if device.NodeName == "" {
		device.NodeName = address.nodeDeviceName()
	}

	return device, nil
}

func parseVMPCIDevices(xmlDesc string) ([]VMPCIDevice, error) {
	var dom domainXML
	if err := xml.Unmarshal([]byte(xmlDesc), &dom); err != nil {
		return nil, fmt.Errorf("parse vm xml: %w", err)
	}

	devices := make([]VMPCIDevice, 0)
	for _, hostDev := range dom.Devices.HostDevs {
		if !strings.EqualFold(strings.TrimSpace(hostDev.Type), "pci") {
			continue
		}
		address, err := pciAddressFromNodeFields(
			hostDev.Source.Address.Domain,
			hostDev.Source.Address.Bus,
			hostDev.Source.Address.Slot,
			hostDev.Source.Address.Function,
		)
		if err != nil {
			return nil, err
		}
		devices = append(devices, VMPCIDevice{
			Address:  address.String(),
			Domain:   address.Domain,
			Bus:      address.Bus,
			Slot:     address.Slot,
			Function: address.Function,
			Managed:  strings.EqualFold(strings.TrimSpace(hostDev.Managed), "yes"),
			Alias:    strings.TrimSpace(hostDev.Alias.Name),
		})
	}

	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Address < devices[j].Address
	})
	return devices, nil
}

func getNodeDeviceDriver(nodeDev *libvirt.NodeDevice) (string, error) {
	xmlDesc, err := nodeDev.GetXMLDesc(0)
	if err != nil {
		return "", fmt.Errorf("get node device xml: %w", err)
	}
	var node nodeDeviceXML
	if err := xml.Unmarshal([]byte(xmlDesc), &node); err != nil {
		return "", fmt.Errorf("parse node device xml: %w", err)
	}
	return strings.TrimSpace(node.Driver.Name), nil
}

func normalizeDriverDisplayName(driver string) string {
	driver = strings.TrimSpace(driver)
	if driver == "" {
		return driverNone
	}
	return driver
}

func isVFIODriver(driver string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(driver)), "vfio")
}

func isNoneDriver(driver string) bool {
	driver = strings.TrimSpace(strings.ToLower(driver))
	return driver == "" || driver == driverNone
}

func forcePCIDriverToNone(address PCIAddress) error {
	driver, err := readCurrentPCIDriver(address)
	if err != nil {
		return err
	}
	if isNoneDriver(driver) {
		return nil
	}

	unbindPath := filepath.Join("/sys/bus/pci/devices", address.String(), "driver", "unbind")
	if err := os.WriteFile(unbindPath, []byte(address.String()), 0); err != nil {
		return fmt.Errorf("set pci %s driver to none: %w", address.String(), err)
	}

	driver, err = readCurrentPCIDriver(address)
	if err != nil {
		return err
	}
	if !isNoneDriver(driver) {
		return fmt.Errorf("set pci %s driver to none: expected none, got %q", address.String(), normalizeDriverDisplayName(driver))
	}
	return nil
}

func readCurrentPCIDriver(address PCIAddress) (string, error) {
	driverLink := filepath.Join("/sys/bus/pci/devices", address.String(), "driver")
	target, err := os.Readlink(driverLink)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read pci %s driver: %w", address.String(), err)
	}
	return filepath.Base(target), nil
}

func pciAddressFromHexParts(domain, bus, slot, function string) (PCIAddress, error) {
	d, err := strconv.ParseUint(domain, 16, 16)
	if err != nil {
		return PCIAddress{}, fmt.Errorf("invalid pci domain %q: %w", domain, err)
	}
	b, err := strconv.ParseUint(bus, 16, 8)
	if err != nil {
		return PCIAddress{}, fmt.Errorf("invalid pci bus %q: %w", bus, err)
	}
	s, err := strconv.ParseUint(slot, 16, 8)
	if err != nil {
		return PCIAddress{}, fmt.Errorf("invalid pci slot %q: %w", slot, err)
	}
	f, err := strconv.ParseUint(function, 16, 8)
	if err != nil {
		return PCIAddress{}, fmt.Errorf("invalid pci function %q: %w", function, err)
	}
	return PCIAddress{
		Domain:   uint16(d),
		Bus:      uint8(b),
		Slot:     uint8(s),
		Function: uint8(f),
	}, nil
}

func pciAddressFromNodeFields(domain, bus, slot, function string) (PCIAddress, error) {
	d, err := parseXMLPCIComponent(domain, 16)
	if err != nil {
		return PCIAddress{}, fmt.Errorf("invalid pci domain %q: %w", domain, err)
	}
	b, err := parseXMLPCIComponent(bus, 8)
	if err != nil {
		return PCIAddress{}, fmt.Errorf("invalid pci bus %q: %w", bus, err)
	}
	s, err := parseXMLPCIComponent(slot, 8)
	if err != nil {
		return PCIAddress{}, fmt.Errorf("invalid pci slot %q: %w", slot, err)
	}
	f, err := parseXMLPCIComponent(function, 8)
	if err != nil {
		return PCIAddress{}, fmt.Errorf("invalid pci function %q: %w", function, err)
	}
	return PCIAddress{
		Domain:   uint16(d),
		Bus:      uint8(b),
		Slot:     uint8(s),
		Function: uint8(f),
	}, nil
}

func parseXMLPCIComponent(raw string, bitSize int) (uint64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}

	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		return strconv.ParseUint(s, 0, bitSize)
	}

	if v, err := strconv.ParseUint(s, 10, bitSize); err == nil {
		return v, nil
	}

	return strconv.ParseUint(s, 16, bitSize)
}

func classLooksLikeGPU(rawClass string) bool {
	s := strings.TrimSpace(rawClass)
	if s == "" {
		return false
	}

	val, err := parseXMLPCIComponent(s, 32)
	if err != nil {
		return strings.HasPrefix(strings.ToLower(s), "0x03")
	}

	baseClass := (val >> 16) & 0xff
	return baseClass == 0x03
}

func ensureHostPCIIsGPU(address PCIAddress) error {
	device, err := lookupHostPCIDevice(address)
	if err != nil {
		return err
	}
	if device.IsGPU {
		return nil
	}
	return fmt.Errorf(
		"pci %s is not a GPU (class %s)",
		address.String(),
		emptyFallback(device.Class, "-"),
	)
}

func lookupHostPCIDevice(address PCIAddress) (HostPCIDevice, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return HostPCIDevice{}, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	nodeDev, err := conn.LookupDeviceByName(address.nodeDeviceName())
	if err != nil {
		return HostPCIDevice{}, fmt.Errorf("lookup host pci %s: %w", address.String(), err)
	}
	defer nodeDev.Free()

	xmlDesc, err := nodeDev.GetXMLDesc(0)
	if err != nil {
		return HostPCIDevice{}, fmt.Errorf("get node device xml: %w", err)
	}

	return parseHostPCIDevice(xmlDesc)
}

func makeAddressSetFromHostDevices(devices []HostPCIDevice) map[string]struct{} {
	set := make(map[string]struct{}, len(devices))
	for _, dev := range devices {
		addr := strings.TrimSpace(dev.Address)
		if addr == "" {
			continue
		}
		set[addr] = struct{}{}
	}
	return set
}

func filterVMPCIDevicesByAddress(devices []VMPCIDevice, allowed map[string]struct{}) []VMPCIDevice {
	if len(devices) == 0 || len(allowed) == 0 {
		return []VMPCIDevice{}
	}

	out := make([]VMPCIDevice, 0, len(devices))
	for _, dev := range devices {
		if _, ok := allowed[dev.Address]; ok {
			out = append(out, dev)
		}
	}
	return out
}

func appendUnique(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}

func emptyFallback(s, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	return s
}

func freeDomains(domains []libvirt.Domain) {
	for i := range domains {
		_ = domains[i].Free()
	}
}

type nodeDeviceXML struct {
	Name       string `xml:"name"`
	Path       string `xml:"path"`
	Driver     driver `xml:"driver"`
	Capability struct {
		Class      string          `xml:"class"`
		Domain     string          `xml:"domain"`
		Bus        string          `xml:"bus"`
		Slot       string          `xml:"slot"`
		Function   string          `xml:"function"`
		Product    textWithID      `xml:"product"`
		Vendor     textWithID      `xml:"vendor"`
		IOMMUGroup iommuGroupValue `xml:"iommuGroup"`
		NUMA       numaNodeValue   `xml:"numa"`
	} `xml:"capability"`
}

type driver struct {
	Name string `xml:"name"`
}

type textWithID struct {
	ID   string `xml:"id,attr"`
	Text string `xml:",chardata"`
}

type iommuGroupValue struct {
	Number string `xml:"number,attr"`
}

type numaNodeValue struct {
	Node string `xml:"node,attr"`
}

type domainXML struct {
	Devices struct {
		HostDevs []hostDevXML `xml:"hostdev"`
	} `xml:"devices"`
}

type hostDevXML struct {
	Type    string `xml:"type,attr"`
	Managed string `xml:"managed,attr"`
	Source  struct {
		Address struct {
			Domain   string `xml:"domain,attr"`
			Bus      string `xml:"bus,attr"`
			Slot     string `xml:"slot,attr"`
			Function string `xml:"function,attr"`
		} `xml:"address"`
	} `xml:"source"`
	Alias struct {
		Name string `xml:"name,attr"`
	} `xml:"alias"`
}

// DetachAllGPUs detaches all host GPUs from their drivers by setting driver to none.
// This is typically called at startup to ensure GPUs are in a neutral state for passthrough.
func DetachAllGPUs() error {
	gpus, err := ListHostGPUs()
	if err != nil {
		return fmt.Errorf("list host GPUs: %w", err)
	}

	var errs []error
	for _, gpu := range gpus {
		addr, err := ParsePCIAddress(gpu.Address)
		if err != nil {
			errs = append(errs, fmt.Errorf("parse GPU address %s: %w", gpu.Address, err))
			continue
		}
		if err := forcePCIDriverToNone(addr); err != nil {
			errs = append(errs, fmt.Errorf("detach GPU %s: %w", gpu.Address, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("detach GPUs errors: %v", errs)
	}
	return nil
}
