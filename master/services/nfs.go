package services

import (
	"512SvMan/db"
	"512SvMan/nfs"
	"512SvMan/nots"
	"512SvMan/protocol"
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	proto "github.com/Maruqes/512SvMan/api/proto/nfs"
	"github.com/Maruqes/512SvMan/logger"
)

func getFolderName(path string) string {
	path = strings.TrimSuffix(path, "/")

	//split by /
	parts := []rune(path)
	name := ""
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == '/' {
			break
		}
		name = string(parts[i]) + name
	}
	return name
}

func ConvertNSFShareToGRPCFolderMount(share []db.NFSShare) *proto.FolderMountList {
	folderMounts := &proto.FolderMountList{
		Mounts: make([]*proto.FolderMount, 0, len(share)),
	}
	for _, s := range share {
		folderMounts.Mounts = append(folderMounts.Mounts, &proto.FolderMount{
			MachineName:     s.MachineName,
			FolderPath:      s.FolderPath,
			Source:          s.Source,
			Target:          s.Target,
			HostNormalMount: s.HostNormalMount,
		})
	}
	return folderMounts
}

type SharePoint struct {
	MachineName     string `json:"machine_name"` //this machine want to share
	FolderPath      string `json:"folder_path"`  //this folder
	Name            string `json:"name"`         //optional friendly name for the share
	HostNormalMount bool   `json:"host_normal_mount"`
}

type NFSService struct {
	SharePoint SharePoint
}

const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

var max = big.NewInt(int64(len(alphabet)))

func ShortID(n int) (string, error) {
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		x, err := rand.Int(rand.Reader, max) // sem viés
		if err != nil {
			return "", err
		}
		b.WriteByte(alphabet[x.Int64()])
	}
	return b.String(), nil
}

func (s *NFSService) CreateSharePoint(ctx context.Context) error {
	//find connection by machine name
	conn := protocol.GetConnectionByMachineName(s.SharePoint.MachineName)
	if conn == nil || conn.Connection == nil {
		return fmt.Errorf("slave not connected")
	}

	// Check if connection is healthy before proceeding
	if !protocol.IsConnectionHealthy(conn.Connection) {
		return fmt.Errorf("slave connection is not healthy")
	}

	//make sure name exists and sanitize it (only letters and numbers), and add it to folder_path
	if s.SharePoint.Name != "" {
		sanitizedName := ""
		for _, r := range s.SharePoint.Name {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				sanitizedName += string(r)
			}
		}
		if sanitizedName != "" {
			shortId, err := ShortID(6)
			if err != nil {
				return err
			}
			if s.SharePoint.FolderPath[len(s.SharePoint.FolderPath)-1] != '/' {
				s.SharePoint.FolderPath = s.SharePoint.FolderPath + "/" + sanitizedName + shortId
			} else {
				s.SharePoint.FolderPath = s.SharePoint.FolderPath + sanitizedName + shortId
			}
		}
	}

	//block certain linux important paths like /root or /boot and others
	blockedPaths := []string{"/root", "/boot", "/etc", "/bin", "/sbin", "/usr", "/lib", "/lib64", "/sys", "/proc", "/dev"}
	for _, blocked := range blockedPaths {
		if strings.HasPrefix(s.SharePoint.FolderPath, blocked) {
			return fmt.Errorf("cannot share system path: %s", blocked)
		}
	}

	mount := &proto.FolderMount{
		MachineName:     s.SharePoint.MachineName,                  // machine that shares
		FolderPath:      s.SharePoint.FolderPath,                   // folder to share
		Source:          conn.Addr + ":" + s.SharePoint.FolderPath, // creates ip:folderpath
		Target:          "/mnt/512SvMan/shared/" + s.SharePoint.MachineName + "_" + getFolderName(s.SharePoint.FolderPath),
		HostNormalMount: s.SharePoint.HostNormalMount,
	}

	if err := nfs.CreateSharedFolder(conn.Connection, mount); err != nil {
		logger.Errorf("CreateSharedFolder failed: %v", err)
		return err
	}

	err := db.AddNFSShare(ctx, mount.MachineName, mount.FolderPath, mount.Source, mount.Target, s.SharePoint.Name, mount.HostNormalMount)
	if err != nil {
		logger.Errorf("AddNFSShare failed: %v", err)
		return err
	}

	err = s.SyncSharedFolder(ctx)
	if err != nil {
		logger.Errorf("SyncSharedFolder failed: %v", err)
		return err
	}

	err = s.MountAllSharedFolders()
	if err != nil {
		logger.Errorf("MountAllSharedFolders failed: %v", err)
		return err
	}
	return nil
}

