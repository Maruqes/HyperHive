package virsh

import (
	"fmt"
	"os"

	libvirt "libvirt.org/go/libvirt"
)

type VMCreationParams struct {
	ConnURI        string
	Name           string
	MemoryMB       int
	VCPUs          int
	DiskPath       string // caminho do disco virtual
	DiskSizeGB     int    // tamanho do disco virtual em GB
	ISOPath        string // caminho do arquivo ISO (opcional)
	Machine        string // tipo de máquina (opcional)
	Network        string // nome da rede libvirt
	GraphicsListen string // endereço para o VNC escutar
	VNCPassword    string // senha para o VNC (opcional)
}

// sem migracao
func CreateVMHostPassthrough(params VMCreationParams) (string, error) {
	if err := EnsureDirs(); err != nil {
		return "", fmt.Errorf("ensure dirs: %w", err)
	}
	disk := ResolveDiskPath(params.DiskPath)
	iso := ResolveISOPath(params.ISOPath)

	// Create/inspect disk and get its format (qcow2/raw/…)
	diskFmt, err := EnsureDiskAndDetectFormat(disk, params.DiskSizeGB)
	if err != nil {
		return "", fmt.Errorf("disk: %w", err)
	}

	// ISO is optional: only include CDROM if the file exists
	hasISO := false
	if params.ISOPath != "" {
		if _, err := os.Stat(iso); err == nil {
			hasISO = true
		} else {
			return "", fmt.Errorf("iso %s: %w", iso, err)
		}
	}

	connURI := params.ConnURI
	if connURI == "" {
		connURI = "qemu:///system"
	}
	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	machineAttr := ""
	if params.Machine != "" {
		machineAttr = fmt.Sprintf(" machine='%s'", params.Machine)
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

	graphicsAttrs := ""
	if params.GraphicsListen != "" {
		graphicsAttrs += fmt.Sprintf(" listen='%s'", params.GraphicsListen)
	}
	if params.VNCPassword != "" {
		graphicsAttrs += fmt.Sprintf(" passwd='%s'", params.VNCPassword)
	}
	if graphicsAttrs == "" {
		graphicsAttrs = " listen='127.0.0.1'"
	}

	bootDev := "hd"
	if hasISO {
		bootDev = "cdrom"
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
    <graphics type='vnc' autoport='yes' port='-1'%s/>
    <video><model type='virtio'/></video>
  </devices>
</domain>`,
		params.Name, params.MemoryMB, params.VCPUs, machineAttr,
		bootDev,
		diskFmt, disk, cdromXML, params.Network, graphicsAttrs,
	)

	xmlPath, err := WriteDomainXMLToDisk(params.Name, domainXML)
	if err != nil {
		return "", fmt.Errorf("write domain xml: %w", err)
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
