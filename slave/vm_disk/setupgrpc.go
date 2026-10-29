package vmdisk

import (
	"context"
	"fmt"
	"google.golang.org/grpc/metadata"
	"io"
	"os"
	"strconv"

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

func (s *Service) DownloadVMDisk(req *pb.VMDiskByNameRequest, stream pb.VMDiskService_DownloadVMDiskServer) error {
	disk, err := findVMDiskContext(stream.Context(), req.BasePath, req.Name)
	if err != nil {
		return err
	}
	info, err := os.Lstat(disk.DiskPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("disk is not a regular file")
	}
	file, err := regularFile(disk.DiskPath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return err
	}
	if err := stream.SendHeader(metadata.Pairs("disk-length", strconv.FormatInt(info.Size(), 10))); err != nil {
		return err
	}
	remaining := info.Size()
	buf := make([]byte, 256*1024)
	for {
		if err := stream.Context().Err(); err != nil {
			return err
		}
		n, err := file.Read(buf)
		if int64(n) > remaining {
			return fmt.Errorf("disk grew during download")
		}
		remaining -= int64(n)
		if n > 0 {
			if sendErr := stream.Send(&pb.VMDiskChunk{Data: buf[:n]}); sendErr != nil {
				return sendErr
			}
		}
		if err == io.EOF {
			if remaining != 0 {
				return io.ErrUnexpectedEOF
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
}