func forcedelete(ctx context.Context, s *NFSService) error {
	//check if exists in db
	if exists, err := db.DoesExistNFSShare(ctx, s.SharePoint.MachineName, s.SharePoint.FolderPath); err != nil {
		return fmt.Errorf("failed to check if NFS share exists: %v", err)
	} else if !exists {
		return fmt.Errorf("NFS share does not exist")
	}

	nfsShare, err := db.GetNFSShareByMachineAndFolder(ctx, s.SharePoint.MachineName, s.SharePoint.FolderPath)
	if err != nil {
		return fmt.Errorf("failed to resolve NFS share: %v", err)
	}
	if nfsShare == nil {
		return fmt.Errorf("cannot resolve NFS share for deletion")
	}

	mount := &proto.FolderMount{
		MachineName:     nfsShare.MachineName,
		FolderPath:      nfsShare.FolderPath,
		Source:          nfsShare.Source,
		Target:          nfsShare.Target,
		HostNormalMount: nfsShare.HostNormalMount,
	}

	virshService := VirshService{}
	vms, err := virshService.GetAllVmsByOnNfsShare(ctx, mount.Target)
	if err != nil {
		return fmt.Errorf("failed to get VMs on NFS share: %v", err)
	}
	for _, vm := range vms {
		if err := virshService.DeleteVM(ctx, vm.Name); err != nil {
			return fmt.Errorf("failed to delete VM %s on NFS share: %v", vm.Name, err)
		}
	}

	backups, err := db.GetVirshBackupsByNfsMountID(ctx, nfsShare.Id)
	if err != nil {
		return fmt.Errorf("failed to query backups using NFS share: %v", err)
	}
	for _, bak := range backups {
		if err := virshService.DeleteBackup(ctx, bak.Id); err != nil {
			return fmt.Errorf("failed to delete backup %d for VM %s: %v", bak.Id, bak.Name, err)
		}
	}

	autoBackups, err := db.GetAutomaticBackupsByNfsMountID(ctx, nfsShare.Id)
	if err != nil {
		return fmt.Errorf("failed to query automatic backups using NFS share: %v", err)
	}
	for _, bak := range autoBackups {
		if err := virshService.DeleteAutoBak(ctx, bak.Id); err != nil {
			return fmt.Errorf("failed to remove automatic backup %d for VM %s: %v", bak.Id, bak.VmName, err)
		}
	}

	isos, err := db.GetAllISOs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ISOs: %v", err)
	}
	for _, iso := range isos {
		if strings.HasPrefix(iso.FilePath, mount.Target) {
			if err := os.Remove(iso.FilePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove ISO file %s: %v", iso.FilePath, err)
			}
			if err := db.RemoveISOByID(ctx, iso.Id); err != nil {
				return fmt.Errorf("failed to remove ISO %s on NFS share: %v", iso.Name, err)
			}
		}
	}

	conns := protocol.GetAllGRPCConnections()
	// unmount on all provided connections
	for _, c := range conns {
		if c == nil {
			continue
		}
		if err := nfs.UnmountSharedFolder(c, mount); err != nil {
			logger.Errorf("UnmountSharedFolder failed: %v", err)
		}
	}

	if conn := protocol.GetConnectionByMachineName(mount.MachineName); conn != nil && conn.Connection != nil {
		if err := nfs.RemoveSharedFolder(conn.Connection, mount); err != nil {
			logger.Errorf("RemoveSharedFolder failed: %v", err)
		}
	}

	if err := db.RemoveNFSShare(ctx, mount.MachineName, mount.FolderPath); err != nil {
		return fmt.Errorf("failed to remove NFS share from database: %v", err)
	}

	return nil
}

