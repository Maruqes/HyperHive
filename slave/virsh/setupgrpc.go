package virsh

import (
	"context"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

type SlaveVirshService struct {
	grpcVirsh.UnimplementedSlaveVirshServiceServer
}

func (s *SlaveVirshService) GetCpuFeatures(ctx context.Context, e *grpcVirsh.Empty) (*grpcVirsh.GetCpuFeaturesResponse, error) {
	cpuFeatures, err := GetCpuFeatures()
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.GetCpuFeaturesResponse{Features: cpuFeatures}, nil
}

func (s *SlaveVirshService) GetCPUXML(ctx context.Context, e *grpcVirsh.Empty) (*grpcVirsh.CPUXMLResponse, error) {
	cpuXML, err := GetHostCPUXML()
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.CPUXMLResponse{CpuXML: cpuXML}, nil
}

func (s *SlaveVirshService) CreateVm(ctx context.Context, req *grpcVirsh.CreateVmRequest) (*grpcVirsh.OkResponse, error) {
	params := VMCreationParams{
		ConnURI:        "qemu:///system",
		Name:           req.Name,
		MemoryMB:       int(req.Memory),
		VCPUs:          int(req.Vcpu),
		DiskFolder:     req.DiskFolder,
		DiskPath:       req.DiskPath,
		DiskSizeGB:     int(req.DiskSizeGB),
		ISOPath:        req.IsoPath,
		Network:        req.Network,
		GraphicsListen: "0.0.0.0",
		VNCPassword:    req.VncPassword,
	}
	_, err := CreateVMHostPassthrough(params)
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

func (s *SlaveVirshService) CreateLiveVM(ctx context.Context, req *grpcVirsh.CreateVmLiveRequest) (*grpcVirsh.OkResponse, error) {
	params := CreateVMCustomCPUOptions{
		ConnURI:        "qemu:///system",
		Name:           req.Vm.Name,
		MemoryMB:       int(req.Vm.Memory),
		VCPUs:          int(req.Vm.Vcpu),
		DiskFolder:     req.Vm.DiskFolder,
		DiskPath:       req.Vm.DiskPath,
		DiskSizeGB:     int(req.Vm.DiskSizeGB),
		ISOPath:        req.Vm.IsoPath,
		Network:        req.Vm.Network,
		GraphicsListen: "0.0.0.0",
		VNCPassword:    req.Vm.VncPassword,
		CPUXml:         req.CpuXml,
	}
	_, err := CreateVMCustomCPU(params)
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

func (s *SlaveVirshService) MigrateVM(ctx context.Context, e *grpcVirsh.MigrateVmRequest) (*grpcVirsh.OkResponse, error) {
	opts := MigrateOptions{
		ConnURI: "qemu:///system",
		Name:    e.Name,
		DestURI: "qemu+ssh://root@" + e.SlaveIp + ":22/system",
		Live:    e.Live,
		SSH: SSHOptions{
			IdentityFile:       "/root/.ssh/id_rsa_512svman",
			SkipHostKeyCheck:   true,
			UserKnownHostsFile: "/dev/null",
		},
	}
	err := MigrateVM(opts)
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{}, nil
}

func (s *SlaveVirshService) GetAllVms(ctx context.Context, e *grpcVirsh.Empty) (*grpcVirsh.GetAllVmsResponse, error) {
	vms, err := GetAllVMs()
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.GetAllVmsResponse{Vms: vms}, nil
}

func (s *SlaveVirshService) GetVmByName(ctx context.Context, req *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.Vm, error) {
	vm, err := GetVMByName(req.Name)
	if err != nil {
		return nil, err
	}
	return vm, nil
}

func (s *SlaveVirshService) ShutdownVM(ctx context.Context, req *grpcVirsh.Vm) (*grpcVirsh.OkResponse, error) {
	if err := ShutdownVM(req.Name); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}
func (s *SlaveVirshService) StartVM(ctx context.Context, req *grpcVirsh.Vm) (*grpcVirsh.OkResponse, error) {
	if err := StartVM(req.Name); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

func (s *SlaveVirshService) RemoveVM(ctx context.Context, req *grpcVirsh.Vm) (*grpcVirsh.OkResponse, error) {
	if err := RemoveVM(req.Name); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

func (s *SlaveVirshService) ForceShutdownVM(ctx context.Context, req *grpcVirsh.Vm) (*grpcVirsh.OkResponse, error) {
	if err := ForceShutdownVM(req.Name); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

func (s *SlaveVirshService) RestartVM(ctx context.Context, req *grpcVirsh.Vm) (*grpcVirsh.OkResponse, error) {
	if err := RestartVM(req.Name); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

func (s *SlaveVirshService) EditVmResources(ctx context.Context, req *grpcVirsh.Vm) (*grpcVirsh.OkResponse, error) {
	if err := EditVm(req.Name, int(req.CpuCount), int(req.MemoryMB), int(req.DiskSizeGB)); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

func (s *SlaveVirshService) RemoveIsoFromVm(ctx context.Context, req *grpcVirsh.Vm) (*grpcVirsh.OkResponse, error) {
	if err := RemoveIsoFromVM(req.Name); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

func (s *SlaveVirshService) PauseVM(ctx context.Context, req *grpcVirsh.Vm) (*grpcVirsh.OkResponse, error) {
	if err := PauseVM(req.Name); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

func (s *SlaveVirshService) ResumeVM(ctx context.Context, req *grpcVirsh.Vm) (*grpcVirsh.OkResponse, error) {
	if err := ResumeVM(req.Name); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}
