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