func (s *NFSService) DeleteSharePoint(ctx context.Context, force bool) error {

	if force {
		return forcedelete(ctx, s)
	}

	conn := protocol.GetConnectionByMachineName(s.SharePoint.MachineName)
	if conn == nil || conn.Connection == nil {
		return fmt.Errorf("slave not connected")
	}

	//check if exists in db
	if exists, err := db.DoesExistNFSShare(ctx, s.SharePoint.MachineName, s.SharePoint.FolderPath); err != nil {
		return fmt.Errorf("failed to check if NFS share exists: %v", err)
	} else if !exists {
		return fmt.Errorf("NFS share does not exist")
	}

	//remove last slash
	mount := &proto.FolderMount{
		MachineName:     s.SharePoint.MachineName,
		FolderPath:      s.SharePoint.FolderPath,
		Source:          conn.Addr + ":" + s.SharePoint.FolderPath,
		Target:          "/mnt/512SvMan/shared/" + s.SharePoint.MachineName + "_" + getFolderName(s.SharePoint.FolderPath),
		HostNormalMount: s.SharePoint.HostNormalMount,
	}

	//get vms and isos and delete them first
	virshService := VirshService{}
	vms, err := virshService.GetAllVmsByOnNfsShare(ctx, mount.Target)
	if err != nil {
		return fmt.Errorf("failed to get VMs on NFS share: %v", err)
	}
	if len(vms) > 0 {
		vmsNames := []string{}
		for _, vm := range vms {
			vmsNames = append(vmsNames, vm.Name)
		}
		return fmt.Errorf("cannot delete NFS share, there are VMs using it: %v", vmsNames)
	}

	//check if any iso is on this nfs share
	isos, err := db.GetAllISOs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ISOs: %v", err)
	}
	for _, iso := range isos {
		if strings.HasPrefix(iso.FilePath, mount.Target) {
			return fmt.Errorf("cannot delete NFS share, there are ISOs using it: %v", iso.Name)
		}
	}

	nfsShare, err := db.GetNFSShareByMachineAndFolder(ctx, mount.MachineName, mount.FolderPath)
	if err != nil {
		return fmt.Errorf("failed to resolve NFS share: %v", err)
	}
	if nfsShare == nil {
		return fmt.Errorf("cannot resolve NFS share for deletion")
	}

	backups, err := db.GetVirshBackupsByNfsMountID(ctx, nfsShare.Id)
	if err != nil {
		return fmt.Errorf("failed to query backups using NFS share: %v", err)
	}
	if len(backups) > 0 {
		backupNames := make([]string, 0, len(backups))
		for _, bak := range backups {
			backupNames = append(backupNames, bak.Name)
		}
		return fmt.Errorf("cannot delete NFS share, there are backups using it: %v", backupNames)
	}

	autoBackups, err := db.GetAutomaticBackupsByNfsMountID(ctx, nfsShare.Id)
	if err != nil {
		return fmt.Errorf("failed to query automatic backups using NFS share: %v", err)
	}
	if len(autoBackups) > 0 {
		vmNames := make([]string, 0, len(autoBackups))
		for _, bak := range autoBackups {
			vmNames = append(vmNames, bak.VmName)
		}
		return fmt.Errorf("cannot delete NFS share, there are automatic backups using it: %v", vmNames)
	}

	if err := nfs.RemoveSharedFolder(conn.Connection, mount); err != nil {
		return fmt.Errorf("failed to remove shared folder: %v", err)
	}

	conns := protocol.GetAllGRPCConnections()
	// unmount on all provided connections
	for _, c := range conns {
		if c == nil {
			continue
		}
		if err := nfs.UnmountSharedFolder(c, mount); err != nil {
			logger.Errorf("UnmountSharedFolder failed: %v", err)
			continue
		}
	}

	err = db.RemoveNFSShare(ctx, mount.MachineName, mount.FolderPath)
	if err != nil {
		return fmt.Errorf("failed to remove NFS share from database: %v", err)
	}

	return nil
}

