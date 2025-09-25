package virsh

import (
	"fmt"
	"os"

	libvirt "libvirt.org/go/libvirt"
)

func CreateVMHostPassthrough(
	connURI, name string,
	memMB, vcpus int,
	diskPath string, diskSizeGB int,
	isoPath string,
	machine, network, graphicsListen string,
) (string, error) {

	if err := EnsureDirs(); err != nil {
		return "", err
	}
	disk := ResolveDiskPath(diskPath)
	iso := ResolveISOPath(isoPath)

	// Create/inspect disk and get its format (qcow2/raw/…)
	diskFmt, err := EnsureDiskAndDetectFormat(disk, diskSizeGB)
	if err != nil {
		return "", fmt.Errorf("disk: %w", err)
	}

	// ISO is optional: only include CDROM if the file exists
	hasISO := false
	if isoPath != "" {
		if _, err := os.Stat(iso); err == nil {
			hasISO = true
		}
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

	cdromXML := ""
	if hasISO {
		cdromXML = fmt.Sprintf(`
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='%s'/>
      <target dev='sda' bus='sata'/>
      <readonly/>
    </disk>`, iso)
	}

	domainXML := fmt.Sprintf(`
<domain type='kvm'>
  <name>%s</name>
  <memory unit='MiB'>%d</memory>
  <vcpu>%d</vcpu>
  <os>
    <type arch='x86_64'%s>hvm</type>
    <boot dev='%s'/>
    <boot dev='hd'/>
  </os>
  <features><acpi/><apic/></features>
  <cpu mode='host-passthrough' check='none'/>
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='%s' cache='none' discard='unmap'/>
      <source file='%s'/>
      <target dev='vda' bus='virtio'/>
    </disk>%s
    <interface type='network'>
      <source network='%s'/>
      <model type='virtio'/>
    </interface>
    <graphics type='vnc' listen='%s'/>
    <video><model type='virtio'/></video>
  </devices>
</domain>`,
		name, memMB, vcpus, machineAttr,
		func() string {
			if hasISO {
				return "cdrom"
			} else {
				return "hd"
			}
		}(),
		diskFmt, disk, cdromXML, network, graphicsListen,
	)

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

func CreateVMHostModel(
	connURI, name string,
	memMB, vcpus int,
	diskPath string, diskSizeGB int,
	isoPath string,
	machine, network, graphicsListen string,
) (string, error) {

	if err := EnsureDirs(); err != nil {
		return "", err
	}
	disk := ResolveDiskPath(diskPath)
	iso := ResolveISOPath(isoPath)

	diskFmt, err := EnsureDiskAndDetectFormat(disk, diskSizeGB)
	if err != nil {
		return "", fmt.Errorf("disk: %w", err)
	}

	hasISO := false
	if isoPath != "" {
		if _, err := os.Stat(iso); err == nil {
			hasISO = true
		}
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

	cdromXML := ""
	if hasISO {
		cdromXML = fmt.Sprintf(`
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='%s'/>
      <target dev='sda' bus='sata'/>
      <readonly/>
    </disk>`, iso)
	}

	domainXML := fmt.Sprintf(`
<domain type='kvm'>
  <name>%s</name>
  <memory unit='MiB'>%d</memory>
  <vcpu>%d</vcpu>
  <os>
    <type arch='x86_64'%s>hvm</type>
    <boot dev='%s'/>
    <boot dev='hd'/>
  </os>
  <features><acpi/><apic/></features>
  <cpu mode='host-model' check='partial'/>
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='%s' cache='none' discard='unmap'/>
      <source file='%s'/>
      <target dev='vda' bus='virtio'/>
    </disk>%s
    <interface type='network'>
      <source network='%s'/>
      <model type='virtio'/>
    </interface>
    <graphics type='vnc' listen='%s'/>
    <video><model type='virtio'/></video>
  </devices>
</domain>`,
		name, memMB, vcpus, machineAttr,
		func() string {
			if hasISO {
				return "cdrom"
			} else {
				return "hd"
			}
		}(),
		diskFmt, disk, cdromXML, network, graphicsListen,
	)

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
