package nfs

import (
	"context"

	pbnfs "github.com/Maruqes/512SvMan/api/proto/nfs"
	"google.golang.org/grpc"
)

func CreateSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) error {
	client := pbnfs.NewNFSServiceClient(conn)

	res, err := client.CreateSharedFolder(context.Background(), folderMount)
	if err != nil {
		return err
	}
	println("Response from CreateSharedFolder:", res.GetOk())
	return nil
}

func MountSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) error {
	client := pbnfs.NewNFSServiceClient(conn)

	res, err := client.MountFolder(context.Background(), folderMount)
	if err != nil {
		return err
	}
	println("Response from MountSharedFolder:", res.GetOk())
	return nil
}

func UnmountSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) error {
	client := pbnfs.NewNFSServiceClient(conn)

	res, err := client.UnmountFolder(context.Background(), folderMount)
	if err != nil {
		return err
	}
	println("Response from UnmountSharedFolder:", res.GetOk())
	return nil
}

func RemoveSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) error {
	client := pbnfs.NewNFSServiceClient(conn)

	res, err := client.RemoveSharedFolder(context.Background(), folderMount)
	if err != nil {
		return err
	}
	println("Response from RemoveSharedFolder:", res.GetOk())
	return nil
}
