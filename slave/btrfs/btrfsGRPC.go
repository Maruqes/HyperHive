package btrfs

import (
	"context"
	"fmt"
	"strings"

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
			Path:       disk.Path,
			Name:       disk.Name,
			Model:      disk.Model,
			Vendor:     disk.Vendor,
			Serial:     disk.Serial,
			Rotational: disk.Rotational,
			SizeGb:     disk.SizeGB,
			Mounted:    disk.Mounted,
			ById:       disk.ByID,
			Transport:  disk.Transport,
			PciPath:    disk.PCIPath,
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
			RaidType:      child.RaidType,
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
			Name:      dev.Name,
			Path:      dev.Path,
			SizeBytes: dev.SizeBytes,
			Mounted:   dev.Mounted,
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
			RaidType:      raid.RaidType,
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
	rt := strings.ToLower(strings.TrimSpace(s))
	if rt == "" {
		return nil
	}

	switch rt {
	case Raid0.sType:
		return &Raid0
	case Raid1.sType, "raid1c2":
		return &Raid1
	case Raid1c3.sType:
		return &Raid1c3
	case Raid1c4.sType:
		return &Raid1c4
	case Single.sType:
		return &Single
	case Dup.sType:
		return &Dup
	case Raid5.sType:
		return &Raid5
	case Raid6.sType:
		return &Raid6
	default:
		return nil
	}
}

func (s *BTRFSService) CreateRaid(ctx context.Context, req *btrfsGrpc.CreateRaidReq) (*btrfsGrpc.Empty, error) {
	_, err := CreateRaid(req.Name, convertStringToRaidType(req.Raid), req.Disks...)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}

func (s *BTRFSService) RemoveRaid(ctx context.Context, req *btrfsGrpc.UUIDReq) (*btrfsGrpc.Empty, error) {
	err := RemoveRaidUUID(req.Uuid)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}

func (s *BTRFSService) MountRaid(ctx context.Context, req *btrfsGrpc.MountReq) (*btrfsGrpc.MountRaidRet, error) {
	res, err := MountRaid(req.Uuid, req.Target, req.Compression)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.MountRaidRet{Degraded: res.Degraded, Problems: res.Problems}, nil
}

func (s *BTRFSService) UMountRaid(ctx context.Context, req *btrfsGrpc.UMountReq) (*btrfsGrpc.Empty, error) {
	mp, err := GetMountPointFromUUID(req.Uuid)
	if err != nil {
		return nil, err
	}
	err = UMountRaid(mp, req.Force)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}

func (s *BTRFSService) AddDiskToRaid(ctx context.Context, req *btrfsGrpc.AddDiskToRaidReq) (*btrfsGrpc.Empty, error) {
	mp, err := GetMountPointFromUUID(req.Uuid)
	if err != nil {
		return nil, err
	}
	err = AddDiskToRaid(req.DiskPath, mp)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}

func (s *BTRFSService) RemoveDiskFromRaid(ctx context.Context, req *btrfsGrpc.RemoveDiskFromRaidReq) (*btrfsGrpc.Empty, error) {
	mp, err := GetMountPointFromUUID(req.Uuid)
	if err != nil {
		return nil, err
	}
	err = RemoveDisk(req.DiskPath, mp)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}

func (s *BTRFSService) ReplaceDiskInRaid(ctx context.Context, req *btrfsGrpc.ReplaceDiskToRaidReq) (*btrfsGrpc.Empty, error) {
	mp, err := GetMountPointFromUUID(req.Uuid)
	if err != nil {
		return nil, err
	}
	err = ReplaceDisk(req.OldDiskPath, req.NewDiskPath, mp)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}

func (s *BTRFSService) ChangeRaidLevel(ctx context.Context, req *btrfsGrpc.ChangeRaidLevelReq) (*btrfsGrpc.Empty, error) {
	mp, err := GetMountPointFromUUID(req.Uuid)
	if err != nil {
		return nil, err
	}
	raidType := convertStringToRaidType(req.RaidType)
	if raidType == nil {
		return nil, fmt.Errorf("invalid raid type: %s", req.RaidType)
	}
	err = ChangeRaidLevel(mp, *raidType)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}

func (s *BTRFSService) BalanceRaid(ctx context.Context, req *btrfsGrpc.BalanceRaidReq) (*btrfsGrpc.Empty, error) {
	mp, err := GetMountPointFromUUID(req.Uuid)
	if err != nil {
		return nil, err
	}
	balanceReq := BalanceRaidReq{
		MountPoint:           mp,
		Force:                req.Force,
		ConvertToCurrentRaid: req.ConvertToCurrentRaid,
	}
	if req.Filters != nil {
		balanceReq.Filters.DataUsageMax = req.Filters.DataUsageMax
		balanceReq.Filters.MetadataUsageMax = req.Filters.MetadataUsageMax
	}
	err = BalanceRaid(balanceReq)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}

