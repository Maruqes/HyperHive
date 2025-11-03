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

func (s *SlaveVirshService) GetVMCPUXml(ctx context.Context, e *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.CPUXMLResponse, error) {
	xml, err := GetVmCPUXML(e.Name)
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.CPUXMLResponse{CpuXML: xml}, nil
}

func (s *SlaveVirshService) CreateVm(ctx context.Context, req *grpcVirsh.CreateVmRequest) (*grpcVirsh.OkResponse, error) {
	params := CreateVMCustomCPUOptions{
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
		CPUXml:         req.CpuXml,
		Raw:            req.Raw,
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
		Timeout: e.TimeoutSeconds,
	}
	err := MigrateVM(opts, ctx)
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{}, nil
}

func (s *SlaveVirshService) ColdMigrateVm(ctx context.Context, e *grpcVirsh.ColdMigrationRequest) (*grpcVirsh.OkResponse, error) {
	opts := ColdMigrationInfo{
		VmName:      e.VmName,
		DiskPath:    e.DiskPath,
		Memory:      e.Memory,
		VCpus:       e.VCpus,
		Network:     e.Network,
		VNCPassword: e.VncPassword,
		CpuXML:      e.CpuXML,
		Live:        e.Live,
		Raw:         e.Raw,
	}
	err := MigrateColdWin(opts)
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{}, nil
}

func (s *SlaveVirshService) UpdateVMCPUXml(ctx context.Context, e *grpcVirsh.UpdateVMCPUXmlRequest) (*grpcVirsh.OkResponse, error) {
	err := UpdateVMCPUXml(e.Name, e.CpuXML)
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{}, nil
}

func (s *SlaveVirshService) GetAllVms(ctx context.Context, e *grpcVirsh.Empty) (*grpcVirsh.GetAllVmsResponse, error) {
	vms, warnings, err := GetAllVMs()
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.GetAllVmsResponse{Vms: vms, Warnings: warnings}, nil
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

func (s *SlaveVirshService) FreezeDisk(ctx context.Context, req *grpcVirsh.Vm) (*grpcVirsh.OkResponse, error) {
	if err := FreezeDisk(req.Name); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

func (s *SlaveVirshService) UnFreezeDisk(ctx context.Context, req *grpcVirsh.Vm) (*grpcVirsh.OkResponse, error) {
	if err := UnFreezeDisk(req.Name); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}
