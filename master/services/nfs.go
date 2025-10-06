package services

import (
	"512SvMan/db"
	"512SvMan/nfs"
	"512SvMan/protocol"
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

type SharePoint struct {
	MachineName string `json:"machine_name"` //this machine want to share
	FolderPath  string `json:"folder_path"`  //this folder
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

	err := db.AddNFSShare(mount.MachineName, mount.FolderPath, mount.Source, mount.Target)
	if err != nil {
		logger.Error("AddNFSShare failed: %v", err)
		return err
	}

	err = nfs.MountAllSharedFolders(protocol.GetAllGRPCConnections(), protocol.GetAllMachineNames())
	if err != nil {
		logger.Error("MountAllSharedFolders failed: %v", err)
		return err
	}
	return nil
}

func (s *NFSService) DeleteSharePoint() error {
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

	if err := nfs.RemoveSharedFolder(conn.Connection, mount); err != nil {
		return fmt.Errorf("failed to remove shared folder: %v", err)
	}

	err := db.RemoveNFSShare(mount.MachineName, mount.FolderPath)
	if err != nil {
		return fmt.Errorf("failed to remove NFS share from database: %v", err)
	}

	return nil
}

func (s *NFSService) GetAllSharedFolders() ([]db.NFSShare, error) {
	return nfs.GetAllSharedFolders()
}
