package nfs

import (
	"context"

	pbnfs "github.com/Maruqes/512SvMan/api/proto/nfs"
	"google.golang.org/grpc"
)

func CreateSharedFolder(conn *grpc.ClientConn, folderMount *pbnfs.FolderMount) error {
	client := pbnfs.NewNFSServiceClient(conn)
	// // Call the CreateSharedFolder method on the client
	// client.CreateSharedFolder(context.Background(), &pbnfs.FolderMount{
	// 	FolderPath: "/var/512svman/shared",
	// 	Source:     "nfs-server:/var/512svman/shared",
	// 	Target:     "/mnt/512svman",
	//
	// })

	res, err := client.CreateSharedFolder(context.Background(), folderMount)
	if err != nil {
		return err
	}
	println("Response from CreateSharedFolder:", res.GetOk())
	return nil
}
