package nfs

import (
	"context"

	pb "github.com/Maruqes/512SvMan/api/proto/nfs"
	"github.com/Maruqes/512SvMan/logger"
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
		logger.Error("MountFolder failed", "error", err)
		return &pb.MountResponse{Ok: false}, err
	}
	logger.Info("MountFolder succeeded", "folder", req.FolderPath, "source", req.Source, "target", req.Target)
	return &pb.MountResponse{Ok: true}, nil
}

func (s *NFSService) UnmountFolder(ctx context.Context, req *pb.FolderMount) (*pb.UnmountResponse, error) {
	err := UnmountSharedFolder(FolderMount{
		FolderPath: req.FolderPath,
		Source:     req.Source,
		Target:     req.Target,
	})
	if err != nil {
		logger.Error("UnmountFolder failed", "error", err)
		return &pb.UnmountResponse{Ok: false}, err
	}
	logger.Info("UnmountFolder succeeded", "folder", req.FolderPath, "source", req.Source, "target", req.Target)
	return &pb.UnmountResponse{Ok: true}, nil
}

func (s *NFSService) RemoveSharedFolder(ctx context.Context, req *pb.FolderMount) (*pb.CreateResponse, error) {
	err := RemoveSharedFolder(FolderMount{
		FolderPath: req.FolderPath,
		Source:     req.Source,
		Target:     req.Target,
	})
	if err != nil {
		logger.Error("RemoveSharedFolder failed", "error", err)
		return &pb.CreateResponse{Ok: false}, err
	}
	logger.Info("RemoveSharedFolder succeeded", "folder", req.FolderPath, "source", req.Source, "target", req.Target)
	return &pb.CreateResponse{Ok: true}, nil
}

func (s *NFSService) SyncSharedFolder(ctx context.Context, req *pb.FolderMountList) (*pb.CreateResponse, error) {
	err := SyncSharedFolder(func() []FolderMount {
		var folders []FolderMount
		for _, f := range req.Mounts {
			folders = append(folders, FolderMount{
				FolderPath: f.FolderPath,
				Source:     f.Source,
				Target:     f.Target,
			})
		}
		return folders
	}())
	if err != nil {
		logger.Error("SyncSharedFolder failed", "error", err)
		return &pb.CreateResponse{Ok: false}, err
	}
	logger.Info("SyncSharedFolder succeeded", "count", len(req.Mounts))
	return &pb.CreateResponse{Ok: true}, nil
}

func (s *NFSService) DownloadIso(ctx context.Context, req *pb.DownloadIsoRequest) (*pb.CreateResponse, error) {
	path, err := DownloadISO(req.IsoUrl, req.IsoName, req.FolderMount.Target)
	if err != nil {
		logger.Error("DownloadISO failed", "error", err)
		return &pb.CreateResponse{Ok: false}, err
	}
	logger.Info("DownloadISO succeeded", "iso", path)
	return &pb.CreateResponse{Ok: true}, nil
}
