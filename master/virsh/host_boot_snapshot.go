package virsh

import (
	"context"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"google.golang.org/grpc"
)

func GetHostBootSnapshot(ctx context.Context, conn *grpc.ClientConn) (*grpcVirsh.HostBootSnapshotResponse, error) {
	return grpcVirsh.NewSlaveVirshServiceClient(conn).GetHostBootSnapshot(ctx, &grpcVirsh.Empty{})
}