func (s *NFSService) GetSharedFolderStatus(folderMount *proto.FolderMount) (*proto.SharedFolderStatusResponse, error) {
	conn := protocol.GetConnectionByMachineName(folderMount.MachineName)
	if conn == nil || conn.Connection == nil {
		return nil, fmt.Errorf("%s", "slave not connected :"+folderMount.MachineName)
	}

	status, err := nfs.GetSharedFolderStatus(conn.Connection, folderMount)
	if err != nil {
		return nil, fmt.Errorf("failed to get shared folder status: %v", err)
	}
	//ask every connection if folder is mounted
	cons := protocol.GetAllGRPCConnections()
	for _, c := range cons {
		if c == nil {
			continue
		}
		statusLoop, err := nfs.GetSharedFolderStatus(c, folderMount)
		if err != nil {
			logger.Errorf("IsFolderMounted failed: %v", err)
			continue
		}
		status.Working = status.Working && statusLoop.Working
	}

	return status, nil
}

func (s *NFSService) GetAllSharedFolders() ([]db.NFSShare, error) {
	return nfs.GetAllSharedFolders()
}

// get all shared folders for each slave and make sure they are shared
func (s *NFSService) SyncSharedFolder(ctx context.Context) error {
	slavesShared, err := db.GetAllMachineNamesWithShares(ctx)
	if err != nil {
		return fmt.Errorf("failed to get all machine names with shares: %v", err)
	}

	notConnected := []string{}
	syncErrors := []string{}
	for _, machineName := range slavesShared {
		conn := protocol.GetConnectionByMachineName(machineName)
		if conn == nil || conn.Connection == nil {
			logger.Warn("slave not connected:", machineName)
			notConnected = append(notConnected, machineName)
			continue
		}

		// Check if connection is healthy before attempting sync
		if !protocol.IsConnectionHealthy(conn.Connection) {
			logger.Warnf("slave %s connection is unhealthy, skipping sync", machineName)
			notConnected = append(notConnected, machineName+" (unhealthy connection)")
			continue
		}

		shares, err := db.GetNFSharesByMachineName(ctx, machineName)
		if err != nil {
			logger.Error("failed to get NFS shares by machine name:", err)
			continue
		}

		if err := nfs.SyncSharedFolder(conn.Connection, ConvertNSFShareToGRPCFolderMount(shares)); err != nil {
			errMsg := fmt.Sprintf("sync shared folder failed on %s: %v", machineName, err)
			logger.Error(errMsg)
			syncErrors = append(syncErrors, errMsg)
		}
	}

	errParts := make([]string, 0, 2)
	if len(notConnected) > 0 {
		errParts = append(errParts, fmt.Sprintf("some slaves not connected: %v", notConnected))
	}
	if len(syncErrors) > 0 {
		errParts = append(errParts, strings.Join(syncErrors, "; "))
	}
	if len(errParts) > 0 {
		return fmt.Errorf("%s", strings.Join(errParts, " | "))
	}

	return nil
}

