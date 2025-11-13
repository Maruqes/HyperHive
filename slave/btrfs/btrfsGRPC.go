package btrfs

import (
	"context"

	btrfsGrpc "github.com/Maruqes/512SvMan/api/proto/btrfs"
)

type BTRFSService struct {
	btrfsGrpc.UnimplementedBtrFSServiceServer
}

func (s *BTRFSService) GetAllDisks(ctx context.Context, _ *btrfsGrpc.Empty) (*btrfsGrpc.MinDiskArr, error) {
	disks, err := GetAllDisks(true)
	if err != nil {
		return nil, err
	}

	grpcDisks := make([]*btrfsGrpc.MinDisk, 0, len(disks))
	for _, disk := range disks {
		grpcDisks = append(grpcDisks, &btrfsGrpc.MinDisk{
			Path:    disk.Path,
			SizeGb:  disk.SizeGB,
			Mounted: disk.Mounted,
		})
	}

	return &btrfsGrpc.MinDiskArr{
		Disks: grpcDisks,
	}, nil
}

func convertChildren(children []FileSystem) []*btrfsGrpc.FileSystem {
	if len(children) == 0 {
		return nil
	}
	result := make([]*btrfsGrpc.FileSystem, 0, len(children))
	for _, child := range children {
		result = append(result, &btrfsGrpc.FileSystem{
			Target:        child.Target,
			Source:        child.Source,
			FsType:        child.FSType,
			Options:       child.Options,
			Uuid:          child.UUID,
			Label:         child.Label,
			Compression:   child.Compression,
			MaxSpace:      child.MaxSpace,
			UsedSpace:     child.UsedSpace,
			RealMaxSpace:  child.RealMaxSpace,
			RealUsedSpace: child.RealUsedSpace,
			Devices:       convertDevices(child.Devices),
			Children:      convertChildren(child.Children),
			Mounted:       child.Mounted,
		})
	}
	return result
}

func convertDevices(devices []BtrfsDevice) []*btrfsGrpc.BtrfsDevice {
	if len(devices) == 0 {
		return nil
	}
	result := make([]*btrfsGrpc.BtrfsDevice, 0, len(devices))
	for _, dev := range devices {
		result = append(result, &btrfsGrpc.BtrfsDevice{
			Name:       dev.Name,
			Path:       dev.Path,
			Type:       dev.Type,
			Label:      dev.Label,
			Uuid:       dev.UUID,
			UuidSub:    dev.UUIDSub,
			SizeBytes:  dev.SizeBytes,
			MountPoint: dev.MountPoint,
			Mounted:    dev.Mounted,
		})
	}
	return result
}

func (s *BTRFSService) GetAllFileSystems(ctx context.Context, _ *btrfsGrpc.Empty) (*btrfsGrpc.FindMntOutput, error) {
	raids, err := GetAllFileSystems()
	if err != nil {
		return nil, err
	}

	filesystems := make([]*btrfsGrpc.FileSystem, 0, len(raids.FileSystems))
	for _, raid := range raids.FileSystems {
		filesystems = append(filesystems, &btrfsGrpc.FileSystem{
			Target:        raid.Target,
			Source:        raid.Source,
			FsType:        raid.FSType,
			Options:       raid.Options,
			Uuid:          raid.UUID,
			Label:         raid.Label,
			Compression:   raid.Compression,
			MaxSpace:      raid.MaxSpace,
			UsedSpace:     raid.UsedSpace,
			RealMaxSpace:  raid.RealMaxSpace,
			RealUsedSpace: raid.RealUsedSpace,
			Devices:       convertDevices(raid.Devices),
			Children:      convertChildren(raid.Children),
			Mounted:       raid.Mounted,
		})
	}

	return &btrfsGrpc.FindMntOutput{
		Filesystems: filesystems,
	}, nil
}

func convertStringToRaidType(s string) *raidType {
	switch s {
	case Raid0.sType:
		return &Raid0
	case Raid1.sType:
		return &Raid1
	case Raid1c3.sType:
		return &Raid1c3
	case Raid1c4.sType:
		return &Raid1c4
	default:
		return nil
	}
}

func (s *BTRFSService) CreateRaid(ctx context.Context, req *btrfsGrpc.CreateRaidReq) (*btrfsGrpc.Empty, error) {
	err := CreateRaid(req.Name, convertStringToRaidType(req.Raid), req.Disks...)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}
