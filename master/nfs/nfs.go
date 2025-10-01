package nfs

import (
	"512SvMan/db"
	"context"

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

func MountAllSharedFolders(conns []*grpc.ClientConn) error {
	serversNFS, err := GetAllSharedFolders()
	if err != nil {
		return err
	}

	// create shared folders on all provided connections
	for _, svNSF := range serversNFS {
		mount := &pbnfs.FolderMount{
			FolderPath: svNSF.FolderPath,
			Source:     svNSF.Source,
			Target:     svNSF.Target,
		}
		for _, conn := range conns {
			if conn == nil {
				continue
			}
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
				FolderPath: svNSF.FolderPath,
				Source:     svNSF.Source,
				Target:     svNSF.Target,
			}
			if err := MountSharedFolder(conn, mount); err != nil {
				return err
			}
		}
	}
	return nil
}