func (s *NFSService) MountAllSharedFolders(folders ...db.NFSShare) error {
	snapshot := protocol.GetConnectionsSnapshot()

	connected := make([]protocol.ConnectionsStruct, 0, len(snapshot))
	connectedByMachine := make(map[string]protocol.ConnectionsStruct, len(snapshot))
	for _, entry := range snapshot {
		if entry.Connection == nil {
			continue
		}
		// Validate connection is actually healthy before adding to connected list
		if !protocol.IsConnectionHealthy(entry.Connection) {
			logger.Warnf("Skipping unhealthy connection for machine %s", entry.MachineName)
			continue
		}
		connected = append(connected, entry)
		if _, exists := connectedByMachine[entry.MachineName]; !exists {
			connectedByMachine[entry.MachineName] = entry
		}
	}

	if len(connected) == 0 {
		return fmt.Errorf("no connected slaves")
	}

	var (
		serversNFS []db.NFSShare
		err        error
	)

	if len(folders) == 0 {
		serversNFS, err = nfs.GetAllSharedFolders()
		if err != nil {
			return err
		}
	} else {
		serversNFS = folders
	}

	mountErrors := make([]string, 0)
	mountable := make([]*proto.FolderMount, 0, len(serversNFS))

	logger.Info("Creating NFS shared folders on all slaves...")
	// Create shared folder only on the source machine for each share.
	for _, svNSF := range serversNFS {
		mount := &proto.FolderMount{
			FolderPath:      svNSF.FolderPath,
			Source:          svNSF.Source,
			Target:          svNSF.Target,
			MachineName:     svNSF.MachineName,
			HostNormalMount: svNSF.HostNormalMount,
		}
		sourceConn, ok := connectedByMachine[svNSF.MachineName]
		if !ok {
			errMsg := fmt.Sprintf("source machine %s for target %s is not connected", svNSF.MachineName, mount.Target)
			logger.Warn(errMsg)
			mountErrors = append(mountErrors, errMsg)
			continue
		}

		// Double-check connection is still healthy before attempting operation
		if !protocol.IsConnectionHealthy(sourceConn.Connection) {
			errMsg := fmt.Sprintf("connection to %s became unhealthy, skipping CreateSharedFolder for %s", sourceConn.MachineName, mount.Target)
			logger.Warn(errMsg)
			mountErrors = append(mountErrors, errMsg)
			continue
		}

		logger.Info("Creating NFS shared folder on machine:", sourceConn.MachineName, " with mount:", mount)
		if err := nfs.CreateSharedFolder(sourceConn.Connection, mount); err != nil {
			errMsg := fmt.Sprintf("CreateSharedFolder on %s target %s failed: %v", sourceConn.MachineName, mount.Target, err)
			logger.Error(errMsg)
			mountErrors = append(mountErrors, errMsg)
			continue
		}
		mountable = append(mountable, mount)
	}

	if len(mountable) == 0 {
		if len(mountErrors) == 0 {
			return nil
		}
		return fmt.Errorf("no mountable NFS shares: %s", strings.Join(mountErrors, "; "))
	}

	logger.Info("Mounting NFS shared folders on all slaves...")
	// Mount every available share on every connected slave.
	for _, conn := range connected {
		// Check connection health before attempting mount operations
		if !protocol.IsConnectionHealthy(conn.Connection) {
			logger.Warnf("Skipping mount operations for %s - connection became unhealthy", conn.MachineName)
			continue
		}

		for _, mount := range mountable {
			logger.Info("Mounting NFS shared folder on machine with mount:", mount)
			if err := nfs.MountSharedFolder(conn.Connection, mount); err != nil {
				errMsg := fmt.Sprintf("MountSharedFolder on %s target %s failed: %v", conn.MachineName, mount.Target, err)
				logger.Error(errMsg)
				mountErrors = append(mountErrors, errMsg)
				continue
			}
		}
	}
	if len(mountErrors) > 0 {
		return fmt.Errorf("one or more NFS mounts failed: %s", strings.Join(mountErrors, "; "))
	}
	return nil
}

func (s *NFSService) UpdateNFSShit(ctx context.Context) error {
	errParts := make([]string, 0, 2)

	err := s.SyncSharedFolder(ctx)
	if err != nil {
		logger.Errorf("SyncSharedFolder failed: %v", err)
		errParts = append(errParts, fmt.Sprintf("sync shared folders: %v", err))
	}

	err = s.MountAllSharedFolders()
	if err != nil {
		logger.Errorf("MountAllSharedFolders failed: %v", err)
		errParts = append(errParts, fmt.Sprintf("mount shared folders: %v", err))
	}

	if len(errParts) > 0 {
		return fmt.Errorf("%s", strings.Join(errParts, " | "))
	}

	return nil
}

