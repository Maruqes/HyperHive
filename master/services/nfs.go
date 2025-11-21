package services

import (
	"512SvMan/db"
	"512SvMan/nfs"
	"512SvMan/protocol"
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"strings"

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

func (s *NFSService) CreateSharePoint() error {
	//find connection by machine name
	conn := protocol.GetConnectionByMachineName(s.SharePoint.MachineName)
	if conn == nil || conn.Connection == nil {
		return fmt.Errorf("slave not connected")
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
		logger.Error("CreateSharedFolder failed: %v", err)
		return err
	}

	err := db.AddNFSShare(mount.MachineName, mount.FolderPath, mount.Source, mount.Target, s.SharePoint.Name, mount.HostNormalMount)
	if err != nil {
		logger.Error("AddNFSShare failed: %v", err)
		return err
	}

	err = s.SyncSharedFolder()
	if err != nil {
		logger.Error("SyncSharedFolder failed: %v", err)
		return err
	}

	err = s.MountAllSharedFolders()
	if err != nil {
		logger.Error("MountAllSharedFolders failed: %v", err)
		return err
	}
	return nil
}

func forcedelete(s *NFSService) error {
	//check if exists in db
	if exists, err := db.DoesExistNFSShare(s.SharePoint.MachineName, s.SharePoint.FolderPath); err != nil {
		return fmt.Errorf("failed to check if NFS share exists: %v", err)
	} else if !exists {
		return fmt.Errorf("NFS share does not exist")
	}

	nfsShare, err := db.GetNFSShareByMachineAndFolder(s.SharePoint.MachineName, s.SharePoint.FolderPath)
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
	vms, err := virshService.GetAllVmsByOnNfsShare(mount.Target)
	if err != nil {
		return fmt.Errorf("failed to get VMs on NFS share: %v", err)
	}
	for _, vm := range vms {
		if err := virshService.DeleteVM(vm.Name); err != nil {
			return fmt.Errorf("failed to delete VM %s on NFS share: %v", vm.Name, err)
		}
	}

	backups, err := db.GetVirshBackupsByNfsMountID(nfsShare.Id)
	if err != nil {
		return fmt.Errorf("failed to query backups using NFS share: %v", err)
	}
	for _, bak := range backups {
		if err := virshService.DeleteBackup(bak.Id); err != nil {
			return fmt.Errorf("failed to delete backup %d for VM %s: %v", bak.Id, bak.Name, err)
		}
	}

	autoBackups, err := db.GetAutomaticBackupsByNfsMountID(nfsShare.Id)
	if err != nil {
		return fmt.Errorf("failed to query automatic backups using NFS share: %v", err)
	}
	for _, bak := range autoBackups {
		if err := virshService.DeleteAutoBak(bak.Id); err != nil {
			return fmt.Errorf("failed to remove automatic backup %d for VM %s: %v", bak.Id, bak.VmName, err)
		}
	}

	isos, err := db.GetAllISOs()
	if err != nil {
		return fmt.Errorf("failed to get ISOs: %v", err)
	}
	for _, iso := range isos {
		if strings.HasPrefix(iso.FilePath, mount.Target) {
			if err := os.Remove(iso.FilePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove ISO file %s: %v", iso.FilePath, err)
			}
			if err := db.RemoveISOByID(iso.Id); err != nil {
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
			logger.Error("UnmountSharedFolder failed: %v", err)
		}
	}

	if conn := protocol.GetConnectionByMachineName(mount.MachineName); conn != nil && conn.Connection != nil {
		if err := nfs.RemoveSharedFolder(conn.Connection, mount); err != nil {
			logger.Error("RemoveSharedFolder failed: %v", err)
		}
	}

	if err := db.RemoveNFSShare(mount.MachineName, mount.FolderPath); err != nil {
		return fmt.Errorf("failed to remove NFS share from database: %v", err)
	}

	return nil
}

func (s *NFSService) DeleteSharePoint(force bool) error {

	if force {
		return forcedelete(s)
	}

	conn := protocol.GetConnectionByMachineName(s.SharePoint.MachineName)
	if conn == nil || conn.Connection == nil {
		return fmt.Errorf("slave not connected")
	}

	//check if exists in db
	if exists, err := db.DoesExistNFSShare(s.SharePoint.MachineName, s.SharePoint.FolderPath); err != nil {
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
	vms, err := virshService.GetAllVmsByOnNfsShare(mount.Target)
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
	isos, err := db.GetAllISOs()
	if err != nil {
		return fmt.Errorf("failed to get ISOs: %v", err)
	}
	for _, iso := range isos {
		if strings.HasPrefix(iso.FilePath, mount.Target) {
			return fmt.Errorf("cannot delete NFS share, there are ISOs using it: %v", iso.Name)
		}
	}

	nfsShare, err := db.GetNFSShareByMachineAndFolder(mount.MachineName, mount.FolderPath)
	if err != nil {
		return fmt.Errorf("failed to resolve NFS share: %v", err)
	}
	if nfsShare == nil {
		return fmt.Errorf("cannot resolve NFS share for deletion")
	}

	backups, err := db.GetVirshBackupsByNfsMountID(nfsShare.Id)
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

	autoBackups, err := db.GetAutomaticBackupsByNfsMountID(nfsShare.Id)
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
			logger.Error("UnmountSharedFolder failed: %v", err)
			continue
		}
	}

	err = db.RemoveNFSShare(mount.MachineName, mount.FolderPath)
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
			logger.Error("IsFolderMounted failed: %v", err)
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
func (s *NFSService) SyncSharedFolder() error {
	slavesShared, err := db.GetAllMachineNamesWithShares()
	if err != nil {
		return fmt.Errorf("failed to get all machine names with shares: %v", err)
	}

	notConnected := []string{}
	for _, machineName := range slavesShared {
		conn := protocol.GetConnectionByMachineName(machineName)
		if conn == nil || conn.Connection == nil {
			logger.Warn("slave not connected:", machineName)
			notConnected = append(notConnected, machineName)
			continue
		}

		shares, err := db.GetNFSharesByMachineName(machineName)
		if err != nil {
			logger.Error("failed to get NFS shares by machine name:", err)
			continue
		}

		nfs.SyncSharedFolder(conn.Connection, ConvertNSFShareToGRPCFolderMount(shares))
	}

	if len(notConnected) > 0 {
		return fmt.Errorf("some slaves not connected: %v", notConnected)
	}

	return nil
}

func (s *NFSService) MountAllSharedFolders() error {
	conns := protocol.GetAllGRPCConnections()
	machineNames := protocol.GetAllMachineNames()

	serversNFS, err := nfs.GetAllSharedFolders()
	if err != nil {
		return err
	}

	if len(conns) != len(machineNames) {
		return fmt.Errorf("length of connections and machine names must be the same")
	}

	mountErrors := make([]string, 0)

	logger.Info("Creating NFS shared folders on all slaves...")
	// create shared folders on all provided connections
	for _, svNSF := range serversNFS {
		mount := &proto.FolderMount{
			FolderPath:      svNSF.FolderPath,
			Source:          svNSF.Source,
			Target:          svNSF.Target,
			MachineName:     svNSF.MachineName,
			HostNormalMount: svNSF.HostNormalMount,
		}
		for i, conn := range conns {
			if conn == nil {
				continue
			}

			// skip if machine name does not match
			if machineNames[i] != svNSF.MachineName {
				continue
			}
			logger.Info("Creating NFS shared folder on machine:", machineNames[i], " with mount:", mount)
			// create shared folder on the specific machine
			if err := nfs.CreateSharedFolder(conn, mount); err != nil {
				errMsg := fmt.Sprintf("CreateSharedFolder on %s target %s failed: %v", machineNames[i], mount.Target, err)
				logger.Error(errMsg)
				mountErrors = append(mountErrors, errMsg)
				continue
			}
		}
	}

	logger.Info("Mounting NFS shared folders on all slaves...")
	// mount on all provided connections
	for i, conn := range conns {
		if conn == nil {
			continue
		}
		for _, svNSF := range serversNFS {
			mount := &proto.FolderMount{
				FolderPath:      svNSF.FolderPath,
				Source:          svNSF.Source,
				Target:          svNSF.Target,
				MachineName:     svNSF.MachineName,
				HostNormalMount: svNSF.HostNormalMount,
			}
			logger.Info("Mounting NFS shared folder on machine with mount:", mount)
			if err := nfs.MountSharedFolder(conn, mount); err != nil {
				errMsg := fmt.Sprintf("MountSharedFolder on %s target %s failed: %v", machineNames[i], mount.Target, err)
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

func (s *NFSService) UpdateNFSShit() error {
	err := s.SyncSharedFolder()
	if err != nil {
		logger.Error("SyncSharedFolder failed: %v", err)
	}

	err = s.MountAllSharedFolders()
	if err != nil {
		logger.Error("MountAllSharedFolders failed: %v", err)
	}
	return nil
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
		logger.Error("DownloadISO failed: %v", err)
		return "", err
	}
	err := nfs.Sync(conn.Connection)
	if err != nil {
		return "", err
	}
	return isoPath, nil
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
	conns := protocol.GetAllGRPCConnections()
	machineNames := protocol.GetAllMachineNames()

	if len(conns) != len(machineNames) {
		return nil, fmt.Errorf("length of connections and machine names must be the same")
	}

	for i, conn := range conns {
		machineName := machineNames[i]
		if conn == nil {
			results[machineName] = false
			continue
		}

		found, err := nfs.CanFindFileOrDir(conn, path)
		if err != nil {
			logger.Error("CanFindFileOrDir failed for machine %s: %v", machineName, err)
			results[machineName] = false
			continue
		}
		results[machineName] = found
	}

	return results, nil
}

// uses "sync" command so nfs fushes all dirty tables
func (s *NFSService) SyncNFSAllSlaves() error {
	var errRet string
	conns := protocol.GetAllGRPCConnections()
	for i, con := range conns {
		if con == nil {
			continue
		}
		err := nfs.Sync(con)
		if err != nil {
			errRet += fmt.Sprintf("err %d: %s\n", i, err.Error())
		}
	}
	if errRet != "" {
		return fmt.Errorf("%s", errRet)
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
