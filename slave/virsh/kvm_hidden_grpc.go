package virsh

import (
	"context"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

func (s *SlaveVirshService) GetKVMHidden(ctx context.Context, req *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.KVMHiddenResponse, error) {
	hidden, err := GetKVMHidden(req.Name)
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.KVMHiddenResponse{
		VmName: req.Name,
		Hidden: hidden,
	}, nil
}

func (s *SlaveVirshService) SetKVMHidden(ctx context.Context, req *grpcVirsh.SetKVMHiddenRequest) (*grpcVirsh.KVMHiddenResponse, error) {
	hidden, err := SetKVMHidden(req.VmName, req.Hidden)
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.KVMHiddenResponse{
		VmName: req.VmName,
		Hidden: hidden,
	}, nil
}