func (s *BTRFSService) PauseBalance(ctx context.Context, req *btrfsGrpc.UUIDReq) (*btrfsGrpc.Empty, error) {
	mp, err := GetMountPointFromUUID(req.Uuid)
	if err != nil {
		return nil, err
	}
	err = PauseBalance(mp)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}

func (s *BTRFSService) ResumeBalance(ctx context.Context, req *btrfsGrpc.UUIDReq) (*btrfsGrpc.Empty, error) {
	mp, err := GetMountPointFromUUID(req.Uuid)
	if err != nil {
		return nil, err
	}
	err = ResumeBalance(mp)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}

func (s *BTRFSService) CancelBalance(ctx context.Context, req *btrfsGrpc.UUIDReq) (*btrfsGrpc.Empty, error) {
	mp, err := GetMountPointFromUUID(req.Uuid)
	if err != nil {
		return nil, err
	}
	err = CancelBalance(mp)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}

func (s *BTRFSService) DefragmentRaid(ctx context.Context, req *btrfsGrpc.UUIDReq) (*btrfsGrpc.Empty, error) {
	mp, err := GetMountPointFromUUID(req.Uuid)
	if err != nil {
		return nil, err
	}
	err = Defragment(mp, true, "")
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}

func (s *BTRFSService) ScrubRaid(ctx context.Context, req *btrfsGrpc.UUIDReq) (*btrfsGrpc.Empty, error) {
	mp, err := GetMountPointFromUUID(req.Uuid)
	if err != nil {
		return nil, err
	}
	err = Scrub(mp, false)
	if err != nil {
		return nil, err
	}
	return &btrfsGrpc.Empty{}, nil
}

func (s *BTRFSService) ScrubStats(ctx context.Context, req *btrfsGrpc.UUIDReq) (*btrfsGrpc.ScrubStatus, error) {
	mp, err := GetMountPointFromUUID(req.Uuid)
	if err != nil {
		return nil, err
	}

	stats, err := GetScrubStats(mp)
	if err != nil {
		return nil, err
	}

	grpcStats := &btrfsGrpc.ScrubStatus{
		Uuid:          stats.UUID,
		Path:          stats.Path,
		Status:        stats.Status,
		StartedAt:     stats.StartedAt,
		Duration:      stats.Duration,
		TimeLeft:      stats.TimeLeft,
		TotalToScrub:  stats.TotalToScrub,
		BytesScrubbed: stats.BytesScrubbed,
		Rate:          stats.Rate,
		ErrorSummary:  stats.ErrorSummary,
	}

	if stats.PercentDone != nil {
		grpcStats.PercentDone = *stats.PercentDone
	}

	return grpcStats, nil
}

func (s *BTRFSService) GetRaidStats(ctx context.Context, req *btrfsGrpc.UUIDReq) (*btrfsGrpc.RaidStats, error) {
	mp, err := GetMountPointFromUUID(req.Uuid)
	if err != nil {
		return nil, err
	}

	stats, err := GetFileSystemStats(mp)
	if err != nil {
		return nil, err
	}

	if stats == nil {
		return nil, fmt.Errorf("no stats available for filesystem at mount point: %s", mp)
	}

	// Convert device stats to gRPC format
	deviceStats := make([]*btrfsGrpc.DeviceStat, 0, len(stats.DeviceStats))
	for _, devStat := range stats.DeviceStats {
		deviceStats = append(deviceStats, &btrfsGrpc.DeviceStat{
			Device:         devStat.Device,
			DevId:          int32(devStat.DevID),
			WriteIoErrs:    int32(devStat.WriteIOErrs),
			ReadIoErrs:     int32(devStat.ReadIOErrs),
			FlushIoErrs:    int32(devStat.FlushIOErrs),
			CorruptionErrs: int32(devStat.CorruptionErrs),
			GenerationErrs: int32(devStat.GenerationErrs),
			BalanceStatus:  devStat.BalanceStatus,

			DeviceSizeBytes: devStat.DeviceSizeBytes,
			DeviceUsedBytes: devStat.DeviceUsedBytes,
			DeviceMissing:   devStat.DeviceMissing,
			FsUuid:          devStat.FSUUID,
			FsLabel:         devStat.FSLabel,
		})
	}

	return &btrfsGrpc.RaidStats{
		Version:      stats.Header.Version,
		FsUuid:       stats.FSUUID,
		FsLabel:      stats.FSLabel,
		TotalDevices: int32(stats.TotalDevices),
		DeviceStats:  deviceStats,
	}, nil
}
