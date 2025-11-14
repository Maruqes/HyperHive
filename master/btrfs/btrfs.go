package btrfs

import (
	"context"

	btrfsGrpc "github.com/Maruqes/512SvMan/api/proto/btrfs"
	"google.golang.org/grpc"
)

func GetAllDisks(conn *grpc.ClientConn) (*btrfsGrpc.MinDiskArr, error) {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	res, err := client.GetAllDisks(context.Background(), &btrfsGrpc.Empty{})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func GetAllFileSystems(conn *grpc.ClientConn) (*btrfsGrpc.FindMntOutput, error) {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	res, err := client.GetAllFileSystems(context.Background(), &btrfsGrpc.Empty{})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func CreateRaid(conn *grpc.ClientConn, req *btrfsGrpc.CreateRaidReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.CreateRaid(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func RemoveRaid(conn *grpc.ClientConn, req *btrfsGrpc.UUIDReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.RemoveRaid(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func MountRaid(conn *grpc.ClientConn, req *btrfsGrpc.MountReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.MountRaid(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func UMountRaid(conn *grpc.ClientConn, req *btrfsGrpc.UMountReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.UMountRaid(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}
