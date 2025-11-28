package docker

import (
	"context"

	dockerGRPC "github.com/Maruqes/512SvMan/api/proto/docker"
)

type DockerService struct {
	dockerGRPC.UnimplementedDockerServiceServer
}

var ImagesService *Image

func (s *DockerService) ImageDownload(ctx context.Context, req *dockerGRPC.DownloadImage) (*dockerGRPC.Empty, error) {
	err := ImagesService.Download(ctx, req.ImageRef, req.Registry)
	return &dockerGRPC.Empty{}, err
}

func (s *DockerService) ImageRemove(ctx context.Context, req *dockerGRPC.Remove) (*dockerGRPC.Empty, error) {
	err := ImagesService.Remove(ctx, req.ImageId, req.Force, req.PruneChild)
	return &dockerGRPC.Empty{}, err
}

func (s *DockerService) ImageList(ctx context.Context, req *dockerGRPC.Empty) (*dockerGRPC.ListOfImages, error) {
	imgs, err := ImagesService.List(ctx)
	if err != nil {
		return nil, err
	}
	var res dockerGRPC.ListOfImages

	for _, img := range imgs {
		res.Imgs = append(res.Imgs, &dockerGRPC.ImageSummary{
			Id:          img.ID,
			ParentId:    img.ParentID,
			RepoTags:    img.RepoTags,
			RepoDigests: img.RepoDigests,
			Created:     img.Created,
			Size:        img.Size,
			SharedSize:  img.SharedSize,
			VirtualSize: img.VirtualSize,
			Labels:      img.Labels,
			Containers:  img.Containers,
		})
	}
	return &res, nil
}