func (s *NFSService) ensureNFSWorkingOnMachine(ctx context.Context, machineName string) error {
	const nfsMountPrefix = "/mnt/512SvMan/shared/"

	machineName = strings.TrimSpace(machineName)
	if machineName == "" {
		return fmt.Errorf("machine name is required")
	}

	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return fmt.Errorf("slave %s not connected", machineName)
	}

	if _, err := protocol.EnsureConnectionReady(conn.Connection, 15*time.Second); err != nil {
		return fmt.Errorf("connection to %s is not ready: %w", machineName, err)
	}

	shares, err := db.GetAllNFShares(ctx)
	if err != nil {
		return fmt.Errorf("failed to load nfs shares: %w", err)
	}
	if len(shares) == 0 {
		return nil
	}

	issues := make([]string, 0)
	for _, share := range shares {
		if err := ctx.Err(); err != nil {
			return err
		}

		mount := &proto.FolderMount{
			MachineName:     share.MachineName,
			FolderPath:      share.FolderPath,
			Source:          share.Source,
			Target:          share.Target,
			HostNormalMount: share.HostNormalMount,
		}

		status, err := nfs.GetSharedFolderStatus(conn.Connection, mount)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s status check failed: %v", share.Target, err))
			continue
		}
		if status == nil || !status.Working {
			issues = append(issues, fmt.Sprintf("%s is not mounted/working", share.Target))
			continue
		}
		if err := nfs.CheckReadWrite(conn.Connection, share.Target); err != nil {
			issues = append(issues, fmt.Sprintf("%s read/write check failed: %v", share.Target, err))
		}
	}

	// Extra check for autostart VM disks on this machine: the actual disk file must be readable.
	autoStart, err := db.GetAllAutoStart(ctx)
	if err != nil {
		return fmt.Errorf("failed to load autostart entries: %w", err)
	}

	virshService := VirshService{}
	for _, auto := range autoStart {
		if err := ctx.Err(); err != nil {
			return err
		}

		vm, err := virshService.GetVmByName(auto.VmName)
		if err != nil || vm == nil {
			// stale autostart entries are handled in VM service, don't block NFS readiness here
			continue
		}
		if vm.MachineName != machineName {
			continue
		}

		diskPath := strings.TrimSpace(vm.DiskPath)
		if diskPath == "" || !strings.HasPrefix(diskPath, nfsMountPrefix) {
			continue
		}

		if err := nfs.CheckFileReadable(conn.Connection, diskPath); err != nil {
			issues = append(issues, fmt.Sprintf("vm %s disk unreadable (%s): %v", vm.Name, diskPath, err))
			continue
		}

		parentDir := filepath.Dir(diskPath)
		if err := nfs.CheckReadWrite(conn.Connection, parentDir); err != nil {
			issues = append(issues, fmt.Sprintf("vm %s disk dir not read/write (%s): %v", vm.Name, parentDir, err))
		}
	}

	if len(issues) > 0 {
		return fmt.Errorf("nfs is not fully working on %s: %s", machineName, strings.Join(issues, "; "))
	}

	return nil
}

func (s *NFSService) EnsureNFSReadyForMachine(ctx context.Context, machineName string, retryDelay time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if retryDelay <= 0 {
		retryDelay = 10 * time.Second
	}

	machineName = strings.TrimSpace(machineName)
	if machineName == "" {
		return fmt.Errorf("machine name is required")
	}

	attempt := 0
	var lastErr error

	for {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("nfs readiness check stopped for %s after %d attempts: %w (last error: %v)", machineName, attempt, err, lastErr)
			}
			return fmt.Errorf("nfs readiness check stopped for %s: %w", machineName, err)
		}

		attempt++
		stepErrs := make([]string, 0, 2)

		if err := s.UpdateNFSShit(ctx); err != nil {
			stepErrs = append(stepErrs, err.Error())
		}
		if err := s.ensureNFSWorkingOnMachine(ctx, machineName); err != nil {
			stepErrs = append(stepErrs, err.Error())
		}

		if len(stepErrs) == 0 {
			if attempt > 1 {
				logger.Infof("NFS became ready on %s after %d attempts", machineName, attempt)
			}
			return nil
		}

		lastErr = fmt.Errorf("%s", strings.Join(stepErrs, " | "))
		logger.Warnf("NFS not ready on %s (attempt %d): %v", machineName, attempt, lastErr)

		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			if lastErr != nil {
				return fmt.Errorf("nfs readiness check stopped for %s after %d attempts: %w (last error: %v)", machineName, attempt, ctx.Err(), lastErr)
			}
			return fmt.Errorf("nfs readiness check stopped for %s: %w", machineName, ctx.Err())
		case <-timer.C:
		}
	}
}

