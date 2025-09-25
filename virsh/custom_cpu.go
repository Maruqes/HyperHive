package virsh

import (
	"fmt"
	"os"
	"sort"
	"strings"

	libvirt "libvirt.org/go/libvirt"
)

// BuildCPUXMLCustom returns a <cpu>...</cpu> block for a custom baseline.
// model examples: "Nehalem", "Westmere", "Haswell-noTSX", ...
// disabledFeatures optional; if nil -> uses a portable default set.
func BuildCPUXMLCustom(model string, disabledFeatures []string) string {
	if strings.TrimSpace(model) == "" {
		model = "Westmere"
	}
	defPortable := []string{
		"vmx", "svm", "hle", "rtm", "invpcid", "umip",
		"ibrs", "ssbd", "stibp", "amd-stibp", "amd-ssbd",
		"md-clear", "spec-ctrl", "flush-l1d",
	}
	if len(disabledFeatures) == 0 {
		disabledFeatures = defPortable
	} else {
		// union + sort
		m := map[string]struct{}{}
		for _, f := range append(disabledFeatures, defPortable...) {
			f = strings.TrimSpace(f)
			if f != "" {
				m[f] = struct{}{}
			}
		}
		disabledFeatures = disabledFeatures[:0]
		for k := range m {
			disabledFeatures = append(disabledFeatures, k)
		}
		sort.Strings(disabledFeatures)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<cpu mode='custom' match='minimum' check='partial'>\n")
	fmt.Fprintf(&b, "  <model fallback='forbid'>%s</model>\n", model)
	for _, f := range disabledFeatures {
		fmt.Fprintf(&b, "  <feature policy='disable' name='%s'/>\n", f)
	}
	b.WriteString("</cpu>")
	return b.String()
}

// CreateVMCustomCPU creates & starts a VM with a fixed baseline CPU model (migration-friendly).
// Files live under ROOTFOLDER; XML saved under xml/<name>.xml.
func CreateVMCustomCPU(
	connURI string,
	name string,
	memMB, vcpus int,
	diskPath string, diskSizeGB int,
	isoPath string,
	machine string,
	network string,
	graphicsListen string,
	cpuModel string, // "Nehalem" | "Westmere" | "Haswell-noTSX" | custom
	disabledFeatures []string, // nil -> default portable set
) (string, error) {

	if err := EnsureDirs(); err != nil {
		return "", err
	}
	disk := ResolveDiskPath(diskPath)
	iso := ResolveISOPath(isoPath)

	if err := EnsureQCOW2(disk, diskSizeGB); err != nil {
		return "", fmt.Errorf("ensure qcow2: %w", err)
	}
	if _, err := os.Stat(iso); err != nil {
		return "", fmt.Errorf("ISO not found: %s", iso)
	}

	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	machineAttr := ""
	if machine != "" {
		machineAttr = fmt.Sprintf(" machine='%s'", machine)
	}
	cpuXML := BuildCPUXMLCustom(cpuModel, disabledFeatures)

	domainXML := fmt.Sprintf(`
<domain type='kvm'>
  <name>%s</name>
  <memory unit='MiB'>%d</memory>
  <vcpu>%d</vcpu>
  <os>
    <type arch='x86_64'%s>hvm</type>
    <boot dev='cdrom'/>
    <boot dev='hd'/>
  </os>
  <features><acpi/><apic/></features>
  %s
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' cache='none' discard='unmap'/>
      <source file='%s'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='%s'/>
      <target dev='sda' bus='sata'/>
      <readonly/>
    </disk>
    <interface type='network'>
      <source network='%s'/>
      <model type='virtio'/>
    </interface>
    <graphics type='vnc' listen='%s'/>
    <video><model type='virtio'/></video>
  </devices>
</domain>`, name, memMB, vcpus, machineAttr, cpuXML, disk, iso, network, graphicsListen)

	xmlPath, err := WriteDomainXMLToDisk(name, domainXML)
	if err != nil {
		return "", err
	}

	dom, err := conn.DomainDefineXML(domainXML)
	if err != nil {
		return "", fmt.Errorf("define: %w", err)
	}
	defer dom.Free()

	if err := dom.Create(); err != nil {
		return "", fmt.Errorf("start: %w", err)
	}
	return xmlPath, nil
}
