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

func MountRaid(conn *grpc.ClientConn, req *btrfsGrpc.MountReq) (*btrfsGrpc.MountRaidRet, error) {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	mountRes, err := client.MountRaid(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return mountRes, nil
}

func UMountRaid(conn *grpc.ClientConn, req *btrfsGrpc.UMountReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.UMountRaid(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func AddDiskToRaid(conn *grpc.ClientConn, req *btrfsGrpc.AddDiskToRaidReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.AddDiskToRaid(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func RemoveDiskFromRaid(conn *grpc.ClientConn, req *btrfsGrpc.RemoveDiskFromRaidReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.RemoveDiskFromRaid(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ReplaceDiskInRaid(conn *grpc.ClientConn, req *btrfsGrpc.ReplaceDiskToRaidReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.ReplaceDiskInRaid(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ChangeRaidLevel(conn *grpc.ClientConn, req *btrfsGrpc.ChangeRaidLevelReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.ChangeRaidLevel(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func BalanceRaid(conn *grpc.ClientConn, req *btrfsGrpc.BalanceRaidReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.BalanceRaid(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func DefragmentRaid(conn *grpc.ClientConn, req *btrfsGrpc.UUIDReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.DefragmentRaid(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ScrubRaid(conn *grpc.ClientConn, req *btrfsGrpc.UUIDReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.ScrubRaid(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func GetRaidStats(conn *grpc.ClientConn, req *btrfsGrpc.UUIDReq) (*btrfsGrpc.RaidStats, error) {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	res, err := client.GetRaidStats(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func PauseBalance(conn *grpc.ClientConn, req *btrfsGrpc.UUIDReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.PauseBalance(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ResumeBalance(conn *grpc.ClientConn, req *btrfsGrpc.UUIDReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.ResumeBalance(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func CancelBalance(conn *grpc.ClientConn, req *btrfsGrpc.UUIDReq) error {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	_, err := client.CancelBalance(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func ScrubStats(conn *grpc.ClientConn, req *btrfsGrpc.UUIDReq) (*btrfsGrpc.ScrubStatus, error) {
	client := btrfsGrpc.NewBtrFSServiceClient(conn)
	res, err := client.ScrubStats(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return res, nil
}
