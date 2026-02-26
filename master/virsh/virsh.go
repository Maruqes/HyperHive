package virsh

import (
	"512SvMan/protocol"
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

func GetVMXml(conn *grpc.ClientConn, vmName string) (string, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.GetVMXml(context.Background(), &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil {
		return "", err
	}
	return resp.VmXML, nil
}

func CreateVM(conn *grpc.ClientConn, name string, memory, vcpu int32, diskFolder, diskPath string, diskSizeGB int32, isoPath, network, VNCPassword string, cpuXML string, autoStart bool, isWindows bool) error {
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
		CpuXml:      cpuXML,
		IsWindows:   isWindows,
	})
	if err != nil {
		return err
	}
	return nil
}

// conn machine will migrate do slaveIp machine
func MigrateVm(ctx context.Context, conn *grpc.ClientConn, name, slaveIp string, live bool, timeoutSeconds int) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.MigrateVM(ctx, &grpcVirsh.MigrateVmRequest{
		Name:           name,
		SlaveIp:        slaveIp,
		Live:           live,
		TimeoutSeconds: int32(timeoutSeconds),
	})
	if err != nil {
		return err
	}
	return nil
}

func ColdMigrateVm(ctx context.Context, conn *grpc.ClientConn, machine *grpcVirsh.ColdMigrationRequest) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.ColdMigrateVm(ctx, machine)
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

func UpdateVMXml(conn *grpc.ClientConn, vmName, vmXml string) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.UpdateVMXml(context.Background(), &grpcVirsh.UpdateVMXmlRequest{
		Name:  vmName,
		VmXML: vmXml,
	})
	if err != nil {
		return err
	}
	return nil
}

func GetAllVms(conn *grpc.ClientConn, empty *grpcVirsh.Empty) (*grpcVirsh.GetAllVmsResponse, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.GetAllVms(context.Background(), empty)
	if err != nil {
		return nil, err
	}
	return resp, nil
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

func StartVm(ctx context.Context, conn *grpc.ClientConn, req *grpcVirsh.Vm) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.StartVM(ctx, req)
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

func UndefineVM(conn *grpc.ClientConn, req *grpcVirsh.Vm) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.UndefineVM(context.Background(), req)
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

func FreezeDisk(conn *grpc.ClientConn, req *grpcVirsh.Vm) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.FreezeDisk(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func UnFreezeDisk(conn *grpc.ClientConn, req *grpcVirsh.Vm) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.UnFreezeDisk(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ChangeNetwork(conn *grpc.ClientConn, req *grpcVirsh.ChangeNetworkReq) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.ChangeNetwork(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ChangeVncPassword(conn *grpc.ClientConn, req *grpcVirsh.ChangeVncPassword) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.ChangeVmPassword(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func AddNoVNCVideo(conn *grpc.ClientConn, vmName string) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.AddNoVNCVideo(context.Background(), &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil {
		return err
	}
	return nil
}

func RemoveNoVNCVideo(conn *grpc.ClientConn, vmName string) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.RemoveNoVNCVideo(context.Background(), &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil {
		return err
	}
	return nil
}

func GetNoVNCVideo(conn *grpc.ClientConn, vmName string) (*grpcVirsh.GetNoVNCVideoResponse, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.GetNoVNCVideo(context.Background(), &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func GetMemoryBallooning(conn *grpc.ClientConn, vmName string) (*grpcVirsh.GetMemoryBallooningResponse, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.GetMemoryBallooning(context.Background(), &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func SetMemoryBallooning(conn *grpc.ClientConn, vmName string, enable bool) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.SetMemoryBallooning(context.Background(), &grpcVirsh.SetMemoryBallooningRequest{
		VmName: vmName,
		Enable: enable,
	})
	if err != nil {
		return err
	}
	return nil
}

func ApplyCPUPinningGRPC(conn *grpc.ClientConn, req *grpcVirsh.CPUPinningRequest) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.ApplyCPUPinning(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func RemoveCPUPinningGRPC(conn *grpc.ClientConn, vmName string) error {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	_, err := client.RemoveCPUPinning(context.Background(), &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil {
		return err
	}
	return nil
}

func GetCPUPinningGRPC(conn *grpc.ClientConn, vmName string) (*grpcVirsh.CPUPinningResponse, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.GetCPUPinning(context.Background(), &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func GetCPUTopologyGRPC(conn *grpc.ClientConn) (*grpcVirsh.CPUTopologyResponse, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.GetCPUTopology(context.Background(), &grpcVirsh.Empty{})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func GetTunedAdmProfilesGRPC(conn *grpc.ClientConn) (*grpcVirsh.TunedAdmProfilesResponse, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.GetTunedAdmProfiles(context.Background(), &grpcVirsh.Empty{})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func SetTunedAdmProfileGRPC(conn *grpc.ClientConn, profile string) (*grpcVirsh.SetTunedAdmProfileResponse, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.SetTunedAdmProfile(context.Background(), &grpcVirsh.SetTunedAdmProfileRequest{
		Profile: profile,
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func GetIrqBalanceStateGRPC(conn *grpc.ClientConn) (*grpcVirsh.IrqBalanceStateResponse, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.GetIrqBalanceState(context.Background(), &grpcVirsh.Empty{})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func SetIrqBalanceStateGRPC(conn *grpc.ClientConn, enabled bool) (*grpcVirsh.SetIrqBalanceStateResponse, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.SetIrqBalanceState(context.Background(), &grpcVirsh.SetIrqBalanceStateRequest{
		Enabled: enabled,
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func GetHostCoreIsolationGRPC(conn *grpc.ClientConn) (*grpcVirsh.HostCoreIsolationStateResponse, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.GetHostCoreIsolation(context.Background(), &grpcVirsh.Empty{})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func SetHostCoreIsolationGRPC(conn *grpc.ClientConn, req *grpcVirsh.SetHostCoreIsolationRequest) (*grpcVirsh.HostCoreIsolationStateResponse, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.SetHostCoreIsolation(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func RemoveHostCoreIsolationGRPC(conn *grpc.ClientConn) (*grpcVirsh.HostCoreIsolationStateResponse, error) {
	client := grpcVirsh.NewSlaveVirshServiceClient(conn)
	resp, err := client.RemoveHostCoreIsolation(context.Background(), &grpcVirsh.Empty{})
	if err != nil {
		return nil, err
	}
	return resp, nil
}
