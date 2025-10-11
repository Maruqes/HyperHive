package virsh

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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

func CreateVMCustomCPU(
	connURI, name string,
	memMB, vcpus int,
	diskPath string, diskSizeGB int,
	isoPath, machine, network, graphicsListen string,
	cpuModel string, disabledFeatures []string,
) (string, error) {

	disk := strings.TrimSpace(diskPath)
	if disk == "" {
		return "", fmt.Errorf("disk path is required")
	}
	if err := ensureParentDirExists(disk); err != nil {
		return "", fmt.Errorf("disk directory: %w", err)
	}

	// detect/create disk & get its format
	diskFmt, err := EnsureDiskAndDetectFormat(disk, diskSizeGB)
	if err != nil {
		return "", fmt.Errorf("disk: %w", err)
	}

	// ISO optional
	hasISO := false
	isoTrim := strings.TrimSpace(isoPath)
	if isoTrim != "" {
		if err := ensureFileExists(isoTrim); err != nil {
			return "", fmt.Errorf("iso path: %w", err)
		}
		hasISO = true
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
  %s
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
    <graphics type='vnc' autoport='yes' port='-1' listen='%s'/>
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
		cpuXML, diskFmt, disk, cdromXML, network, graphicsListen,
	)

	xmlPath, err := WriteDomainXMLToDisk(name, domainXML, disk)
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

	Password string

	SSH SSHOptions
}

// MigrateVM shells out to virsh, allowing ssh configuration via sshpass or LIBVIRT_SSH_OPTS.
func MigrateVM(opts MigrateOptions) error {
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

	password := strings.TrimSpace(opts.Password)
	if password == "" {
		password = strings.TrimSpace(opts.SSH.Password)
	}

	var cmd *exec.Cmd
	if password != "" {
		args := append([]string{"-p", password, "virsh"}, baseArgs...)
		cmd = exec.Command("sshpass", args...)
	} else {
		cmd = exec.Command("virsh", baseArgs...)
	}
	cmd.Env = env

	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		fullCmd := strings.Join(cmd.Args, " ")
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if output == "" {
				output = "no output captured"
			}
			return fmt.Errorf("virsh migrate failed (%s): exit code %d: %s", fullCmd, exitErr.ExitCode(), output)
		}
		if output != "" {
			return fmt.Errorf("virsh migrate failed (%s): %s: %w", fullCmd, output, err)
		}
		return fmt.Errorf("virsh migrate failed (%s): %w", fullCmd, err)
	}

	if output != "" {
		fmt.Println(output)
	}
	return nil
}
