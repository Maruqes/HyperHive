package virsh

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slave/extra"
	"strconv"
	"strings"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
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

	baseArgs := []string{
		"-c", connURI,
		"migrate",
		"--persistent",
		"--verbose",
		"--undefinesource",
		"--p2p",
		"--tunnelled",
		"--timeout", strconv.Itoa(int(opts.Timeout)),
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

// returns qcow2file path, cpuXML
func MigrateColdLose(name string) (string, string, error) {
	return "", "", nil
}

// cria uma maquina com passthrough, com x nome usando o disco qcow2file
func MigrateColdWin(name string, qcow2file string, cpuXML string) error {
	return nil
}
