package virsh

import (
	"context"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"google.golang.org/grpc"
)

func GetVMInterfaceStats(ctx context.Context, conn *grpc.ClientConn) (*grpcVirsh.VMInterfaceStatsResponse, error) {
	return grpcVirsh.NewSlaveVirshServiceClient(conn).GetVMInterfaceStats(ctx, &grpcVirsh.Empty{})
}
