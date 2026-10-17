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
	if err := Local.Configure(ctx, req.Config, req.Start); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return (&Service{}).GetStatus(ctx, &pb.Empty{})
}

func (*Service) GetStatus(context.Context, *pb.Empty) (*pb.Status, error) {
	if Local == nil {
		return nil, status.Error(codes.Unavailable, "celld manager is not ready")
	}
	s := Local.Status()
	s.Master = env512.MasterIP == env512.SlaveIP
	return s, nil
}
