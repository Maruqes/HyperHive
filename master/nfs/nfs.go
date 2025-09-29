package nfs

import (
	pbnfs "github.com/Maruqes/512SvMan/api/proto/nfs"
	"google.golang.org/grpc"
)

func CreateSharedFolder(conn *grpc.ClientConn) error {
	pbnfs.NewNfsServiceClient(conn)
	return nil
}
