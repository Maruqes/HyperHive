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

func SyncSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMountList) error {
	client := pbnfs.NewNFSServiceClient(conn)

	res, err := client.SyncSharedFolder(context.Background(), folderMount)
	if err != nil {
		return err
	}
	logger.Info("Response from SyncSharedFolder: ", res.GetOk(), ", Synced folderMount:", folderMount)
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

func DownloadISO(conn *grpc.ClientConn, isoRequest *pbnfs.DownloadIsoRequest) error {
	client := pbnfs.NewNFSServiceClient(conn)

	res, err := client.DownloadIso(context.Background(), isoRequest)
	if err != nil {
		return err
	}
	logger.Info("Response from DownloadISO: ", res.GetOk())
	return nil
}

func GetAllSharedFolders() ([]db.NFSShare, error) {
	return db.GetAllNFShares()
}
