package nfs

import (
	"context"
	"fmt"
	"slave/extra"

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
		FolderPath:      req.FolderPath,
		Source:          req.Source,
		Target:          req.Target,
		HostNormalMount: req.HostNormalMount,
	})
	if err != nil {
		extra.SendNotifications("Could not Mount", fmt.Sprintf("Could not Mount %s", req.Target), "/", true)
		logger.Error("MountFolder failed", "error", err)
		return &pb.MountResponse{Ok: false}, err
	}
	logger.Info("MountFolder succeeded", "folder", req.FolderPath, "source", req.Source, "target", req.Target, "HostNormalMount", req.HostNormalMount)
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
	path, err := DownloadISO(ctx, req.IsoUrl, req.IsoName, req.FolderMount.Target)
	if err != nil {
		logger.Error("DownloadISO failed", "error", err)
		return &pb.CreateResponse{Ok: false}, err
	}
	logger.Info("DownloadISO succeeded", "iso", path)
	return &pb.CreateResponse{Ok: true}, nil
}

func (s *NFSService) GetSharedFolderStatus(ctx context.Context, req *pb.FolderMount) (*pb.SharedFolderStatusResponse, error) {
	status, err := GetSharedFolderStatus(FolderMount{
		FolderPath: req.FolderPath,
		Source:     req.Source,
		Target:     req.Target,
	})
	if err != nil {
		logger.Error("GetSharedFolderStatus failed", "error", err)
		return &pb.SharedFolderStatusResponse{Working: false}, err
	}
	return &pb.SharedFolderStatusResponse{
		Working:         status.Working,
		SpaceOccupiedGB: status.SpaceOccupiedGB,
		SpaceFreeGB:     status.SpaceFreeGB,
		SpaceTotalGB:    status.SpaceTotalGB,
	}, nil
}

func (s *NFSService) ListFolderContents(ctx context.Context, req *pb.FolderPath) (*pb.FolderContents, error) {
	contents, err := GetFolderContentList(req.Path)
	if err != nil {
		logger.Error("ListFolderContents failed", "error", err)
		return &pb.FolderContents{}, err
	}
	items := &pb.FolderContents{}
	items.Files = append(items.Files, contents.Files...)
	items.Directories = append(items.Directories, contents.Dirs...)
	return items, nil
}

func (s *NFSService) CanFindFileOrDir(ctx context.Context, req *pb.FolderPath) (*pb.CreateResponse, error) {
	found, err := CanFindFileOrDir(req.Path)
	if err != nil {
		logger.Error("CanFindFileOrDir failed", "error", err)
		return &pb.CreateResponse{Ok: false}, err
	}
	logger.Info("CanFindFileOrDir succeeded", "path", req.Path)
	return &pb.CreateResponse{Ok: found}, nil
}

func (s *NFSService) CheckReadWrite(ctx context.Context, req *pb.FolderPath) (*pb.OkResponse, error) {
	if err := CheckReadWrite(req.Path); err != nil {
		logger.Error("CheckReadWrite failed", "error", err)
		return &pb.OkResponse{Ok: false, Message: err.Error()}, err
	}
	logger.Info("CheckReadWrite succeeded", "path", req.Path)
	return &pb.OkResponse{Ok: true, Message: "ok"}, nil
}

func (s *NFSService) Sync(ctx context.Context, req *pb.Empty) (*pb.OkResponse, error) {
	err := Sync()
	if err != nil {
		return nil, err
	}
	return &pb.OkResponse{Ok: true}, nil
}
