package virsh

import (
	"context"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"google.golang.org/grpc"
)

// GetVMSnapshot preserves the caller's cancellation instead of starting work in
// the background. The RPC fails if secure inactive XML cannot be read.
func GetVMSnapshot(ctx context.Context, conn *grpc.ClientConn, name string) (*grpcVirsh.VMSnapshotResponse, error) {
	return grpcVirsh.NewSlaveVirshServiceClient(conn).GetVMSnapshot(ctx, &grpcVirsh.GetVmByNameRequest{Name: name})
}
