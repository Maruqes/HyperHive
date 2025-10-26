package virsh

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slave/extra"
	"strconv"
	"strings"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
	"github.com/Maruqes/512SvMan/logger"
	libvirt "libvirt.org/go/libvirt"
)

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

	if opts.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative")
	}
	baseArgs := []string{
		"-c", connURI,
		"migrate",
		"--persistent",
		"--verbose",
		"--undefinesource",
		"--p2p",
		"--tunnelled",
		"--live",
		"--auto-converge",
		"--compressed",
		"--comp-methods", "zstd",
		"--comp-zstd-level", "3",
		"--bandwidth", "0",
		"--downtime", "1000",
		"--abort-on-error",
	}

	if opts.Timeout > 0 {
		baseArgs = append(baseArgs, "--timeout", strconv.FormatInt(int64(opts.Timeout), 10))
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
	logger.Info("Executing: " + cmd.String())
	errors := extra.ExecWithOutToSocketCMD(ctx, extraGrpc.WebSocketsMessageType_MigrateVm, cmd)
	if errors != nil {
		//convert []error to a single error
		var errMsgs []string
		for _, e := range errors {
			errMsgs = append(errMsgs, e.Error())
		}
		return fmt.Errorf("virsh migrate: %s", strings.Join(errMsgs, "; "))
	}

	return nil
}

func extractDiskPath(xmlDesc string) string {
	startTag := "<source file='"
	endTag := "'"
	startIdx := strings.Index(xmlDesc, startTag)
	if startIdx == -1 {
		return ""
	}
	startIdx += len(startTag)
	endIdx := strings.Index(xmlDesc[startIdx:], endTag)
	if endIdx == -1 {
		return ""
	}
	return xmlDesc[startIdx : startIdx+endIdx]
}

// can be <cpu </cpu> or just <cpu .../>
var cpuRe = regexp.MustCompile(`(?s)<cpu\b[^>]*?/>|<cpu\b[^>]*?>.*?</cpu>`)

// Returns the first <cpu .../> or <cpu>...</cpu>, or "" if none.
func extractCPUXML(s string) string {
	m := cpuRe.FindString(s)
	if m == "" {
		return ""
	}
	return m
}

type ColdMigrationInfo struct {
	VmName      string
	Memory      int32
	VCpus       int32
	Network     string
	VNCPassword string
	CpuXML      string
	DiskPath    string //qcow file
	Live        bool
}

// returns qcow2file path, cpuXML
func MigrateColdLose(name string) (*ColdMigrationInfo, error) {
	//destroy
	//undefine (sem delete ao qcow2)

	return nil, nil
}

// cria uma maquina com passthrough, com x nome usando o disco qcow2file
func MigrateColdWin(coldFile ColdMigrationInfo) error {

	if coldFile.CpuXML == "" || !coldFile.Live {
		coldFile.CpuXML = "<cpu mode='host-passthrough'/>"
	}

	//check coldFile info is valid
	if strings.TrimSpace(coldFile.VmName) == "" {
		return fmt.Errorf("vm name is required")
	}
	if strings.TrimSpace(coldFile.DiskPath) == "" {
		return fmt.Errorf("disk path is required")
	}
	if coldFile.Memory <= 0 {
		return fmt.Errorf("memory must be positive")
	}
	if coldFile.VCpus <= 0 {
		return fmt.Errorf("vcpus must be positive")
	}
	if strings.TrimSpace(coldFile.Network) == "" {
		return fmt.Errorf("network is required")
	}

	//check if diskPath qcow2 file exists
	if _, err := os.Stat(coldFile.DiskPath); os.IsNotExist(err) {
		return fmt.Errorf("disk path qcow2 file does not exist: %w", err)
	}

	err := validateCPUXML(coldFile.CpuXML)
	if err != nil {
		return fmt.Errorf("invalid CPU XML: %w", err)
	}

	//get folder of diskPath qcow file
	parentDir := strings.TrimSpace(filepath.Dir(coldFile.DiskPath))

	params := CreateVMCustomCPUOptions{
		ConnURI:           "qemu:///system",
		Name:              coldFile.VmName,
		MemoryMB:          int(coldFile.Memory),
		VCPUs:             int(coldFile.VCpus),
		DiskAlreadyExists: true,
		DiskFolder:        parentDir,
		DiskPath:          coldFile.DiskPath,
		DiskSizeGB:        0,
		ISOPath:           "",
		Network:           coldFile.Network,
		GraphicsListen:    "0.0.0.0",
		VNCPassword:       coldFile.VNCPassword,
		CPUXml:            coldFile.CpuXML,
	}

	_, err = CreateVMCustomCPU(params)
	if err != nil {
		return err
	}

	return nil
}
