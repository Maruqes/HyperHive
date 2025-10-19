package virsh

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slave/extra"
	"sort"
	"strings"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
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
	ConnURI        string
	Name           string
	MemoryMB       int
	VCPUs          int
	DiskFolder     string //fazer
	DiskPath       string
	DiskSizeGB     int
	ISOPath        string
	Machine        string
	Network        string
	GraphicsListen string
	VNCPassword    string // fazer
	CPUXml         string
}

func CreateVMCustomCPU(opts CreateVMCustomCPUOptions) (string, error) {

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

	// detect/create disk & get its format
	if _, err := EnsureDiskAndDetectFormat(disk, opts.DiskSizeGB); err != nil {
		return "", fmt.Errorf("disk: %w", err)
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

	graphicsAttrs := ""
	if opts.GraphicsListen != "" {
		graphicsAttrs += fmt.Sprintf(" listen='%s'", opts.GraphicsListen)
	}
	if opts.VNCPassword != "" {
		graphicsAttrs += fmt.Sprintf(" passwd='%s'", opts.VNCPassword)
	}
	if graphicsAttrs == "" {
		graphicsAttrs = " listen='127.0.0.1'"
	}

	cputuneXML, err := buildCPUTuneXML(opts.VCPUs)
	if err != nil {
		return "", fmt.Errorf("cputune: %w", err)
	}

	bootDev := "hd"
	if hasISO {
		bootDev = "cdrom"
	}

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
	  <driver name='qemu' type='qcow2' cache='none' io='native'/>
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
		opts.Name, opts.MemoryMB, opts.VCPUs,
		cputuneXML,
		machineAttr,
		bootDev,
		cpuXML, disk, cdromXML, opts.Network, graphicsAttrs,
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

	Live bool

	SSH SSHOptions
}

// MigrateVM shells out to virsh, allowing ssh configuration via sshpass or LIBVIRT_SSH_OPTS.
func MigrateVM(opts MigrateOptions, ctx context.Context) error {

	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	//check se a machine esta aberta
	dom, err := conn.LookupDomainByName(opts.Name)
	if err != nil {
		return fmt.Errorf("lookup domain: %w", err)
	}
	defer dom.Free()

	state, _, err := dom.GetState()
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}

	if state != libvirt.DOMAIN_RUNNING {
		return fmt.Errorf("domain %s is not running, it needs to be running to do a live migration", opts.Name)
	}

	connURI := strings.TrimSpace(opts.ConnURI)
	if connURI == "" {
		return fmt.Errorf("conn uri is required")
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return fmt.Errorf("domain name is required")
	}
	destURI := strings.TrimSpace(opts.DestURI)
	if destURI == "" {
		return fmt.Errorf("destination URI is required")
	}

	baseArgs := []string{
		"-c", connURI,
		"migrate",
		"--persistent",
		"--verbose",
		"--undefinesource",
		"--p2p",
		"--tunnelled",
	}

	if opts.Live {
		baseArgs = append(baseArgs, "--live")
	}

	baseArgs = append(baseArgs, name, destURI)

	var sshOpts []string
	if opts.SSH.SkipHostKeyCheck {
		sshOpts = append(sshOpts, "-o", "StrictHostKeyChecking=no")
		if opts.SSH.UserKnownHostsFile == "" {
			sshOpts = append(sshOpts, "-o", "UserKnownHostsFile=/dev/null")
		}
	}
	if file := strings.TrimSpace(opts.SSH.UserKnownHostsFile); file != "" {
		sshOpts = append(sshOpts, "-o", "UserKnownHostsFile="+file)
	}
	if key := strings.TrimSpace(opts.SSH.IdentityFile); key != "" {
		sshOpts = append(sshOpts, "-i", key)
	}
	if len(opts.SSH.AdditionalSSHOptions) > 0 {
		sshOpts = append(sshOpts, opts.SSH.AdditionalSSHOptions...)
	}

	env := os.Environ()
	if len(sshOpts) > 0 {
		env = append(env, fmt.Sprintf("LIBVIRT_SSH_OPTS=%s", strings.Join(sshOpts, " ")))
	}

	cmd := exec.Command("virsh", baseArgs...)
	cmd.Env = env

	errors := extra.ExecWithOutToSocketCMD(ctx, extraGrpc.WebSocketsMessageType_MigrateVm, cmd)
	if errors != nil {
		//convert []error to a single error
		var errMsgs []string
		for _, e := range errors {
			errMsgs = append(errMsgs, e.Error())
		}
		return fmt.Errorf("virsh migrate: %s", strings.Join(errMsgs, "; "))
	}

	// var stdoutBuf, stderrBuf bytes.Buffer
	// cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	// cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	// err := cmd.Run()
	// output := strings.TrimSpace(stdoutBuf.String() + "\n" + stderrBuf.String())
	// if err != nil {
	// 	fullCmd := strings.Join(cmd.Args, " ")
	// 	var exitErr *exec.ExitError
	// 	if errors.As(err, &exitErr) {
	// 		if output == "" {
	// 			output = "no output captured"
	// 		}
	// 		return fmt.Errorf("virsh migrate failed (%s): exit code %d: %s", fullCmd, exitErr.ExitCode(), output)
	// 	}
	// 	if output != "" {
	// 		return fmt.Errorf("virsh migrate failed (%s): %s: %w", fullCmd, output, err)
	// 	}
	// 	return fmt.Errorf("virsh migrate failed (%s): %w", fullCmd, err)
	// }

	return nil
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
