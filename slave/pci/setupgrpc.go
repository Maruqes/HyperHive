package pci

import (
	"context"

	pciGrpc "github.com/Maruqes/512SvMan/api/proto/pci"
)

type PCIService struct {
	pciGrpc.UnimplementedSlavePCIServiceServer
}

func hostGPUToProto(dev HostPCIDevice) *pciGrpc.HostGPU {
	attachedVMs := make([]string, len(dev.AttachedToVMs))
	copy(attachedVMs, dev.AttachedToVMs)

	return &pciGrpc.HostGPU{
		NodeName:      dev.NodeName,
		Path:          dev.Path,
		Address:       dev.Address,
		Domain:        uint32(dev.Domain),
		Bus:           uint32(dev.Bus),
		Slot:          uint32(dev.Slot),
		Function:      uint32(dev.Function),
		Driver:        dev.Driver,
		Vendor:        dev.Vendor,
		VendorId:      dev.VendorID,
		Product:       dev.Product,
		ProductId:     dev.ProductID,
		Class:         dev.Class,
		IommuGroup:    int32(dev.IOMMUGroup),
		NumaNode:      int32(dev.NUMANode),
		ManagedByVfio: dev.ManagedByVFIO,
		AttachedToVms: attachedVMs,
	}
}

func vmGPUToProto(dev VMPCIDevice) *pciGrpc.VMGPU {
	return &pciGrpc.VMGPU{
		Address:  dev.Address,
		Domain:   uint32(dev.Domain),
		Bus:      uint32(dev.Bus),
		Slot:     uint32(dev.Slot),
		Function: uint32(dev.Function),
		Managed:  dev.Managed,
		Alias:    dev.Alias,
	}
}

func (s *PCIService) ListHostGPUs(ctx context.Context, _ *pciGrpc.Empty) (*pciGrpc.HostGPUList, error) {
	devices, err := ListHostGPUs()
	if err != nil {
		return nil, err
	}

	out := make([]*pciGrpc.HostGPU, 0, len(devices))
	for _, dev := range devices {
		out = append(out, hostGPUToProto(dev))
	}

	return &pciGrpc.HostGPUList{Gpus: out}, nil
}

func (s *PCIService) ListHostGPUsWithIOMMU(ctx context.Context, _ *pciGrpc.Empty) (*pciGrpc.HostGPUList, error) {
	devices, err := ListHostGPUsWithIOMMU()
	if err != nil {
		return nil, err
	}

	out := make([]*pciGrpc.HostGPU, 0, len(devices))
	for _, dev := range devices {
		out = append(out, hostGPUToProto(dev))
	}

	return &pciGrpc.HostGPUList{Gpus: out}, nil
}

func (s *PCIService) ListVMGPUs(ctx context.Context, req *pciGrpc.VmNameRequest) (*pciGrpc.VMGPUList, error) {
	devices, err := ListVMGPUs(req.GetVmName())
	if err != nil {
		return nil, err
	}

	out := make([]*pciGrpc.VMGPU, 0, len(devices))
	for _, dev := range devices {
		out = append(out, vmGPUToProto(dev))
	}

	return &pciGrpc.VMGPUList{Gpus: out}, nil
}

func (s *PCIService) AttachGPUToVM(ctx context.Context, req *pciGrpc.VMGPURequest) (*pciGrpc.OkResponse, error) {
	if err := AttachGPUToVM(req.GetVmName(), req.GetGpuRef()); err != nil {
		return nil, err
	}
	return &pciGrpc.OkResponse{Ok: true}, nil
}

func (s *PCIService) DetachGPUFromVM(ctx context.Context, req *pciGrpc.VMGPURequest) (*pciGrpc.OkResponse, error) {
	if err := DetachGPUFromVM(req.GetVmName(), req.GetGpuRef()); err != nil {
		return nil, err
	}
	return &pciGrpc.OkResponse{Ok: true}, nil
}

func (s *PCIService) ReturnGPUToHost(ctx context.Context, req *pciGrpc.GPUReferenceRequest) (*pciGrpc.OkResponse, error) {
	if err := ReturnGPUToHost(req.GetGpuRef()); err != nil {
		return nil, err
	}
	return &pciGrpc.OkResponse{Ok: true}, nil
}
