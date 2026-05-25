package vmdisk

import (
	"context"

	pb "github.com/Maruqes/512SvMan/api/proto/vm_disk"
)

type Service struct {
	pb.UnimplementedVMDiskServiceServer
}

func (s *Service) CreateVMDisk(ctx context.Context, req *pb.CreateVMDiskRequest) (*pb.VMDiskResponse, error) {
	disk, err := CreateVMDisk(req.BasePath, req.Name, req.SizeGB, req.Format)
	if err != nil {
		return &pb.VMDiskResponse{Ok: false}, err
	}
	return vmDiskResponse(disk), nil
}

func (s *Service) DeleteVMDisk(ctx context.Context, req *pb.VMDiskByNameRequest) (*pb.VMDiskResponse, error) {
	disk, err := DeleteVMDisk(req.BasePath, req.Name)
	if err != nil {
		return &pb.VMDiskResponse{Ok: false}, err
	}
	return vmDiskResponse(disk), nil
}

func (s *Service) GrowVMDisk(ctx context.Context, req *pb.GrowVMDiskRequest) (*pb.VMDiskResponse, error) {
	disk, err := GrowVMDisk(req.BasePath, req.Name, req.SizeGB)
	if err != nil {
		return &pb.VMDiskResponse{Ok: false}, err
	}
	return vmDiskResponse(disk), nil
}

func (s *Service) GetVMDiskInfo(ctx context.Context, req *pb.VMDiskByNameRequest) (*pb.VMDiskResponse, error) {
	disk, err := GetVMDiskInfo(req.BasePath, req.Name)
	if err != nil {
		return &pb.VMDiskResponse{Ok: false}, err
	}
	return vmDiskResponse(disk), nil
}

func vmDiskResponse(disk *VMDisk) *pb.VMDiskResponse {
	return &pb.VMDiskResponse{
		Ok:            true,
		DiskPath:      disk.DiskPath,
		FolderPath:    disk.FolderPath,
		Format:        disk.Format,
		SizeGB:        disk.SizeGB,
		OccupiedGB:    disk.OccupiedGB,
		OccupiedBytes: disk.OccupiedBytes,
	}
}
