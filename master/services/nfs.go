package services

import (
	"512SvMan/db"
	"512SvMan/nfs"
	"512SvMan/protocol"
	"context"
	"fmt"
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
			MachineName: s.MachineName,
			FolderPath:  s.FolderPath,
			Source:      s.Source,
			Target:      s.Target,
		})
	}
	return folderMounts
}

type SharePoint struct {
	MachineName string `json:"machine_name"` //this machine want to share
	FolderPath  string `json:"folder_path"`  //this folder
	Name        string `json:"name"`         //optional friendly name for the share
}

type NFSService struct {
	SharePoint SharePoint
}

func (s *NFSService) CreateSharePoint() error {
	//find connection by machine name
	conn := protocol.GetConnectionByMachineName(s.SharePoint.MachineName)
	if conn == nil || conn.Connection == nil {
		return fmt.Errorf("slave not connected")
	}

	//make sure name exists and sanitize it (only letters and numbers), and add it to folder_path
	if s.SharePoint.Name != "" {
		sanitized := ""
		for _, r := range s.SharePoint.Name {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				sanitized += string(r)
			}
		}
		if sanitized != "" {
			if s.SharePoint.FolderPath[len(s.SharePoint.FolderPath)-1] != '/' {
				s.SharePoint.FolderPath = s.SharePoint.FolderPath + "/" + sanitized
			} else {
				s.SharePoint.FolderPath = s.SharePoint.FolderPath + sanitized
			}
		}
	}

	mount := &proto.FolderMount{
		MachineName: s.SharePoint.MachineName,                  // machine that shares
		FolderPath:  s.SharePoint.FolderPath,                   // folder to share
		Source:      conn.Addr + ":" + s.SharePoint.FolderPath, // creates ip:folderpath
		Target:      "/mnt/512SvMan/shared/" + s.SharePoint.MachineName + "_" + getFolderName(s.SharePoint.FolderPath),
	}

	if err := nfs.CreateSharedFolder(conn.Connection, mount); err != nil {
		logger.Error("CreateSharedFolder failed: %v", err)
		return err
	}

	err := db.AddNFSShare(mount.MachineName, mount.FolderPath, mount.Source, mount.Target, s.SharePoint.Name)
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

	//remove last slash
	mount := &proto.FolderMount{
		MachineName: s.SharePoint.MachineName,
		FolderPath:  s.SharePoint.FolderPath,
		Source:      "",
		Target:      "/mnt/512SvMan/shared/" + s.SharePoint.MachineName + "_" + getFolderName(s.SharePoint.FolderPath),
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

	//remove all VMs and ISOs on this nfs share
	virshService := VirshService{}
	vms, err := virshService.GetAllVmsByOnNfsShare(mount.Target)
	if err != nil {
		return fmt.Errorf("failed to get VMs on NFS share: %v", err)
	}
	for _, vm := range vms {
		err := virshService.DeleteVM(vm.Name)
		if err != nil {
			logger.Error("failed to delete VM %s on NFS share: %v", vm.Name, err)
			continue
		}
	}

	isos, err := db.GetAllISOs()
	if err != nil {
		return fmt.Errorf("failed to get ISOs: %v", err)
	}
	for _, iso := range isos {
		if strings.HasPrefix(iso.FilePath, mount.Target) {
			err := db.RemoveISOByID(iso.Id)
			if err != nil {
				logger.Error("failed to remove ISO %s on NFS share: %v", iso.Name, err)
				continue
			}
		}
	}

	err = db.RemoveNFSShare(mount.MachineName, mount.FolderPath)
	if err != nil {
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
		MachineName: s.SharePoint.MachineName,
		FolderPath:  s.SharePoint.FolderPath,
		Source:      conn.Addr + ":" + s.SharePoint.FolderPath,
		Target:      "/mnt/512SvMan/shared/" + s.SharePoint.MachineName + "_" + getFolderName(s.SharePoint.FolderPath),
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

	logger.Info("Creating NFS shared folders on all slaves...")
	// create shared folders on all provided connections
	for _, svNSF := range serversNFS {
		mount := &proto.FolderMount{
			FolderPath:  svNSF.FolderPath,
			Source:      svNSF.Source,
			Target:      svNSF.Target,
			MachineName: svNSF.MachineName,
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
				return err
			}
		}
	}

	logger.Info("Mounting NFS shared folders on all slaves...")
	// mount on all provided connections
	for _, conn := range conns {
		if conn == nil {
			continue
		}
		for _, svNSF := range serversNFS {
			mount := &proto.FolderMount{
				FolderPath:  svNSF.FolderPath,
				Source:      svNSF.Source,
				Target:      svNSF.Target,
				MachineName: svNSF.MachineName,
			}
			logger.Info("Mounting NFS shared folder on machine with mount:", mount)
			if err := nfs.MountSharedFolder(conn, mount); err != nil {
				return err
			}
		}
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
			MachineName: nfsShare.MachineName,
			FolderPath:  nfsShare.FolderPath,
			Source:      nfsShare.Source,
			Target:      nfsShare.Target,
		},
	}

	if err := nfs.DownloadISO(conn.Connection, ctx, isoRequest); err != nil {
		logger.Error("DownloadISO failed: %v", err)
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
