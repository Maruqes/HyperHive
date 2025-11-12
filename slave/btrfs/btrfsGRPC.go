package btrfs

import (
	"context"

	btrfsGrpc "github.com/Maruqes/512SvMan/api/proto/btrfs"
)

type BTRFSService struct {
	btrfsGrpc.UnimplementedBtrFSServiceServer
}

func (s *BTRFSService) GetAllDisks(ctx context.Context, _ *btrfsGrpc.Empty) (*btrfsGrpc.MinDiskArr, error) {
	disks, err := GetAllDisks()
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
