package btrfs

import (
	"context"

	btrfsGrpc "github.com/Maruqes/512SvMan/api/proto/btrfs"
	"google.golang.org/grpc"
)

func GetAllDisks(conn *grpc.ClientConn) ([]*btrfsGrpc.MinDisk, error) {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	res, err := client.GetAllDisks(context.Background(), &btrfsGrpc.Empty{})
	if err != nil {
		return nil, err
	}
	return res.Disks, nil
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
