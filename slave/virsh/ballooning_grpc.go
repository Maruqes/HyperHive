package virsh

import (
	"context"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

func (s *SlaveVirshService) GetMemoryBallooning(ctx context.Context, req *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.GetMemoryBallooningResponse, error) {
	info, err := GetMemoryBallooning(req.Name)
	if err != nil {
		return nil, err
	}

	return &grpcVirsh.GetMemoryBallooningResponse{
		Enabled:         info.Enabled,
		MemoryLocked:    info.MemoryLocked,
		HasMemballoon:   info.HasMemballoon,
		MemballoonModel: info.MemballoonModel,
	}, nil
}

func (s *SlaveVirshService) SetMemoryBallooning(ctx context.Context, req *grpcVirsh.SetMemoryBallooningRequest) (*grpcVirsh.OkResponse, error) {
	if err := SetMemoryBallooning(req.VmName, req.Enable); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}
