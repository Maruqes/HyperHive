package virsh

import (
	"context"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"google.golang.org/grpc"
)

func GetCpuFeatures(conn *grpc.ClientConn) []string {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.GetCpuFeatures(context.Background(), &grpcVirsh.Empty{})
	if err != nil {
		return nil
	}
	return resp.Features
}
