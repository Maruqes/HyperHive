package virsh

import (
	"context"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

func (s *SlaveVirshService) AddNoVNCVideo(ctx context.Context, req *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.OkResponse, error) {
	if err := AddNoVNCVideo(req.Name); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

func (s *SlaveVirshService) RemoveNoVNCVideo(ctx context.Context, req *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.OkResponse, error) {
	if err := RemoveNoVNCVideo(req.Name); err != nil {
		return nil, err
	}
	return &grpcVirsh.OkResponse{Ok: true}, nil
}

func (s *SlaveVirshService) GetNoVNCVideo(ctx context.Context, req *grpcVirsh.GetVmByNameRequest) (*grpcVirsh.GetNoVNCVideoResponse, error) {
	videoInfo, err := GetNoVNCVideo(req.Name)
	if err != nil {
		return nil, err
	}

	return &grpcVirsh.GetNoVNCVideoResponse{
		Enabled:    videoInfo.Enabled,
		VideoCount: videoInfo.VideoCount,
		ModelType:  videoInfo.ModelType,
		VideoXml:   videoInfo.VideoXML,
	}, nil
}
