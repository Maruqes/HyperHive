package docker

import (
	"context"

	dockerGrpc "github.com/Maruqes/512SvMan/api/proto/docker"
	"google.golang.org/grpc"
)

func NetworkList(conn *grpc.ClientConn) (*dockerGrpc.NetworkListResponse, error) {
	client := dockerGrpc.NewDockerServiceClient(conn)
	return client.NetworkList(context.Background(), &dockerGrpc.Empty{})
}

func NetworkCreate(conn *grpc.ClientConn, req *dockerGrpc.NetworkCreateRequest) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.NetworkCreate(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}

func NetworkRemove(conn *grpc.ClientConn, req *dockerGrpc.NetworkRemoveRequest) error {
	client := dockerGrpc.NewDockerServiceClient(conn)
	_, err := client.NetworkRemove(context.Background(), req)
	if err != nil {
		return err
	}
	return nil
}
