package extra

import (
	"context"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
)

type ExtraService struct {
	extraGrpc.UnimplementedExtraServiceServer
}

func (s *ExtraService) CheckForUpdates(ctx context.Context, req *extraGrpc.Empty) (*extraGrpc.AllUpdates, error) {
	updates, err := CheckForUpdates()
	if err != nil {
		return nil, err
	}
	res := &extraGrpc.AllUpdates{}
	for _, update := range updates {
		res.Updates = append(res.Updates, &extraGrpc.UpdateInfo{
			Name:    update.Name,
			Version: update.Version,
		})
	}
	return res, nil
}

func (s *ExtraService) PerformUpdate(ctx context.Context, req *extraGrpc.UpdateRequest) (*extraGrpc.Empty, error) {
	err := PerformUpdate(req.Name, req.RestartAfter)
	if err != nil {
		return &extraGrpc.Empty{}, err
	}
	return &extraGrpc.Empty{}, nil
}
