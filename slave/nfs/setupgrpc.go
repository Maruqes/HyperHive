package nfs

import (
	"context"

	pb "github.com/Maruqes/512SvMan/api/proto/nfs"
)

type NFSService struct {
	pb.UnimplementedNFSServiceServer
}

func (s *NFSService) CreateSharedFolder(ctx context.Context, req *pb.FolderMount) (*pb.CreateResponse, error) {
	err := CreateSharedFolder(FolderMount{
		FolderPath: req.FolderPath,
		Source:     req.Source,
		Target:     req.Target,
	})
	if err != nil {
		return &pb.CreateResponse{Ok: false}, err
	}
	return &pb.CreateResponse{Ok: true}, nil
}

func (s *NFSService) MountFolder(ctx context.Context, req *pb.FolderMount) (*pb.MountResponse, error) {
	err := MountSharedFolder(FolderMount{
		FolderPath: req.FolderPath,
		Source:     req.Source,
		Target:     req.Target,
	})
	if err != nil {
		return &pb.MountResponse{Ok: false}, err
	}
	return &pb.MountResponse{Ok: true}, nil
}

func (s *NFSService) UnmountFolder(ctx context.Context, req *pb.FolderMount) (*pb.UnmountResponse, error) {
	err := UnmountSharedFolder(FolderMount{
		FolderPath: req.FolderPath,
		Source:     req.Source,
		Target:     req.Target,
	})
	if err != nil {
		return &pb.UnmountResponse{Ok: false}, err
	}
	return &pb.UnmountResponse{Ok: true}, nil
}
