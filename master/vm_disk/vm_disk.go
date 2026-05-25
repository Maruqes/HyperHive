package vmdisk

import (
	"context"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/vm_disk"
	"google.golang.org/grpc"
)

const defaultVMDiskTimeout = 90 * time.Second

func CreateVMDisk(conn *grpc.ClientConn, req *pb.CreateVMDiskRequest) (*pb.VMDiskResponse, error) {
	client := pb.NewVMDiskServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultVMDiskTimeout)
	defer cancel()
	return client.CreateVMDisk(ctx, req)
}

func DeleteVMDisk(conn *grpc.ClientConn, req *pb.VMDiskByNameRequest) (*pb.VMDiskResponse, error) {
	client := pb.NewVMDiskServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultVMDiskTimeout)
	defer cancel()
	return client.DeleteVMDisk(ctx, req)
}

func GrowVMDisk(conn *grpc.ClientConn, req *pb.GrowVMDiskRequest) (*pb.VMDiskResponse, error) {
	client := pb.NewVMDiskServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultVMDiskTimeout)
	defer cancel()
	return client.GrowVMDisk(ctx, req)
}

func GetVMDiskInfo(conn *grpc.ClientConn, req *pb.VMDiskByNameRequest) (*pb.VMDiskResponse, error) {
	client := pb.NewVMDiskServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultVMDiskTimeout)
	defer cancel()
	return client.GetVMDiskInfo(ctx, req)
}
