package virsh

import (
	"context"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

func (s *SlaveVirshService) ListMachineTypes(ctx context.Context, req *grpcVirsh.Empty) (*grpcVirsh.MachineTypesResponse, error) {
	machineTypes, err := ListMachineTypes()
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.MachineTypesResponse{MachineTypes: machineTypes}, nil
}

func (s *SlaveVirshService) SetMachineType(ctx context.Context, req *grpcVirsh.SetMachineTypeRequest) (*grpcVirsh.MachineTypeResponse, error) {
	machineType, err := SetMachineType(req.VmName, req.MachineType)
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.MachineTypeResponse{
		VmName:      req.VmName,
		MachineType: machineType,
	}, nil
}