func (s *NFSService) DownloadISO(ctx context.Context, url, isoName string, nfsShare db.NFSShare) (string, error) {
	conn := protocol.GetConnectionByMachineName(nfsShare.MachineName)
	if conn == nil || conn.Connection == nil {
		return "", fmt.Errorf("slave not connected")
	}
	if nfsShare.Target[len(nfsShare.Target)-1] == '/' {
		nfsShare.Target = nfsShare.Target[:len(nfsShare.Target)-1]
	}
	isoPath := nfsShare.Target + "/" + isoName

	isoRequest := &proto.DownloadIsoRequest{
		IsoUrl:  url,
		IsoName: isoName,
		FolderMount: &proto.FolderMount{
			MachineName:     nfsShare.MachineName,
			FolderPath:      nfsShare.FolderPath,
			Source:          nfsShare.Source,
			Target:          nfsShare.Target,
			HostNormalMount: nfsShare.HostNormalMount,
		},
	}

	if err := nfs.DownloadISO(conn.Connection, ctx, isoRequest); err != nil {
		logger.Errorf("DownloadISO failed: %v", err)
		return "", err
	}
	err := nfs.Sync(conn.Connection)
	if err != nil {
		return "", err
	}
	return isoPath, nil
}

func (s *NFSService) DownloadISOAsync(url, isoName string, nfsShare db.NFSShare) error {
	conn := protocol.GetConnectionByMachineName(nfsShare.MachineName)
	if conn == nil || conn.Connection == nil {
		return fmt.Errorf("slave not connected")
	}

	target := strings.TrimSuffix(nfsShare.Target, "/")
	isoPath := target + "/" + isoName

	isoRequest := &proto.DownloadIsoRequest{
		IsoUrl:  url,
		IsoName: isoName,
		FolderMount: &proto.FolderMount{
			MachineName:     nfsShare.MachineName,
			FolderPath:      nfsShare.FolderPath,
			Source:          nfsShare.Source,
			Target:          target,
			HostNormalMount: nfsShare.HostNormalMount,
		},
	}

	go func() {
		taskCtx := context.Background()
		if err := nfs.DownloadISO(conn.Connection, taskCtx, isoRequest); err != nil {
			nots.SendGlobalNotification("ISO download failed", "ISO download failed for "+isoName+" on "+nfsShare.MachineName, err.Error(), true)
			return
		}
		if err := nfs.Sync(conn.Connection); err != nil {
			nots.SendGlobalNotification("ISO sync failed", "ISO sync failed for "+isoName+" on "+nfsShare.MachineName, err.Error(), true)
			return
		}
		if err := db.AddISO(taskCtx, nfsShare.MachineName, isoPath, isoName); err != nil {
			nots.SendGlobalNotification("ISO register failed", "ISO download finished but failed to save "+isoName+" on "+nfsShare.MachineName, err.Error(), true)
			return
		}
		nots.SendGlobalNotification("ISO download done", "ISO "+isoName+" downloaded on "+nfsShare.MachineName, "/", true)
	}()

	return nil
}

func (s *NFSService) ListFolderContents(machineName string, path string) (*proto.FolderContents, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return nil, fmt.Errorf("slave not connected")
	}

	contents, err := nfs.ListFolderContents(conn.Connection, path)
	if err != nil {
		return nil, fmt.Errorf("failed to list folder contents: %v", err)
	}
	return contents, nil
}

func (s *NFSService) CanFindFileOrDir(machineName, path string) (bool, error) {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return false, fmt.Errorf("slave not connected")
	}

	found, err := nfs.CanFindFileOrDir(conn.Connection, path)
	if err != nil {
		return false, fmt.Errorf("failed to find file or dir: %v", err)
	}
	return found, nil
}

