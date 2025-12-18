package virsh

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slave/extra"
	"strings"
	"time"

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

	if len(sshOpts) > 0 {
		_ = os.Setenv("LIBVIRT_SSH_OPTS", strings.Join(sshOpts, " "))
	}

	logger.Info("Starting libvirt migration (API) from %s to %s for %s", connURI, destURI, name)

	// notify migration start (websocket + push)
	extra.SendWebsocketMessage("migration started", name, extraGrpc.WebSocketsMessageType_MigrateVm)
	// notify migration start
	extra.SendNotifications("VM migration started", fmt.Sprintf("Starting migration of %s to %s", name, destURI), "/", false)

	// best-effort live progress using libvirt job info while virsh runs
	progressCtx, cancelProgress := context.WithCancel(ctx)
	defer cancelProgress()
	doneCmd := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		identifier := fmt.Sprintf("%s-%d", name, time.Now().Unix())
		for {
			select {
			case <-progressCtx.Done():
				return
			case <-doneCmd:
				return
			case <-ticker.C:
				info, infoErr := dom.GetJobInfo()
				if infoErr != nil {
					logger.Error("migration progress (GetJobInfo): %v", infoErr)
					return
				}

				pct := int64(-1)
				if info.MemTotal > 0 {
					pct = int64(info.MemProcessed * 100 / info.MemTotal)
				} else if info.DataTotal > 0 {
					pct = int64(info.DataProcessed * 100 / info.DataTotal)
				}

				if pct >= 0 {
					if pct > 100 {
						pct = 100
					}
					msg := fmt.Sprintf("migration progress: %d%%", pct)
					if err := extra.SendWebsocketMessage(msg, identifier, extraGrpc.WebSocketsMessageType_MigrateVm); err != nil {
						logger.Error("SendWebsocketMessage progress: %v", err)
					}
				}
			}
		}
	}()
	flags := libvirt.MIGRATE_PERSIST_DEST | libvirt.MIGRATE_UNDEFINE_SOURCE | libvirt.MIGRATE_PEER2PEER | libvirt.MIGRATE_TUNNELLED | libvirt.MIGRATE_AUTO_CONVERGE | libvirt.MIGRATE_ABORT_ON_ERROR
	if opts.Live {
		flags |= libvirt.MIGRATE_LIVE
	}

	// run migrate respecting context; abort job on cancellation
	migrateErrCh := make(chan error, 1)
	go func() {
		migrateErrCh <- dom.MigrateToURI2(destURI, "", "", flags, "", 0)
	}()

	var errRun error
	select {
	case <-ctx.Done():
		logger.Error("migration context canceled, aborting job")
		if abortErr := dom.AbortJob(); abortErr != nil {
			logger.Error("AbortJob failed: %v", abortErr)
		}
		errRun = ctx.Err()
	case errRun = <-migrateErrCh:
	}
	close(doneCmd)
	if errRun != nil {
		errMsg := errRun.Error()
		extra.SendNotifications("VM migration failed", fmt.Sprintf("Migration of %s to %s failed: %s", name, destURI, errMsg), "/", true)
		return fmt.Errorf("virsh migrate: %s", errMsg)
	}

	// success
	extra.SendWebsocketMessage("migration finished", name, extraGrpc.WebSocketsMessageType_MigrateVm)
	extra.SendNotifications("VM migration succeeded", fmt.Sprintf("Migration of %s to %s completed successfully", name, destURI), "/", false)

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
