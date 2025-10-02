package nfs

import (
	"512SvMan/db"
	"context"
	"fmt"

	pbnfs "github.com/Maruqes/512SvMan/api/proto/nfs"
	"github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
)

func CreateSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) error {
	client := pbnfs.NewNFSServiceClient(conn)

	res, err := client.CreateSharedFolder(context.Background(), folderMount)
	if err != nil {
		return err
	}
	logger.Info("Response from CreateSharedFolder: ", res.GetOk(), ", Created folderMount:", folderMount)
	return nil
}

func MountSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) error {
	client := pbnfs.NewNFSServiceClient(conn)

	res, err := client.MountFolder(context.Background(), folderMount)
	if err != nil {
		return err
	}
	logger.Info("Response from MountSharedFolder: ", res.GetOk(), ", Mounted folderMount:", folderMount)
	return nil
}

func UnmountSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) error {
	client := pbnfs.NewNFSServiceClient(conn)

	res, err := client.UnmountFolder(context.Background(), folderMount)
	if err != nil {
		return err
	}
	logger.Info("Response from UnmountSharedFolder: ", res.GetOk(), ", Unmounted folderMount:", folderMount)
	return nil
}

func RemoveSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) error {
	client := pbnfs.NewNFSServiceClient(conn)

	res, err := client.RemoveSharedFolder(context.Background(), folderMount)
	if err != nil {
		return err
	}
	logger.Info("Response from RemoveSharedFolder: ", res.GetOk(), ", Removed folderMount:", folderMount)
	return nil
}

func GetAllSharedFolders() ([]db.NFSShare, error) {
	return db.GetAllNFShares()
}

func MountAllSharedFolders(conns []*grpc.ClientConn, machineNames []string) error {
	serversNFS, err := GetAllSharedFolders()
	if err != nil {
		return err
	}

	if len(conns) != len(machineNames) {
		return fmt.Errorf("length of connections and machine names must be the same")
	}

	// create shared folders on all provided connections
	for _, svNSF := range serversNFS {
		mount := &pbnfs.FolderMount{
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
			// create shared folder on the specific machine
			if err := CreateSharedFolder(conn, mount); err != nil {
				return err
			}
		}
	}

	// mount on all provided connections
	for _, conn := range conns {
		if conn == nil {
			continue
		}
		for _, svNSF := range serversNFS {
			mount := &pbnfs.FolderMount{
				FolderPath:  svNSF.FolderPath,
				Source:      svNSF.Source,
				Target:      svNSF.Target,
				MachineName: svNSF.MachineName,
			}
			if err := MountSharedFolder(conn, mount); err != nil {
				return err
			}
		}
	}
	return nil
}
