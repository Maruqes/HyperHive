package celld

import (
	"context"
	"net"
	"slave/env512"

	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct {
	pb.UnimplementedCelldServiceServer
}

func (*Service) Configure(ctx context.Context, req *pb.ConfigureRequest) (*pb.Status, error) {
	if Local == nil {
		return nil, status.Error(codes.Unavailable, "celld manager is not ready")
	}
	if req.GetConfig() == nil || req.Config.Endpoint != "http://"+net.JoinHostPort(env512.MasterIP, "9000") {
		return nil, status.Error(codes.InvalidArgument, "storage must be on the master management IP")
	}
	s, err := Local.Configure(ctx, req.Config, req.Start)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return s, nil
}

func (*Service) GetStatus(_ context.Context, req *pb.Empty) (*pb.Status, error) {
	if Local == nil {
		return nil, status.Error(codes.Unavailable, "celld manager is not ready")
	}
	s := Local.Status(req.FleetId)
	s.Master = env512.MasterIP == env512.SlaveIP
	return s, nil
}

func (*Service) Sync(_ context.Context, req *pb.SyncRequest) (*pb.Empty, error) {
	if Local == nil {
		return nil, status.Error(codes.Unavailable, "celld manager is not ready")
	}
	if err := Local.Sync(req.AssignedFleetIds, req.RunningFleetIds); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &pb.Empty{}, nil
}

func (*Service) Stop(ctx context.Context, req *pb.Empty) (*pb.Status, error) {
	if Local == nil {
		return nil, status.Error(codes.Unavailable, "celld manager is not ready")
	}
	if err := Local.Stop(req.FleetId, false); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return (&Service{}).GetStatus(ctx, req)
}

func (*Service) Remove(_ context.Context, req *pb.Empty) (*pb.Empty, error) {
	if Local == nil {
		return nil, status.Error(codes.Unavailable, "celld manager is not ready")
	}
	if err := Local.Stop(req.FleetId, true); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &pb.Empty{}, nil
}

func (*Service) Prepare(ctx context.Context, req *pb.PrepareRequest) (*pb.Status, error) {
	if Local == nil {
		return nil, status.Error(codes.Unavailable, "celld manager is not ready")
	}
	if err := Local.Prepare(ctx, req.Version); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return (&Service{}).GetStatus(ctx, &pb.Empty{})
}
