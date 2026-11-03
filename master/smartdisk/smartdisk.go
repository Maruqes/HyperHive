package smartdisk

import (
	"context"
	"time"

	smartdiskGrpc "github.com/Maruqes/512SvMan/api/proto/smartdisk"
	"google.golang.org/grpc"
)

// smartInfoTimeout bounds one smartctl read on the slave. The slave also
// enforces its own 90 s exec deadline, so this is the tighter limit.
const smartInfoTimeout = 60 * time.Second

// reallocStatusTimeout bounds the cheap in-memory status calls, so a stuck
// connection cannot hang an HTTP handler.
const reallocStatusTimeout = 15 * time.Second

func GetSmartInfo(ctx context.Context, conn *grpc.ClientConn, req *smartdiskGrpc.SmartInfoRequest) (*smartdiskGrpc.SmartDiskInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, smartInfoTimeout)
	defer cancel()
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.GetSmartInfo(ctx, req)
}

func RunSelfTest(ctx context.Context, conn *grpc.ClientConn, req *smartdiskGrpc.SelfTestRequest) (*smartdiskGrpc.SelfTestResponse, error) {
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.RunSelfTest(ctx, req)
}

func GetSelfTestProgress(conn *grpc.ClientConn, req *smartdiskGrpc.SmartInfoRequest) (*smartdiskGrpc.SelfTestProgress, error) {
	ctx, cancel := context.WithTimeout(context.Background(), smartInfoTimeout)
	defer cancel()
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.GetSelfTestProgress(ctx, req)
}

func CancelSelfTest(ctx context.Context, conn *grpc.ClientConn, req *smartdiskGrpc.CancelSelfTestRequest) (*smartdiskGrpc.SelfTestResponse, error) {
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.CancelSelfTest(ctx, req)
}

func StartFullWipe(ctx context.Context, conn *grpc.ClientConn, req *smartdiskGrpc.ForceReallocRequest) (*smartdiskGrpc.ForceReallocResponse, error) {
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.StartFullWipe(ctx, req)
}

func StartNonDestructiveRealloc(ctx context.Context, conn *grpc.ClientConn, req *smartdiskGrpc.ForceReallocRequest) (*smartdiskGrpc.ForceReallocResponse, error) {
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.StartNonDestructiveRealloc(ctx, req)
}

func GetReallocStatus(conn *grpc.ClientConn, req *smartdiskGrpc.ForceReallocStatusRequest) (*smartdiskGrpc.ForceReallocStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), reallocStatusTimeout)
	defer cancel()
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.GetReallocStatus(ctx, req)
}

func ListReallocStatus(conn *grpc.ClientConn, req *smartdiskGrpc.ListReallocStatusRequest) (*smartdiskGrpc.ForceReallocStatusList, error) {
	ctx, cancel := context.WithTimeout(context.Background(), reallocStatusTimeout)
	defer cancel()
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.ListReallocStatus(ctx, req)
}

func CancelRealloc(ctx context.Context, conn *grpc.ClientConn, req *smartdiskGrpc.ForceReallocRequest) (*smartdiskGrpc.ForceReallocResponse, error) {
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.CancelRealloc(ctx, req)
}