func (s *NFSService) CanFindFileOrDirOnAllSlaves(path string) (map[string]bool, error) {
	results := make(map[string]bool)
	snapshot := protocol.GetConnectionsSnapshot()

	for _, conn := range snapshot {
		machineName := conn.MachineName
		if conn.Connection == nil {
			results[machineName] = false
			continue
		}

		found, err := nfs.CanFindFileOrDir(conn.Connection, path)
		if err != nil {
			logger.Errorf("CanFindFileOrDir failed for machine %s: %v", machineName, err)
			results[machineName] = false
			continue
		}
		results[machineName] = found
	}

	return results, nil
}

// uses "sync" command so nfs fushes all dirty tables
func (s *NFSService) SyncNFSAllSlaves(machineNames ...string) error {
	var errRet string

	if len(machineNames) == 0 {
		conns := protocol.GetAllGRPCConnections()
		for i, con := range conns {
			if con == nil {
				continue
			}
			if err := nfs.Sync(con); err != nil {
				errRet += fmt.Sprintf("err %d: %s\n", i, err.Error())
			}
		}
	} else {
		for _, machineName := range machineNames {
			conn := protocol.GetConnectionByMachineName(machineName)
			if conn == nil || conn.Connection == nil {
				errRet += fmt.Sprintf("machine %s: connection unavailable\n", machineName)
				continue
			}
			if err := nfs.Sync(conn.Connection); err != nil {
				errRet += fmt.Sprintf("machine %s: %s\n", machineName, err.Error())
			}
		}
	}

	if errRet != "" {
		return fmt.Errorf("%s", strings.TrimSpace(errRet))
	}
	return nil
}

func (s *NFSService) SyncNFSSlavesByMachineName(machineName string) error {
	conn := protocol.GetConnectionByMachineName(machineName)
	if conn == nil || conn.Connection == nil {
		return fmt.Errorf("machineName doe snot exist or conn is nil")
	}

	err := nfs.Sync(conn.Connection)
	if err != nil {
		return err
	}
	return nil
}

func (s *NFSService) MaintainNFS() {

	for {
		time.Sleep(2 * time.Minute)

		folders, err := s.GetAllSharedFolders()
		if err != nil {
			logger.Error(err.Error())
			continue
		}

		for _, folder := range folders {
			conn := protocol.GetConnectionByMachineName(folder.MachineName)
			if conn == nil || conn.Connection == nil {
				logger.Warn("cannot maintain NFS share, slave not connected:", folder.MachineName)
				continue
			}

			status, err := s.GetSharedFolderStatus(&proto.FolderMount{
				MachineName:     folder.MachineName,
				FolderPath:      folder.FolderPath,
				Source:          folder.Source,
				Target:          folder.Target,
				HostNormalMount: folder.HostNormalMount,
			})
			if err != nil {
				logger.Error(err.Error())
				continue
			}

			if status.Working {
				continue
			}

			if err := s.SyncNFSAllSlaves(folder.MachineName); err != nil {
				logger.Errorf("SyncNFSAllSlaves failed while maintaining NFS for %s: %v", folder.MachineName, err)
			}

			if err := s.MountAllSharedFolders(folder); err != nil {
				logger.Errorf("MountAllSharedFolders failed while maintaining NFS for %s: %v", folder.MachineName, err)
			}
		}
	}
}

func (s *NFSService) RemountShareByID(ctx context.Context, id int) error {
	share, err := db.GetNFSShareByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get NFS share by id %d: %w", id, err)
	}
	if share == nil {
		return fmt.Errorf("nfs share with id %d not found", id)
	}

	conn := protocol.GetConnectionByMachineName(share.MachineName)
	if conn == nil || conn.Connection == nil {
		return fmt.Errorf("cannot remount NFS share, slave %s not connected", share.MachineName)
	}

	if err := s.SyncNFSAllSlaves(share.MachineName); err != nil {
		return fmt.Errorf("failed to sync NFS on %s: %w", share.MachineName, err)
	}

	if err := s.MountAllSharedFolders(*share); err != nil {
		return fmt.Errorf("failed to remount share %d: %w", id, err)
	}

	return nil
}
