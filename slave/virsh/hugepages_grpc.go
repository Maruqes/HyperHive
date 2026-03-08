package virsh

import (
	"context"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

func (s *SlaveVirshService) GetHugePages(ctx context.Context, req *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.GetHugePagesResponse, error) {
	info, err := GetHugePages(req.Name)
	if err != nil {
		return nil, err
	}

	return &grpcVirsh.GetHugePagesResponse{
		Enabled:      info.Enabled,
		HasHugepages: info.HasHugepages,
		MemoryLocked: info.MemoryLocked,
	}, nil
}

func (s *SlaveVirshService) SetHugePages(ctx context.Context, req *grpcVirsh.SetHugePagesRequest) (*grpcVirsh.OkResponse, error) {
	if err := SetHugePages(req.VmName, req.Enable); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}
