package info

import (
	"context"

	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
	"google.golang.org/grpc"
)

func GetCPUInfo(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.CPUCoreInfo, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetCPUInfo(context.Background(), empty)
}

func GetMemSummary(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.MemSummary, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetMemSummary(context.Background(), empty)
}

func GetDiskSummary(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.DiskSummary, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetDiskSummary(context.Background(), empty)
}

func GetNetworkSummary(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.NetworkSummary, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetNetworkSummary(context.Background(), empty)
}

func StressCPU(ctx context.Context, conn *grpc.ClientConn, params *infoGrpc.StressCPUParams) (*infoGrpc.Empty, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.StressCPU(ctx, params)
}

func TestRamMEM(ctx context.Context, conn *grpc.ClientConn, params *infoGrpc.TestRamMEMParams) (*infoGrpc.Ok, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.TestRamMEM(ctx, params)
}
