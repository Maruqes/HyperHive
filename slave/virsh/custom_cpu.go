package virsh

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	libvirt "libvirt.org/go/libvirt"
)

func BuildCPUXMLCustom(model string, disabledFeatures []string) string {
	if strings.TrimSpace(model) == "" {
		model = "Westmere"
	}
	defPortable := []string{
		"vmx", "svm", "hle", "rtm", "invpcid", "umip",
		"ibrs", "ssbd", "stibp", "amd-stibp", "amd-ssbd",
		"md-clear", "spec-ctrl", "flush-l1d", "pdcm", "pcid", "ss", "erms",
	}

	if len(disabledFeatures) == 0 {
		disabledFeatures = defPortable
	} else {
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

type CreateVMCustomCPUOptions struct {
	ConnURI           string
	Name              string
	MemoryMB          int
	VCPUs             int
	DiskAlreadyExists bool   //already exists qcow2 file?
	DiskFolder        string // creates folder, can be "" ignored
	DiskPath          string // qcow2 file
	DiskSizeGB        int
	ISOPath           string
	Machine           string
	Network           string
	GraphicsListen    string
	VNCPassword       string
	CPUXml            string
}

func isValidVMName(vmName string) error {
	if vmName == "" {
		return fmt.Errorf("VM name cannot be empty")
	}
	matched, err := regexp.MatchString("^[a-zA-Z0-9]+$", vmName)
	if err != nil {
		return fmt.Errorf("regex error: %w", err)
	}
	if !matched {
		return fmt.Errorf("VM name can only contain letters and numbers")
	}
	return nil
}

func CreateVMCustomCPU(opts CreateVMCustomCPUOptions) (string, error) {
	err := isValidVMName(opts.Name)
	if err != nil {
		return "", err
	}

	if opts.DiskAlreadyExists {
		if opts.ISOPath != "" {
			return "", fmt.Errorf("how the hell i want to create a vm with an already DiskAlreadyExists")
		}
	}

	//make sure DiskFolder exists
	if opts.DiskFolder != "" {
		if err := os.MkdirAll(opts.DiskFolder, 0o777); err != nil {
			return "", fmt.Errorf("creating disk folder: %w", err)
		}
		if err := os.Chmod(opts.DiskFolder, 0o777); err != nil {
			return "", fmt.Errorf("chmod disk folder: %w", err)
		}
	}

	disk := strings.TrimSpace(opts.DiskPath)
	if disk == "" {
		return "", fmt.Errorf("disk path is required")
	}
	parentDir := strings.TrimSpace(filepath.Dir(disk))
	if parentDir == "" || parentDir == "." {
		return "", fmt.Errorf("disk path must include a directory")
	}
	if err := os.MkdirAll(parentDir, 0o777); err != nil {
		return "", fmt.Errorf("create disk directory: %w", err)
	}
	if err := os.Chmod(parentDir, 0o777); err != nil {
		return "", fmt.Errorf("chmod disk directory: %w", err)
	}
	if err := ensureParentDirExists(disk); err != nil {
		return "", fmt.Errorf("disk directory: %w", err)
	}

	if !opts.DiskAlreadyExists {
		// detect/create disk & get its format
		if _, err := EnsureDiskAndDetectFormat(disk, opts.DiskSizeGB); err != nil {
			return "", fmt.Errorf("disk: %w", err)
		}
	}

	// ISO optional
	hasISO := false
	isoTrim := strings.TrimSpace(opts.ISOPath)
	if isoTrim != "" {
		if err := ensureFileExists(isoTrim); err != nil {
			return "", fmt.Errorf("iso path: %w", err)
		}
		hasISO = true
	}

	connURI := strings.TrimSpace(opts.ConnURI)
	if connURI == "" {
		connURI = "qemu:///system"
	}

	conn, err := libvirt.NewConnect(connURI)
	if err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	machineAttr := ""
	if opts.Machine != "" {
		machineAttr = fmt.Sprintf(" machine='%s'", opts.Machine)
	}
	cpuXML := strings.TrimSpace(opts.CPUXml)
	if cpuXML == "" {
		cpuXML = "<cpu mode='host-passthrough' check='none'/>"
	}

	err = validateCPUXML(cpuXML)
	if err != nil {
		return "", fmt.Errorf("invalid CPU XML: %w", err)
	}

	cdromXML := ""
	if hasISO {
		cdromXML = fmt.Sprintf(`
	<disk type='file' device='cdrom'>
	  <driver name='qemu' type='raw'/>
	  <source file='%s'/>
	  <target dev='sda' bus='sata'/>
	  <readonly/>
	</disk>`, isoTrim)
	}

	listenAddr := strings.TrimSpace(opts.GraphicsListen)
	if listenAddr == "" {
		listenAddr = "127.0.0.1"
	}

	var vncAttrs strings.Builder
	fmt.Fprintf(&vncAttrs, " listen='%s'", listenAddr)
	if opts.VNCPassword != "" {
		fmt.Fprintf(&vncAttrs, " passwd='%s'", opts.VNCPassword)
	}
	vncGraphicsXML := fmt.Sprintf("<graphics type='vnc' autoport='yes' port='-1'%s/>", vncAttrs.String())

	spicePasswordAttr := ""
	if opts.VNCPassword != "" {
		spicePasswordAttr = fmt.Sprintf(" passwd='%s'", opts.VNCPassword)
	}
	spiceGraphicsXML := fmt.Sprintf(`
	<graphics type='spice' autoport='yes' port='-1' listen='%s'%s>
	  <listen type='address' address='%s'/>
	  <image compression='auto_glz'/>
	  <jpeg compression='auto'/>
	  <zlib compression='auto'/>
	  <playback compression='on'/>
	  <streaming mode='all'/>
	  <clipboard copypaste='yes'/>
	  <mouse mode='client'/>
	  <filetransfer enable='yes'/>
	  <gl enable='yes'/>
	</graphics>`, listenAddr, spicePasswordAttr, listenAddr)

	videoXML := `
	<video>
	  <model type='qxl' ram='65536' vram='65536' vgamem='16384' heads='1' primary='yes'/>
	</video>`

	cputuneXML, err := buildCPUTuneXML(opts.VCPUs)
	if err != nil {
		return "", fmt.Errorf("cputune: %w", err)
	}

	bootDev := "hd"
	if hasISO {
		bootDev = "cdrom"
	}

	//<driver name='qemu' type='qcow2' cache='writeback' io='threads'/>
	//<driver name='qemu' type='qcow2' cache='none' io='native'/>
	networkXML := ""
	if opts.Network == "512rede" {
		networkXML = `<interface type='direct'>
  <source dev='512rede' mode='bridge'/>
  <model type='virtio'/>
</interface>`
	} else {
		networkXML = fmt.Sprintf(`<interface type='network'>
	  <source network='%s'/>
	  <model type='virtio'/>
	</interface>`, opts.Network)
	}

	driverType := "qcow2"

	virtioSerialControllerXML := `
 <controller type='virtio-serial' index='0'/>`
	guestAgentChannelXML := `
  <channel type='unix'>
	<source mode='bind'/>
	<target type='virtio' name='org.qemu.guest_agent.0'/>
  </channel>`
	spiceChannelXML := `
  <channel type='spicevmc'>
	<target type='virtio' name='com.redhat.spice.0'/>
  </channel>`

	inputDevicesXML := `
	<input type='tablet' bus='usb'/>
	<input type='mouse' bus='ps2'/>`

	domainXML := fmt.Sprintf(`
<domain type='kvm'>
  <seclabel type='none'/>
  <name>%s</name>
  <memory unit='MiB'>%d</memory>
  <vcpu placement='static'>%d</vcpu>

  <iothreads>1</iothreads>

  %s
  <os>
	<type arch='x86_64'%s>hvm</type>
	<boot dev='%s'/>
	<boot dev='hd'/>
  </os>
  <features><acpi/><apic/></features>
  %s
  <devices>
	<disk type='file' device='disk'>
	  <driver name='qemu' type='%s' cache='none' io='native'/>
	  <source file='%s'/>
	  <target dev='vda' bus='virtio'/>
	</disk>%s%s%s%s%s%s%s%s
	<sound model='ich9'/>
	%s
  </devices>
</domain>`,
		opts.Name, opts.MemoryMB, opts.VCPUs,
		cputuneXML,
		machineAttr,
		bootDev,
		cpuXML, driverType, disk, cdromXML, networkXML, virtioSerialControllerXML, guestAgentChannelXML, spiceChannelXML, inputDevicesXML, vncGraphicsXML, spiceGraphicsXML, videoXML,
	)

	xmlPath, err := WriteDomainXMLToDisk(opts.Name, domainXML, disk)
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

type SSHOptions struct {
	Password             string
	IdentityFile         string
	SkipHostKeyCheck     bool
	UserKnownHostsFile   string
	AdditionalSSHOptions []string
}

type MigrateOptions struct {
	ConnURI string
	Name    string
	DestURI string
	Live    bool
	Timeout int32

	SSH SSHOptions
}

func GetCpuFeatures() ([]string, error) {
	//call "sudo virsh -c qemu:///system capabilities | xmlstarlet sel -t -m '/capabilities/host/cpu/feature' -v '@name' -n | sort -u"
	cmd := exec.Command("bash", "-c", "sudo virsh -c qemu:///system capabilities | xmlstarlet sel -t -m '/capabilities/host/cpu/feature' -v '@name' -n | sort -u")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to get CPU features: %w", err)
	}
	return strings.Split(strings.TrimSpace(out.String()), "\n"), nil
}

func GetHostCPUXML() (string, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	caps, err := conn.GetCapabilities()
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`(?s)<host>.*?(<cpu>.*?</cpu>).*</host>`)
	m := re.FindStringSubmatch(caps)
	if len(m) < 2 {
		return "", fmt.Errorf("no <cpu> block found in capabilities")
	}
	return m[1], nil
}

func validateCPUXML(cpuXML string) error {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	// Compare this CPU against the host CPU
	res, err := conn.CompareCPU(cpuXML, 0)
	if err != nil {
		return fmt.Errorf("compare failed: %w", err)
	}

	switch int(res) {
	case 0:
		return fmt.Errorf("CPU incompatible: host lacks required features")
	case 1:
		return nil // perfect match
	case 2:
		return nil // host supports all required features
	default:
		return fmt.Errorf("unknown compare result: %v", res)
	}
}

func UpdateVMCPUXml(vmName, cpuXml string) error {

	//validate cpuXml has <cpu> block
	if err := validateCPUXML(cpuXml); err != nil {
		return fmt.Errorf("validate CPU XML: %w", err)
	}

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("lookup domain: %w", err)
	}
	defer dom.Free()

	//get state if != shutdown return error
	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}
	if state != libvirt.DOMAIN_SHUTOFF {
		return fmt.Errorf("domain %s must be shut off to update CPU XML", vmName)
	}

	//get current xml
	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return fmt.Errorf("get xml desc: %w", err)
	}

	//old cpu block
	oldCpu, err := GetVmCPUXML(vmName)
	if err != nil {
		return fmt.Errorf("get old cpu xml: %w", err)
	}

	//replace old cpu block with new cpu block
	newXmlDesc := strings.Replace(xmlDesc, oldCpu, cpuXml, 1)

	//undefine domain
	if err := dom.Undefine(); err != nil {
		return fmt.Errorf("undefine domain: %w", err)
	}

	//define domain with new xml
	newDom, err := conn.DomainDefineXML(newXmlDesc)
	if err != nil {
		return fmt.Errorf("define domain with new xml: %w", err)
	}
	defer newDom.Free()

	return nil
}
