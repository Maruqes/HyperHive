package smartdisk

import (
	"context"

	smartdiskGrpc "github.com/Maruqes/512SvMan/api/proto/smartdisk"
	"google.golang.org/grpc"
)

func GetSmartInfo(conn *grpc.ClientConn, req *smartdiskGrpc.SmartInfoRequest) (*smartdiskGrpc.SmartDiskInfo, error) {
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.GetSmartInfo(context.Background(), req)
}

func RunSelfTest(ctx context.Context, conn *grpc.ClientConn, req *smartdiskGrpc.SelfTestRequest) (*smartdiskGrpc.SelfTestResponse, error) {
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.RunSelfTest(ctx, req)
}

func GetSelfTestProgress(conn *grpc.ClientConn, req *smartdiskGrpc.SmartInfoRequest) (*smartdiskGrpc.SelfTestProgress, error) {
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.GetSelfTestProgress(context.Background(), req)
}

func CancelSelfTest(ctx context.Context, conn *grpc.ClientConn, req *smartdiskGrpc.CancelSelfTestRequest) (*smartdiskGrpc.SelfTestResponse, error) {
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.CancelSelfTest(ctx, req)
}

func StartForceReallocation(ctx context.Context, conn *grpc.ClientConn, req *smartdiskGrpc.ForceReallocRequest) (*smartdiskGrpc.ForceReallocResponse, error) {
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.StartForceReallocation(ctx, req)
}

func GetForceReallocationProgress(conn *grpc.ClientConn, req *smartdiskGrpc.ForceReallocRequest) (*smartdiskGrpc.ForceReallocProgress, error) {
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.GetForceReallocationProgress(context.Background(), req)
}

func GetAllForceReallocationProgress(conn *grpc.ClientConn) ([]*smartdiskGrpc.ForceReallocProgress, error) {
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	resp, err := client.GetAllForceReallocationProgress(context.Background(), &smartdiskGrpc.Empty{})
	if err != nil {
		return nil, err
	}
	return resp.GetJobs(), nil
}

func CancelForceReallocation(ctx context.Context, conn *grpc.ClientConn, req *smartdiskGrpc.ForceReallocRequest) (*smartdiskGrpc.ForceReallocResponse, error) {
	client := smartdiskGrpc.NewSmartDiskServiceClient(conn)
	return client.CancelForceReallocation(ctx, req)
}
