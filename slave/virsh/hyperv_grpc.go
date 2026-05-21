package virsh

import (
	"context"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

func (s *SlaveVirshService) GetHyperV(ctx context.Context, req *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.HyperVResponse, error) {
	enabled, err := GetHyperV(req.Name)
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.HyperVResponse{
		VmName: req.Name,
		Hyperv: enabled,
	}, nil
}

func (s *SlaveVirshService) SetHyperV(ctx context.Context, req *grpcVirsh.SetHyperVRequest) (*grpcVirsh.HyperVResponse, error) {
	enabled, err := SetHyperV(req.VmName, req.Hyperv)
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.HyperVResponse{
		VmName: req.VmName,
		Hyperv: enabled,
	}, nil
}
