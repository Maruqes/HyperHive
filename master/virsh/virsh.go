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

func CreateVM(conn *grpc.ClientConn, name string, memory, vcpu int32, diskPath string, diskSizeGB int32, isoPath, network, VNCPassword string) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.CreateVm(context.Background(), &grpcVirsh.CreateVmRequest{
		Name:        name,
		Memory:      memory,
		Vcpu:        vcpu,
		DiskPath:    diskPath,
		DiskSizeGB:  diskSizeGB,
		IsoPath:     isoPath,
		Network:     network,
		VNCPassword: VNCPassword,
	})
	if err != nil {
		return err
	}
	return nil
}
