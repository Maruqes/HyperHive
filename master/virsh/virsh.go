package virsh

import (
	"512SvMan/protocol"
	"context"
	"fmt"

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

func GetCPUXML(conn *grpc.ClientConn) (string, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.GetCPUXML(context.Background(), &grpcVirsh.Empty{})
	if err != nil {
		return "", err
	}
	return resp.CpuXML, nil
}

func GetVMCPUXml(conn *grpc.ClientConn, vmName string) (string, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.GetVMCPUXml(context.Background(), &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil {
		return "", err
	}
	return resp.CpuXML, nil
}

func CreateVM(conn *grpc.ClientConn, name string, memory, vcpu int32, diskFolder, diskPath string, diskSizeGB int32, isoPath, network, VNCPassword string) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.CreateVm(context.Background(), &grpcVirsh.CreateVmRequest{
		Name:        name,
		Memory:      memory,
		Vcpu:        vcpu,
		DiskFolder:  diskFolder,
		DiskPath:    diskPath,
		DiskSizeGB:  diskSizeGB,
		IsoPath:     isoPath,
		Network:     network,
		VncPassword: VNCPassword,
	})
	if err != nil {
		return err
	}
	return nil
}

func CreateLiveVM(conn *grpc.ClientConn, name string, memory, vcpu int32, diskFolder, diskPath string, diskSizeGB int32, isoPath, network, VNCPassword string, cpuXml string) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	fmt.Println("Creating live VM with CPU XML:", cpuXml)
	_, err := client.CreateLiveVM(context.Background(), &grpcVirsh.CreateVmLiveRequest{
		Vm: &grpcVirsh.CreateVmRequest{
			Name:        name,
			Memory:      memory,
			Vcpu:        vcpu,
			DiskFolder:  diskFolder,
			DiskPath:    diskPath,
			DiskSizeGB:  diskSizeGB,
			IsoPath:     isoPath,
			Network:     network,
			VncPassword: VNCPassword,
		},
		CpuXml: cpuXml,
	})
	if err != nil {
		return err
	}
	return nil
}

// conn machine will migrate do slaveIp machine
func MigrateVm(ctx context.Context, conn *grpc.ClientConn, name, slaveIp string, live bool) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.MigrateVM(ctx, &grpcVirsh.MigrateVmRequest{
		Name:    name,
		SlaveIp: slaveIp,
		Live:    live,
	})
	if err != nil {
		return err
	}
	return nil
}

func UpdateVMCPUXml(conn *grpc.ClientConn, vmName, cpuXml string) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.UpdateVMCPUXml(context.Background(), &grpcVirsh.UpdateVMCPUXmlRequest{
		Name:   vmName,
		CpuXML: cpuXml,
	})
	if err != nil {
		return err
	}
	return nil
}

func GetAllVms(conn *grpc.ClientConn, empty *grpcVirsh.Empty) (*grpcVirsh.GetAllVmsResponse, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	return client.GetAllVms(context.Background(), empty)
}

func GetVmByName(conn *grpc.ClientConn, empty *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.Vm, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.GetVmByName(context.Background(), empty)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func DoesVMExist(name string) (bool, error) {
	con := protocol.GetAllGRPCConnections()
	for _, conn := range con {
		_, err := GetVmByName(conn, &grpcVirsh.GetVmByNameRequest{Name: name})
		if err == nil {
			return true, nil
		}
	}
	return false, nil
}

func StartVm(conn *grpc.ClientConn, req *grpcVirsh.Vm) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.StartVM(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ShutdownVM(conn *grpc.ClientConn, req *grpcVirsh.Vm) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.ShutdownVM(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ForceShutdownVM(conn *grpc.ClientConn, req *grpcVirsh.Vm) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.ForceShutdownVM(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func RemoveVM(conn *grpc.ClientConn, req *grpcVirsh.Vm) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.RemoveVM(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func RestartVM(conn *grpc.ClientConn, req *grpcVirsh.Vm) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.RestartVM(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func EditVm(conn *grpc.ClientConn, req *grpcVirsh.Vm) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.EditVmResources(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func RemoveIso(conn *grpc.ClientConn, req *grpcVirsh.Vm) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.RemoveIsoFromVm(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func PauseVM(conn *grpc.ClientConn, req *grpcVirsh.Vm) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.PauseVM(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ResumeVM(conn *grpc.ClientConn, req *grpcVirsh.Vm) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.ResumeVM(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}
