package virsh

import (
	"context"
	"slave/info"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

type SlaveVirshService struct {
	grpcVirsh.UnimplementedSlaveVirshServiceServer
}

// por algum motivo o grpc quer context aqui ahah, so Deus sabe, so Deus faz
func (s *SlaveVirshService) GetCpuFeatures(ctx context.Context, e *grpcVirsh.Empty) (*grpcVirsh.GetCpuFeaturesResponse, error) {
	cpu, err := info.CPUInfo.GetCPUInfo()
	if err != nil {
		return nil, err
	}
	return &grpcVirsh.GetCpuFeaturesResponse{Features: cpu.FeatureSet()}, nil
}
