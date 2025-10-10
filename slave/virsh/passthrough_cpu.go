package virsh

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"

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
	disk := strings.TrimSpace(params.DiskPath)
	if disk == "" {
		return "", fmt.Errorf("disk path is required")
	}
	if err := ensureParentDirExists(disk); err != nil {
		return "", fmt.Errorf("disk directory: %w", err)
	}

	// Create/inspect disk and get its format (qcow2/raw/…)
	diskFmt, err := EnsureDiskAndDetectFormat(disk, params.DiskSizeGB)
	if err != nil {
		return "", fmt.Errorf("disk: %w", err)
	}

	// ISO is optional: only include CDROM if the file exists
	hasISO := false
	isoPath := strings.TrimSpace(params.ISOPath)
	if isoPath != "" {
		if err := ensureFileExists(isoPath); err != nil {
			return "", fmt.Errorf("iso path: %w", err)
		}
		hasISO = true
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
    </disk>`, isoPath)
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

	cputuneXML, err := buildCPUTuneXML(params.VCPUs)
	if err != nil {
		return "", fmt.Errorf("cputune: %w", err)
	}

	domainXML := fmt.Sprintf(`
<domain type='kvm'>
  <name>%s</name>
  <memory unit='MiB'>%d</memory>
  <vcpu placement='static'>%d</vcpu>

  <!-- Optional: give virtio-disk its own thread so we can pin it -->
  <iothreads>1</iothreads>

  %s

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
		params.Name, params.MemoryMB, params.VCPUs,
		cputuneXML, // <- new
		machineAttr,
		bootDev,
		diskFmt, disk, cdromXML, params.Network, graphicsAttrs,
	)

	xmlPath, err := WriteDomainXMLToDisk(params.Name, domainXML, disk)
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

func buildCPUTuneXML(vcpuCount int) (string, error) {
	if vcpuCount <= 0 {
		return "", nil
	}

	hostCPUs, err := detectOnlineCPUs()
	if err != nil {
		return "", err
	}
	if len(hostCPUs) == 0 {
		return "", fmt.Errorf("no online host CPUs detected")
	}

	var pinPool []int
	emulatorCPUs := hostCPUs

	if len(hostCPUs) > vcpuCount {
		pinPool = append([]int(nil), hostCPUs[1:]...)
		if len(pinPool) == 0 {
			pinPool = append([]int(nil), hostCPUs...)
		}
		emulatorCPUs = hostCPUs[:1]
	} else {
		pinPool = append([]int(nil), hostCPUs...)
	}

	var vcpuPins []string
	for vcpu := 0; vcpu < vcpuCount; vcpu++ {
		hostCPU := pinPool[vcpu%len(pinPool)]
		vcpuPins = append(vcpuPins, fmt.Sprintf("    <vcpupin vcpu='%d' cpuset='%d'/>", vcpu, hostCPU))
	}

	emulatorSet := formatCPUSet(emulatorCPUs)
	if emulatorSet == "" {
		emulatorSet = formatCPUSet(hostCPUs)
	}
	iothreadSet := emulatorSet

	shares := vcpuCount * 1024
	if shares < 1024 {
		shares = 1024
	}

	var b strings.Builder
	b.WriteString("  <cputune>\n")
	for _, line := range vcpuPins {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("    <emulatorpin cpuset='%s'/>\n", emulatorSet))
	b.WriteString(fmt.Sprintf("    <iothreadpin iothread='1' cpuset='%s'/>\n", iothreadSet))
	b.WriteString(fmt.Sprintf("    <shares>%d</shares>\n", shares))
	b.WriteString("  </cputune>")

	return b.String(), nil
}

func detectOnlineCPUs() ([]int, error) {
	const cpuOnlinePath = "/sys/devices/system/cpu/online"

	data, err := os.ReadFile(cpuOnlinePath)
	if err == nil {
		cpus, parseErr := parseCPUSet(strings.TrimSpace(string(data)))
		if parseErr != nil {
			return nil, parseErr
		}
		if len(cpus) > 0 {
			return cpus, nil
		}
	}

	count := runtime.NumCPU()
	if count <= 0 {
		return nil, fmt.Errorf("runtime reported no CPUs")
	}
	cpus := make([]int, count)
	for i := range cpus {
		cpus[i] = i
	}
	return cpus, nil
}

func parseCPUSet(spec string) ([]int, error) {
	if spec == "" {
		return nil, nil
	}

	var cpus []int
	parts := strings.Split(spec, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			if len(bounds) != 2 {
				return nil, fmt.Errorf("invalid cpu range: %s", part)
			}
			start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid cpu range start %q: %w", bounds[0], err)
			}
			end, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid cpu range end %q: %w", bounds[1], err)
			}
			if end < start {
				return nil, fmt.Errorf("invalid cpu range %s", part)
			}
			for cpu := start; cpu <= end; cpu++ {
				cpus = append(cpus, cpu)
			}
			continue
		}

		value, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid cpu value %q: %w", part, err)
		}
		cpus = append(cpus, value)
	}

	if len(cpus) == 0 {
		return cpus, nil
	}
	sort.Ints(cpus)
	deduped := cpus[:1]
	for _, cpu := range cpus[1:] {
		if cpu != deduped[len(deduped)-1] {
			deduped = append(deduped, cpu)
		}
	}
	return deduped, nil
}

func formatCPUSet(cpus []int) string {
	if len(cpus) == 0 {
		return ""
	}
	sorted := append([]int(nil), cpus...)
	sort.Ints(sorted)

	var parts []string
	start := sorted[0]
	prev := sorted[0]

	for i := 1; i < len(sorted); i++ {
		current := sorted[i]
		if current == prev+1 {
			prev = current
			continue
		}
		parts = append(parts, renderCPURange(start, prev))
		start = current
		prev = current
	}
	parts = append(parts, renderCPURange(start, prev))
	return strings.Join(parts, ",")
}

func renderCPURange(start, end int) string {
	if start == end {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}
