package docker

import (
	"context"

	dockerGrpc "github.com/Maruqes/512SvMan/api/proto/docker"
	"google.golang.org/grpc"
)

func GitClone(conn *grpc.ClientConn, req *dockerGrpc.GitCloneReq) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.GitClone(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func GitList(conn *grpc.ClientConn) (*dockerGrpc.GitListReq, error) {
	client := dockerGrpc.NewDockerServiceClient(conn)
	res, err := client.GitList(context.Background(), &dockerGrpc.Empty{})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func GitRemove(conn *grpc.ClientConn, req *dockerGrpc.GitRemoveReq) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.GitRemove(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func GitUpdate(conn *grpc.ClientConn, req *dockerGrpc.GitUpdateReq) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.GitUpdate(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}
