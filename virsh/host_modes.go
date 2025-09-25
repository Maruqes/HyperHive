package virsh

import (
	"fmt"
	"os"

	libvirt "libvirt.org/go/libvirt"
)

// CreateVMHostPassthrough creates & starts a VM using CPU mode=host-passthrough.
// All files & XML are stored under ROOTFOLDER hierarchy.
func CreateVMHostPassthrough(
	connURI string, // e.g. "qemu:///system"
	name string, // VM name
	memMB, vcpus int, // resources
	diskPath string, // relative -> ROOTFOLDER/qcow2/<disk>; absolute -> as is
	diskSizeGB int, // if disk doesn't exist, it will be created
	isoPath string, // relative -> ROOTFOLDER/iso/<iso>; absolute -> as is
	machine string, // e.g. "pc-q35-8.2" or "" (auto)
	network string, // e.g. "default"
	graphicsListen string, // e.g. "0.0.0.0"
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
  <cpu mode='host-passthrough' check='none'/>
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
</domain>`, name, memMB, vcpus, machineAttr, disk, iso, network, graphicsListen)

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

// CreateVMHostModel creates & starts a VM using CPU mode=host-model (bom para single-host).
func CreateVMHostModel(
	connURI string,
	name string,
	memMB, vcpus int,
	diskPath string, diskSizeGB int,
	isoPath string,
	machine string,
	network string,
	graphicsListen string,
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
  <cpu mode='host-model' check='partial'/>
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
</domain>`, name, memMB, vcpus, machineAttr, disk, iso, network, graphicsListen)

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
